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

package platform

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/traefik/hub-agent-traefik/pkg/topology"
)

const testToken = "123"

func TestClient_Link(t *testing.T) {
	tests := []struct {
		desc             string
		returnClusterID  string
		returnStatusCode int
		wantErr          assert.ErrorAssertionFunc
	}{
		{
			desc:             "cluster successfully linked",
			returnClusterID:  "clusterID",
			returnStatusCode: http.StatusOK,
			wantErr:          assert.NoError,
		},
		{
			desc:             "failed to link cluster",
			returnStatusCode: http.StatusTeapot,
			wantErr:          assert.Error,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			var callCount int

			mux := http.NewServeMux()
			mux.HandleFunc("/link", func(rw http.ResponseWriter, req *http.Request) {
				callCount++

				if req.Method != http.MethodPost {
					http.Error(rw, fmt.Sprintf("unsupported to method: %s", req.Method), http.StatusMethodNotAllowed)
					return
				}

				if req.Header.Get("Authorization") != "Bearer "+testToken {
					http.Error(rw, "Invalid token", http.StatusUnauthorized)
					return
				}

				b, err := io.ReadAll(req.Body)
				if err != nil {
					http.Error(rw, err.Error(), http.StatusInternalServerError)
					return
				}

				if !bytes.Equal([]byte(`{"platform":"other","version":"dev"}`), b) {
					http.Error(rw, fmt.Sprintf("invalid body: %s", string(b)), http.StatusBadRequest)
					return
				}

				rw.WriteHeader(test.returnStatusCode)
				err = json.NewEncoder(rw).Encode(linkClusterResp{ClusterID: test.returnClusterID})
				require.NoError(t, err)
			})

			srv := httptest.NewServer(mux)

			t.Cleanup(srv.Close)

			c, err := NewClient(srv.URL, testToken)
			require.NoError(t, err)
			c.httpClient = srv.Client()

			clusterID, err := c.Link(context.Background())
			test.wantErr(t, err)

			if test.returnStatusCode == http.StatusOK {
				require.Equal(t, clusterID, test.returnClusterID)
			}
			require.Equal(t, 1, callCount)
		})
	}
}

func TestClient_GetConfig(t *testing.T) {
	tests := []struct {
		desc             string
		returnStatusCode int
		wantConfig       Config
		wantErr          assert.ErrorAssertionFunc
	}{
		{
			desc:             "get config succeeds",
			returnStatusCode: http.StatusOK,
			wantConfig: Config{
				Metrics: MetricsConfig{
					Interval: time.Minute,
					Tables:   []string{"1m", "10m"},
				},
			},
			wantErr: assert.NoError,
		},
		{
			desc:             "get config fails",
			returnStatusCode: http.StatusTeapot,
			wantConfig:       Config{},
			wantErr:          assert.Error,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			var callCount int

			mux := http.NewServeMux()
			mux.HandleFunc("/config", func(rw http.ResponseWriter, req *http.Request) {
				callCount++

				if req.Method != http.MethodGet {
					http.Error(rw, fmt.Sprintf("unsupported to method: %s", req.Method), http.StatusMethodNotAllowed)
					return
				}

				if req.Header.Get("Authorization") != "Bearer "+testToken {
					http.Error(rw, "Invalid token", http.StatusUnauthorized)
					return
				}

				rw.WriteHeader(test.returnStatusCode)
				_ = json.NewEncoder(rw).Encode(test.wantConfig)
			})

			srv := httptest.NewServer(mux)

			t.Cleanup(srv.Close)

			c, err := NewClient(srv.URL, testToken)
			require.NoError(t, err)
			c.httpClient = srv.Client()

			agentCfg, err := c.GetConfig(context.Background())
			test.wantErr(t, err)

			require.Equal(t, 1, callCount)

			assert.Equal(t, test.wantConfig, agentCfg)
		})
	}
}

func TestClient_Ping(t *testing.T) {
	tests := []struct {
		desc             string
		returnStatusCode int
		wantErr          assert.ErrorAssertionFunc
	}{
		{
			desc:             "ping successfully sent",
			returnStatusCode: http.StatusOK,
			wantErr:          assert.NoError,
		},
		{
			desc:             "ping sent for an unknown cluster",
			returnStatusCode: http.StatusNotFound,
			wantErr:          assert.Error,
		},
		{
			desc:             "error on ping",
			returnStatusCode: http.StatusInternalServerError,
			wantErr:          assert.Error,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			var callCount int

			mux := http.NewServeMux()
			mux.HandleFunc("/ping", func(rw http.ResponseWriter, req *http.Request) {
				callCount++

				if req.Method != http.MethodPost {
					http.Error(rw, fmt.Sprintf("unsupported to method: %s", req.Method), http.StatusMethodNotAllowed)
					return
				}

				if req.Header.Get("Authorization") != "Bearer "+testToken {
					http.Error(rw, "Invalid token", http.StatusUnauthorized)
					return
				}

				rw.WriteHeader(test.returnStatusCode)
			})

			srv := httptest.NewServer(mux)

			t.Cleanup(srv.Close)

			c, err := NewClient(srv.URL, testToken)
			require.NoError(t, err)
			c.httpClient = srv.Client()

			err = c.Ping(context.Background())
			test.wantErr(t, err)

			require.Equal(t, 1, callCount)
		})
	}
}

func TestClient_FetchTopology(t *testing.T) {
	tests := []struct {
		desc         string
		statusCode   int
		resp         []byte
		wantVersion  int64
		wantTopology topology.Cluster
		wantErr      error
	}{
		{
			desc:       "fetch topology succeed",
			statusCode: http.StatusOK,
			resp: []byte(`{
				"version": 1,
				"topology": {
					"overview": {
						"serviceCount": 1
					},
					"services": {
						"service-1": {
							"container": {
								"name": "service-1",
								"networks": ["network"]
							},
							"name": "service-1",
							"externalPorts": [8080]
						}
					}
				}
			}`),
			wantVersion: 1,
			wantTopology: topology.Cluster{
				Services: map[string]*topology.Service{
					"service-1": {
						Name:  "service-1",
						Ports: []int{8080},
						Container: &topology.Container{
							Name:     "service-1",
							Networks: []string{"network"},
						},
					},
				},
			},
		},
		{
			desc:       "fetch topology unexpected error",
			statusCode: http.StatusTeapot,
			wantErr: &APIError{
				StatusCode: http.StatusTeapot,
				Message:    "error",
			},
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			var callCount int

			mux := http.NewServeMux()
			mux.HandleFunc("/topology", func(rw http.ResponseWriter, req *http.Request) {
				callCount++

				if req.Method != http.MethodGet {
					http.Error(rw, fmt.Sprintf("unsupported method: %s", req.Method), http.StatusMethodNotAllowed)
					return
				}

				if req.Header.Get("Authorization") != "Bearer 123" {
					http.Error(rw, "Invalid token", http.StatusUnauthorized)
					return
				}

				rw.WriteHeader(test.statusCode)

				switch test.statusCode {
				case http.StatusOK:
					_, _ = rw.Write(test.resp)
				default:
					_ = json.NewEncoder(rw).Encode(APIError{Message: "error"})
				}
			})

			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			c, err := NewClient(srv.URL, "123")
			require.NoError(t, err)
			c.httpClient = srv.Client()

			gotTopology, gotVersion, err := c.FetchTopology(context.Background())
			if test.wantErr != nil {
				require.ErrorAs(t, err, test.wantErr)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, 1, callCount)
			assert.Equal(t, test.wantVersion, gotVersion)
			assert.Equal(t, test.wantTopology, gotTopology)
		})
	}
}

func TestClient_PatchTopology(t *testing.T) {
	tests := []struct {
		desc             string
		statusCode       int
		patch            []byte
		lastKnownVersion int64
		resp             []byte
		wantVersion      int64
		wantErr          error
	}{
		{
			desc:       "patch topology succeed",
			statusCode: http.StatusOK,
			patch: []byte(`{
				"services": {
					"service-1": null,
					"service-2": {
						"externalPorts": [8080]
					}
				}
			}`),
			lastKnownVersion: 1,
			resp:             []byte(`{"version": 2}`),
			wantVersion:      2,
		},
		{
			desc:             "patch conflict",
			statusCode:       http.StatusConflict,
			patch:            []byte(`{"services": {"service-1": null}}`),
			lastKnownVersion: 1,
			wantErr: &APIError{
				StatusCode: http.StatusConflict,
				Message:    "error",
			},
		},
		{
			desc:       "patch topology unexpected error",
			statusCode: http.StatusInternalServerError,
			wantErr: &APIError{
				StatusCode: http.StatusInternalServerError,
				Message:    "error",
			},
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			var callCount int

			mux := http.NewServeMux()
			mux.HandleFunc("/topology", func(rw http.ResponseWriter, req *http.Request) {
				callCount++

				if req.Method != http.MethodPatch {
					http.Error(rw, fmt.Sprintf("unsupported method: %s", req.Method), http.StatusMethodNotAllowed)
					return
				}

				if req.Header.Get("Authorization") != "Bearer 456" {
					http.Error(rw, "Invalid token", http.StatusUnauthorized)
					return
				}
				if req.Header.Get("Content-Type") != "application/merge-patch+json" {
					http.Error(rw, "Invalid Content-Type", http.StatusBadRequest)
					return
				}
				if req.Header.Get("Last-Known-Version") != strconv.FormatInt(test.lastKnownVersion, 10) {
					http.Error(rw, "Invalid Content-Type", http.StatusBadRequest)
					return
				}
				if req.Header.Get("Content-Encoding") != "gzip" {
					http.Error(rw, "Invalid Content-Encoding", http.StatusBadRequest)
					return
				}

				reader, err := gzip.NewReader(req.Body)
				if err != nil {
					http.Error(rw, err.Error(), http.StatusInternalServerError)
					return
				}
				defer func() { _ = reader.Close() }()

				body, err := io.ReadAll(reader)
				if err != nil {
					http.Error(rw, err.Error(), http.StatusInternalServerError)
					return
				}

				if !bytes.Equal(test.patch, body) {
					http.Error(rw, "invalid patch", http.StatusBadRequest)
					return
				}

				rw.WriteHeader(test.statusCode)

				switch test.statusCode {
				case http.StatusOK:
					_, _ = rw.Write(test.resp)
				default:
					_ = json.NewEncoder(rw).Encode(APIError{Message: "error"})
				}
			})

			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			c, err := NewClient(srv.URL, "456")
			require.NoError(t, err)
			c.httpClient = srv.Client()

			gotVersion, err := c.PatchTopology(context.Background(), test.patch, test.lastKnownVersion)
			if test.wantErr != nil {
				require.ErrorAs(t, err, test.wantErr)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, 1, callCount)
			assert.EqualValues(t, test.wantVersion, gotVersion)
		})
	}
}
