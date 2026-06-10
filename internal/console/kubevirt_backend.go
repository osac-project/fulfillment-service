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
	"net/url"

	"golang.org/x/net/websocket"
)

// KubeVirtBackendBuilder builds a KubeVirtBackend.
type KubeVirtBackendBuilder struct {
	logger *slog.Logger
	caPool *x509.CertPool
}

// kubeVirtBackend connects to compute instance consoles via pre-computed
// WebSocket URIs embedded in encrypted console tickets.
type kubeVirtBackend struct {
	logger *slog.Logger
	caPool *x509.CertPool
}

// NewKubeVirtBackend creates a new builder for the KubeVirt backend.
func NewKubeVirtBackend() *KubeVirtBackendBuilder {
	return &KubeVirtBackendBuilder{}
}

func (b *KubeVirtBackendBuilder) SetLogger(value *slog.Logger) *KubeVirtBackendBuilder {
	b.logger = value
	return b
}

// SetCAPool sets a CA pool for TLS when dialing backend WebSocket endpoints.
func (b *KubeVirtBackendBuilder) SetCAPool(value *x509.CertPool) *KubeVirtBackendBuilder {
	b.caPool = value
	return b
}

func (b *KubeVirtBackendBuilder) Build() (Backend, error) {
	if b.logger == nil {
		return nil, errors.New("logger is mandatory")
	}
	return &kubeVirtBackend{
		logger: b.logger,
		caPool: b.caPool,
	}, nil
}

// Connect dials the pre-computed WebSocket URI from the target, using the
// backend's CA pool for TLS verification.
func (b *kubeVirtBackend) Connect(ctx context.Context, target Target) (io.ReadWriteCloser, error) {
	b.logger.InfoContext(ctx, "Connecting to console",
		slog.String("uri", target.BackendURI),
	)

	parsed, err := url.Parse(target.BackendURI)
	if err != nil {
		return nil, fmt.Errorf("failed to parse backend URI %q: %w", target.BackendURI, err)
	}

	originScheme := "https"
	if parsed.Scheme == "ws" {
		originScheme = "http"
	}
	origin := fmt.Sprintf("%s://%s", originScheme, parsed.Host)

	wsConfig, err := websocket.NewConfig(target.BackendURI, origin)
	if err != nil {
		return nil, fmt.Errorf("failed to create websocket config: %w", err)
	}

	if target.BackendToken != "" {
		wsConfig.Header.Set("Authorization", "Bearer "+target.BackendToken)
	}

	if parsed.Scheme == "wss" {
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13}
		if b.caPool != nil {
			tlsConfig.RootCAs = b.caPool
		}
		wsConfig.TlsConfig = tlsConfig
	}

	conn, err := wsConfig.DialContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to console: %w", err)
	}

	conn.PayloadType = websocket.BinaryFrame

	b.logger.InfoContext(ctx, "Connected to console",
		slog.String("uri", target.BackendURI),
	)

	return conn, nil
}
