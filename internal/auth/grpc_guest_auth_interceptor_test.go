/*
Copyright (c) 2025 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package auth

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	. "github.com/osac-project/fulfillment-common/testing"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

var _ = Describe("GrpcGuestAuthInterceptor", func() {
	var (
		ctx         context.Context
		interceptor *GrpcGuestAuthInterceptor
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("Build", func() {
		It("should fail if logger is not set", func() {
			_, err := NewGrpcGuestAuthInterceptor().
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("logger is mandatory"))
		})

		It("should succeed with all required parameters", func() {
			interceptor, err := NewGrpcGuestAuthInterceptor().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(interceptor).ToNot(BeNil())
		})
	})

	Describe("UnaryServer", func() {
		BeforeEach(func() {
			var err error
			interceptor, err = NewGrpcGuestAuthInterceptor().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		It("should add guest subject to the context", func() {
			var handlerCtx context.Context
			handler := func(ctx context.Context, req any) (any, error) {
				handlerCtx = ctx
				return "response", nil
			}

			ctx = metadata.NewIncomingContext(ctx, metadata.Pairs())
			info := &grpc.UnaryServerInfo{FullMethod: "/my_package/MyMethod"}
			response, err := interceptor.UnaryServer(ctx, nil, info, handler)
			Expect(err).ToNot(HaveOccurred())
			Expect(response).To(Equal("response"))

			// Verify the guest subject is set in the context:
			subject := SubjectFromContext(handlerCtx)
			Expect(subject).To(BeIdenticalTo(Guest))
		})

		It("should ignore authorization header", func() {
			var handlerCtx context.Context
			handler := func(ctx context.Context, req any) (any, error) {
				handlerCtx = ctx
				return "response", nil
			}

			ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(
				"Authorization", "Bad junk",
			))
			info := &grpc.UnaryServerInfo{FullMethod: "/my_package/MyMethod"}
			response, err := interceptor.UnaryServer(ctx, nil, info, handler)
			Expect(err).ToNot(HaveOccurred())
			Expect(response).To(Equal("response"))

			// Verify the guest subject is still set (auth header ignored):
			subject := SubjectFromContext(handlerCtx)
			Expect(subject).To(BeIdenticalTo(Guest))
		})

		It("should ignore bearer token", func() {
			var handlerCtx context.Context
			handler := func(ctx context.Context, req any) (any, error) {
				handlerCtx = ctx
				return "response", nil
			}

			bearer := MakeTokenString("Bearer", 1*time.Minute)
			ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(
				"Authorization", "Bearer "+bearer,
			))
			info := &grpc.UnaryServerInfo{FullMethod: "/my_package/MyMethod"}
			response, err := interceptor.UnaryServer(ctx, nil, info, handler)
			Expect(err).ToNot(HaveOccurred())
			Expect(response).To(Equal("response"))

			// Verify the guest subject is still set (token ignored):
			subject := SubjectFromContext(handlerCtx)
			Expect(subject).To(BeIdenticalTo(Guest))
		})
	})

	Describe("StreamServer", func() {
		BeforeEach(func() {
			var err error
			interceptor, err = NewGrpcGuestAuthInterceptor().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		It("should add guest subject to the stream context", func() {
			var handlerStream grpc.ServerStream
			handler := func(srv any, stream grpc.ServerStream) error {
				handlerStream = stream
				return nil
			}

			mockStream := &mockServerStream{ctx: ctx}
			info := &grpc.StreamServerInfo{FullMethod: "/my_package/MyMethod"}
			err := interceptor.StreamServer(nil, mockStream, info, handler)
			Expect(err).ToNot(HaveOccurred())

			// Verify the guest subject is set in the stream context:
			subject := SubjectFromContext(handlerStream.Context())
			Expect(subject).To(BeIdenticalTo(Guest))
		})
	})
})

// mockServerStream is a mock implementation of grpc.ServerStream for testing.
type mockServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context {
	return m.ctx
}

func (m *mockServerStream) SendMsg(msg any) error {
	return nil
}

func (m *mockServerStream) RecvMsg(msg any) error {
	return nil
}
