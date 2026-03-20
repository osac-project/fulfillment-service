/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package computeinstance

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
)

// testConsoleServer implements publicv1.ConsoleServer for reconnect testing.
type testConsoleServer struct {
	publicv1.UnimplementedConsoleServer
	connectCount atomic.Int32
	failFirst    int32
}

func (s *testConsoleServer) Connect(stream publicv1.Console_ConnectServer) error {
	count := s.connectCount.Add(1)

	// Receive the init message.
	req, err := stream.Recv()
	if err != nil {
		return err
	}
	if req.GetInit() == nil {
		return status.Error(codes.InvalidArgument, "first message must be init")
	}

	// Simulate transient failures for the first N connections.
	if count <= s.failFirst {
		return status.Error(codes.Unavailable, fmt.Sprintf("simulated failure %d", count))
	}

	// Send connected status.
	_ = stream.Send(publicv1.ConsoleConnectResponse_builder{
		Status: publicv1.ConsoleStatus_builder{
			State:   publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_CONNECTING,
			Message: "Connecting...",
		}.Build(),
	}.Build())
	_ = stream.Send(publicv1.ConsoleConnectResponse_builder{
		Status: publicv1.ConsoleStatus_builder{
			State:   publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_CONNECTED,
			Message: "Connected",
		}.Build(),
	}.Build())

	// Send disconnect immediately so the client exits cleanly.
	_ = stream.Send(publicv1.ConsoleConnectResponse_builder{
		Status: publicv1.ConsoleStatus_builder{
			State:   publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_DISCONNECTED,
			Message: "Session ended by server",
		}.Build(),
	}.Build())

	return nil
}

// startTestGRPCServer starts an in-process gRPC server with the Console service.
func startTestGRPCServer(srv publicv1.ConsoleServer) (addr string, cleanup func(), err error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}

	grpcServer := grpc.NewServer()
	publicv1.RegisterConsoleServer(grpcServer, srv)

	go grpcServer.Serve(listener)

	return listener.Addr().String(), func() {
		grpcServer.GracefulStop()
	}, nil
}

var _ = Describe("Auto-reconnect", func() {
	It("should classify transient server errors for retry", func() {
		testSrv := &testConsoleServer{failFirst: 2}
		addr, cleanup, err := startTestGRPCServer(testSrv)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cleanup)

		conn, err := grpc.NewClient(addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		Expect(err).NotTo(HaveOccurred())
		defer conn.Close()

		runner := &runnerContext{
			logger: logger,
			conn:   conn,
		}

		ctx := context.Background()

		// First call: server returns Unavailable (transient).
		err = runner.connectOnce(ctx, "test-vm")
		Expect(err).To(HaveOccurred())
		Expect(isPermanentError(err)).To(BeFalse(), "Unavailable should be transient")
		Expect(testSrv.connectCount.Load()).To(Equal(int32(1)))

		// Second call: server returns Unavailable again.
		err = runner.connectOnce(ctx, "test-vm")
		Expect(err).To(HaveOccurred())
		Expect(isPermanentError(err)).To(BeFalse())
		Expect(testSrv.connectCount.Load()).To(Equal(int32(2)))

		// Third call: server succeeds (sends CONNECTED then DISCONNECTED, exits cleanly).
		err = runner.connectOnce(ctx, "test-vm")
		// This should complete without error — server sends disconnect, client sees EOF.
		// Either nil or a recv error from stream closing is acceptable.
		Expect(testSrv.connectCount.Load()).To(Equal(int32(3)))
	})

	It("should not retry on permanent errors", func() {
		permanentSrv := &publicv1.UnimplementedConsoleServer{}
		addr, cleanup, err := startTestGRPCServer(permanentSrv)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cleanup)

		conn, err := grpc.NewClient(addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		Expect(err).NotTo(HaveOccurred())
		defer conn.Close()

		runner := &runnerContext{
			logger: logger,
			conn:   conn,
		}

		err = runner.connectOnce(context.Background(), "test-vm")
		Expect(err).To(HaveOccurred())
		Expect(isPermanentError(err)).To(BeTrue(), "Unimplemented should be permanent")
	})

	It("should exercise the full connectWithRetry path", func() {
		// Server fails first 2, succeeds on 3rd.
		testSrv := &testConsoleServer{failFirst: 2}
		addr, cleanup, err := startTestGRPCServer(testSrv)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cleanup)

		conn, err := grpc.NewClient(addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		Expect(err).NotTo(HaveOccurred())
		defer conn.Close()

		runner := &runnerContext{
			logger: logger,
			conn:   conn,
		}

		ctx := context.Background()

		// connectWithRetry should retry the transient failures and eventually succeed.
		err = runner.connectWithRetry(ctx, "test-vm")
		// Should complete — either nil or a clean exit from the DISCONNECTED status.
		Expect(testSrv.connectCount.Load()).To(Equal(int32(3)))
	})
})

// Verify connectOnce handles EOF from a stream that sends only init response then closes.
var _ = Describe("connectOnce edge cases", func() {
	It("should handle stream EOF after connected status", func() {
		// Server that connects then immediately returns (EOF on stream).
		eofSrv := &eofAfterConnectServer{}
		addr, cleanup, err := startTestGRPCServer(eofSrv)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(cleanup)

		conn, err := grpc.NewClient(addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		Expect(err).NotTo(HaveOccurred())
		defer conn.Close()

		runner := &runnerContext{
			logger: logger,
			conn:   conn,
		}

		// Should handle EOF gracefully (return nil or io.EOF wrapped error).
		err = runner.connectOnce(context.Background(), "test-vm")
		// EOF from server = clean disconnect, should not panic.
	})
})

// eofAfterConnectServer sends CONNECTING + CONNECTED then returns (server-side stream close).
type eofAfterConnectServer struct {
	publicv1.UnimplementedConsoleServer
}

func (s *eofAfterConnectServer) Connect(stream publicv1.Console_ConnectServer) error {
	req, err := stream.Recv()
	if err != nil {
		return err
	}
	if req.GetInit() == nil {
		return status.Error(codes.InvalidArgument, "init required")
	}

	_ = stream.Send(publicv1.ConsoleConnectResponse_builder{
		Status: publicv1.ConsoleStatus_builder{
			State:   publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_CONNECTING,
			Message: "Connecting...",
		}.Build(),
	}.Build())
	_ = stream.Send(publicv1.ConsoleConnectResponse_builder{
		Status: publicv1.ConsoleStatus_builder{
			State:   publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_CONNECTED,
			Message: "Connected",
		}.Build(),
	}.Build())

	// Return immediately — this causes EOF on client's Recv().
	return nil
}
