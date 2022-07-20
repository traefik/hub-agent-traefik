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

package traefik

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/rs/zerolog/log"
	"github.com/traefik/genconf/dynamic"
	"github.com/traefik/hub-agent-traefik/pkg/logger"
)

const hostname = "proxy.traefik"

// Client allows interacting with a Traefik instance.
type Client struct {
	baseURL *url.URL

	httpClient *http.Client
}

// NewClient returns a new Client.
func NewClient(baseURL string, insecure bool, ca, cert, key string) (*Client, error) {
	u, err := url.ParseRequestURI(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	tlsCfg, err := createTLSConf(insecure, ca, cert, key)
	if err != nil {
		return nil, fmt.Errorf("create tls configuration with ca=%q, cert=%q, key=%q: %w", ca, cert, key, err)
	}

	rc := retryablehttp.NewClient()
	rc.RetryMax = 4
	rc.Logger = logger.NewWrappedLogger(log.Logger.With().Str("component", "traefik_client").Logger())
	rc.HTTPClient.Transport = &http.Transport{TLSClientConfig: tlsCfg}

	return &Client{
		baseURL:    u,
		httpClient: rc.StandardClient(),
	}, nil
}

// createTLSConf creates TLS configuration with the provided certificates.
func createTLSConf(insecure bool, ca, cert, key string) (*tls.Config, error) {
	if insecure && (ca != "" || cert != "" || key != "") {
		return nil, errors.New("unexpected certs fields with tls.insecure activated")
	}
	if !insecure && (ca == "" || cert == "" || key == "") {
		return nil, errors.New("invalid tls configuration")
	}

	if insecure {
		certificate, err := DefaultCertificate()
		if err != nil {
			return nil, fmt.Errorf("default certificate generation: %w", err)
		}

		return &tls.Config{
			ServerName:         hostname,
			Certificates:       []tls.Certificate{*certificate},
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS13,
		}, nil
	}

	caPool, err := loadCA(ca)
	if err != nil {
		return nil, fmt.Errorf("load CA: %w", err)
	}

	certificate, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		return nil, fmt.Errorf("load certificate:%w", err)
	}

	// mTLS. ClientCAs and RootCAs are defined to work both for server and client.
	return &tls.Config{
		ClientCAs:    caPool,
		RootCAs:      caPool,
		Certificates: []tls.Certificate{certificate},
		ServerName:   hostname,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

func loadCA(caPath string) (*x509.CertPool, error) {
	caCertFile, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("error reading CA certificate: %w", err)
	}
	certPool := x509.NewCertPool()
	certPool.AppendCertsFromPEM(caCertFile)

	return certPool, nil
}

type configRequest struct {
	UnixNano      int64                  `json:"unixNano"`
	Configuration *dynamic.Configuration `json:"configuration"`
}

// PushDynamic pushes a dynamic configuration.
func (c *Client) PushDynamic(ctx context.Context, unixNano int64, cfg *dynamic.Configuration) error {
	endpoint, err := c.baseURL.Parse(path.Join(c.baseURL.Path, "config"))
	if err != nil {
		return err
	}

	payload := configRequest{
		UnixNano:      unixNano,
		Configuration: cfg,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("serialize configuration: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("build request for %q: %w", endpoint.String(), err)
	}

	resp, err := c.doReq(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)

		return fmt.Errorf("expected status code %d; got %d: %s", http.StatusOK, resp.StatusCode, bytes.TrimSpace(data))
	}

	return nil
}

// GetAgentReachableIP returns an IP address the Hub provider can reach the Agent from.
func (c *Client) GetAgentReachableIP(ctx context.Context) (string, error) {
	// First, start an ephemeral HTTP server that Traefik will try to call to make sure the Agent is reachable.
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		return "", err
	}

	port := listener.Addr().(*net.TCPAddr).Port
	nonce := generateNonce(16)

	mux := http.NewServeMux()

	var ok bool
	mux.HandleFunc("/", func(_ http.ResponseWriter, req *http.Request) {
		if req.URL.Query().Get("nonce") == nonce {
			ok = true
		}
	})

	s := &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second}

	defer func() { _ = s.Close() }()

	transport, isTransport := c.httpClient.Transport.(*http.Transport)
	if !isTransport {
		return "", fmt.Errorf("http client transport is not an http.Transport")
	}
	s.TLSConfig = transport.TLSClientConfig

	go func(s *http.Server) {
		if errServe := s.ServeTLS(listener, "", ""); !errors.Is(errServe, http.ErrServerClosed) {
			log.Error().Err(errServe).Msg("Unable to serve temporary discovery server")
			return
		}
	}(s)

	// Then, signal Traefik to try and call us.
	endpoint, err := c.baseURL.Parse(path.Join(c.baseURL.Path, "discover-ip"))
	if err != nil {
		return "", err
	}

	q := make(url.Values)
	q.Set("port", strconv.Itoa(port))
	q.Set("nonce", nonce)
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), http.NoBody)
	if err != nil {
		return "", fmt.Errorf("build request for %q: %w", endpoint.String(), err)
	}

	resp, err := c.doReq(req)
	if err != nil {
		return "", fmt.Errorf("do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)

		return "", fmt.Errorf("expected status code %d; got %d: %s", http.StatusOK, resp.StatusCode, bytes.TrimSpace(b))
	}

	if !ok {
		return "", errors.New("ip discovery failed")
	}

	var ip string
	if err = json.NewDecoder(resp.Body).Decode(&ip); err != nil {
		return "", fmt.Errorf("deserialize ip: %w", err)
	}

	return ip, nil
}

// ProviderState is the state of a Hub provider.
type ProviderState struct {
	LastConfigUnixNano int64 `json:"lastConfigUnixNano"`
}

// GetProviderState returns the current ProviderState.
func (c *Client) GetProviderState(ctx context.Context) (ProviderState, error) {
	endpoint, err := c.baseURL.Parse(path.Join(c.baseURL.Path, "state"))
	if err != nil {
		return ProviderState{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), http.NoBody)
	if err != nil {
		return ProviderState{}, fmt.Errorf("build request for %q: %w", endpoint.String(), err)
	}

	resp, err := c.doReq(req)
	if err != nil {
		return ProviderState{}, fmt.Errorf("request %q: %w", endpoint.String(), err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)

		return ProviderState{}, fmt.Errorf("expected status code %d; got %d: %s", http.StatusOK, resp.StatusCode, bytes.TrimSpace(b))
	}

	var ps ProviderState
	if err = json.NewDecoder(resp.Body).Decode(&ps); err != nil {
		return ProviderState{}, fmt.Errorf("deserialize provider state: %w", err)
	}

	return ps, nil
}

func generateNonce(n int) string {
	charSet := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

	b := make([]rune, n)
	for i := range b {
		b[i] = charSet[rand.Intn(len(charSet))]
	}

	return string(b)
}

// GetMetrics returns the Traefik metrics.
func (c *Client) GetMetrics(ctx context.Context) ([]*dto.MetricFamily, error) {
	endpoint, err := c.baseURL.Parse(path.Join(c.baseURL.Path, "metrics"))
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build request for %q: %w", endpoint.String(), err)
	}

	resp, err := c.doReq(req)
	if err != nil {
		return nil, fmt.Errorf("request %q: %w", endpoint.String(), err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)

		return nil, fmt.Errorf("expected status code %d; got %d: %s", http.StatusOK, resp.StatusCode, bytes.TrimSpace(b))
	}

	var m []*dto.MetricFamily
	dec := expfmt.NewDecoder(resp.Body, expfmt.ResponseFormat(resp.Header))
	for {
		var fam dto.MetricFamily
		err = dec.Decode(&fam)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return m, nil
			}

			return nil, err
		}

		m = append(m, &fam)
	}
}

func (c *Client) doReq(req *http.Request) (*http.Response, error) {
	req.Host = hostname

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}

	return resp, nil
}
