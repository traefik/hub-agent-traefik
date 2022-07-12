/*
Copyright (C) 2022 Traefik Labs

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published
by the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.
*/

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/traefik/genconf/dynamic"
	"github.com/traefik/genconf/dynamic/tls"
	"github.com/traefik/hub-agent-traefik/pkg/certificate"
	"github.com/traefik/hub-agent-traefik/pkg/edge"
	"github.com/traefik/hub-agent-traefik/pkg/traefik"
)

const quotaExceededMiddleware = "quota-exceeded"

const defaultHubTunnelEntrypoint = "traefikhub-tunl"

// EdgeUpdater keep edge ingresses and Traefik configuration synchronized.
type EdgeUpdater struct {
	certClient    *certificate.Client
	traefikClient *traefik.Client
	provider      ProviderWatcher

	authServerReachableAddr string
	catchAllURL             string
	maxSecuredRoute         int
}

// NewEdgeUpdater creates EdgeUpdater.
func NewEdgeUpdater(certClient *certificate.Client, traefikClient *traefik.Client, provider ProviderWatcher, authServerReachableAddr, catchAllURL string, maxSecuredRoute int) *EdgeUpdater {
	return &EdgeUpdater{
		certClient:              certClient,
		traefikClient:           traefikClient,
		provider:                provider,
		authServerReachableAddr: authServerReachableAddr,
		catchAllURL:             catchAllURL,
		maxSecuredRoute:         maxSecuredRoute,
	}
}

// Update updates Traefik configuration from edge ingresses and ACPs.
func (e EdgeUpdater) Update(ctx context.Context, ingresses []edge.Ingress, acps []edge.ACP) error {
	cfg, err := e.defaultDynamicConfiguration(ctx)
	if err != nil {
		return fmt.Errorf("default configuration: %w", err)
	}

	if err = e.appendACPToTraefikCfg(cfg, acps); err != nil {
		return fmt.Errorf("append acp to traefik cfg: %w", err)
	}

	if err = e.appendEdgeToTraefikCfg(ctx, cfg, ingresses); err != nil {
		return fmt.Errorf("append edge to traefik cfg: %w", err)
	}

	if err = e.traefikClient.PushDynamic(ctx, time.Now().UnixNano(), cfg); err != nil {
		return fmt.Errorf("push dynamic: %w", err)
	}

	return nil
}

func (e EdgeUpdater) appendEdgeToTraefikCfg(ctx context.Context, cfg *dynamic.Configuration, edgeIngresses []edge.Ingress) error {
	for _, ingress := range edgeIngresses {
		logger := log.With().Str("workspace_id", ingress.WorkspaceID).
			Str("cluster_id", ingress.ClusterID).
			Str("edge_ingress_id", ingress.ID).
			Str("service_name", ingress.Service.Name).
			Str("service_network", ingress.Service.Network).
			Logger()

		ip, err := e.provider.GetIP(ctx, "/"+ingress.Service.Name, ingress.Service.Network)
		if err != nil {
			logger.Error().Err(err).Msg("unable to get IP")
			continue
		}

		if ip == "" {
			logger.Error().Msg("Unable to get service IP")
			continue
		}

		var middleware []string
		if ingress.ACP != nil {
			middleware = append(middleware, ingress.ACP.Name)
		}

		var customDomains []string
		for _, domain := range ingress.CustomDomains {
			if domain.Verified {
				customDomains = append(customDomains, domain.Name)
			}
		}

		routerRule := fmt.Sprintf("Host(`%s`)", ingress.Domain)
		if len(customDomains) > 0 {
			certCustom, err := e.certClient.GetCertificateByDomains(ctx, customDomains)
			if err != nil {
				return fmt.Errorf("get certificate by domains %q: %w", strings.Join(customDomains, ","), err)
			}

			cfg.TLS.Certificates = append(cfg.TLS.Certificates, &tls.CertAndStores{
				Certificate: tls.Certificate{
					CertFile: string(certCustom.Certificate),
					KeyFile:  string(certCustom.PrivateKey),
				},
			})

			routerRule = fmt.Sprintf("Host(`%s`)", ingress.Domain+"`,`"+strings.Join(customDomains, "`,`"))
		}

		cfg.HTTP.Routers[ingress.Name] = &dynamic.Router{
			EntryPoints: []string{defaultHubTunnelEntrypoint},
			Middlewares: middleware,
			Service:     ingress.Name,
			Rule:        routerRule,
			Priority:    60,
			TLS:         &dynamic.RouterTLSConfig{},
		}

		cfg.HTTP.Services[ingress.Name] = &dynamic.Service{
			LoadBalancer: &dynamic.ServersLoadBalancer{
				Servers: []dynamic.Server{
					{URL: "http://" + net.JoinHostPort(ip, strconv.Itoa(ingress.Service.Port))},
				},
			},
		}
	}

	return nil
}

func (e EdgeUpdater) appendACPToTraefikCfg(cfg *dynamic.Configuration, acps []edge.ACP) error {
	for _, acp := range acps {
		headerToFwd, err := headerToForward(acp)
		if err != nil {
			return err
		}

		cfg.HTTP.Middlewares[acp.Name] = &dynamic.Middleware{
			ForwardAuth: &dynamic.ForwardAuth{
				Address:             fmt.Sprintf("%s/%s", e.authServerReachableAddr, acp.Name),
				AuthResponseHeaders: headerToFwd,
			},
		}
	}

	return nil
}

func (e EdgeUpdater) defaultDynamicConfiguration(ctx context.Context) (*dynamic.Configuration, error) {
	cert, err := e.certClient.GetCertificate(ctx)
	if err != nil {
		return nil, fmt.Errorf("get certificate: %w", err)
	}

	return &dynamic.Configuration{
		HTTP: &dynamic.HTTPConfiguration{
			Routers: map[string]*dynamic.Router{
				"catch-all": {
					EntryPoints: []string{defaultHubTunnelEntrypoint},
					Middlewares: []string{"strip", "add"},
					Service:     "catch-all",
					Rule:        "PathPrefix(`/`)",
					Priority:    1,
					TLS:         &dynamic.RouterTLSConfig{},
				},
			},
			Middlewares: map[string]*dynamic.Middleware{
				quotaExceededMiddleware: {
					IPWhiteList: &dynamic.IPWhiteList{
						SourceRange: []string{"8.8.8.8"},
						IPStrategy: &dynamic.IPStrategy{
							ExcludedIPs: []string{"0.0.0.0/0"},
						},
					},
				},
				"strip": {
					StripPrefixRegex: &dynamic.StripPrefixRegex{
						Regex: []string{".*"},
					},
				},
				"add": {
					AddPrefix: &dynamic.AddPrefix{
						Prefix: "/edge-ingresses/in-progress",
					},
				},
			},
			Services: map[string]*dynamic.Service{
				"catch-all": {
					LoadBalancer: &dynamic.ServersLoadBalancer{
						PassHostHeader: func(v bool) *bool { return &v }(false),
						Servers: []dynamic.Server{
							{URL: e.catchAllURL},
						},
					},
				},
			},
			ServersTransports: make(map[string]*dynamic.ServersTransport),
		},
		TCP: &dynamic.TCPConfiguration{
			Routers:  make(map[string]*dynamic.TCPRouter),
			Services: make(map[string]*dynamic.TCPService),
		},
		TLS: &dynamic.TLSConfiguration{
			Stores:  make(map[string]tls.Store),
			Options: make(map[string]tls.Options),
			Certificates: []*tls.CertAndStores{
				{
					Certificate: tls.Certificate{
						CertFile: string(cert.Certificate),
						KeyFile:  string(cert.PrivateKey),
					},
				},
			},
		},
		UDP: &dynamic.UDPConfiguration{
			Routers:  make(map[string]*dynamic.UDPRouter),
			Services: make(map[string]*dynamic.UDPService),
		},
	}, nil
}

func headerToForward(acp edge.ACP) ([]string, error) {
	var headerToFwd []string

	switch {
	case acp.JWT != nil:
		for headerName := range acp.JWT.ForwardHeaders {
			headerToFwd = append(headerToFwd, headerName)
		}
		if acp.JWT.StripAuthorizationHeader {
			headerToFwd = append(headerToFwd, "Authorization")
		}

	case acp.BasicAuth != nil:
		if headerName := acp.BasicAuth.ForwardUsernameHeader; headerName != "" {
			headerToFwd = append(headerToFwd, headerName)
		}
		if acp.BasicAuth.StripAuthorizationHeader {
			headerToFwd = append(headerToFwd, "Authorization")
		}

	default:
		return nil, errors.New("unsupported ACP type")
	}

	return headerToFwd, nil
}
