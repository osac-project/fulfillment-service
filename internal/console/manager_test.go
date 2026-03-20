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
	"bytes"
	"context"
	"io"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// mockBackend is a test Backend that returns a mockConnection.
type mockBackend struct {
	connectFunc func(ctx context.Context, target Target) (io.ReadWriteCloser, error)
}

func (b *mockBackend) Connect(ctx context.Context, target Target) (io.ReadWriteCloser, error) {
	return b.connectFunc(ctx, target)
}

// mockConnection is a simple in-memory ReadWriteCloser for testing.
type mockConnection struct {
	readBuf  *bytes.Buffer
	writeBuf *bytes.Buffer
	closed   bool
}

func newMockConnection() *mockConnection {
	return &mockConnection{
		readBuf:  bytes.NewBufferString("hello from vm\n"),
		writeBuf: &bytes.Buffer{},
	}
}

func (c *mockConnection) Read(p []byte) (int, error) {
	if c.closed {
		return 0, io.EOF
	}
	return c.readBuf.Read(p)
}

func (c *mockConnection) Write(p []byte) (int, error) {
	if c.closed {
		return 0, io.ErrClosedPipe
	}
	return c.writeBuf.Write(p)
}

func (c *mockConnection) Close() error {
	c.closed = true
	return nil
}

// slowMockConnection blocks on Read until context cancels, simulating a long-lived connection.
type slowMockConnection struct {
	ctx    context.Context
	closed bool
}

func newSlowMockConnection(ctx context.Context) *slowMockConnection {
	return &slowMockConnection{ctx: ctx}
}

func (c *slowMockConnection) Read(p []byte) (int, error) {
	<-c.ctx.Done()
	return 0, c.ctx.Err()
}

func (c *slowMockConnection) Write(p []byte) (int, error) {
	if c.closed {
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}

func (c *slowMockConnection) Close() error {
	c.closed = true
	return nil
}

// contextAwareBackend creates connections that respect context cancellation.
type contextAwareBackend struct{}

func (b *contextAwareBackend) Connect(ctx context.Context, target Target) (io.ReadWriteCloser, error) {
	return newSlowMockConnection(ctx), nil
}

var _ = Describe("Manager", func() {
	var (
		backend *mockBackend
		mgr     *Manager
	)

	BeforeEach(func() {
		backend = &mockBackend{
			connectFunc: func(ctx context.Context, target Target) (io.ReadWriteCloser, error) {
				return newMockConnection(), nil
			},
		}
		var err error
		mgr, err = NewManager().
			SetLogger(logger).
			SetSessionTimeout(5*time.Second).
			AddBackend("compute_instance", backend).
			Build()
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("Build", func() {
		It("should fail without logger", func() {
			_, err := NewManager().
				AddBackend("test", backend).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("logger"))
		})

		It("should fail without backends", func() {
			_, err := NewManager().
				SetLogger(logger).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("backend"))
		})
	})

	Describe("Connect", func() {
		It("should establish a connection", func() {
			ctx := context.Background()
			target := Target{
				ResourceType: "compute_instance",
				ResourceID:   "test-123",
				HubID:        "hub-1",
				Namespace:    "ns",
				VMName:       "vm-1",
			}

			conn, err := mgr.Connect(ctx, target, "testuser")
			Expect(err).NotTo(HaveOccurred())
			Expect(conn).NotTo(BeNil())
			Expect(mgr.ActiveSessions()).To(Equal(1))

			err = conn.Close()
			Expect(err).NotTo(HaveOccurred())
			Expect(mgr.ActiveSessions()).To(Equal(0))
		})

		It("should reject unsupported resource type", func() {
			ctx := context.Background()
			target := Target{ResourceType: "unknown"}
			_, err := mgr.Connect(ctx, target, "testuser")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported"))
		})

		It("should reject concurrent sessions on same resource", func() {
			ctx := context.Background()
			target := Target{
				ResourceType: "compute_instance",
				ResourceID:   "test-123",
			}

			conn1, err := mgr.Connect(ctx, target, "user1")
			Expect(err).NotTo(HaveOccurred())

			_, err = mgr.Connect(ctx, target, "user2")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("already has an active console session"))

			conn1.Close()

			// Should succeed after the first session is closed.
			conn2, err := mgr.Connect(ctx, target, "user2")
			Expect(err).NotTo(HaveOccurred())
			conn2.Close()
		})

		It("should allow sessions to different resources", func() {
			ctx := context.Background()
			target1 := Target{ResourceType: "compute_instance", ResourceID: "vm-1"}
			target2 := Target{ResourceType: "compute_instance", ResourceID: "vm-2"}

			conn1, err := mgr.Connect(ctx, target1, "user1")
			Expect(err).NotTo(HaveOccurred())

			conn2, err := mgr.Connect(ctx, target2, "user1")
			Expect(err).NotTo(HaveOccurred())

			Expect(mgr.ActiveSessions()).To(Equal(2))

			conn1.Close()
			conn2.Close()
			Expect(mgr.ActiveSessions()).To(Equal(0))
		})

		It("should handle double close gracefully", func() {
			ctx := context.Background()
			target := Target{ResourceType: "compute_instance", ResourceID: "vm-1"}
			conn, err := mgr.Connect(ctx, target, "user1")
			Expect(err).NotTo(HaveOccurred())

			err = conn.Close()
			Expect(err).NotTo(HaveOccurred())

			err = conn.Close()
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("Session Timeout", func() {
		It("should cancel the session context after timeout", func() {
			timeoutMgr, err := NewManager().
				SetLogger(logger).
				SetSessionTimeout(100*time.Millisecond).
				AddBackend("compute_instance", &contextAwareBackend{}).
				Build()
			Expect(err).NotTo(HaveOccurred())

			ctx := context.Background()
			target := Target{
				ResourceType: "compute_instance",
				ResourceID:   "timeout-test",
			}

			conn, err := timeoutMgr.Connect(ctx, target, "testuser")
			Expect(err).NotTo(HaveOccurred())
			Expect(timeoutMgr.ActiveSessions()).To(Equal(1))

			// Read should block until timeout fires, then return context.DeadlineExceeded.
			buf := make([]byte, 64)
			_, readErr := conn.Read(buf)
			Expect(readErr).To(HaveOccurred())
			Expect(readErr.Error()).To(ContainSubstring("deadline exceeded"))

			conn.Close()
			Expect(timeoutMgr.ActiveSessions()).To(Equal(0))
		})

		It("should allow new session after timeout-expired session is closed", func() {
			timeoutMgr, err := NewManager().
				SetLogger(logger).
				SetSessionTimeout(50*time.Millisecond).
				AddBackend("compute_instance", &contextAwareBackend{}).
				Build()
			Expect(err).NotTo(HaveOccurred())

			ctx := context.Background()
			target := Target{
				ResourceType: "compute_instance",
				ResourceID:   "timeout-reuse",
			}

			conn1, err := timeoutMgr.Connect(ctx, target, "user1")
			Expect(err).NotTo(HaveOccurred())

			// Wait for timeout.
			time.Sleep(100 * time.Millisecond)
			conn1.Close()

			// Should be able to open a new session.
			conn2, err := timeoutMgr.Connect(ctx, target, "user2")
			Expect(err).NotTo(HaveOccurred())
			conn2.Close()
		})
	})

	Describe("CancelSessions", func() {
		It("should cancel all active sessions", func() {
			ctx := context.Background()
			target1 := Target{ResourceType: "compute_instance", ResourceID: "vm-1"}
			target2 := Target{ResourceType: "compute_instance", ResourceID: "vm-2"}

			conn1, err := mgr.Connect(ctx, target1, "user1")
			Expect(err).NotTo(HaveOccurred())
			conn2, err := mgr.Connect(ctx, target2, "user2")
			Expect(err).NotTo(HaveOccurred())

			Expect(mgr.ActiveSessions()).To(Equal(2))

			mgr.CancelSessions()

			conn1.Close()
			conn2.Close()
		})
	})
})
