/*
Copyright (c) 2025 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

// consoleConnectMethod is the gRPC method path used in ext-auth checks for WebSocket console
// connections. This matches the actual gRPC Console/Connect method so that the same authorization
// policies apply to both the gRPC and WebSocket paths.
const consoleConnectMethod = "/osac.public.v1.Console/Connect"

// WebSocketAuthenticator authenticates incoming WebSocket requests and returns a context
// containing the Subject. Implementations handle token extraction and validation according
// to the auth mode (guest or external).
type WebSocketAuthenticator interface {
	Authenticate(ctx context.Context, r *http.Request, resourceID string) (context.Context, error)
}

// GuestWebSocketAuthBuilder builds a GuestWebSocketAuth.
type GuestWebSocketAuthBuilder struct {
	logger *slog.Logger
}

// GuestWebSocketAuth authenticates WebSocket requests in guest mode. Since there is no credential
// to validate, it enforces same-origin as its access control: the Origin header (if present) must
// match the request Host. This prevents arbitrary external websites from connecting to the dev
// server. Cross-origin requests are rejected.
type GuestWebSocketAuth struct {
	logger *slog.Logger
}

// NewGuestWebSocketAuth creates a new builder for the guest WebSocket authenticator.
func NewGuestWebSocketAuth() *GuestWebSocketAuthBuilder {
	return &GuestWebSocketAuthBuilder{}
}

func (b *GuestWebSocketAuthBuilder) SetLogger(value *slog.Logger) *GuestWebSocketAuthBuilder {
	b.logger = value
	return b
}

func (b *GuestWebSocketAuthBuilder) Build() (*GuestWebSocketAuth, error) {
	if b.logger == nil {
		return nil, errors.New("logger is mandatory")
	}
	return &GuestWebSocketAuth{
		logger: b.logger,
	}, nil
}

func (a *GuestWebSocketAuth) Authenticate(ctx context.Context, r *http.Request, resourceID string) (context.Context, error) {
	origin := r.Header.Get("Origin")
	if origin != "" {
		// The Origin header includes the scheme (e.g. "https://host:port"), so we
		// extract just the host portion to compare against the request Host.
		originHost := strings.TrimPrefix(origin, "https://")
		originHost = strings.TrimPrefix(originHost, "http://")
		if originHost != r.Host {
			a.logger.WarnContext(ctx, "Guest WebSocket auth rejected cross-origin request",
				slog.String("origin", origin),
				slog.String("host", r.Host),
			)
			return ctx, grpcstatus.Errorf(grpccodes.PermissionDenied, "cross-origin requests are not allowed in guest mode")
		}
	}
	a.logger.DebugContext(ctx, "Guest WebSocket auth granted")
	return ContextWithSubject(ctx, Guest), nil
}

// ExternalWebSocketAuthBuilder builds an ExternalWebSocketAuth.
type ExternalWebSocketAuthBuilder struct {
	logger  *slog.Logger
	checker *ExternalAuthChecker
}

// ExternalWebSocketAuth authenticates WebSocket requests using the external auth service.
// It extracts the bearer token from the Authorization header or the token query parameter,
// then delegates to the shared ExternalAuthChecker.
type ExternalWebSocketAuth struct {
	logger  *slog.Logger
	checker *ExternalAuthChecker
}

// NewExternalWebSocketAuth creates a new builder for the external WebSocket authenticator.
func NewExternalWebSocketAuth() *ExternalWebSocketAuthBuilder {
	return &ExternalWebSocketAuthBuilder{}
}

func (b *ExternalWebSocketAuthBuilder) SetLogger(value *slog.Logger) *ExternalWebSocketAuthBuilder {
	b.logger = value
	return b
}

func (b *ExternalWebSocketAuthBuilder) SetChecker(value *ExternalAuthChecker) *ExternalWebSocketAuthBuilder {
	b.checker = value
	return b
}

func (b *ExternalWebSocketAuthBuilder) Build() (*ExternalWebSocketAuth, error) {
	if b.logger == nil {
		return nil, errors.New("logger is mandatory")
	}
	if b.checker == nil {
		return nil, errors.New("checker is mandatory")
	}
	return &ExternalWebSocketAuth{
		logger:  b.logger,
		checker: b.checker,
	}, nil
}

func (a *ExternalWebSocketAuth) Authenticate(ctx context.Context, r *http.Request, resourceID string) (context.Context, error) {
	// Extract the bearer token from the Authorization header or the token query parameter.
	// The query parameter is needed because the browser WebSocket API cannot set custom headers.
	token := ""
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		token = authHeader
	} else if r.URL != nil {
		token = r.URL.Query().Get("token")
		if token != "" {
			// Normalize to Bearer format for the ext-auth check.
			if !strings.HasPrefix(strings.ToLower(token), "bearer ") {
				token = fmt.Sprintf("Bearer %s", token)
			}
		}
	}

	if token == "" {
		return ctx, grpcstatus.Errorf(grpccodes.Unauthenticated, "missing authentication token")
	}

	// Build context extensions with the resource ID so the authorization service can make
	// fine-grained access decisions.
	extensions := map[string]string{}
	if resourceID != "" {
		extensions["id"] = resourceID
	}

	// Delegate to the shared checker using the same method path as the gRPC Console/Connect
	// stream, so that the same authorization policies apply.
	return a.checker.Check(ctx, CheckParams{
		Method: http.MethodPost,
		Path:   consoleConnectMethod,
		Headers: map[string]string{
			"authorization": token,
		},
		ContextExtensions: extensions,
	})
}
