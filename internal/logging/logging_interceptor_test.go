/*
Copyright (c) 2025 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package logging

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"

	testsv1 "github.com/osac-project/fulfillment-service/internal/api/osac/tests/v1"
)

var _ = Describe("Interceptor", func() {
	var (
		ctx         context.Context
		server      *grpc.Server
		listener    net.Listener
		conn        *grpc.ClientConn
		interceptor *Interceptor
		buffer      *bytes.Buffer
		logger      *slog.Logger
	)

	BeforeEach(func() {
		var err error

		// Create the context:
		ctx = context.Background()

		// Create a buffer for the log output:
		buffer = &bytes.Buffer{}

		// Create a logger that writes to the buffer:
		logger, err = NewLogger().
			SetLevel(slog.LevelDebug.String()).
			SetOut(buffer).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Create the interceptor:
		interceptor, err = NewInterceptor().
			SetLogger(logger).
			SetHeaders(true).
			SetBodies(true).
			SetRedact(false).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Create a test server:
		listener, err = net.Listen("tcp", "127.0.0.1:0")
		Expect(err).ToNot(HaveOccurred())
		server = grpc.NewServer()
		go func() {
			defer GinkgoRecover()
			_ = server.Serve(listener)
		}()
		DeferCleanup(server.Stop)

		// Create a client connection with interceptor:
		conn, err = grpc.NewClient(
			listener.Addr().String(),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithUnaryInterceptor(interceptor.UnaryClient),
			grpc.WithStreamInterceptor(interceptor.StreamClient),
		)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(conn.Close)
	})

	Describe("Unary client", func() {
		It("Logs unary client requests and responses", func() {
			method := "/test.Service/TestMethod"

			// Mock request and response:
			request := testsv1.Object_builder{
				Id:       "my_id",
				MyString: "my_value",
			}.Build()
			response := testsv1.Object_builder{}.Build()

			// Mock invoker that copies request to response:
			invoker := func(ctx context.Context, method string, request, response any, cc *grpc.ClientConn,
				options ...grpc.CallOption) error {
				requestObject := response.(*testsv1.Object)
				responseObject := request.(*testsv1.Object)
				requestObject.Id = responseObject.Id
				requestObject.MyString = responseObject.MyString
				return nil
			}

			// Add metadata to context:
			ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer my_token"))

			// Call the interceptor:
			err := interceptor.UnaryClient(ctx, method, request, response, conn, invoker)
			Expect(err).ToNot(HaveOccurred())

			// Parse the log messages:
			messages := Parse(buffer)
			Expect(messages).To(HaveLen(2))

			// Verify request log:
			requestMessage := messages[0]
			Expect(requestMessage["msg"]).To(Equal("Sending unary request"))
			Expect(requestMessage["method"]).To(Equal(method))
			Expect(requestMessage["target"]).To(Equal(listener.Addr().String()))
			Expect(requestMessage["metadata"]).To(HaveKey("authorization"))
			Expect(requestMessage["request"]).To(HaveKey("myString"))

			// Verify response log:
			responseMessage := messages[1]
			Expect(responseMessage["msg"]).To(Equal("Received unary response"))
			Expect(responseMessage["method"]).To(Equal(method))
			Expect(responseMessage["target"]).To(Equal(listener.Addr().String()))
			Expect(responseMessage["code"]).To(Equal("OK"))
			Expect(responseMessage["response"]).To(HaveKey("myString"))
		})

		It("Logs errors in unary client calls", func() {
			method := "/test.Service/TestMethod"

			// Mock request and response:
			request := &testsv1.Object{}
			response := &testsv1.Object{}

			// Mock invoker that returns an error:
			invoker := func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
				return grpcstatus.Error(grpccodes.NotFound, "not found")
			}

			// Call the interceptor:
			err := interceptor.UnaryClient(ctx, method, request, response, conn, invoker)
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.NotFound))
			Expect(status.Message()).To(Equal("not found"))

			// Parse the log messages:
			messages := Parse(buffer)
			Expect(messages).To(HaveLen(2))

			// Verify response log contains error:
			responseMessage := messages[1]
			Expect(responseMessage["msg"]).To(Equal("Received unary response"))
			Expect(responseMessage["code"]).To(Equal("NotFound"))
			Expect(responseMessage["error"]).ToNot(BeNil())
		})

		It("Skips logging for reflection methods", func() {
			method := "/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo"

			// Mock invoker:
			invoker := func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
				return nil
			}

			// Call the interceptor:
			err := interceptor.UnaryClient(ctx, method, nil, nil, conn, invoker)
			Expect(err).ToNot(HaveOccurred())

			// Parse the log messages - should be empty:
			messages := Parse(buffer)
			Expect(messages).To(BeEmpty())
		})

		It("Skips logging for health check methods", func() {
			method := "/grpc.health.v1.Health/Check"

			// Mock invoker:
			invoker := func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
				return nil
			}

			// Call the interceptor:
			err := interceptor.UnaryClient(ctx, method, nil, nil, conn, invoker)
			Expect(err).ToNot(HaveOccurred())

			// Parse the log messages - should be empty:
			messages := Parse(buffer)
			Expect(messages).To(BeEmpty())
		})

		It("Skips logging when debug is disabled", func() {
			// Create a logger with info level:
			infoLogger, err := NewLogger().
				SetLevel(slog.LevelInfo.String()).
				SetOut(buffer).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Create interceptor with info logger:
			infoInterceptor, err := NewInterceptor().
				SetLogger(infoLogger).
				Build()
			Expect(err).ToNot(HaveOccurred())

			method := "/test.Service/TestMethod"

			// Mock invoker:
			invoker := func(ctx context.Context, method string, req, resp any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
				return nil
			}

			// Call the interceptor:
			err = infoInterceptor.UnaryClient(ctx, method, nil, nil, conn, invoker)
			Expect(err).ToNot(HaveOccurred())

			// Parse the log messages - should be empty:
			messages := Parse(buffer)
			Expect(messages).To(BeEmpty())
		})
	})

	Describe("Stream client", func() {
		It("Logs stream client start and operations", func() {
			method := "/test.Service/TestStream"
			desc := &grpc.StreamDesc{
				StreamName:    "TestStream",
				ClientStreams: true,
				ServerStreams: true,
			}

			// Mock streamer:
			streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string,
				opts ...grpc.CallOption) (stream grpc.ClientStream, err error) {
				stream = &mockClientStream{
					ctx: ctx,
				}
				return
			}

			// Add metadata to context:
			ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer my_token"))

			// Call the interceptor:
			stream, err := interceptor.StreamClient(ctx, desc, conn, method, streamer)
			Expect(err).ToNot(HaveOccurred())
			Expect(stream).ToNot(BeNil())

			// Use the stream to trigger object logging:
			object := &testsv1.Object{MyString: "my_value", Id: "my_id"}
			err = stream.SendMsg(object)
			Expect(err).ToNot(HaveOccurred())
			err = stream.RecvMsg(object)
			Expect(err).To(Equal(io.EOF))

			// Parse the log messages:
			messages := Parse(buffer)
			Expect(messages).To(HaveLen(1))

			// Verify send log:
			sendMessage := messages[0]
			Expect(sendMessage["msg"]).To(Equal("Sent stream message"))
		})

		It("Skips logging for reflection methods", func() {
			method := "/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo"
			desc := &grpc.StreamDesc{}

			// Mock stream:
			mockStream := &mockClientStream{ctx: ctx}

			// Mock streamer:
			streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string,
				opts ...grpc.CallOption) (grpc.ClientStream, error) {
				return mockStream, nil
			}

			// Call the interceptor:
			stream, err := interceptor.StreamClient(ctx, desc, conn, method, streamer)
			Expect(err).ToNot(HaveOccurred())
			Expect(stream).To(Equal(mockStream)) // Should return original stream

			// Parse the log messages - should be empty:
			messages := Parse(buffer)
			Expect(messages).To(BeEmpty())
		})

		It("Skips logging for health check methods", func() {
			method := "/grpc.health.v1.Health/Watch"
			desc := &grpc.StreamDesc{}

			// Mock stream:
			mockStream := &mockClientStream{ctx: ctx}

			// Mock streamer:
			streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string,
				opts ...grpc.CallOption) (grpc.ClientStream, error) {
				return mockStream, nil
			}

			// Call the interceptor:
			stream, err := interceptor.StreamClient(ctx, desc, conn, method, streamer)
			Expect(err).ToNot(HaveOccurred())
			Expect(stream).To(Equal(mockStream)) // Should return original stream

			// Parse the log messages - should be empty:
			messages := Parse(buffer)
			Expect(messages).To(BeEmpty())
		})
	})
})

type mockClientStream struct {
	ctx context.Context
}

func (m *mockClientStream) Context() context.Context {
	return m.ctx
}

func (m *mockClientStream) Header() (result metadata.MD, err error) {
	result = metadata.New(map[string]string{
		"my_header": "my_value",
	})
	return
}

func (m *mockClientStream) Trailer() metadata.MD {
	return metadata.New(map[string]string{
		"my_trailer": "my_value",
	})
}

func (m *mockClientStream) CloseSend() error {
	return nil
}

func (m *mockClientStream) SendMsg(message any) error {
	return nil
}

func (m *mockClientStream) RecvMsg(message any) error {
	return io.EOF
}
