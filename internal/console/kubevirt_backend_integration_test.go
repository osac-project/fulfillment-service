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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/net/websocket"
	"k8s.io/client-go/rest"
)

var _ = Describe("KubeVirt Backend Integration", func() {
	var (
		wsServer *mockWSServer
	)

	BeforeEach(func() {
		var err error
		wsServer, err = newMockWSServer()
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(wsServer.Close)
	})

	It("should connect to mock KubeVirt console and exchange data", func() {
		backend, err := NewKubeVirtBackend().
			SetLogger(logger).
			SetHubConfigProvider(func(ctx context.Context, hubID string) (*rest.Config, error) {
				return &rest.Config{
					Host: "ws://" + wsServer.Addr(),
					TLSClientConfig: rest.TLSClientConfig{
						Insecure: true,
					},
				}, nil
			}).
			Build()
		Expect(err).NotTo(HaveOccurred())

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		conn, err := backend.Connect(ctx, Target{
			HubID:     "hub-1",
			Namespace: "test-ns",
			VMName:    "test-vm",
		})
		Expect(err).NotTo(HaveOccurred())
		defer conn.Close()

		// Read the banner.
		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(buf[:n])).To(Equal("Welcome to mock console\r\n"))

		// Send data.
		_, err = conn.Write([]byte("hello"))
		Expect(err).NotTo(HaveOccurred())

		// Read the echo.
		n, err = conn.Read(buf)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(buf[:n])).To(Equal("echo: hello"))
	})

	It("should connect through the Manager and exchange data", func() {
		backend, err := NewKubeVirtBackend().
			SetLogger(logger).
			SetHubConfigProvider(func(ctx context.Context, hubID string) (*rest.Config, error) {
				return &rest.Config{
					Host: "ws://" + wsServer.Addr(),
					TLSClientConfig: rest.TLSClientConfig{
						Insecure: true,
					},
				}, nil
			}).
			Build()
		Expect(err).NotTo(HaveOccurred())

		mgr, err := NewManager().
			SetLogger(logger).
			AddBackend("compute_instance", backend).
			Build()
		Expect(err).NotTo(HaveOccurred())

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		conn, err := mgr.Connect(ctx, Target{
			ResourceType: "compute_instance",
			ResourceID:   "ci-123",
			HubID:        "hub-1",
			Namespace:    "test-ns",
			VMName:       "test-vm",
		}, "testuser")
		Expect(err).NotTo(HaveOccurred())
		Expect(mgr.ActiveSessions()).To(Equal(1))
		defer conn.Close()

		// Read banner.
		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(buf[:n])).To(ContainSubstring("Welcome"))

		// Send and receive.
		_, err = conn.Write([]byte("ls\n"))
		Expect(err).NotTo(HaveOccurred())

		n, err = conn.Read(buf)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(buf[:n])).To(Equal("echo: ls\n"))

		// Close and verify session removed.
		err = conn.Close()
		Expect(err).NotTo(HaveOccurred())
		Expect(mgr.ActiveSessions()).To(Equal(0))
	})

	It("should handle server-side connection close gracefully", func() {
		// Create a mock WS server that sends banner then closes the WebSocket.
		closeServer, err := newMockWSServerWithHandler(func(ws *websocket.Conn) {
			ws.PayloadType = websocket.BinaryFrame
			ws.Write([]byte("goodbye\r\n"))
			// Close the WebSocket connection immediately after banner.
			ws.Close()
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(closeServer.Close)

		backend, err := NewKubeVirtBackend().
			SetLogger(logger).
			SetHubConfigProvider(func(ctx context.Context, hubID string) (*rest.Config, error) {
				return &rest.Config{
					Host: "ws://" + closeServer.Addr(),
					TLSClientConfig: rest.TLSClientConfig{
						Insecure: true,
					},
				}, nil
			}).
			Build()
		Expect(err).NotTo(HaveOccurred())

		ctx := context.Background()
		conn, err := backend.Connect(ctx, Target{
			HubID:     "hub-1",
			Namespace: "test-ns",
			VMName:    "test-vm",
		})
		Expect(err).NotTo(HaveOccurred())

		// Read the banner.
		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(buf[:n])).To(Equal("goodbye\r\n"))

		// Next read should return EOF since server closed the connection.
		_, err = conn.Read(buf)
		Expect(err).To(HaveOccurred())

		conn.Close()
	})

	It("should reject concurrent sessions to the same resource", func() {
		backend, err := NewKubeVirtBackend().
			SetLogger(logger).
			SetHubConfigProvider(func(ctx context.Context, hubID string) (*rest.Config, error) {
				return &rest.Config{
					Host: "ws://" + wsServer.Addr(),
					TLSClientConfig: rest.TLSClientConfig{
						Insecure: true,
					},
				}, nil
			}).
			Build()
		Expect(err).NotTo(HaveOccurred())

		mgr, err := NewManager().
			SetLogger(logger).
			AddBackend("compute_instance", backend).
			Build()
		Expect(err).NotTo(HaveOccurred())

		ctx := context.Background()
		target := Target{
			ResourceType: "compute_instance",
			ResourceID:   "ci-same",
			HubID:        "hub-1",
			Namespace:    "test-ns",
			VMName:       "test-vm",
		}

		conn1, err := mgr.Connect(ctx, target, "user1")
		Expect(err).NotTo(HaveOccurred())
		defer conn1.Close()

		// Second connection to same resource should fail.
		_, err = mgr.Connect(ctx, target, "user2")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("already has an active console session"))

		// After closing first, second should succeed.
		conn1.Close()
		conn2, err := mgr.Connect(ctx, target, "user2")
		Expect(err).NotTo(HaveOccurred())
		conn2.Close()
	})
})
