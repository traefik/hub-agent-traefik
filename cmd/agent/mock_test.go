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

	"github.com/traefik/hub-agent-traefik/pkg/topology"
)

type providerMock struct{}

func (m providerMock) Watch(ctx context.Context, fn func(map[string]*topology.Service)) error {
	return nil
}

func (m providerMock) GetIP(ctx context.Context, containerName, network string) (string, error) {
	return "127.0.0.1", nil
}
