package traefik

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/traefik/genconf/dynamic"
	traefiktls "github.com/traefik/genconf/dynamic/tls"
)

func TestMTLS(t *testing.T) {
	caPath := "./fixtures/rootCA.pem"
	caPool, err := loadCA(caPath)
	require.NoError(t, err)

	mux := http.NewServeMux()
	callCount := 0
	mux.HandleFunc("/", func(rw http.ResponseWriter, req *http.Request) {
		callCount++
	})

	serverCertificate, err := tls.LoadX509KeyPair(
		"./fixtures/proxy.traefik-client.pem",
		"./fixtures/proxy.traefik-client-key.pem")
	require.NoError(t, err)

	ts := httptest.NewUnstartedServer(mux)
	ts.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCertificate},
		ClientCAs:    caPool,
		ServerName:   "proxy.traefik",
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}
	ts.StartTLS()
	t.Cleanup(ts.Close)

	certPath := "./fixtures/agent.traefik-client.pem"
	keyPath := "./fixtures/agent.traefik-client-key.pem"
	client, err := NewClient(ts.URL, false, caPath, certPath, keyPath)
	require.NoError(t, err)

	err = client.PushDynamic(context.Background(), time.Now().UnixNano(), emptyDynamicConfiguration())
	require.NoError(t, err)

	assert.Equal(t, 1, callCount)
}

func emptyDynamicConfiguration() *dynamic.Configuration {
	return &dynamic.Configuration{
		HTTP: &dynamic.HTTPConfiguration{
			Routers:           make(map[string]*dynamic.Router),
			Middlewares:       make(map[string]*dynamic.Middleware),
			Services:          make(map[string]*dynamic.Service),
			ServersTransports: make(map[string]*dynamic.ServersTransport),
		},
		TCP: &dynamic.TCPConfiguration{
			Routers:  make(map[string]*dynamic.TCPRouter),
			Services: make(map[string]*dynamic.TCPService),
		},
		TLS: &dynamic.TLSConfiguration{
			Stores:  make(map[string]traefiktls.Store),
			Options: make(map[string]traefiktls.Options),
		},
		UDP: &dynamic.UDPConfiguration{
			Routers:  make(map[string]*dynamic.UDPRouter),
			Services: make(map[string]*dynamic.UDPService),
		},
	}
}
