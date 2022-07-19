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

package certificate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setup(t *testing.T) (*Client, *http.ServeMux) {
	t.Helper()

	mux := http.NewServeMux()

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, "token")
	require.NoError(t, err)
	c.httpClient = srv.Client()

	return c, mux
}

func TestClient_GetCertificate(t *testing.T) {
	tests := []struct {
		desc            string
		wantCertificate Certificate
		wantStatusCode  int
		wantError       require.ErrorAssertionFunc
	}{
		{
			desc: "get certificate",
			wantCertificate: Certificate{
				Domains:     []string{"example.com"},
				NotBefore:   time.Date(2022, 5, 11, 15, 51, 0, 0, time.UTC),
				NotAfter:    time.Date(2022, 5, 21, 15, 51, 0, 0, time.UTC),
				Certificate: []byte("cert"),
				PrivateKey:  []byte("key"),
			},
			wantStatusCode: http.StatusOK,
			wantError:      require.NoError,
		},
		{
			desc:           "internal server error",
			wantStatusCode: http.StatusInternalServerError,
			wantError: func(t require.TestingT, err error, i ...interface{}) {
				require.ErrorAs(t, err, &APIError{})
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			c, mux := setup(t)

			var count int
			mux.HandleFunc("/wildcard-certificate", func(rw http.ResponseWriter, req *http.Request) {
				count++

				if req.Method != http.MethodGet {
					http.Error(rw, fmt.Sprintf("unsupported to method: %s", req.Method), http.StatusMethodNotAllowed)
					return
				}

				if req.Header.Get("Authorization") != "Bearer token" {
					http.Error(rw, "Invalid token", http.StatusUnauthorized)
					return
				}

				rw.WriteHeader(test.wantStatusCode)

				err := json.NewEncoder(rw).Encode(test.wantCertificate)
				if err != nil {
					http.Error(rw, err.Error(), http.StatusInternalServerError)
					return
				}
			})

			gotCertificate, err := c.GetWildcardCertificate(context.Background())
			test.wantError(t, err)

			require.Equal(t, 1, count)
			assert.Equal(t, test.wantCertificate, gotCertificate)
		})
	}
}

func Test_GetCertificateByDomains(t *testing.T) {
	tests := []struct {
		desc       string
		statusCode int
		wantCert   Certificate
		wantErr    error
	}{
		{
			desc:       "get certificate succeed",
			statusCode: http.StatusOK,
			wantCert: Certificate{
				Certificate: []byte("cert"),
				PrivateKey:  []byte("key"),
			},
		},
		{
			desc:       "get certificate unexpected error",
			statusCode: http.StatusTeapot,
			wantCert:   Certificate{},
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

			c, mux := setup(t)

			var callCount int
			mux.HandleFunc("/certificate", func(rw http.ResponseWriter, req *http.Request) {
				callCount++

				if req.Method != http.MethodGet {
					http.Error(rw, fmt.Sprintf("unsupported method: %s", req.Method), http.StatusMethodNotAllowed)
					return
				}

				if req.Header.Get("Authorization") != "Bearer token" {
					http.Error(rw, "Invalid token", http.StatusUnauthorized)
					return
				}

				gotDomain := req.URL.Query()["domains"]
				assert.Equal(t, []string{"a.com", "b.com"}, gotDomain)

				rw.WriteHeader(test.statusCode)

				switch test.statusCode {
				case http.StatusAccepted:
				case http.StatusOK:
					_ = json.NewEncoder(rw).Encode(test.wantCert)

				default:
					_ = json.NewEncoder(rw).Encode(APIError{Message: "error"})
				}
			})

			gotCert, err := c.GetCertificateByDomains(context.Background(), []string{"a.com", "b.com"})
			if test.wantErr != nil {
				require.ErrorAs(t, err, test.wantErr)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, 1, callCount)
			assert.Equal(t, test.wantCert, gotCert)
		})
	}
}
