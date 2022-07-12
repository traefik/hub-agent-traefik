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

package edge

import "time"

// Ingress represents an edge ingress configuration on a cluster.
type Ingress struct {
	ID string `json:"id"`

	WorkspaceID string `json:"workspaceId"`
	ClusterID   string `json:"clusterId"`
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`

	Domain        string   `json:"domain"`
	CustomDomains []Domain `json:"customDomains"`
	Service       Service  `json:"service"`
	ACP           *ACPInfo `json:"acp,omitempty"`

	Version   string    `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Domain holds domain information.
type Domain struct {
	Name     string `json:"name"`
	Verified bool   `json:"verified"`
}

// Service represents an endpoint for an Ingress.
type Service struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Network string `json:"network"`
	Port    int    `json:"port"`
}

// ACPInfo represents an ACP for an Ingress.
type ACPInfo struct {
	Name string `json:"name"`
}
