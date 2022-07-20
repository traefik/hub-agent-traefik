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
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/traefik/hub-agent-traefik/pkg/platform"
	"github.com/traefik/hub-agent-traefik/pkg/topology"
)

func TestStore_Write_fetchAndPatch(t *testing.T) {
	tests := []struct {
		desc            string
		fetchedVersion  int64
		fetchedTopology topology.Cluster
		newTopology     topology.Cluster
		wantPatch       string
		wantVersion     int64
	}{
		{
			desc:            "add one service",
			fetchedVersion:  1,
			fetchedTopology: topology.Cluster{},
			newTopology: topology.Cluster{
				Services: map[string]*topology.Service{
					"service-1": {
						Name: "service-1",
						Container: &topology.Container{
							Name:     "service-1",
							Networks: []string{"network"},
						},
						Ports: []int{8080},
					},
				},
			},
			wantPatch: `{
				"services": {
					"service-1": {
						"container": {
							"name": "service-1",
							"networks": ["network"]
						},
						"externalPorts": [8080],
						"name":"service-1"
					}
				}
			}`,
			wantVersion: 2,
		},
		{
			desc:           "update a single service property",
			fetchedVersion: 1,
			fetchedTopology: topology.Cluster{
				Services: map[string]*topology.Service{
					"service-1": {
						Name: "service-1",
						Container: &topology.Container{
							Name:     "service-1",
							Networks: []string{"network"},
						},
						Ports: []int{8080},
					},
				},
			},
			newTopology: topology.Cluster{
				Services: map[string]*topology.Service{
					"service-1": {
						Name: "service-1",
						Container: &topology.Container{
							Name:     "service-1",
							Networks: []string{"new-network"},
						},
						Ports: []int{8080},
					},
				},
			},
			wantPatch: `{
				"services": {
					"service-1": {
						"container":{"networks":["new-network"]}
					}
				}
			}`,
			wantVersion: 2,
		},
		{
			desc:           "delete a single service property",
			fetchedVersion: 1,
			fetchedTopology: topology.Cluster{
				Services: map[string]*topology.Service{
					"service-1": {
						Name: "service-1",
						Container: &topology.Container{
							Name:     "service-1",
							Networks: []string{"network"},
						},
						Ports: []int{8080},
					},
				},
			},
			newTopology: topology.Cluster{
				Services: map[string]*topology.Service{
					"service-1": {
						Name: "service-1",
						Container: &topology.Container{
							Name:     "service-1",
							Networks: []string{"network"},
						},
					},
				},
			},
			wantPatch: `{
				"services": {
					"service-1": {
						"externalPorts": null
					}
				}
			}`,
			wantVersion: 2,
		},
		{
			desc:           "added one port in a service",
			fetchedVersion: 1,
			fetchedTopology: topology.Cluster{
				Services: map[string]*topology.Service{
					"service-1": {
						Name: "service-1",
						Container: &topology.Container{
							Name:     "service-1",
							Networks: []string{"network"},
						},
						Ports: []int{8080},
					},
				},
			},
			newTopology: topology.Cluster{
				Services: map[string]*topology.Service{
					"service-1": {
						Name: "service-1",
						Container: &topology.Container{
							Name:     "service-1",
							Networks: []string{"network"},
						},
						Ports: []int{8080, 8081},
					},
				},
			},
			wantPatch: `{
				"services": {
					"service-1": {
						"externalPorts": [8080, 8081]
					}
				}
			}`,
			wantVersion: 2,
		},
		{
			desc:           "delete a service",
			fetchedVersion: 1,
			fetchedTopology: topology.Cluster{
				Services: map[string]*topology.Service{
					"service-1": {
						Name: "service-1",
						Container: &topology.Container{
							Name:     "service-1",
							Networks: []string{"network"},
						},
						Ports: []int{8080},
					},
				},
			},
			newTopology: topology.Cluster{
				Services: map[string]*topology.Service{},
			},
			wantPatch: `{
				"services": {
					"service-1": null
				}
			}`,
			wantVersion: 2,
		},
		{
			desc:           "mixed update and delete",
			fetchedVersion: 1,
			fetchedTopology: topology.Cluster{
				Services: map[string]*topology.Service{
					"service-1": {
						Name: "service-1",
						Container: &topology.Container{
							Name:     "service-1",
							Networks: []string{"network"},
						},
						Ports: []int{8080},
					},
					"service-2": {
						Name: "service-2",
						Container: &topology.Container{
							Name:     "service-2",
							Networks: []string{"network"},
						},
						Ports: []int{8080},
					},
				},
			},
			newTopology: topology.Cluster{
				Services: map[string]*topology.Service{
					"service-2": {
						Name: "service-2",
						Container: &topology.Container{
							Name:     "service-2",
							Networks: []string{"new-network"},
						},
						Ports: []int{8080},
					},
				},
			},
			wantPatch: `{
				"services": {
					"service-1": null,
					"service-2": {
						"container":{"networks":["new-network"]}
					}
				}
			}`,
			wantVersion: 2,
		},
		{
			desc:           "no different",
			fetchedVersion: 1,
			fetchedTopology: topology.Cluster{
				Services: map[string]*topology.Service{
					"service-1": {
						Name: "service-1",
						Container: &topology.Container{
							Name:     "service-1",
							Networks: []string{"network"},
						},
						Ports: []int{8080},
					},
				},
			},
			newTopology: topology.Cluster{
				Services: map[string]*topology.Service{
					"service-1": {
						Name: "service-1",
						Container: &topology.Container{
							Name:     "service-1",
							Networks: []string{"network"},
						},
						Ports: []int{8080},
					},
				},
			},
			wantVersion: 1,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			platformClient := newPlatformClientMock(t).
				OnFetchTopology().TypedReturns(test.fetchedTopology, test.fetchedVersion, nil).Once()

			if test.wantPatch != "" {
				patch := []byte(removeSpaces(test.wantPatch))

				platformClient.OnPatchTopology(patch, test.fetchedVersion).TypedReturns(test.wantVersion, nil).Once()
			}

			s := New(platformClient.Parent)

			err := s.Write(context.Background(), test.newTopology)
			require.NoError(t, err)

			assert.Equal(t, test.wantVersion, s.lastKnownVersion)
			assert.NotEmpty(t, s.lastTopology)
		})
	}
}

func TestStore_Write_alreadyFetched(t *testing.T) {
	platformClient := newPlatformClientMock(t).
		OnPatchTopology([]byte(removeSpaces(`{
			"services": {
				"service-1": {
					"container":{"networks":["new-network"]}
				}
			}
		}`)), 1).
		TypedReturns(2, nil).
		Once().
		Parent

	var err error

	s := New(platformClient)
	s.lastKnownVersion = 1
	s.lastTopology, err = json.Marshal(topology.Cluster{
		Services: map[string]*topology.Service{
			"service-1": {
				Name: "service-1",
				Container: &topology.Container{
					Name:     "service-1",
					Networks: []string{"network"},
				},
				Ports: []int{8080},
			},
		},
	})
	require.NoError(t, err)

	newTopology := topology.Cluster{
		Services: map[string]*topology.Service{
			"service-1": {
				Name: "service-1",
				Container: &topology.Container{
					Name:     "service-1",
					Networks: []string{"new-network"},
				},
				Ports: []int{8080},
			},
		},
	}

	err = s.Write(context.Background(), newTopology)
	require.NoError(t, err)

	assert.EqualValues(t, 2, s.lastKnownVersion)
}

func TestStore_Write_retryOnPatchRetryableFailure(t *testing.T) {
	platformClient := newPlatformClientMock(t).
		OnFetchTopology().
		TypedReturns(topology.Cluster{
			Services: map[string]*topology.Service{
				"service-1": {Name: "service-1", Ports: []int{8080}, Container: &topology.Container{Networks: []string{"network"}}},
			},
		}, 1, nil).Once().
		OnFetchTopology().
		TypedReturns(topology.Cluster{
			Services: map[string]*topology.Service{
				"service-1": {Name: "service-1", Ports: []int{8080, 8081}, Container: &topology.Container{Networks: []string{"network"}}},
			},
		}, 2, nil).Once().
		OnFetchTopology().
		TypedReturns(topology.Cluster{
			Services: map[string]*topology.Service{
				"service-1": {Name: "service-1", Ports: []int{8080, 8081, 8082}, Container: &topology.Container{Networks: []string{"network"}}},
			},
		}, 3, nil).Once().
		OnPatchTopology([]byte(removeSpaces(`{
			"services": {
				"service-1": {
					"container": {"name":"service-1"}
				}
			}
		}`)), 1).TypedReturns(0, platform.APIError{StatusCode: http.StatusConflict}).Once().
		OnPatchTopology([]byte(removeSpaces(`{
			"services": {
				"service-1": {
					"container": {"name":"service-1"},
					"externalPorts": [8080]
				}
			}
		}`)), 2).TypedReturns(0, platform.APIError{StatusCode: http.StatusConflict}).Once().
		OnPatchTopology([]byte(removeSpaces(`{
			"services": {
				"service-1": {
					"container": {"name":"service-1"},
					"externalPorts": [8080]
				}
			}
		}`)), 3).TypedReturns(4, nil).Once().
		Parent

	s := New(platformClient)

	newTopology := topology.Cluster{
		Services: map[string]*topology.Service{
			"service-1": {
				Name: "service-1",
				Container: &topology.Container{
					Name:     "service-1",
					Networks: []string{"network"},
				},
				Ports: []int{8080},
			},
		},
	}

	err := s.Write(context.Background(), newTopology)
	require.NoError(t, err)
	assert.EqualValues(t, 4, s.lastKnownVersion)

	// Apply the same topology and make sure it did nothing.
	err = s.Write(context.Background(), newTopology)
	require.NoError(t, err)
	assert.EqualValues(t, 4, s.lastKnownVersion)
}

func TestStore_Write_doNotRetryOnPatchFatalFailure(t *testing.T) {
	platformClient := newPlatformClientMock(t).
		OnFetchTopology().TypedReturns(topology.Cluster{Services: map[string]*topology.Service{"service-1": {Name: "service-1"}}}, 1, nil).Once().
		OnPatchTopology([]byte(`{"services":{"service-1":{"externalPorts":[8080]}}}`), 1).TypedReturns(0, errors.New("boom")).Once().
		OnFetchTopology().TypedReturns(topology.Cluster{Services: map[string]*topology.Service{"service-1": {Name: "service-1"}}}, 2, nil).Once().
		OnPatchTopology([]byte(`{"services":{"service-1":{"externalPorts":[8080]}}}`), 2).TypedReturns(0, platform.APIError{StatusCode: http.StatusInternalServerError}).Once().
		OnFetchTopology().TypedReturns(topology.Cluster{Services: map[string]*topology.Service{"service-1": {Name: "service-1"}}}, 3, nil).Once().
		OnPatchTopology([]byte(`{"services":{"service-1":{"externalPorts":[8080]}}}`), 3).TypedReturns(4, nil).Once().
		Parent

	s := New(platformClient)

	newTopology := topology.Cluster{
		Services: map[string]*topology.Service{
			"service-1": {
				Name:  "service-1",
				Ports: []int{8080},
			},
		},
	}

	err := s.Write(context.Background(), newTopology)
	require.Error(t, err)
	assert.EqualValues(t, 0, s.lastKnownVersion)

	err = s.Write(context.Background(), newTopology)
	require.Error(t, err)
	assert.EqualValues(t, 0, s.lastKnownVersion)

	// Apply the same topology with a successful patch.
	err = s.Write(context.Background(), newTopology)
	require.NoError(t, err)
	assert.EqualValues(t, 4, s.lastKnownVersion)
}

func TestStore_Write_abortOnFetchFailure(t *testing.T) {
	platformClient := newPlatformClientMock(t).
		OnFetchTopology().TypedReturns(topology.Cluster{}, 0, errors.New("boom")).Once().
		OnFetchTopology().
		TypedReturns(topology.Cluster{
			Services: map[string]*topology.Service{
				"service-1": {Name: "service-1", Ports: []int{8080}, Container: &topology.Container{Name: "service-1"}},
			},
		}, 1, nil).Once().
		OnPatchTopology([]byte(removeSpaces(`{
			"services": {
				"service-1": {
					"container": {"networks":["network"]}
				}
			}
		}`)), 1).TypedReturns(2, nil).Once().
		Parent

	s := New(platformClient)

	newTopology := topology.Cluster{
		Services: map[string]*topology.Service{
			"service-1": {
				Name: "service-1",
				Container: &topology.Container{
					Name:     "service-1",
					Networks: []string{"network"},
				},
				Ports: []int{8080},
			},
		},
	}

	err := s.Write(context.Background(), newTopology)
	require.Error(t, err)
	assert.EqualValues(t, 0, s.lastKnownVersion)

	// Make sure that if the fetch didn't fail the next time it will patch the topology successfully.
	err = s.Write(context.Background(), newTopology)
	require.NoError(t, err)
	assert.EqualValues(t, 2, s.lastKnownVersion)
}

func TestStore_Write_giveUpOnRetryingWhenReachedBackoffLimit(t *testing.T) {
	platformClient := newPlatformClientMock(t).
		OnFetchTopology().TypedReturns(topology.Cluster{Services: map[string]*topology.Service{"service-1": {Name: "service-1"}}}, 1, nil).Once().
		OnFetchTopology().TypedReturns(topology.Cluster{Services: map[string]*topology.Service{"service-1": {Name: "service-2"}}}, 2, nil).Once().
		OnFetchTopology().TypedReturns(topology.Cluster{Services: map[string]*topology.Service{"service-1": {Name: "service-3"}}}, 3, nil).Once().
		OnPatchTopology([]byte(`{"services":{"service-1":{"name":"service-5"}}}`), 1).TypedReturns(0, platform.APIError{StatusCode: http.StatusConflict}).Once().
		OnPatchTopology([]byte(`{"services":{"service-1":{"name":"service-5"}}}`), 2).TypedReturns(0, platform.APIError{StatusCode: http.StatusConflict}).Once().
		OnPatchTopology([]byte(`{"services":{"service-1":{"name":"service-5"}}}`), 3).TypedReturns(0, platform.APIError{StatusCode: http.StatusConflict}).Once().
		OnFetchTopology().TypedReturns(topology.Cluster{Services: map[string]*topology.Service{"service-1": {Name: "service-4"}}}, 4, nil).Once().
		OnPatchTopology([]byte(`{"services":{"service-1":{"name":"service-5"}}}`), 4).TypedReturns(5, nil).Once().
		Parent

	s := New(platformClient)
	s.maxPatchRetry = 3

	newTopology := topology.Cluster{
		Services: map[string]*topology.Service{"service-1": {Name: "service-5"}},
	}

	err := s.Write(context.Background(), newTopology)
	require.Error(t, err)
	assert.EqualValues(t, 0, s.lastKnownVersion)

	// Apply the same topology with a successful patch.
	err = s.Write(context.Background(), newTopology)
	require.NoError(t, err)
	assert.EqualValues(t, 5, s.lastKnownVersion)
}

func removeSpaces(s string) string {
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\t", "")

	return s
}
