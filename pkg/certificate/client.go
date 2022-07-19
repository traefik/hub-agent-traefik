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
	"io"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/rs/zerolog/log"
	"github.com/traefik/hub-agent-traefik/pkg/logger"
)

// APIError represents an error returned by the API.
type APIError struct {
	StatusCode int
	Message    string `json:"error"`
}

func (a APIError) Error() string {
	return fmt.Sprintf("failed with code %d: %s", a.StatusCode, a.Message)
}

// Certificate represents the certificate returned by the platform.
type Certificate struct {
	Domains     []string  `json:"domains"`
	NotAfter    time.Time `json:"notAfter"`
	NotBefore   time.Time `json:"notBefore"`
	Certificate []byte    `json:"certificate"`
	PrivateKey  []byte    `json:"privateKey"`
}

// Client allows interacting with the certificates service.
type Client struct {
	baseURL    *url.URL
	httpClient *http.Client

	token string
}

// NewClient creates a new certificates for the certificates service.
func NewClient(baseURL, token string) (*Client, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid certificate client url: %w", err)
	}

	rc := retryablehttp.NewClient()
	rc.RetryMax = 4
	rc.Logger = logger.NewRetryableHTTPWrapper(log.Logger.With().Str("component", "certificate-client").Logger())
	rc.HTTPClient.Timeout = 5 * time.Second

	return &Client{
		baseURL:    base,
		token:      token,
		httpClient: rc.StandardClient(),
	}, nil
}

// GetWildcardCertificate obtains a certificate for the workspace.
func (c *Client) GetWildcardCertificate(ctx context.Context) (Certificate, error) {
	baseURL, err := c.baseURL.Parse(path.Join(c.baseURL.Path, "/wildcard-certificate"))
	if err != nil {
		return Certificate{}, fmt.Errorf("parse endpoint: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL.String(), http.NoBody)
	if err != nil {
		return Certificate{}, fmt.Errorf("build request: %w", err)
	}

	var cert Certificate
	err = c.do(req, &cert)
	if err != nil {
		return Certificate{}, err
	}

	return cert, nil
}

// GetCertificateByDomains obtains a certificate for the workspace.
func (c *Client) GetCertificateByDomains(ctx context.Context, domains []string) (Certificate, error) {
	baseURL, err := c.baseURL.Parse(path.Join(c.baseURL.Path, "/certificate"))
	if err != nil {
		return Certificate{}, fmt.Errorf("parse endpoint: %w", err)
	}

	query := baseURL.Query()
	for _, domain := range domains {
		query.Add("domains", domain)
	}
	baseURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL.String(), http.NoBody)
	if err != nil {
		return Certificate{}, fmt.Errorf("build request: %w", err)
	}

	var cert Certificate
	err = c.do(req, &cert)
	if err != nil {
		return Certificate{}, err
	}

	return cert, nil
}

func (c Client) do(req *http.Request, result interface{}) error {
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 != 2 {
		all, _ := io.ReadAll(resp.Body)

		apiErr := APIError{StatusCode: resp.StatusCode}
		if err = json.Unmarshal(all, &apiErr); err != nil {
			apiErr.Message = string(all)
		}

		return apiErr
	}

	if result != nil {
		if err = json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("decode config: %w", err)
		}
	}

	return nil
}
