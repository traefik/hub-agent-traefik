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
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strings"

	"github.com/docker/docker/api/types"
	eventtypes "github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/rs/zerolog/log"
	"github.com/traefik/hub-agent-traefik/pkg/topology"
)

const (
	labelDockerComposeProject = "com.docker.compose.project"
	labelDockerComposeService = "com.docker.compose.service"
)

// Docker is a Docker client.
type Docker struct {
	client      client.APIClient
	traefikHost string
}

// NewDocker creates Docker.
func NewDocker(dockerClient client.APIClient, traefikHost string) *Docker {
	return &Docker{
		client:      dockerClient,
		traefikHost: traefikHost,
	}
}

// Watch watches docker events.
func (d Docker) Watch(ctx context.Context, fn func(map[string]*topology.Service)) error {
	f := filters.NewArgs()
	f.Add("type", "container")
	options := types.EventsOptions{
		Filters: f,
	}

	startStopHandle := func(_ eventtypes.Message) {
		services, err := d.getServices(ctx)
		if err != nil {
			log.Error().Err(err).Send()
			return
		}

		fn(services)
	}

	startStopHandle(eventtypes.Message{})

	eventsc, errc := d.client.Events(ctx, options)
	for {
		select {
		case event := <-eventsc:
			if event.Action == "start" ||
				event.Action == "die" ||
				event.Action == "destroy" ||
				event.Action == "stop" ||
				strings.HasPrefix(event.Action, "health_status") {
				startStopHandle(event)
			}
		case err := <-errc:
			if errors.Is(err, io.EOF) {
				log.Debug().Msg("Provider event stream closed")
			}
			return err
		case <-ctx.Done():
			return nil
		}
	}
}

func (d Docker) getServices(ctx context.Context) (map[string]*topology.Service, error) {
	networks, err := d.getTraefikNetworks(ctx)
	if err != nil {
		return nil, fmt.Errorf("get Traefik networks: %w", err)
	}

	containers, err := d.client.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	services := make(map[string]*topology.Service)
	for _, container := range containers {
		containerInspect, err := d.client.ContainerInspect(ctx, container.ID)
		if err != nil {
			return nil, fmt.Errorf("inspect container %s: %w", container.ID, err)
		}

		if containerInspect.State != nil && containerInspect.State.Health != nil &&
			containerInspect.State.Health.Status != "" && containerInspect.State.Health.Status != "healthy" {
			log.Debug().Msg("Filtering unhealthy or starting container")
			continue
		}

		var ports []int
		for _, port := range container.Ports {
			ports = append(ports, int(port.PrivatePort))
		}

		info := d.getContainerInfo(ctx, networks, containerInspect)
		if info == nil {
			continue
		}

		sort.Ints(ports)

		serviceName := getServiceName(containerInspect)
		services[serviceName] = &topology.Service{
			Name:      serviceName,
			Container: info,
			Ports:     ports,
		}
	}

	return services, nil
}

func (d Docker) getContainerInfo(ctx context.Context, networks []string, container types.ContainerJSON) *topology.Container {
	if container.HostConfig.NetworkMode.IsHost() {
		return &topology.Container{Name: container.Name, Networks: []string{"HOST"}}
	}

	if container.HostConfig.NetworkMode.IsContainer() {
		connectedContainer := container.HostConfig.NetworkMode.ConnectedContainer()
		containerInspect, err := d.client.ContainerInspect(ctx, connectedContainer)
		if err != nil {
			log.Warn().
				Str("container_name", container.Name).
				Str("connected_container", connectedContainer).
				Err(err).
				Msg("Unable to get IP address")

			return nil
		}

		container = containerInspect
	}

	if container.NetworkSettings != nil && len(container.NetworkSettings.Networks) > 0 {
		c := &topology.Container{Name: strings.TrimPrefix(container.Name, "/")}

		for network := range container.NetworkSettings.Networks {
			if contains(networks, network) {
				c.Networks = append(c.Networks, network)
			}
		}

		return c
	}

	return nil
}

func (d Docker) getTraefikNetworks(ctx context.Context) ([]string, error) {
	traefikIP, err := getTraefikIP(d.traefikHost)
	if err != nil {
		return nil, fmt.Errorf("get Traefik IP: %w", err)
	}

	networks, err := d.client.NetworkList(ctx, types.NetworkListOptions{})
	if err != nil {
		return nil, err
	}

	containerName, err := getContainerName(networks, traefikIP)
	if err != nil {
		return nil, err
	}

	traefik, err := d.client.ContainerInspect(ctx, containerName)
	if err != nil {
		return nil, err
	}

	var networkNames []string
	for name := range traefik.NetworkSettings.Networks {
		networkNames = append(networkNames, name)
	}

	return networkNames, nil
}

// GetIP gets container IP.
func (d Docker) GetIP(ctx context.Context, serviceName, network string) (string, error) {
	containerName := serviceName

	splitted := strings.Split(strings.TrimPrefix(serviceName, "/"), "~")
	if len(splitted) == 2 {
		containers, err := d.client.ContainerList(ctx, types.ContainerListOptions{Filters: filters.NewArgs(filters.KeyValuePair{
			Key:   "label",
			Value: fmt.Sprintf("%s=%s", labelDockerComposeProject, splitted[0]),
		}, filters.KeyValuePair{
			Key:   "label",
			Value: fmt.Sprintf("%s=%s", labelDockerComposeService, splitted[1]),
		})})
		if err != nil {
			return "", fmt.Errorf("list containers: %w", err)
		}

		if len(containers) > 0 {
			containerName = containers[0].ID
		}
	}

	container, err := d.client.ContainerInspect(ctx, containerName)
	if err != nil {
		return "", err
	}

	if container.HostConfig.NetworkMode.IsHost() {
		if network != "HOST" {
			return "", fmt.Errorf("the network mode %s is different from HOST", network)
		}

		if container.Node != nil && container.Node.IPAddress != "" {
			return container.Node.IPAddress, nil
		}

		if host, err := net.LookupHost("host.docker.internal"); err == nil {
			return host[0], nil
		}

		return "127.0.0.1", nil
	}

	if container.HostConfig.NetworkMode.IsContainer() {
		connectedContainer := container.HostConfig.NetworkMode.ConnectedContainer()
		containerInspect, err := d.client.ContainerInspect(ctx, connectedContainer)
		if err != nil {
			log.Error().
				Str("container_name", container.Name).
				Str("connected_container", connectedContainer).
				Err(err).
				Msg("Unable to get IP address")

			return "", nil
		}

		return getContainerIP(containerInspect, network)
	}

	return getContainerIP(container, network)
}

func getContainerName(networks []types.NetworkResource, ip net.IP) (string, error) {
	for _, network := range networks {
		for _, config := range network.IPAM.Config {
			_, ipNet, err := net.ParseCIDR(config.Subnet)
			if err != nil {
				return "", err
			}

			if !ipNet.Contains(ip) {
				continue
			}

			for name, resource := range network.Containers {
				containerIP, _, err := net.ParseCIDR(resource.IPv4Address)
				if err != nil {
					return "", err
				}

				if containerIP.Equal(ip) {
					return name, nil
				}
			}
		}
	}

	return "", fmt.Errorf("unable to find container with ip: %s", ip)
}

func getContainerIP(container types.ContainerJSON, network string) (string, error) {
	if container.NetworkSettings == nil {
		return "", fmt.Errorf("%s: no network settings", network)
	}

	for name, settings := range container.NetworkSettings.Networks {
		if network == name {
			return settings.IPAddress, nil
		}
	}

	return "", fmt.Errorf("%s: no IP address", network)
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}

	return false
}

func getServiceName(container types.ContainerJSON) string {
	name := strings.TrimPrefix(container.Name, "/")

	if container.Config == nil {
		return name
	}

	dcp, okp := container.Config.Labels[labelDockerComposeProject]
	dcs, oks := container.Config.Labels[labelDockerComposeService]
	if okp && oks {
		name = dcp + "~" + dcs
	}

	return name
}
