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

package provider

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	swarmtypes "github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/versions"
	"github.com/docker/docker/client"
	"github.com/rs/zerolog/log"
	"github.com/traefik/hub-agent-traefik/pkg/topology"
)

// DockerSwarm is a DockerSwarm client.
type DockerSwarm struct {
	client      client.APIClient
	traefikHost string
	interval    time.Duration
}

// NewDockerSwarm creates DockerSwarm.
func NewDockerSwarm(dockerClient client.APIClient, traefikHost string, interval time.Duration) *DockerSwarm {
	return &DockerSwarm{
		client:      dockerClient,
		traefikHost: traefikHost,
		interval:    interval,
	}
}

// Watch watches docker events.
func (d DockerSwarm) Watch(ctx context.Context, fn func(map[string]*topology.Service)) error {
	ticker := time.NewTicker(d.interval)

	log.Info().Msg("watch")

	services, err := d.getServices(ctx)
	if err == nil {
		fn(services)
	}

	go func(ctx context.Context) {
		for {
			select {
			case <-ticker.C:
				services, err := d.getServices(ctx)
				if err != nil {
					log.Error().Err(err).Msg("Failed to list services for docker")
					continue
				}

				fn(services)

			case <-ctx.Done():
				ticker.Stop()
				return
			}
		}
	}(ctx)

	<-ctx.Done()

	return nil
}

func (d DockerSwarm) getServices(ctx context.Context) (map[string]*topology.Service, error) {
	traefikIP, err := getTraefikIP(d.traefikHost)
	if err != nil {
		return nil, fmt.Errorf("get Traefik IP: %w", err)
	}

	serviceList, err := d.client.ServiceList(ctx, dockertypes.ServiceListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}

	networkMap, err := d.getFilteredNetworks(ctx, serviceList, traefikIP)
	if err != nil {
		return nil, fmt.Errorf("get filtered networks: %w", err)
	}

	services := make(map[string]*topology.Service)
	for _, service := range serviceList {
		svc := &topology.Service{
			Name: strings.TrimPrefix(service.Spec.Name, "/"),
		}

		loggerSvc := log.With().
			Str("service_id", service.ID).
			Str("service_name", svc.Name).
			Logger()

		for _, port := range service.Endpoint.Ports {
			svc.Ports = append(svc.Ports, int(port.TargetPort))
		}

		sort.Ints(svc.Ports)

		serviceInfo := d.getServiceInfo(loggerSvc.WithContext(ctx), service, networkMap)
		if serviceInfo == nil {
			continue
		}

		svc.Container = serviceInfo
		services[svc.Name] = svc
	}

	return services, nil
}

func (d DockerSwarm) getFilteredNetworks(ctx context.Context, serviceList []swarmtypes.Service, ip net.IP) (map[string]*dockertypes.NetworkResource, error) {
	logger := log.Ctx(ctx)

	networks, err := d.getAllNetworks(ctx)
	if err != nil {
		logger.Debug().Err(err).Msg("Failed to inspect Docker networks")
		return nil, fmt.Errorf("get networks: %w", err)
	}

	networkMap := toNetworkMap(networks)

	networkIDs := d.getTraefikNetworkIDs(serviceList, networkMap, ip)

	filteredNetworks := make(map[string]*dockertypes.NetworkResource)
	for _, id := range networkIDs {
		if networkMap[id] != nil {
			filteredNetworks[id] = networkMap[id]
		}
	}

	return filteredNetworks, nil
}

func (d DockerSwarm) getTraefikNetworkIDs(serviceList []swarmtypes.Service, networkMap map[string]*dockertypes.NetworkResource, ip net.IP) []string {
	for _, service := range serviceList {
		if service.Spec.EndpointSpec == nil {
			continue
		}

		if service.Spec.EndpointSpec.Mode != swarmtypes.ResolutionModeVIP {
			continue
		}

		var ntwk []string
		var found bool

		for _, virtualIP := range service.Endpoint.VirtualIPs {
			networkService := networkMap[virtualIP.NetworkID]
			if networkService == nil {
				continue
			}

			if networkService.Ingress {
				continue
			}

			if virtualIP.Addr == "" {
				continue
			}

			vIP, _, err := net.ParseCIDR(virtualIP.Addr)
			if err != nil || vIP == nil {
				continue
			}

			ntwk = append(ntwk, virtualIP.NetworkID)

			if vIP.Equal(ip) {
				found = true
			}
		}

		if found {
			return ntwk
		}
	}

	return nil
}

func (d DockerSwarm) getServiceInfo(ctx context.Context, service swarmtypes.Service, networkMap map[string]*dockertypes.NetworkResource) *topology.Container {
	logger := log.Ctx(ctx)

	if service.Spec.EndpointSpec == nil {
		return nil
	}

	switch service.Spec.EndpointSpec.Mode {
	case swarmtypes.ResolutionModeDNSRR:
		logger.Debug().Msgf("Ignored %s endpoint-mode not supported", swarmtypes.ResolutionModeDNSRR)
	case swarmtypes.ResolutionModeVIP:
		c := &topology.Container{Name: strings.TrimPrefix(service.Spec.Name, "/")}

		for _, virtualIP := range service.Endpoint.VirtualIPs {
			networkService := networkMap[virtualIP.NetworkID]
			if networkService == nil {
				logger.Debug().Str("network_id", virtualIP.NetworkID).Msg("Network not found")
				continue
			}

			if networkService.Ingress {
				continue
			}

			if virtualIP.Addr == "" {
				logger.Debug().Str("network_id", virtualIP.NetworkID).Msg("No virtual IPs found in network")
				continue
			}

			ip, _, err := net.ParseCIDR(virtualIP.Addr)
			if err != nil || ip == nil {
				continue
			}

			c.Networks = append(c.Networks, networkService.Name)
		}

		return c
	}

	return nil
}

func (d DockerSwarm) getAllNetworks(ctx context.Context) ([]dockertypes.NetworkResource, error) {
	serverVersion, err := d.client.ServerVersion(ctx)
	if err != nil {
		return nil, err
	}

	networkListArgs := filters.NewArgs()
	// https://docs.docker.com/engine/api/v1.29/#tag/Network (Docker 17.06)
	if versions.GreaterThanOrEqualTo(serverVersion.APIVersion, "1.29") {
		networkListArgs.Add("scope", "swarm")
	} else {
		networkListArgs.Add("driver", "overlay")
	}

	return d.client.NetworkList(ctx, dockertypes.NetworkListOptions{Filters: networkListArgs})
}

// GetIP gets service IP.
func (d DockerSwarm) GetIP(ctx context.Context, serviceName, network string) (string, error) {
	logger := log.Ctx(ctx)

	service, _, err := d.client.ServiceInspectWithRaw(ctx, serviceName, dockertypes.ServiceInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("service inspect: %w", err)
	}

	networks, err := d.getAllNetworks(ctx)
	if err != nil {
		return "", fmt.Errorf("get networks: %w", err)
	}

	networkMap := toNetworkMap(networks)

	if service.Spec.EndpointSpec == nil {
		return "", nil
	}

	switch service.Spec.EndpointSpec.Mode {
	case swarmtypes.ResolutionModeDNSRR:
		logger.Debug().Msgf("Ignored %s endpoint-mode not supported", swarmtypes.ResolutionModeDNSRR)
	case swarmtypes.ResolutionModeVIP:
		return getServiceIP(ctx, service, networkMap, network), nil
	}

	return "", nil
}

func getServiceIP(ctx context.Context, service swarmtypes.Service, networkMap map[string]*dockertypes.NetworkResource, network string) string {
	logger := log.Ctx(ctx)

	for _, virtualIP := range service.Endpoint.VirtualIPs {
		networkService := networkMap[virtualIP.NetworkID]

		if networkService == nil {
			logger.Debug().Str("network_id", virtualIP.NetworkID).Msg("Network not found")
			continue
		}

		if networkService.Ingress {
			continue
		}

		if virtualIP.Addr == "" {
			logger.Debug().Str("network_id", virtualIP.NetworkID).Msg("No virtual IPs found in network")
			continue
		}

		if networkService.Name != network {
			continue
		}

		ip, _, err := net.ParseCIDR(virtualIP.Addr)
		if err != nil || ip == nil {
			continue
		}

		return ip.String()
	}

	return ""
}

func toNetworkMap(networkList []dockertypes.NetworkResource) map[string]*dockertypes.NetworkResource {
	networkMap := make(map[string]*dockertypes.NetworkResource)
	for _, network := range networkList {
		networkToAdd := network
		networkMap[network.ID] = &networkToAdd
	}

	return networkMap
}
