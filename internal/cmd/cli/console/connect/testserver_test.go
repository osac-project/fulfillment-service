/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package connect

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"time"

	. "github.com/onsi/gomega"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
)

// recvInit reads the first message from a console stream and validates
// that it is an init message.
func recvInit(stream publicv1.Console_ConnectServer) error {
	req, err := stream.Recv()
	if err != nil {
		return err
	}
	if req.GetInit() == nil {
		return status.Error(codes.InvalidArgument, "first message must be init")
	}
	return nil
}

// sendStatus sends a console status message on the stream.
func sendStatus(stream publicv1.Console_ConnectServer, state publicv1.ConsoleConnectionState, msg string) {
	_ = stream.Send(publicv1.ConsoleConnectResponse_builder{
		Status: publicv1.ConsoleStatus_builder{
			State:   state,
			Message: msg,
		}.Build(),
	}.Build())
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

// openTestStream starts a gRPC server, connects a client, sends the init
// handshake, and waits for CONNECTED status. Returns the ready-to-use
// bidirectional stream, the request context, and a cleanup function.
func openTestStream(srv publicv1.ConsoleServer) (
	stream grpc.BidiStreamingClient[publicv1.ConsoleConnectRequest, publicv1.ConsoleConnectResponse],
	ctx context.Context,
	cancel context.CancelFunc,
	cleanup func(),
) {
	addr, grpcCleanup, err := startTestGRPCServer(srv)
	Expect(err).NotTo(HaveOccurred())

	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	Expect(err).NotTo(HaveOccurred())

	client := publicv1.NewConsoleClient(conn)
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)

	stream, err = client.Connect(ctx)
	Expect(err).NotTo(HaveOccurred())

	err = stream.Send(publicv1.ConsoleConnectRequest_builder{
		Init: publicv1.ConsoleConnectInit_builder{
			ResourceType: publicv1.ConsoleResourceType_CONSOLE_RESOURCE_TYPE_COMPUTE_INSTANCE,
			ResourceId:   "test-vm",
			Type:         publicv1.ConsoleType_CONSOLE_TYPE_VNC,
			ClientId:     "test-client",
		}.Build(),
	}.Build())
	Expect(err).NotTo(HaveOccurred())

	resp, err := stream.Recv()
	Expect(err).NotTo(HaveOccurred())
	Expect(resp.GetStatus().GetState()).To(Equal(
		publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_CONNECTED))

	cleanup = func() {
		cancel()
		conn.Close()
		grpcCleanup()
	}
	return
}

// drainHandler is a StreamHandler that reads from the stream until
// it receives a DISCONNECTED status or EOF.
func drainHandler(_ context.Context, _ context.CancelFunc, stream grpc.BidiStreamingClient[publicv1.ConsoleConnectRequest, publicv1.ConsoleConnectResponse]) error {
	for {
		resp, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if st := resp.GetStatus(); st != nil {
			if st.GetState() == publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_DISCONNECTED {
				return nil
			}
		}
	}
}

// testConsoleServer implements publicv1.ConsoleServer for reconnect testing.
type testConsoleServer struct {
	publicv1.UnimplementedConsoleServer
	connectCount atomic.Int32
	failFirst    int32
}

func (s *testConsoleServer) Connect(stream publicv1.Console_ConnectServer) error {
	count := s.connectCount.Add(1)

	if err := recvInit(stream); err != nil {
		return err
	}

	// Simulate transient failures for the first N connections.
	if count <= s.failFirst {
		return status.Error(codes.Unavailable, fmt.Sprintf("simulated failure %d", count))
	}

	sendStatus(stream, publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_CONNECTING, "Connecting...")
	sendStatus(stream, publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_CONNECTED, "Connected")

	// Send disconnect immediately so the client exits cleanly.
	sendStatus(stream, publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_DISCONNECTED, "Session ended by server")

	return nil
}

// eofAfterConnectServer sends CONNECTING + CONNECTED then returns (server-side stream close).
type eofAfterConnectServer struct {
	publicv1.UnimplementedConsoleServer
}

func (s *eofAfterConnectServer) Connect(stream publicv1.Console_ConnectServer) error {
	if err := recvInit(stream); err != nil {
		return err
	}

	sendStatus(stream, publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_CONNECTING, "Connecting...")
	sendStatus(stream, publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_CONNECTED, "Connected")

	// Return immediately — this causes EOF on client's Recv().
	return nil
}

// echoConsoleServer echoes data back through the console stream.
type echoConsoleServer struct {
	publicv1.UnimplementedConsoleServer
	connectCount atomic.Int32
}

func (s *echoConsoleServer) Connect(stream publicv1.Console_ConnectServer) error {
	s.connectCount.Add(1)

	if err := recvInit(stream); err != nil {
		return err
	}

	sendStatus(stream, publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_CONNECTED, "Connected")

	// Echo loop.
	for {
		req, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if input := req.GetInput(); input != nil {
			data := input.GetData()
			if len(data) > 0 {
				err := stream.Send(publicv1.ConsoleConnectResponse_builder{
					Output: publicv1.ConsoleOutput_builder{
						Data: data,
					}.Build(),
				}.Build())
				if err != nil {
					return err
				}
			}
		}
	}
}

// disconnectAfterConnectServer sends CONNECTED then DISCONNECTED immediately.
type disconnectAfterConnectServer struct {
	publicv1.UnimplementedConsoleServer
}

func (s *disconnectAfterConnectServer) Connect(stream publicv1.Console_ConnectServer) error {
	if err := recvInit(stream); err != nil {
		return err
	}

	sendStatus(stream, publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_CONNECTED, "Connected")
	sendStatus(stream, publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_DISCONNECTED, "Session ended")

	return nil
}
