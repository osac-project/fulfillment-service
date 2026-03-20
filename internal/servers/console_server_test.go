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
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	authpkg "github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/console"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/gvks"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/labels"
)

// mockCIServer implements just the Get method of privatev1.ComputeInstancesServer.
type mockCIServer struct {
	privatev1.UnimplementedComputeInstancesServer
	getResponse *privatev1.ComputeInstancesGetResponse
	getError    error
}

func (m *mockCIServer) Get(ctx context.Context, req *privatev1.ComputeInstancesGetRequest) (*privatev1.ComputeInstancesGetResponse, error) {
	return m.getResponse, m.getError
}

// mockHubServer implements just the Get method of privatev1.HubsServer.
type mockHubServer struct {
	privatev1.UnimplementedHubsServer
	getResponse *privatev1.HubsGetResponse
	getError    error
}

func (m *mockHubServer) Get(ctx context.Context, req *privatev1.HubsGetRequest) (*privatev1.HubsGetResponse, error) {
	return m.getResponse, m.getError
}

// mockBackendForServer is a test backend that returns a mockConn.
type mockBackendForServer struct {
	conn    io.ReadWriteCloser
	connErr error
}

func (b *mockBackendForServer) Connect(ctx context.Context, target console.Target) (io.ReadWriteCloser, error) {
	return b.conn, b.connErr
}

type mockConn struct {
	readBuf  *bytes.Buffer
	writeBuf *bytes.Buffer
	closeCh  chan struct{}
	closed   bool
}

func newMockConn(readData string) *mockConn {
	return &mockConn{
		readBuf:  bytes.NewBufferString(readData),
		writeBuf: &bytes.Buffer{},
		closeCh:  make(chan struct{}),
	}
}

func (c *mockConn) Read(p []byte) (int, error) {
	if c.closed {
		return 0, io.EOF
	}
	n, err := c.readBuf.Read(p)
	if err == io.EOF {
		// Block until Close is called instead of returning EOF immediately.
		// This prevents the backend-read goroutine from exiting before the
		// client-write goroutine has processed all input.
		<-c.closeCh
		return 0, io.EOF
	}
	return n, err
}

func (c *mockConn) Write(p []byte) (int, error) {
	return c.writeBuf.Write(p)
}

func (c *mockConn) Close() error {
	if !c.closed {
		c.closed = true
		close(c.closeCh)
	}
	return nil
}

// mockTxManager provides a no-op transaction manager for testing.
type mockTxManager struct{}

func (m *mockTxManager) Begin(ctx context.Context) (database.Tx, error) {
	return &mockTx{}, nil
}

func (m *mockTxManager) End(ctx context.Context, tx database.Tx) error {
	return nil
}

// mockTx is a no-op transaction for testing.
type mockTx struct{}

func (m *mockTx) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	return nil, nil
}

func (m *mockTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	return nil
}

func (m *mockTx) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (m *mockTx) ReportError(err *error) {}

// newFakeHubClientFactory returns a HubClientFactory that ignores the kubeconfig
// and always returns the provided fake client.
func newFakeHubClientFactory(client clnt.Client) HubClientFactory {
	return func(kubeconfig []byte) (clnt.Client, error) {
		return client, nil
	}
}

// newComputeInstanceCR creates an unstructured ComputeInstance CR for testing.
func newComputeInstanceCR(id, namespace, vmNamespace, vmName string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvks.ComputeInstance)
	obj.SetName("ci-" + id)
	obj.SetNamespace(namespace)
	obj.SetLabels(map[string]string{
		labels.ComputeInstanceUuid: id,
	})
	if vmNamespace != "" || vmName != "" {
		_ = unstructured.SetNestedMap(obj.Object, map[string]interface{}{
			"namespace":                  vmNamespace,
			"kubeVirtVirtualMachineName": vmName,
		}, "status", "virtualMachineReference")
	}
	return obj
}

// newFakeClient creates a fake K8s client with the given objects.
func newFakeClient(objects ...clnt.Object) clnt.Client {
	scheme := runtime.NewScheme()
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()
}

var _ = Describe("Console Server", func() {
	var (
		ciServer  *mockCIServer
		hubServer *mockHubServer
	)

	BeforeEach(func() {
		ciServer = &mockCIServer{}
		hubServer = &mockHubServer{}
	})

	// setupHubMock configures the hub server mock and creates a fake K8s client
	// with a ComputeInstance CR that has a VM reference.
	setupHubMock := func(instanceID, hubNamespace, vmNamespace, vmName string) clnt.Client {
		hubServer.getResponse = privatev1.HubsGetResponse_builder{
			Object: privatev1.Hub_builder{
				Id:         "hub-1",
				Kubeconfig: []byte("fake-kubeconfig"),
				Namespace:  hubNamespace,
			}.Build(),
		}.Build()
		cr := newComputeInstanceCR(instanceID, hubNamespace, vmNamespace, vmName)
		return newFakeClient(cr)
	}

	Describe("Build", func() {
		It("should fail without logger", func() {
			_, err := NewConsoleServer().
				SetManager(&console.Manager{}).
				SetComputeInstancesServer(ciServer).
				SetHubServer(hubServer).
				SetTxManager(&mockTxManager{}).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("logger"))
		})

		It("should fail without manager", func() {
			_, err := NewConsoleServer().
				SetLogger(logger).
				SetComputeInstancesServer(ciServer).
				SetHubServer(hubServer).
				SetTxManager(&mockTxManager{}).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("manager"))
		})

		It("should fail without compute instances server", func() {
			mgr, err := console.NewManager().
				SetLogger(logger).
				AddBackend("compute_instance", &mockBackendForServer{}).
				Build()
			Expect(err).NotTo(HaveOccurred())

			_, err = NewConsoleServer().
				SetLogger(logger).
				SetManager(mgr).
				SetHubServer(hubServer).
				SetTxManager(&mockTxManager{}).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("compute instances"))
		})

		It("should fail without hubs server", func() {
			mgr, err := console.NewManager().
				SetLogger(logger).
				AddBackend("compute_instance", &mockBackendForServer{}).
				Build()
			Expect(err).NotTo(HaveOccurred())

			_, err = NewConsoleServer().
				SetLogger(logger).
				SetManager(mgr).
				SetComputeInstancesServer(ciServer).
				SetTxManager(&mockTxManager{}).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("hubs server"))
		})

		It("should build successfully with all dependencies", func() {
			mgr, err := console.NewManager().
				SetLogger(logger).
				AddBackend("compute_instance", &mockBackendForServer{}).
				Build()
			Expect(err).NotTo(HaveOccurred())

			server, err := NewConsoleServer().
				SetLogger(logger).
				SetManager(mgr).
				SetComputeInstancesServer(ciServer).
				SetHubServer(hubServer).
				SetTxManager(&mockTxManager{}).
				Build()
			Expect(err).NotTo(HaveOccurred())
			Expect(server).NotTo(BeNil())
		})
	})

	Describe("GetAccess", func() {
		var (
			server  publicv1.ConsoleServer
			fakeK8s clnt.Client
		)

		buildServer := func() {
			backend := &mockBackendForServer{conn: newMockConn("")}
			mgr, err := console.NewManager().
				SetLogger(logger).
				AddBackend("compute_instance", backend).
				Build()
			Expect(err).NotTo(HaveOccurred())

			server, err = NewConsoleServer().
				SetLogger(logger).
				SetManager(mgr).
				SetComputeInstancesServer(ciServer).
				SetHubServer(hubServer).
				SetHubClientFactory(newFakeHubClientFactory(fakeK8s)).
				SetTxManager(&mockTxManager{}).
				Build()
			Expect(err).NotTo(HaveOccurred())
		}

		It("should return available when compute instance is running with VM reference on hub", func() {
			ciServer.getResponse = privatev1.ComputeInstancesGetResponse_builder{
				Object: privatev1.ComputeInstance_builder{
					Id: "ci-123",
					Status: privatev1.ComputeInstanceStatus_builder{
						State: privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING,
						Hub:   "hub-1",
					}.Build(),
				}.Build(),
			}.Build()

			fakeK8s = setupHubMock("ci-123", "test-ns", "vm-ns", "test-vm")
			buildServer()

			ctx := authpkg.ContextWithSubject(context.Background(), &authpkg.Subject{User: "testuser"})
			resp, err := server.GetAccess(ctx, publicv1.ConsoleGetAccessRequest_builder{
				ResourceType: publicv1.ConsoleResourceType_CONSOLE_RESOURCE_TYPE_COMPUTE_INSTANCE,
				ResourceId:   "ci-123",
			}.Build())
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.GetAvailable()).To(BeTrue())
			Expect(resp.GetSupportedTypes()).To(ContainElement(publicv1.ConsoleType_CONSOLE_TYPE_SERIAL))
		})

		It("should return unavailable when compute instance is not running", func() {
			ciServer.getResponse = privatev1.ComputeInstancesGetResponse_builder{
				Object: privatev1.ComputeInstance_builder{
					Id: "ci-123",
					Status: privatev1.ComputeInstanceStatus_builder{
						State: privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_STARTING,
					}.Build(),
				}.Build(),
			}.Build()

			fakeK8s = newFakeClient()
			buildServer()

			ctx := authpkg.ContextWithSubject(context.Background(), &authpkg.Subject{User: "testuser"})
			resp, err := server.GetAccess(ctx, publicv1.ConsoleGetAccessRequest_builder{
				ResourceType: publicv1.ConsoleResourceType_CONSOLE_RESOURCE_TYPE_COMPUTE_INSTANCE,
				ResourceId:   "ci-123",
			}.Build())
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.GetAvailable()).To(BeFalse())
			Expect(resp.GetReason()).To(ContainSubstring("not running"))
		})

		It("should return unavailable when CR not found on hub", func() {
			ciServer.getResponse = privatev1.ComputeInstancesGetResponse_builder{
				Object: privatev1.ComputeInstance_builder{
					Id: "ci-123",
					Status: privatev1.ComputeInstanceStatus_builder{
						State: privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING,
						Hub:   "hub-1",
					}.Build(),
				}.Build(),
			}.Build()

			// Hub returns successfully but no CR exists on the cluster.
			hubServer.getResponse = privatev1.HubsGetResponse_builder{
				Object: privatev1.Hub_builder{
					Id:         "hub-1",
					Kubeconfig: []byte("fake-kubeconfig"),
					Namespace:  "test-ns",
				}.Build(),
			}.Build()
			fakeK8s = newFakeClient()
			buildServer()

			ctx := authpkg.ContextWithSubject(context.Background(), &authpkg.Subject{User: "testuser"})
			resp, err := server.GetAccess(ctx, publicv1.ConsoleGetAccessRequest_builder{
				ResourceType: publicv1.ConsoleResourceType_CONSOLE_RESOURCE_TYPE_COMPUTE_INSTANCE,
				ResourceId:   "ci-123",
			}.Build())
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.GetAvailable()).To(BeFalse())
			Expect(resp.GetReason()).To(ContainSubstring("not found on hub"))
		})

		It("should return unavailable when CR has no VM reference on hub", func() {
			ciServer.getResponse = privatev1.ComputeInstancesGetResponse_builder{
				Object: privatev1.ComputeInstance_builder{
					Id: "ci-123",
					Status: privatev1.ComputeInstanceStatus_builder{
						State: privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING,
						Hub:   "hub-1",
					}.Build(),
				}.Build(),
			}.Build()

			// CR exists but without virtualMachineReference.
			fakeK8s = setupHubMock("ci-123", "test-ns", "", "")
			buildServer()

			ctx := authpkg.ContextWithSubject(context.Background(), &authpkg.Subject{User: "testuser"})
			resp, err := server.GetAccess(ctx, publicv1.ConsoleGetAccessRequest_builder{
				ResourceType: publicv1.ConsoleResourceType_CONSOLE_RESOURCE_TYPE_COMPUTE_INSTANCE,
				ResourceId:   "ci-123",
			}.Build())
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.GetAvailable()).To(BeFalse())
			Expect(resp.GetReason()).To(ContainSubstring("no VM reference on hub"))
		})

		It("should return unavailable when compute instance not found", func() {
			ciServer.getError = status.Error(codes.NotFound, "not found")

			fakeK8s = newFakeClient()
			buildServer()

			ctx := authpkg.ContextWithSubject(context.Background(), &authpkg.Subject{User: "testuser"})
			resp, err := server.GetAccess(ctx, publicv1.ConsoleGetAccessRequest_builder{
				ResourceType: publicv1.ConsoleResourceType_CONSOLE_RESOURCE_TYPE_COMPUTE_INSTANCE,
				ResourceId:   "ci-missing",
			}.Build())
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.GetAvailable()).To(BeFalse())
			Expect(resp.GetReason()).To(ContainSubstring("not found"))
		})

		It("should return unavailable for unsupported resource type", func() {
			fakeK8s = newFakeClient()
			buildServer()

			ctx := authpkg.ContextWithSubject(context.Background(), &authpkg.Subject{User: "testuser"})
			resp, err := server.GetAccess(ctx, publicv1.ConsoleGetAccessRequest_builder{
				ResourceType: publicv1.ConsoleResourceType_CONSOLE_RESOURCE_TYPE_HOST,
				ResourceId:   "host-1",
			}.Build())
			Expect(err).NotTo(HaveOccurred())
			Expect(resp.GetAvailable()).To(BeFalse())
			Expect(resp.GetReason()).To(ContainSubstring("unsupported"))
		})
	})

	Describe("Connect", func() {
		var (
			server  publicv1.ConsoleServer
			backend *mockBackendForServer
			fakeK8s clnt.Client
		)

		BeforeEach(func() {
			backend = &mockBackendForServer{}
		})

		buildServer := func() {
			mgr, err := console.NewManager().
				SetLogger(logger).
				AddBackend("compute_instance", backend).
				Build()
			Expect(err).NotTo(HaveOccurred())

			server, err = NewConsoleServer().
				SetLogger(logger).
				SetManager(mgr).
				SetComputeInstancesServer(ciServer).
				SetHubServer(hubServer).
				SetHubClientFactory(newFakeHubClientFactory(fakeK8s)).
				SetTxManager(&mockTxManager{}).
				Build()
			Expect(err).NotTo(HaveOccurred())
		}

		It("should reject when first message is not init", func() {
			backend.conn = newMockConn("")
			fakeK8s = newFakeClient()
			buildServer()

			stream := newMockStream(authpkg.ContextWithSubject(context.Background(), &authpkg.Subject{User: "testuser"}))
			// Send input without init first.
			stream.addRecv(publicv1.ConsoleConnectRequest_builder{
				Input: publicv1.ConsoleInput_builder{
					Data: []byte("hello"),
				}.Build(),
			}.Build())

			err := server.Connect(stream)
			Expect(err).To(HaveOccurred())
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			Expect(err.Error()).To(ContainSubstring("ConsoleConnectInit"))
		})

		It("should reject when compute instance is not running", func() {
			backend.conn = newMockConn("")
			fakeK8s = newFakeClient()
			buildServer()

			ciServer.getResponse = privatev1.ComputeInstancesGetResponse_builder{
				Object: privatev1.ComputeInstance_builder{
					Id: "ci-123",
					Status: privatev1.ComputeInstanceStatus_builder{
						State: privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_STARTING,
					}.Build(),
				}.Build(),
			}.Build()

			stream := newMockStream(authpkg.ContextWithSubject(context.Background(), &authpkg.Subject{User: "testuser"}))
			stream.addRecv(publicv1.ConsoleConnectRequest_builder{
				Init: publicv1.ConsoleConnectInit_builder{
					ResourceType: publicv1.ConsoleResourceType_CONSOLE_RESOURCE_TYPE_COMPUTE_INSTANCE,
					ResourceId:   "ci-123",
					Type:         publicv1.ConsoleType_CONSOLE_TYPE_SERIAL,
				}.Build(),
			}.Build())

			err := server.Connect(stream)
			Expect(err).To(HaveOccurred())
			Expect(status.Code(err)).To(Equal(codes.FailedPrecondition))
		})

		It("should return error for unsupported resource type", func() {
			backend.conn = newMockConn("")
			fakeK8s = newFakeClient()
			buildServer()

			stream := newMockStream(authpkg.ContextWithSubject(context.Background(), &authpkg.Subject{User: "testuser"}))
			stream.addRecv(publicv1.ConsoleConnectRequest_builder{
				Init: publicv1.ConsoleConnectInit_builder{
					ResourceType: publicv1.ConsoleResourceType_CONSOLE_RESOURCE_TYPE_HOST,
					ResourceId:   "host-1",
					Type:         publicv1.ConsoleType_CONSOLE_TYPE_SERIAL,
				}.Build(),
			}.Build())

			err := server.Connect(stream)
			Expect(err).To(HaveOccurred())
			Expect(status.Code(err)).To(Equal(codes.Unimplemented))
		})

		It("should connect and relay data bidirectionally", func() {
			mockConnection := newMockConn("hello from vm\n")
			backend.conn = mockConnection

			ciServer.getResponse = privatev1.ComputeInstancesGetResponse_builder{
				Object: privatev1.ComputeInstance_builder{
					Id: "ci-123",
					Status: privatev1.ComputeInstanceStatus_builder{
						State: privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING,
						Hub:   "hub-1",
					}.Build(),
				}.Build(),
			}.Build()

			fakeK8s = setupHubMock("ci-123", "test-ns", "vm-ns", "test-vm")
			buildServer()

			ctx, cancel := context.WithCancel(
				authpkg.ContextWithSubject(context.Background(), &authpkg.Subject{User: "testuser"}),
			)

			stream := newMockStream(ctx)
			// Init message.
			stream.addRecv(publicv1.ConsoleConnectRequest_builder{
				Init: publicv1.ConsoleConnectInit_builder{
					ResourceType: publicv1.ConsoleResourceType_CONSOLE_RESOURCE_TYPE_COMPUTE_INSTANCE,
					ResourceId:   "ci-123",
					Type:         publicv1.ConsoleType_CONSOLE_TYPE_SERIAL,
				}.Build(),
			}.Build())
			// Input data.
			stream.addRecv(publicv1.ConsoleConnectRequest_builder{
				Input: publicv1.ConsoleInput_builder{
					Data: []byte("ls -la\n"),
				}.Build(),
			}.Build())
			// Then EOF to terminate the client side.
			stream.addRecvErr(io.EOF)

			// Run Connect in a goroutine since it blocks.
			var connectErr error
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				connectErr = server.Connect(stream)
			}()

			// Wait for Connect to finish.
			wg.Wait()
			cancel()

			// Verify we got status messages (CONNECTING, CONNECTED).
			sent := stream.getSent()
			Expect(len(sent)).To(BeNumerically(">=", 2))

			// First should be CONNECTING.
			Expect(sent[0].GetStatus()).NotTo(BeNil())
			Expect(sent[0].GetStatus().GetState()).To(Equal(
				publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_CONNECTING))

			// Second should be CONNECTED.
			Expect(sent[1].GetStatus()).NotTo(BeNil())
			Expect(sent[1].GetStatus().GetState()).To(Equal(
				publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_CONNECTED))

			// Verify the input was relayed to the backend.
			Expect(mockConnection.writeBuf.String()).To(Equal("ls -la\n"))

			// Connect should return nil (clean termination via EOF).
			Expect(connectErr).NotTo(HaveOccurred())
		})

		It("should return error when backend connection fails", func() {
			backend.connErr = fmt.Errorf("connection refused")

			ciServer.getResponse = privatev1.ComputeInstancesGetResponse_builder{
				Object: privatev1.ComputeInstance_builder{
					Id: "ci-123",
					Status: privatev1.ComputeInstanceStatus_builder{
						State: privatev1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING,
						Hub:   "hub-1",
					}.Build(),
				}.Build(),
			}.Build()

			fakeK8s = setupHubMock("ci-123", "test-ns", "vm-ns", "test-vm")
			buildServer()

			stream := newMockStream(authpkg.ContextWithSubject(context.Background(), &authpkg.Subject{User: "testuser"}))
			stream.addRecv(publicv1.ConsoleConnectRequest_builder{
				Init: publicv1.ConsoleConnectInit_builder{
					ResourceType: publicv1.ConsoleResourceType_CONSOLE_RESOURCE_TYPE_COMPUTE_INSTANCE,
					ResourceId:   "ci-123",
					Type:         publicv1.ConsoleType_CONSOLE_TYPE_SERIAL,
				}.Build(),
			}.Build())

			err := server.Connect(stream)
			Expect(err).To(HaveOccurred())
			Expect(status.Code(err)).To(Equal(codes.Internal))
			Expect(err.Error()).To(ContainSubstring("connect"))
		})
	})
})

// mockStream implements publicv1.Console_ConnectServer for testing.
type mockStream struct {
	ctx    context.Context
	recvCh chan recvItem
	sent   []*publicv1.ConsoleConnectResponse
	sentMu sync.Mutex
}

type recvItem struct {
	req *publicv1.ConsoleConnectRequest
	err error
}

func newMockStream(ctx context.Context) *mockStream {
	return &mockStream{
		ctx:    ctx,
		recvCh: make(chan recvItem, 100),
	}
}

func (s *mockStream) addRecv(req *publicv1.ConsoleConnectRequest) {
	s.recvCh <- recvItem{req: req}
}

func (s *mockStream) addRecvErr(err error) {
	s.recvCh <- recvItem{err: err}
}

func (s *mockStream) getSent() []*publicv1.ConsoleConnectResponse {
	s.sentMu.Lock()
	defer s.sentMu.Unlock()
	result := make([]*publicv1.ConsoleConnectResponse, len(s.sent))
	copy(result, s.sent)
	return result
}

func (s *mockStream) Recv() (*publicv1.ConsoleConnectRequest, error) {
	select {
	case item := <-s.recvCh:
		return item.req, item.err
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

func (s *mockStream) Send(resp *publicv1.ConsoleConnectResponse) error {
	s.sentMu.Lock()
	defer s.sentMu.Unlock()
	s.sent = append(s.sent, resp)
	return nil
}

func (s *mockStream) Context() context.Context {
	return s.ctx
}

func (s *mockStream) SetHeader(metadata.MD) error  { return nil }
func (s *mockStream) SendHeader(metadata.MD) error { return nil }
func (s *mockStream) SetTrailer(metadata.MD)       {}
func (s *mockStream) SendMsg(interface{}) error    { return nil }
func (s *mockStream) RecvMsg(interface{}) error    { return nil }
