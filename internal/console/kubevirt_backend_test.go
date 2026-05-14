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
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/rest"
)

var _ = Describe("KubeVirt Backend", func() {

	Describe("Build", func() {
		It("should fail without logger", func() {
			_, err := NewKubeVirtBackend().
				SetHubConfigProvider(func(ctx context.Context, hubID string) (*rest.Config, error) {
					return nil, nil
				}).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("logger"))
		})

		It("should fail without hub config provider", func() {
			_, err := NewKubeVirtBackend().
				SetLogger(logger).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("hub config provider"))
		})

		It("should build successfully with all dependencies", func() {
			backend, err := NewKubeVirtBackend().
				SetLogger(logger).
				SetHubConfigProvider(func(ctx context.Context, hubID string) (*rest.Config, error) {
					return &rest.Config{Host: "https://localhost:6443"}, nil
				}).
				Build()
			Expect(err).NotTo(HaveOccurred())
			Expect(backend).NotTo(BeNil())
		})
	})

	Describe("Connect", func() {
		It("should fail when hub config provider returns error", func() {
			backend, err := NewKubeVirtBackend().
				SetLogger(logger).
				SetHubConfigProvider(func(ctx context.Context, hubID string) (*rest.Config, error) {
					return nil, fmt.Errorf("hub %q not found", hubID)
				}).
				Build()
			Expect(err).NotTo(HaveOccurred())

			_, err = backend.Connect(context.Background(), Target{
				HubID:       "missing-hub",
				Namespace:   "test-ns",
				CRName:      "test-cr",
				ConsoleType: ConsoleTypeSerial,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("missing-hub"))
			Expect(err.Error()).To(ContainSubstring("hub config"))
		})

		It("should fail when connecting to unreachable host", func() {
			backend, err := NewKubeVirtBackend().
				SetLogger(logger).
				SetHubConfigProvider(func(ctx context.Context, hubID string) (*rest.Config, error) {
					return &rest.Config{
						Host: "https://localhost:1",
						TLSClientConfig: rest.TLSClientConfig{
							Insecure: true,
						},
					}, nil
				}).
				Build()
			Expect(err).NotTo(HaveOccurred())

			_, err = backend.Connect(context.Background(), Target{
				HubID:       "hub-1",
				Namespace:   "test-ns",
				CRName:      "test-cr",
				ConsoleType: ConsoleTypeSerial,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to connect"))
		})
	})

	Describe("authHeadersFromConfig", func() {
		It("should return Authorization header for BearerToken", func() {
			config := &rest.Config{
				Host:        "https://localhost:6443",
				BearerToken: "my-test-token",
			}
			headers, err := authHeadersFromConfig(config)
			Expect(err).NotTo(HaveOccurred())
			Expect(headers.Get("Authorization")).To(Equal("Bearer my-test-token"))
		})

		It("should return Authorization header for BearerTokenFile", func() {
			tokenDir := GinkgoT().TempDir()
			tokenFile := filepath.Join(tokenDir, "token")
			err := os.WriteFile(tokenFile, []byte("file-based-token"), 0600)
			Expect(err).NotTo(HaveOccurred())

			config := &rest.Config{
				Host:            "https://localhost:6443",
				BearerTokenFile: tokenFile,
			}
			headers, err := authHeadersFromConfig(config)
			Expect(err).NotTo(HaveOccurred())
			Expect(headers.Get("Authorization")).To(Equal("Bearer file-based-token"))
		})

		It("should read updated token from BearerTokenFile", func() {
			tokenDir := GinkgoT().TempDir()
			tokenFile := filepath.Join(tokenDir, "token")
			err := os.WriteFile(tokenFile, []byte("initial-token"), 0600)
			Expect(err).NotTo(HaveOccurred())

			config := &rest.Config{
				Host:            "https://localhost:6443",
				BearerTokenFile: tokenFile,
			}

			headers, err := authHeadersFromConfig(config)
			Expect(err).NotTo(HaveOccurred())
			Expect(headers.Get("Authorization")).To(Equal("Bearer initial-token"))

			// Update the token file and verify the new token is read.
			err = os.WriteFile(tokenFile, []byte("rotated-token"), 0600)
			Expect(err).NotTo(HaveOccurred())

			headers, err = authHeadersFromConfig(config)
			Expect(err).NotTo(HaveOccurred())
			Expect(headers.Get("Authorization")).To(Equal("Bearer rotated-token"))
		})

		It("should return no Authorization header when no auth is configured", func() {
			config := &rest.Config{
				Host: "https://localhost:6443",
			}
			headers, err := authHeadersFromConfig(config)
			Expect(err).NotTo(HaveOccurred())
			Expect(headers.Get("Authorization")).To(BeEmpty())
		})

		It("should prefer BearerTokenFile over BearerToken when both are set", func() {
			tokenDir := GinkgoT().TempDir()
			tokenFile := filepath.Join(tokenDir, "token")
			err := os.WriteFile(tokenFile, []byte("file-token"), 0600)
			Expect(err).NotTo(HaveOccurred())

			config := &rest.Config{
				Host:            "https://localhost:6443",
				BearerToken:     "inline-token",
				BearerTokenFile: tokenFile,
			}
			headers, err := authHeadersFromConfig(config)
			Expect(err).NotTo(HaveOccurred())
			// client-go treats BearerTokenFile as a refreshable source that
			// takes precedence over the static BearerToken.
			Expect(headers.Get("Authorization")).To(Equal("Bearer file-token"))
		})
	})

	Describe("HubConfigProviderFromKubeconfigs", func() {
		It("should return error when getter fails", func() {
			provider := HubConfigProviderFromKubeconfigs(
				func(ctx context.Context, id string) ([]byte, error) {
					return nil, fmt.Errorf("db error")
				},
			)

			_, err := provider(context.Background(), "hub-1")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("db error"))
		})

		It("should return error for invalid kubeconfig", func() {
			provider := HubConfigProviderFromKubeconfigs(
				func(ctx context.Context, id string) ([]byte, error) {
					return []byte("not-a-kubeconfig"), nil
				},
			)

			_, err := provider(context.Background(), "hub-1")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("parse kubeconfig"))
		})

		It("should return config for valid kubeconfig", func() {
			kubeconfig := []byte(`
apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://my-hub.example.com:6443
    insecure-skip-tls-verify: true
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
users:
- name: test-user
  user:
    token: my-token
`)
			provider := HubConfigProviderFromKubeconfigs(
				func(ctx context.Context, id string) ([]byte, error) {
					return kubeconfig, nil
				},
			)

			config, err := provider(context.Background(), "hub-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(config).NotTo(BeNil())
			Expect(config.Host).To(Equal("https://my-hub.example.com:6443"))
			Expect(config.BearerToken).To(Equal("my-token"))
		})
	})
})
