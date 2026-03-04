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
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"

	"golang.org/x/net/websocket"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// HubConfigProvider returns a *rest.Config for the given hub ID.
type HubConfigProvider func(ctx context.Context, hubID string) (*rest.Config, error)

// KubeVirtBackendBuilder builds a KubeVirtBackend.
type KubeVirtBackendBuilder struct {
	logger            *slog.Logger
	hubConfigProvider HubConfigProvider
}

// kubeVirtBackend connects to KubeVirt VirtualMachineInstance console subresource.
type kubeVirtBackend struct {
	logger            *slog.Logger
	hubConfigProvider HubConfigProvider
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
	}, nil
}

// Connect opens a WebSocket connection to the KubeVirt serial console subresource.
func (b *kubeVirtBackend) Connect(ctx context.Context, target Target) (io.ReadWriteCloser, error) {
	b.logger.InfoContext(ctx, "Connecting to KubeVirt console",
		slog.String("hub", target.HubID),
		slog.String("namespace", target.Namespace),
		slog.String("vm", target.VMName),
	)

	config, err := b.hubConfigProvider(ctx, target.HubID)
	if err != nil {
		return nil, fmt.Errorf("failed to get hub config for %q: %w", target.HubID, err)
	}

	// Build the WebSocket URL for the KubeVirt console subresource.
	host := config.Host
	if !strings.Contains(host, "://") {
		host = "https://" + host
	}
	parsed, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("failed to parse host %q: %w", host, err)
	}

	scheme := "wss"
	if parsed.Scheme == "http" || parsed.Scheme == "ws" {
		scheme = "ws"
	}

	consolePath := fmt.Sprintf(
		"/apis/subresources.kubevirt.io/v1/namespaces/%s/virtualmachineinstances/%s/console",
		url.PathEscape(target.Namespace),
		url.PathEscape(target.VMName),
	)

	wsURL := fmt.Sprintf("%s://%s%s", scheme, parsed.Host, consolePath)
	originScheme := "https"
	if scheme == "ws" {
		originScheme = "http"
	}
	origin := fmt.Sprintf("%s://%s", originScheme, parsed.Host)

	// Create WebSocket config.
	wsConfig, err := websocket.NewConfig(wsURL, origin)
	if err != nil {
		return nil, fmt.Errorf("failed to create websocket config: %w", err)
	}

	// Build TLS config from the REST config (only for wss).
	if scheme == "wss" {
		tlsConfig, err := rest.TLSConfigFor(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create TLS config: %w", err)
		}
		if tlsConfig == nil {
			tlsConfig = &tls.Config{}
		}
		wsConfig.TlsConfig = tlsConfig
	}

	// Add authentication headers from the REST config.
	if config.BearerToken != "" {
		wsConfig.Header.Set("Authorization", "Bearer "+config.BearerToken)
	}

	conn, err := websocket.DialConfig(wsConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to KubeVirt console: %w", err)
	}

	// Set binary mode for raw console I/O.
	conn.PayloadType = websocket.BinaryFrame

	b.logger.InfoContext(ctx, "Connected to KubeVirt console",
		slog.String("hub", target.HubID),
		slog.String("namespace", target.Namespace),
		slog.String("vm", target.VMName),
	)

	return conn, nil
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
