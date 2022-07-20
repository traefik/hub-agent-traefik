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
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/ettle/strcase"
	"github.com/rs/zerolog/log"
	"github.com/traefik/hub-agent-traefik/pkg/acp"
	"github.com/traefik/hub-agent-traefik/pkg/certificate"
	"github.com/traefik/hub-agent-traefik/pkg/edge"
	"github.com/traefik/hub-agent-traefik/pkg/heartbeat"
	"github.com/traefik/hub-agent-traefik/pkg/logger"
	"github.com/traefik/hub-agent-traefik/pkg/platform"
	"github.com/traefik/hub-agent-traefik/pkg/provider"
	"github.com/traefik/hub-agent-traefik/pkg/topology"
	topostore "github.com/traefik/hub-agent-traefik/pkg/topology/store"
	"github.com/traefik/hub-agent-traefik/pkg/traefik"
	"github.com/traefik/hub-agent-traefik/pkg/tunnel"
	"github.com/traefik/hub-agent-traefik/pkg/version"
	"github.com/urfave/cli/v2"
	"golang.org/x/sync/errgroup"
)

// ProviderWatcher watches provider changes.
type ProviderWatcher interface {
	Watch(ctx context.Context, fn func(map[string]*topology.Service)) error
	GetIP(ctx context.Context, containerName, network string) (string, error)
}

type runCmd struct {
	flags []cli.Flag
}

func newRunCmd() runCmd {
	return runCmd{
		flags: []cli.Flag{
			&cli.StringFlag{
				Name:    flagLogLevel,
				Usage:   "Log level to use (debug, info, warn, error or fatal)",
				EnvVars: []string{strcase.ToSNAKE(flagLogLevel)},
				Value:   "info",
			},
			&cli.StringFlag{
				Name:    flagLogFormat,
				Usage:   "Log format to use (json or console)",
				EnvVars: []string{strcase.ToSNAKE(flagLogFormat)},
				Value:   "json",
			},
			&cli.StringFlag{
				Name:     flagTraefikHost,
				Usage:    "Host to advertise for Traefik to reach the Agent authentication server. Required when the automatic discovery fails",
				EnvVars:  []string{strcase.ToSNAKE(flagTraefikHost)},
				Required: true,
			},
			&cli.StringFlag{
				Name:    flagTraefikAPIPort,
				Usage:   "Port of the Traefik entrypoint for API communication with Traefik",
				EnvVars: []string{strcase.ToSNAKE(flagTraefikAPIPort)},
				Value:   "9900",
			},
			&cli.StringFlag{
				Name:    flagTraefikTunnelPort,
				Usage:   "Port of the Traefik entrypoint for tunnel communication",
				EnvVars: []string{strcase.ToSNAKE(flagTraefikTunnelPort)},
				Value:   "9901",
			},
			&cli.StringFlag{
				Name:     flagHubToken,
				Usage:    "The token to use for Hub platform API calls",
				EnvVars:  []string{strcase.ToSNAKE(flagHubToken)},
				Required: true,
			},
			&cli.StringFlag{
				Name:    flagHubURL,
				Usage:   "The URL where to reach the Hub platform API",
				Value:   "https://platform.hub.traefik.io/agent",
				EnvVars: []string{strcase.ToSNAKE(flagHubURL)},
				Hidden:  true,
			},
			&cli.StringFlag{
				Name:    flagHubUIURL,
				Usage:   "The Hub frontend URL",
				Value:   "https://hub.traefik.io",
				EnvVars: []string{strcase.ToSNAKE(flagHubUIURL)},
				Hidden:  true,
			},
			&cli.StringFlag{
				Name:    flagAuthServerListenAddr,
				Usage:   "Address on which the auth server listens for auth requests",
				EnvVars: []string{strcase.ToSNAKE(flagAuthServerListenAddr)},
				Value:   "0.0.0.0:80",
			},
			&cli.StringFlag{
				Name:    flagAuthServerAdvertiseURL,
				Usage:   "Address on which Traefik can reach the Agent auth server. Required when the automatic IP discovery fails",
				EnvVars: []string{strcase.ToSNAKE(flagAuthServerAdvertiseURL)},
			},
			&cli.StringFlag{
				Name:     flagTraefikTLSCA,
				Usage:    "Path to the certificate authority which signed TLS credentials",
				EnvVars:  []string{strcase.ToSNAKE(flagTraefikTLSCA)},
				Required: false,
			},
			&cli.StringFlag{
				Name:     flagTraefikTLSCert,
				Usage:    "Path to the certificate (must have `agent.traefik` domain name) used to communicate with Traefik Proxy",
				EnvVars:  []string{strcase.ToSNAKE(flagTraefikTLSCert)},
				Required: false,
			},
			&cli.StringFlag{
				Name:     flagTraefikTLSKey,
				Usage:    "Path to the key used to communicate with Traefik Proxy",
				EnvVars:  []string{strcase.ToSNAKE(flagTraefikTLSKey)},
				Required: false,
			},
			&cli.BoolFlag{
				Name:     flagTraefikTLSInsecure,
				Usage:    "Activate insecure TLS",
				EnvVars:  []string{strcase.ToSNAKE(flagTraefikTLSInsecure)},
				Required: false,
			},
			&cli.BoolFlag{
				Name:     flagTraefikDockerSwarmMode,
				Usage:    "Activate Traefik Docker Swarm Mode",
				EnvVars:  []string{strcase.ToSNAKE(flagTraefikDockerSwarmMode)},
				Required: false,
			},
			&cli.StringFlag{
				Name:    flagTraefikDockerEndpoint,
				Usage:   "Docker server endpoint. Can be a tcp or a unix socket endpoint.",
				EnvVars: []string{strcase.ToSNAKE(flagTraefikDockerEndpoint)},
				Value:   "unix:///var/run/docker.sock",
			},
			&cli.DurationFlag{
				Name:    flagTraefikDockerHTTPClientTimeout,
				Usage:   "Client timeout for HTTP connections.",
				EnvVars: []string{strcase.ToSNAKE(flagTraefikDockerHTTPClientTimeout)},
			},
			&cli.StringFlag{
				Name:    flagTraefikDockerTLSCA,
				Usage:   "Docker CA",
				EnvVars: []string{strcase.ToSNAKE(flagTraefikDockerTLSCA)},
			},
			&cli.BoolFlag{
				Name:    flagTraefikDockerTLSCAOptional,
				Usage:   "Docker CA Optional",
				EnvVars: []string{strcase.ToSNAKE(flagTraefikDockerTLSCAOptional)},
			},
			&cli.StringFlag{
				Name:    flagTraefikDockerTLSCert,
				Usage:   "Docker certificate",
				EnvVars: []string{strcase.ToSNAKE(flagTraefikDockerTLSCert)},
			},
			&cli.StringFlag{
				Name:    flagTraefikDockerTLSKey,
				Usage:   "Docker private key",
				EnvVars: []string{strcase.ToSNAKE(flagTraefikDockerTLSKey)},
			},
			&cli.BoolFlag{
				Name:    flagTraefikDockerTLSInsecureSkipVerify,
				Usage:   "Insecure skip verify",
				EnvVars: []string{strcase.ToSNAKE(flagTraefikDockerTLSInsecureSkipVerify)},
			},
		},
	}
}

func (r runCmd) build() *cli.Command {
	return &cli.Command{
		Name:   "run",
		Usage:  "Runs the Hub Agent",
		Flags:  r.flags,
		Action: r.runAgent,
	}
}

func (r runCmd) runAgent(cliCtx *cli.Context) error {
	logger.Setup(cliCtx.String(flagLogLevel), cliCtx.String(flagLogFormat))

	version.Log()

	platformURL, token := cliCtx.String(flagHubURL), cliCtx.String(flagHubToken)
	platformClient, err := platform.NewClient(platformURL, token)
	if err != nil {
		return fmt.Errorf("new platform client: %w", err)
	}

	agentCfg, err := initAgent(cliCtx.Context, platformClient)
	if err != nil {
		return fmt.Errorf("get platform config: %w", err)
	}

	traefikHost := cliCtx.String(flagTraefikHost)
	traefikAPIPort := cliCtx.String(flagTraefikAPIPort)
	traefikTunnelPort := cliCtx.String(flagTraefikTunnelPort)

	apiURL := "https://" + net.JoinHostPort(traefikHost, traefikAPIPort)
	tunnelAddr := net.JoinHostPort(traefikHost, traefikTunnelPort)

	traefikTLSCA := cliCtx.String(flagTraefikTLSCA)
	traefikTLSCert := cliCtx.String(flagTraefikTLSCert)
	traefikTLSKey := cliCtx.String(flagTraefikTLSKey)
	traefikTLSInsecure := cliCtx.Bool(flagTraefikTLSInsecure)

	traefikClient, err := traefik.NewClient(apiURL, traefikTLSInsecure, traefikTLSCA, traefikTLSCert, traefikTLSKey)
	if err != nil {
		return fmt.Errorf("create Traefik client: %w", err)
	}

	_, err = traefikClient.GetProviderState(cliCtx.Context)
	if err != nil {
		return fmt.Errorf("unreachable Traefik probably because the Hub TLS configuration in Traefik is missing: %w", err)
	}

	listenAddr, reachableURL := cliCtx.String(flagAuthServerListenAddr), cliCtx.String(flagAuthServerAdvertiseURL)
	if reachableURL == "" {
		log.Debug().Msg("Using auto-discovery to find Agent reachable address")
		reachableURL, err = getAgentReachableAddress(cliCtx.Context, traefikClient, listenAddr)
		if err != nil {
			return fmt.Errorf("get agent reachable address: %w. Consider using the `%s` flag", err, flagAuthServerAdvertiseURL)
		}
	}

	_, err = url.ParseRequestURI(reachableURL)
	if err != nil {
		return fmt.Errorf("invalid URL in `%s` flag: %w", flagAuthServerAdvertiseURL, err)
	}

	log.Info().Str("addr", reachableURL).Msg("Using Agent reachable address")

	acpServer := acp.NewServer(listenAddr)

	certClient, err := certificate.NewClient(platformURL, token)
	if err != nil {
		return fmt.Errorf("create certificate client: %w", err)
	}

	dcOpts := createDockerClientOpts(cliCtx)

	dockerClient, err := provider.CreateDockerClient(dcOpts)
	if err != nil {
		return fmt.Errorf("create docker client: %w", err)
	}

	var dockerProvider ProviderWatcher
	if dcOpts.SwarmMode {
		dockerProvider = provider.NewDockerSwarm(dockerClient, traefikHost, 30*time.Second)
	} else {
		dockerProvider = provider.NewDocker(dockerClient, traefikHost)
	}

	store := topostore.New(platformClient)

	cfgWatcher := platform.NewConfigWatcher(15*time.Minute, platformClient)
	metricsMgr, metricsStore, err := newMetrics(token, platformURL, agentCfg.Metrics, cfgWatcher, traefikClient)
	if err != nil {
		return err
	}

	edgeClient, err := edge.NewClient(platformURL, token)
	if err != nil {
		return fmt.Errorf("create edge client: %w", err)
	}

	hubUIURL := cliCtx.String(flagHubUIURL)
	edgeUpdater := NewEdgeUpdater(certClient, traefikClient, dockerProvider, reachableURL, hubUIURL, agentCfg.AccessControl.MaxSecuredRoutes)

	edgeWatcher := edge.NewWatcher(edgeClient, time.Minute)

	edgeWatcher.AddListener(edgeUpdater.Update)
	edgeWatcher.AddListener(func(_ context.Context, _ []edge.Ingress, acps []edge.ACP) error {
		return acpServer.UpdateHandler(acps)
	})

	tunnelClient, err := tunnel.NewClient(platformURL, token)
	if err != nil {
		return fmt.Errorf("create tunnel client: %w", err)
	}

	tunnelManager := tunnel.NewManager(tunnelClient, tunnelAddr, token, time.Minute)

	heartBeater := heartbeat.NewHeartbeater(platformClient)

	group, ctx := errgroup.WithContext(cliCtx.Context)
	group.Go(func() error {
		heartBeater.Run(ctx)
		return nil
	})

	group.Go(func() error {
		return listenDocker(ctx, dockerProvider, store)
	})

	group.Go(func() error {
		edgeWatcher.Run(ctx)
		return nil
	})

	group.Go(func() error {
		return acpServer.Run(ctx)
	})

	group.Go(func() error {
		return metricsMgr.Run(ctx, traefikHost)
	})

	group.Go(func() error {
		return runAlerting(ctx, token, platformURL, metricsStore)
	})

	group.Go(func() error {
		tunnelManager.Run(ctx)
		return nil
	})

	return group.Wait()
}

func createDockerClientOpts(cliCtx *cli.Context) provider.DockerClientOpts {
	dcOpts := provider.DockerClientOpts{
		HTTPClientTimeout: cliCtx.Duration(flagTraefikDockerHTTPClientTimeout),
		Endpoint:          cliCtx.String(flagTraefikDockerEndpoint),
		SwarmMode:         cliCtx.Bool(flagTraefikDockerSwarmMode),
	}

	if cliCtx.IsSet(flagTraefikDockerTLSCA) ||
		cliCtx.IsSet(flagTraefikDockerTLSCAOptional) ||
		cliCtx.IsSet(flagTraefikDockerTLSCert) ||
		cliCtx.IsSet(flagTraefikDockerTLSKey) ||
		cliCtx.IsSet(flagTraefikDockerTLSInsecureSkipVerify) {
		dcOpts.TLSClientConfig = &provider.ClientTLS{
			CA:                 cliCtx.String(flagTraefikDockerTLSCA),
			CAOptional:         cliCtx.Bool(flagTraefikDockerTLSCAOptional),
			Cert:               cliCtx.String(flagTraefikDockerTLSCert),
			Key:                cliCtx.String(flagTraefikDockerTLSKey),
			InsecureSkipVerify: cliCtx.Bool(flagTraefikDockerTLSInsecureSkipVerify),
		}
	}

	return dcOpts
}

func listenDocker(ctx context.Context, dockerProvider ProviderWatcher, store *topostore.Store) error {
	err := dockerProvider.Watch(ctx, func(services map[string]*topology.Service) {
		cluster := topology.Cluster{
			Services: services,
		}

		err := store.Write(ctx, cluster)
		if err != nil {
			log.Error().Err(err).Msg("Topology write")
			return
		}
	})
	if err != nil {
		return fmt.Errorf("docker provider watch: %w", err)
	}

	return nil
}

func getAgentReachableAddress(ctx context.Context, traefikClient *traefik.Client, listenAddr string) (string, error) {
	reachableIP, err := traefikClient.GetAgentReachableIP(ctx)
	if err != nil {
		return "", err
	}

	_, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return "", err
	}

	return "http://" + net.JoinHostPort(reachableIP, port), nil
}

func initAgent(ctx context.Context, platformClient *platform.Client) (platform.Config, error) {
	_, err := platformClient.Link(ctx)
	if err != nil {
		return platform.Config{}, fmt.Errorf("link agent: %w", err)
	}

	agentCfg, err := platformClient.GetConfig(ctx)
	if err != nil {
		return platform.Config{}, fmt.Errorf("fetch agent config: %w", err)
	}

	return agentCfg, nil
}
