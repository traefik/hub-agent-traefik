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

package acp

import (
	"context"
	"errors"
	"fmt"
	stdlog "log"
	"net/http"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/traefik/hub-agent-traefik/pkg/acp/basicauth"
	"github.com/traefik/hub-agent-traefik/pkg/acp/jwt"
	"github.com/traefik/hub-agent-traefik/pkg/edge"
)

// Server serves ACP endpoints.
type Server struct {
	listenAddr string
	handler    *httpHandler
}

// NewServer creates a new ACP Server.
func NewServer(listenAddr string) *Server {
	return &Server{
		listenAddr: listenAddr,
		handler:    newHTTPHandler(),
	}
}

// UpdateHandler updates auth routes served by the Server.
func (s *Server) UpdateHandler(acps []edge.ACP) error {
	routes, err := buildRoutes(acps)
	if err != nil {
		return fmt.Errorf("build routes: %w", err)
	}

	s.handler.Update(routes)

	return nil
}

// Run runs the ACP auth server.
func (s *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()

	mux.Handle("/_live", http.HandlerFunc(func(rw http.ResponseWriter, request *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	mux.Handle("/_ready", http.HandlerFunc(func(rw http.ResponseWriter, request *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))

	mux.Handle("/", s.handler)

	server := &http.Server{
		Addr:              s.listenAddr,
		Handler:           mux,
		ErrorLog:          stdlog.New(log.Logger.Level(zerolog.DebugLevel), "", 0),
		ReadHeaderTimeout: 2 * time.Second,
	}

	srvDone := make(chan struct{})

	go func() {
		log.Info().Str("addr", s.listenAddr).Msg("Starting auth server")
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Err(err).Msg("Unable to listen and serve auth requests")
		}
		close(srvDone)
	}()

	select {
	case <-ctx.Done():
		gracefulCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		//nolint:contextcheck // False positive.
		if err := server.Shutdown(gracefulCtx); err != nil {
			log.Error().Err(err).Msg("Failed to shutdown auth server gracefully")
			if err = server.Close(); err != nil {
				return fmt.Errorf("close auth server: %w", err)
			}
		}

		return nil
	case <-srvDone:
		return errors.New("auth server stopped")
	}
}

func buildRoutes(acps []edge.ACP) (http.Handler, error) {
	mux := http.NewServeMux()

	for _, acp := range acps {
		switch {
		case acp.JWT != nil:
			jwtHandler, err := jwt.NewHandler(acp.JWT, acp.Name)
			if err != nil {
				return nil, fmt.Errorf("create %q JWT ACP handler: %w", acp.Name, err)
			}

			path := "/" + acp.Name

			log.Debug().Str("acp_name", acp.Name).Str("path", path).Msg("Registering JWT ACP handler")

			mux.Handle(path, jwtHandler)

		case acp.BasicAuth != nil:
			h, err := basicauth.NewHandler(acp.BasicAuth, acp.Name)
			if err != nil {
				return nil, fmt.Errorf("create %q basic auth ACP handler: %w", acp.Name, err)
			}
			path := "/" + acp.Name
			log.Debug().Str("acp_name", acp.Name).Str("path", path).Msg("Registering basic auth ACP handler")
			mux.Handle(path, h)

		default:
			return nil, errors.New("unknown ACP handler type")
		}
	}

	return mux, nil
}
