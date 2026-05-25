/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package console

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/websocket"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/transport"
)

// Subresource names for the console.osac.openshift.io aggregated API.
const (
	subresourceConsole = "console"
	subresourceVNC     = "vnc"
)

// HubConfigProvider returns a *rest.Config for the given hub ID.
type HubConfigProvider func(ctx context.Context, hubID string) (*rest.Config, error)

// KubeVirtBackendBuilder builds a KubeVirtBackend.
type KubeVirtBackendBuilder struct {
	logger            *slog.Logger
	hubConfigProvider HubConfigProvider
	caPool            *x509.CertPool
}

// kubeVirtBackend connects to compute instance serial consoles on hub clusters
// via the console.osac.openshift.io aggregated API.
type kubeVirtBackend struct {
	logger            *slog.Logger
	hubConfigProvider HubConfigProvider
	caPool            *x509.CertPool
}

// NewKubeVirtBackend creates a new builder for the KubeVirt backend.
func NewKubeVirtBackend() *KubeVirtBackendBuilder {
	return &KubeVirtBackendBuilder{}
}

func (b *KubeVirtBackendBuilder) SetLogger(value *slog.Logger) *KubeVirtBackendBuilder {
	b.logger = value
	return b
}

func (b *KubeVirtBackendBuilder) SetHubConfigProvider(value HubConfigProvider) *KubeVirtBackendBuilder {
	b.hubConfigProvider = value
	return b
}

// SetCAPool sets a CA pool for TLS when dialing backend WebSocket endpoints
// using pre-computed URIs (proxy path). When using kubeconfig-based connections,
// the TLS config comes from the kubeconfig itself.
func (b *KubeVirtBackendBuilder) SetCAPool(value *x509.CertPool) *KubeVirtBackendBuilder {
	b.caPool = value
	return b
}

func (b *KubeVirtBackendBuilder) Build() (Backend, error) {
	if b.logger == nil {
		return nil, errors.New("logger is mandatory")
	}
	if b.hubConfigProvider == nil {
		return nil, errors.New("hub config provider is mandatory")
	}
	return &kubeVirtBackend{
		logger:            b.logger,
		hubConfigProvider: b.hubConfigProvider,
		caPool:            b.caPool,
	}, nil
}

// buildConsoleURL constructs the WebSocket URL and origin for a console subresource
// from a REST config.
func buildConsoleURL(config *rest.Config, namespace, crName, consoleType string) (wsURL, origin string, err error) {
	host := config.Host
	if !strings.Contains(host, "://") {
		host = "https://" + host
	}
	parsed, err := url.Parse(host)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse host %q: %w", host, err)
	}

	scheme := "wss"
	if parsed.Scheme == "http" || parsed.Scheme == "ws" {
		scheme = "ws"
	}

	var subresource string
	switch consoleType {
	case ConsoleTypeSerial:
		subresource = subresourceConsole
	case ConsoleTypeVNC:
		subresource = subresourceVNC
	default:
		return "", "", fmt.Errorf("unsupported console type %q", consoleType)
	}

	consolePath := fmt.Sprintf(
		"/apis/console.osac.openshift.io/v1alpha1/namespaces/%s/computeinstances/%s/%s",
		url.PathEscape(namespace),
		url.PathEscape(crName),
		subresource,
	)

	wsURL = fmt.Sprintf("%s://%s%s", scheme, parsed.Host, consolePath)
	originScheme := "https"
	if scheme == "ws" {
		originScheme = "http"
	}
	origin = fmt.Sprintf("%s://%s", originScheme, parsed.Host)
	return wsURL, origin, nil
}

// extractBearerToken extracts the bearer token from a rest.Config using the
// same transport wrapper chain that client-go uses internally.
func extractBearerToken(config *rest.Config) (string, error) {
	authHeaders, err := authHeadersFromConfig(config)
	if err != nil {
		return "", err
	}
	authValue := authHeaders.Get("Authorization")
	if strings.HasPrefix(authValue, "Bearer ") {
		return authValue[7:], nil
	}
	return "", nil
}

// Connect opens a WebSocket to the compute instance console subresource on the target hub.
func (b *kubeVirtBackend) Connect(ctx context.Context, target Target) (io.ReadWriteCloser, error) {
	// Pre-computed URI path: the proxy embeds the full WebSocket URL and
	// bearer token in the ticket. No kubeconfig parsing needed.
	if target.BackendURI != "" {
		return b.connectDirect(ctx, target.BackendURI, target.BackendToken)
	}

	b.logger.InfoContext(ctx, "Connecting to console",
		slog.String("hub", target.HubID),
		slog.String("namespace", target.Namespace),
		slog.String("compute_instance", target.CRName),
		slog.String("console_type", target.ConsoleType),
	)

	config, err := b.hubConfigProvider(ctx, target.HubID)
	if err != nil {
		return nil, fmt.Errorf("failed to get hub config for %q: %w", target.HubID, err)
	}

	wsURL, origin, err := buildConsoleURL(config, target.Namespace, target.CRName, target.ConsoleType)
	if err != nil {
		return nil, err
	}

	// Create WebSocket config.
	wsConfig, err := websocket.NewConfig(wsURL, origin)
	if err != nil {
		return nil, fmt.Errorf("failed to create websocket config: %w", err)
	}

	// Build TLS config from the REST config (only for wss).
	if strings.HasPrefix(wsURL, "wss://") {
		tlsConfig, err := rest.TLSConfigFor(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create TLS config: %w", err)
		}
		if tlsConfig == nil {
			tlsConfig = &tls.Config{}
		}
		wsConfig.TlsConfig = tlsConfig
	}

	// Extract authentication headers from the REST config using client-go's
	// transport layer. This handles all auth methods: BearerToken, BearerTokenFile,
	// ExecProvider, and basic auth. Client certificates are handled separately
	// by the TLS config above.
	authHeaders, err := authHeadersFromConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to build auth headers for hub %q: %w", target.HubID, err)
	}
	for key, values := range authHeaders {
		wsConfig.Header.Del(key)
		for _, value := range values {
			wsConfig.Header.Add(key, value)
		}
	}

	conn, err := websocket.DialConfig(wsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to console: %w", err)
	}

	// Set binary mode for raw console I/O.
	conn.PayloadType = websocket.BinaryFrame

	b.logger.InfoContext(ctx, "Connected to console",
		slog.String("hub", target.HubID),
		slog.String("namespace", target.Namespace),
		slog.String("compute_instance", target.CRName),
		slog.String("console_type", target.ConsoleType),
	)

	return conn, nil
}

// authHeadersFromConfig extracts authentication headers from a rest.Config by
// building the same transport wrapper chain that client-go uses internally, then
// capturing the headers it sets on a dummy request.
func authHeadersFromConfig(config *rest.Config) (http.Header, error) {
	transportConfig, err := config.TransportConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get transport config: %w", err)
	}

	capture := &headerCaptureRoundTripper{}
	rt, err := transport.HTTPWrappersForConfig(transportConfig, capture)
	if err != nil {
		return nil, fmt.Errorf("failed to build auth wrappers: %w", err)
	}

	req, err := http.NewRequest(http.MethodGet, "https://placeholder", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// RoundTrip populates the request with auth headers, then our capture
	// round-tripper saves them and returns a sentinel error.
	resp, err := rt.RoundTrip(req)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil && !errors.Is(err, errHeaderCaptureOnly) {
		return nil, fmt.Errorf("failed to apply auth wrappers: %w", err)
	}
	return capture.headers, nil
}

var errHeaderCaptureOnly = errors.New("header capture only")

// headerCaptureRoundTripper is a fake http.RoundTripper that saves the request
// headers set by transport wrappers (auth, user-agent, impersonation) without
// making a network call.
type headerCaptureRoundTripper struct {
	headers http.Header
}

func (h *headerCaptureRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	h.headers = req.Header.Clone()
	// Return an error rather than nil response — the RoundTripper contract
	// requires a valid *http.Response or an error, and a nil response would
	// panic any wrapper that tries to access it.
	return nil, errHeaderCaptureOnly
}

// connectDirect dials a pre-computed WebSocket URI with a bearer token,
// using the backend's CA pool for TLS verification.
func (b *kubeVirtBackend) connectDirect(ctx context.Context, uri, token string) (io.ReadWriteCloser, error) {
	b.logger.InfoContext(ctx, "Connecting to console (direct)",
		slog.String("uri", uri),
	)

	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("failed to parse backend URI %q: %w", uri, err)
	}

	originScheme := "https"
	if parsed.Scheme == "ws" {
		originScheme = "http"
	}
	origin := fmt.Sprintf("%s://%s", originScheme, parsed.Host)

	wsConfig, err := websocket.NewConfig(uri, origin)
	if err != nil {
		return nil, fmt.Errorf("failed to create websocket config: %w", err)
	}

	if token != "" {
		wsConfig.Header.Set("Authorization", "Bearer "+token)
	}

	if parsed.Scheme == "wss" {
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13}
		if b.caPool != nil {
			tlsConfig.RootCAs = b.caPool
		}
		wsConfig.TlsConfig = tlsConfig
	}

	conn, err := websocket.DialConfig(wsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to console: %w", err)
	}

	conn.PayloadType = websocket.BinaryFrame

	b.logger.InfoContext(ctx, "Connected to console (direct)",
		slog.String("uri", uri),
	)

	return conn, nil
}

// BuildBackendTarget extracts the WebSocket URI and bearer token from a raw
// kubeconfig. This is called at CreateSession time to pre-compute the values
// that are embedded in the encrypted ticket.
func BuildBackendTarget(kubeconfig []byte, namespace, crName, consoleType string) (uri, token string, err error) {
	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	uri, _, err = buildConsoleURL(config, namespace, crName, consoleType)
	if err != nil {
		return "", "", err
	}

	token, err = extractBearerToken(config)
	if err != nil {
		return "", "", fmt.Errorf("failed to extract bearer token: %w", err)
	}

	return uri, token, nil
}

// HubConfigProviderFromKubeconfigs returns a HubConfigProvider that builds
// REST configs from raw kubeconfig bytes retrieved by the given function.
func HubConfigProviderFromKubeconfigs(hubGetter func(ctx context.Context, id string) ([]byte, error)) HubConfigProvider {
	return func(ctx context.Context, hubID string) (*rest.Config, error) {
		kubeconfig, err := hubGetter(ctx, hubID)
		if err != nil {
			return nil, fmt.Errorf("failed to get kubeconfig for hub %q: %w", hubID, err)
		}
		config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to parse kubeconfig for hub %q: %w", hubID, err)
		}
		return config, nil
	}
}
