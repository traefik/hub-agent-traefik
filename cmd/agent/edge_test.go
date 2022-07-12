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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/traefik/genconf/dynamic"
	"github.com/traefik/genconf/dynamic/tls"
	"github.com/traefik/hub-agent-traefik/pkg/certificate"
	"github.com/traefik/hub-agent-traefik/pkg/edge"
	"github.com/traefik/hub-agent-traefik/pkg/traefik"
)

func TestEdgeUpdater_Update(t *testing.T) {
	certClient, certClientMux := setupCertClient(t)

	var getCertCall int
	certClientMux.HandleFunc("/wildcard-certificate", func(rw http.ResponseWriter, req *http.Request) {
		getCertCall++
		if req.Method != http.MethodGet {
			http.Error(rw, fmt.Sprintf("unsupported to method: %s", req.Method), http.StatusMethodNotAllowed)
			return
		}

		file, err := os.Open("fixtures/cert.json")
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}

		rw.WriteHeader(http.StatusOK)
		_, _ = io.Copy(rw, file)
	})

	var getCertByDomainsCall int
	certClientMux.HandleFunc("/certificate", func(rw http.ResponseWriter, req *http.Request) {
		getCertByDomainsCall++
		if req.Method != http.MethodGet {
			http.Error(rw, fmt.Sprintf("unsupported to method: %s", req.Method), http.StatusMethodNotAllowed)
			return
		}

		assert.Equal(t, []string{"a.com", "b.com"}, req.URL.Query()["domains"])

		file, err := os.Open("fixtures/customcert.json")
		if err != nil {
			http.Error(rw, err.Error(), http.StatusInternalServerError)
			return
		}

		rw.WriteHeader(http.StatusOK)
		_, _ = io.Copy(rw, file)
	})

	traefikClient, traefikClientMux := setupTraefikClient(t)

	var pushedCfg *dynamic.Configuration
	var pushTraefikCfgCall int
	traefikClientMux.HandleFunc("/config", func(rw http.ResponseWriter, req *http.Request) {
		pushTraefikCfgCall++
		if req.Method != http.MethodPost {
			http.Error(rw, fmt.Sprintf("unsupported to method: %s", req.Method), http.StatusMethodNotAllowed)
			return
		}
		all, err := io.ReadAll(req.Body)
		require.NoError(t, err)

		type configRequest struct {
			UnixNano      int64                  `json:"unixNano"`
			Configuration *dynamic.Configuration `json:"configuration"`
		}

		var gotCfg configRequest
		err = json.Unmarshal(all, &gotCfg)
		require.NoError(t, err)

		pushedCfg = gotCfg.Configuration
		rw.WriteHeader(http.StatusOK)
	})

	ingresses := []edge.Ingress{
		{
			WorkspaceID: "workspace-id",
			ClusterID:   "cluster-id",
			Namespace:   "namespace",
			Name:        "name",
			Domain:      "https://majestic-beaver-123.traefik-hub.io",
			CustomDomains: []edge.Domain{
				{
					Name:     "a.com",
					Verified: true,
				},
				{
					Name:     "b.com",
					Verified: true,
				},
				{
					Name: "unverified.com",
				},
			},
			Version: "version",
			Service: edge.Service{
				ID:      "service-id",
				Name:    "service-name",
				Network: "foo_network",
				Port:    8080,
			},
			ACP: &edge.ACPInfo{
				Name: "acp-name",
			},
			CreatedAt: time.Now().Add(-time.Hour).UTC().Truncate(time.Millisecond),
			UpdatedAt: time.Now().UTC().Truncate(time.Millisecond),
		},
	}

	acps := []edge.ACP{
		{
			ID:          "acp-id",
			WorkspaceID: "workspace-id",
			ClusterID:   "cluster-id",
			Version:     "version",
			Name:        "acp-name",
			BasicAuth: &edge.ACPBasicAuthConfig{
				Users:                    []string{"toto"},
				Realm:                    "foo",
				StripAuthorizationHeader: false,
				ForwardUsernameHeader:    "Authorization",
			},
			CreatedAt: time.Now().Add(-time.Hour).UTC().Truncate(time.Millisecond),
			UpdatedAt: time.Now().UTC().Truncate(time.Millisecond),
		},
	}

	edgeUpdater := NewEdgeUpdater(certClient, traefikClient, providerMock{}, "127.0.0.1", "localhost", 2)
	err := edgeUpdater.Update(context.Background(), ingresses, acps)
	require.NoError(t, err)

	assert.Equal(t, 1, pushTraefikCfgCall)
	assert.Equal(t, 1, getCertCall)
	assert.Equal(t, 1, getCertByDomainsCall)

	expectedCfg := &dynamic.Configuration{
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
				"name": {
					EntryPoints: []string{defaultHubTunnelEntrypoint},
					Middlewares: []string{"acp-name"},
					Service:     "name",
					Rule:        "Host(`https://majestic-beaver-123.traefik-hub.io`,`a.com`,`b.com`)",
					Priority:    60,
					TLS:         &dynamic.RouterTLSConfig{},
				},
			},
			Services: map[string]*dynamic.Service{
				"catch-all": {
					LoadBalancer: &dynamic.ServersLoadBalancer{
						PassHostHeader: func(v bool) *bool { return &v }(false),
						Servers: []dynamic.Server{
							{URL: "localhost"},
						},
					},
				},
				"name": {
					LoadBalancer: &dynamic.ServersLoadBalancer{
						Servers: []dynamic.Server{
							{URL: "http://127.0.0.1:8080"},
						},
					},
				},
			},
			Middlewares: map[string]*dynamic.Middleware{
				"acp-name": {
					ForwardAuth: &dynamic.ForwardAuth{
						Address:             "127.0.0.1/acp-name",
						AuthResponseHeaders: []string{"Authorization"},
					},
				},
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
		},

		TLS: &dynamic.TLSConfiguration{
			Certificates: []*tls.CertAndStores{
				{
					Certificate: tls.Certificate{
						CertFile: "cert",
						KeyFile:  "key",
					},
				},
				{
					Certificate: tls.Certificate{
						CertFile: "customcert",
						KeyFile:  "customkey",
					},
				},
			},
		},
		TCP: &dynamic.TCPConfiguration{},
		UDP: &dynamic.UDPConfiguration{},
	}
	assert.Equal(t, expectedCfg, pushedCfg)
}

func setupTraefikClient(t *testing.T) (*traefik.Client, *http.ServeMux) {
	t.Helper()

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := traefik.NewClient(srv.URL, true, "", "", "")
	require.NoError(t, err)

	return client, mux
}

func setupCertClient(t *testing.T) (*certificate.Client, *http.ServeMux) {
	t.Helper()

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := certificate.NewClient(srv.URL, "token")
	require.NoError(t, err)

	return client, mux
}
