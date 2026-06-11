/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package servers

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"golang.org/x/net/websocket"
)

// handoffConn wraps a backend connection for ownership transfer across
// the WebSocket upgrade boundary. If the upgrade fails, the deferred
// CloseIfNotTaken() releases the connection and its Manager session.
type handoffConn struct {
	conn  io.ReadWriteCloser
	taken bool
}

func newHandoffConn(conn io.ReadWriteCloser) *handoffConn {
	return &handoffConn{conn: conn}
}

func (h *handoffConn) Take() io.ReadWriteCloser {
	h.taken = true
	return h.conn
}

func (h *handoffConn) CloseIfNotTaken() {
	if !h.taken && h.conn != nil {
		h.conn.Close()
	}
}

// ConsoleProxyWSHandler serves WebSocket console connections.
// Auth is verified BEFORE upgrade -- invalid tickets get HTTP 401.
type ConsoleProxyWSHandler struct {
	core           *ConsoleProxyCore
	allowedOrigins []string
}

// NewConsoleProxyWSHandler creates a new WebSocket console proxy handler.
// The allowedOrigins list controls which Origins are accepted for cookie-based
// authentication. A wildcard "*" permits all origins.
func NewConsoleProxyWSHandler(core *ConsoleProxyCore, allowedOrigins []string) *ConsoleProxyWSHandler {
	return &ConsoleProxyWSHandler{
		core:           core,
		allowedOrigins: allowedOrigins,
	}
}

// checkOrigin validates the request's Origin header against the allowed origins list.
// Returns false if the Origin header is empty or not in the allow-list.
func (h *ConsoleProxyWSHandler) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	for _, allowed := range h.allowedOrigins {
		if allowed == "*" {
			return true
		}
		if strings.EqualFold(origin, allowed) {
			return true
		}
	}
	return false
}

// extractTicket gets the ticket from Authorization header or console-ticket cookie.
// Authorization header takes precedence.
func extractTicket(r *http.Request) (string, error) {
	if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		return ExtractBearerToken(authHeader)
	}

	cookie, err := r.Cookie("console-ticket")
	if err == nil && cookie.Value != "" {
		return cookie.Value, nil
	}

	return "", errors.New("missing ticket: set Authorization header or console-ticket cookie")
}

func (h *ConsoleProxyWSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Cookie-based auth (no Authorization header) requires Origin validation
	// to prevent cross-site WebSocket hijacking (CSWSH). Bearer-authenticated
	// requests skip this check because non-browser clients may omit Origin.
	if r.Header.Get("Authorization") == "" {
		if !h.checkOrigin(r) {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
	}

	// Phase 1: Extract and verify ticket BEFORE upgrade.
	rawTicket, err := extractTicket(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	ticket, err := h.core.OpenTicket(ctx, rawTicket)
	if err != nil {
		http.Error(w, "invalid ticket", http.StatusUnauthorized)
		return
	}

	// Expose console_type to outer middleware (ConsoleMetrics reads it after handler returns).
	setConsoleType(r.Context(), ticket.ConsoleType)

	// Phase 2: Connect backend BEFORE upgrade (fail fast with HTTP error).
	backend, sessionCtx, err := h.core.ConnectBackend(ctx, ticket)
	if err != nil {
		h.core.logger.ErrorContext(ctx, "Failed to connect backend", slog.Any("error", err))
		http.Error(w, "failed to connect to console backend", http.StatusBadGateway)
		return
	}

	// Wrap for ownership transfer across the upgrade boundary.
	handoff := newHandoffConn(backend)
	defer handoff.CloseIfNotTaken()

	// Phase 3: Upgrade to WebSocket.
	wsServer := websocket.Server{
		Handshake: func(config *websocket.Config, r *http.Request) error {
			// Defense-in-depth: validate Origin during WebSocket handshake
			// for cookie-authenticated requests.
			if r.Header.Get("Authorization") == "" {
				if !h.checkOrigin(r) {
					return errors.New("origin not allowed")
				}
			}
			for _, proto := range config.Protocol {
				if strings.EqualFold(proto, "binary") {
					config.Protocol = []string{"binary"}
					return nil
				}
			}
			// Accept without subprotocol for generic clients.
			config.Protocol = nil
			return nil
		},
		Handler: func(ws *websocket.Conn) {
			// Take ownership -- deferred CloseIfNotTaken becomes a no-op.
			// Relay owns closing backendConn via its own defer.
			backendConn := handoff.Take()

			ws.PayloadType = websocket.BinaryFrame

			// Phase 4: Relay with sessionCtx: cancelled on eviction, timeout,
			// or client disconnect. Relay logs errors internally.
			_ = h.core.Relay(sessionCtx, ws, backendConn)
		},
	}
	wsServer.ServeHTTP(w, r)
}
