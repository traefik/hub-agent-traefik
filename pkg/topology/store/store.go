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

package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/rs/zerolog/log"
	"github.com/traefik/hub-agent-traefik/pkg/platform"
	"github.com/traefik/hub-agent-traefik/pkg/topology"
)

// PlatformClient is capable of interacting with the platform.
type PlatformClient interface {
	FetchTopology(ctx context.Context) (topo topology.Cluster, version int64, err error)
	PatchTopology(ctx context.Context, patch []byte, lastKnownVersion int64) (int64, error)
}

// Store stores the topology on the platform.
type Store struct {
	platform      PlatformClient
	maxPatchRetry int

	lastTopology     []byte
	lastKnownVersion int64
}

// New instantiates a new Store.
func New(platformClient PlatformClient) *Store {
	return &Store{
		platform:      platformClient,
		maxPatchRetry: 5,
	}
}

// Write writes the topology on the platform.
func (s *Store) Write(ctx context.Context, st topology.Cluster) error {
	retryCount := 0

	for {
		if s.lastKnownVersion == 0 {
			topo, version, err := s.platform.FetchTopology(ctx)
			if err != nil {
				return fmt.Errorf("fetch topology: %w", err)
			}

			s.lastTopology, err = json.Marshal(topo)
			if err != nil {
				return fmt.Errorf("marshal topology: %w", err)
			}

			s.lastKnownVersion = version
		}

		patch, newTopology, err := s.buildPatch(s.lastTopology, st)
		if err != nil {
			return fmt.Errorf("build topology patch: %w", err)
		}
		if patch == nil {
			return nil
		}

		s.lastKnownVersion, err = s.platform.PatchTopology(ctx, patch, s.lastKnownVersion)
		if err == nil {
			s.lastTopology = newTopology
			return nil
		}

		var apiErr platform.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusConflict {
			return fmt.Errorf("patch topology: %w", err)
		}

		retryCount++
		if retryCount >= s.maxPatchRetry {
			return errors.New("too many retries")
		}

		log.Ctx(ctx).Warn().Err(err).Msg("Unable to patch topology")
	}
}

func (s *Store) buildPatch(lastTopology []byte, st topology.Cluster) ([]byte, []byte, error) {
	newTopology, err := json.Marshal(st)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal topology: %w", err)
	}

	if bytes.Equal(newTopology, lastTopology) {
		return nil, newTopology, nil
	}

	patch, err := jsonpatch.CreateMergePatch(lastTopology, newTopology)
	if err != nil {
		return nil, nil, fmt.Errorf("build merge patch: %w", err)
	}
	return patch, newTopology, nil
}
