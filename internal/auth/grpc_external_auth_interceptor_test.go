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
	"crypto/x509"
	"encoding/json"
	"errors"
	"os"

	envoycorev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoyauthv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"github.com/osac-project/fulfillment-common/network"
	. "github.com/osac-project/fulfillment-common/testing"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
)

// GrpcExternalAuthMock is a mock implementation of the authv3.AuthorizationServer interface.
type GrpcExternalAuthMock struct {
	envoyauthv3.UnimplementedAuthorizationServer
	Func func(context.Context, *envoyauthv3.CheckRequest) (*envoyauthv3.CheckResponse, error)
}

func (m *GrpcExternalAuthMock) Check(ctx context.Context,
	req *envoyauthv3.CheckRequest) (response *envoyauthv3.CheckResponse, err error) {
	if m.Func == nil {
		err = errors.New("func is not set")
		return
	}
	response, err = m.Func(ctx, req)
	return
}

var _ = Describe("External authentication and authorization interceptor", func() {
	var (
		ctx     context.Context
		mock    *GrpcExternalAuthMock
		address string
		caPool  *x509.CertPool
	)

	BeforeEach(func() {
		var err error

		// Create a context:
		ctx = context.Background()

		// Create the moc:
		mock = &GrpcExternalAuthMock{}

		// Get the test certificate files:
		crtFile, keyFile, caFile := LocalhostCertificateFiles()
		DeferCleanup(func() {
			_ = os.Remove(crtFile)
			_ = os.Remove(keyFile)
			_ = os.Remove(caFile)
		})

		// Create the CA pool:
		caPool, err = network.NewCertPool().
			SetLogger(logger).
			AddFile(caFile).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Create the listener:
		listener, err := network.NewListener().
			SetLogger(logger).
			SetAddress("127.0.0.1:0").
			SetTLSCrt(crtFile).
			SetTLSKey(keyFile).
			Build()
		Expect(err).ToNot(HaveOccurred())
		address = listener.Addr().String()

		// Create the server:
		server := grpc.NewServer()
		DeferCleanup(server.Stop)
		envoyauthv3.RegisterAuthorizationServer(server, mock)
		go server.Serve(listener)
	})

	Describe("Build", func() {
		It("Should fail if logger is not set", func() {
			_, err := NewGrpcExternalAuthInterceptor().
				SetAddress(address).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("logger is mandatory"))
		})

		It("Should fail if address is not set", func() {
			_, err := NewGrpcExternalAuthInterceptor().
				SetLogger(logger).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("address is mandatory"))
		})

		It("Should succeed with all required parameters", func() {
			interceptor, err := NewGrpcExternalAuthInterceptor().
				SetLogger(logger).
				SetAddress(address).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(interceptor).ToNot(BeNil())
		})

		It("Should fail with invalid public method regex", func() {
			_, err := NewGrpcExternalAuthInterceptor().
				SetLogger(logger).
				SetAddress(address).
				AddPublicMethodRegex(`[invalid`).
				Build()
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Unary server interceptor", func() {
		var interceptor *GrpcExternalAuthInterceptor

		BeforeEach(func() {
			var err error
			interceptor, err = NewGrpcExternalAuthInterceptor().
				SetLogger(logger).
				SetAddress(address).
				SetCaPool(caPool).
				AddPublicMethodRegex(`^/public\..*$`).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		// makeOkResponse creates an OK response with a subject header. If the subject is nil, then the response will
		// not contain a subject header.
		var makeOkResponse = func(subject *Subject) *envoyauthv3.CheckResponse {
			header := ""
			if subject != nil {
				data, err := json.Marshal(subject)
				Expect(err).ToNot(HaveOccurred())
				header = string(data)
			}
			return &envoyauthv3.CheckResponse{
				Status: &status.Status{
					Code: int32(grpccodes.OK),
				},
				HttpResponse: &envoyauthv3.CheckResponse_OkResponse{
					OkResponse: &envoyauthv3.OkHttpResponse{
						Headers: []*envoycorev3.HeaderValueOption{{
							Header: &envoycorev3.HeaderValue{
								Key:   SubjectHeader,
								Value: header,
							},
						}},
					},
				},
			}
		}

		// makeDeniedResponse creates a denied response.
		var makeDeniedResponse = func() *envoyauthv3.CheckResponse {
			return &envoyauthv3.CheckResponse{
				Status: &status.Status{
					Code: int32(grpccodes.PermissionDenied),
				},
			}
		}

		It("Should skip auth for public methods and set guest subject", func() {
			var handlerCtx context.Context
			handler := func(ctx context.Context, req any) (any, error) {
				handlerCtx = ctx
				return "response", nil
			}

			info := &grpc.UnaryServerInfo{
				FullMethod: "/public.v1.Service/Method",
			}
			response, err := interceptor.UnaryServer(ctx, nil, info, handler)
			Expect(err).ToNot(HaveOccurred())
			Expect(response).To(Equal("response"))

			// Verify the guest subject is set in the context:
			subject := SubjectFromContext(handlerCtx)
			Expect(subject).To(Equal(Guest))
		})

		It("Should call handler when allowed", func() {
			mock.Func = func(ctx context.Context,
				request *envoyauthv3.CheckRequest) (response *envoyauthv3.CheckResponse, err error) {
				response = makeOkResponse(&Subject{
					Source: SubjectSourceJwt,
					User:   "my-user",
					Groups: []string{
						"my-group",
					},
				})
				return
			}
			called := false
			handler := func(context.Context, any) (any, error) {
				called = true
				return nil, nil
			}
			info := &grpc.UnaryServerInfo{
				FullMethod: "/private.v1.Service/Method",
			}
			_, err := interceptor.UnaryServer(ctx, nil, info, handler)
			Expect(err).ToNot(HaveOccurred())
			Expect(called).To(BeTrue())
		})

		It("Should deny when auth fails", func() {
			mock.Func = func(ctx context.Context,
				request *envoyauthv3.CheckRequest) (response *envoyauthv3.CheckResponse, err error) {
				response = makeDeniedResponse()
				return
			}
			handler := func(context.Context, any) (any, error) {
				return nil, nil
			}
			info := &grpc.UnaryServerInfo{
				FullMethod: "/private.v1.Service/Method",
			}
			_, err := interceptor.UnaryServer(ctx, nil, info, handler)
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
			Expect(status.Message()).To(Equal("permission denied"))
		})

		It("Should add subject to context when allowed", func() {
			mock.Func = func(ctx context.Context,
				request *envoyauthv3.CheckRequest) (response *envoyauthv3.CheckResponse, err error) {
				response = makeOkResponse(&Subject{
					Source: SubjectSourceJwt,
					User:   "my-user",
					Groups: []string{
						"my-group",
					},
				})
				return
			}
			handler := func(ctx context.Context, _ any) (any, error) {
				subject := SubjectFromContext(ctx)
				Expect(subject).ToNot(BeNil())
				Expect(subject.Source).To(Equal(SubjectSourceJwt))
				Expect(subject.User).To(Equal("my-user"))
				Expect(subject.Groups).To(ContainElements(
					"my-group",
				))
				return nil, nil
			}
			info := &grpc.UnaryServerInfo{
				FullMethod: "/private.v1.Service/Method",
			}
			_, err := interceptor.UnaryServer(ctx, nil, info, handler)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Fails if the external service doesn't return a subject header", func() {
			mock.Func = func(ctx context.Context,
				request *envoyauthv3.CheckRequest) (response *envoyauthv3.CheckResponse, err error) {
				response = makeOkResponse(nil)
				return
			}
			handler := func(ctx context.Context, _ any) (any, error) {
				return nil, nil
			}
			info := &grpc.UnaryServerInfo{
				FullMethod: "/private.v1.Service/Method",
			}
			_, err := interceptor.UnaryServer(ctx, nil, info, handler)
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.Internal))
			Expect(status.Message()).To(Equal("failed to check permissions"))
		})

		It("Fails if the external service returns a subject without a source", func() {
			mock.Func = func(ctx context.Context,
				request *envoyauthv3.CheckRequest) (response *envoyauthv3.CheckResponse, err error) {
				response = makeOkResponse(&Subject{
					User: "my-user",
					Groups: []string{
						"my-group",
					},
				})
				return
			}
			handler := func(ctx context.Context, _ any) (any, error) {
				return nil, nil
			}
			info := &grpc.UnaryServerInfo{
				FullMethod: "/private.v1.Service/Method",
			}
			_, err := interceptor.UnaryServer(ctx, nil, info, handler)
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.Internal))
			Expect(status.Message()).To(Equal("failed to check permissions"))
		})

		It("Fails if the external service returns a subject without a name", func() {
			mock.Func = func(ctx context.Context,
				request *envoyauthv3.CheckRequest) (response *envoyauthv3.CheckResponse, err error) {
				response = makeOkResponse(&Subject{
					Source: SubjectSourceJwt,
					Groups: []string{
						"my-group",
					},
				})
				return
			}
			handler := func(ctx context.Context, _ any) (any, error) {
				return nil, nil
			}
			info := &grpc.UnaryServerInfo{
				FullMethod: "/private.v1.Service/Method",
			}
			_, err := interceptor.UnaryServer(ctx, nil, info, handler)
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.Internal))
			Expect(status.Message()).To(Equal("failed to check permissions"))
		})

		It("Should include incoming metadata as headers", func() {
			mock.Func = func(ctx context.Context,
				request *envoyauthv3.CheckRequest) (response *envoyauthv3.CheckResponse, err error) {
				Expect(request).ToNot(BeNil())
				headers := request.GetAttributes().GetRequest().GetHttp().GetHeaders()
				Expect(headers).To(HaveKeyWithValue("authorization", "Bearer my-token"))
				Expect(headers).To(HaveKeyWithValue("x-my-header", "my-value"))
				response = makeOkResponse(&Subject{
					Source: SubjectSourceJwt,
					User:   "my-user",
					Groups: []string{
						"my-group",
					},
				})
				return
			}
			handler := func(context.Context, any) (any, error) {
				return nil, nil
			}
			md := metadata.Pairs(
				"authorization", "Bearer my-token",
				"x-my-header", "my-value",
			)
			ctx = metadata.NewIncomingContext(ctx, md)
			info := &grpc.UnaryServerInfo{
				FullMethod: "/private.v1.Service/Method",
			}
			_, err := interceptor.UnaryServer(ctx, nil, info, handler)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should deny permission when external service call fails", func() {
			mock.Func = func(ctx context.Context,
				request *envoyauthv3.CheckRequest) (response *envoyauthv3.CheckResponse, err error) {
				err = grpcstatus.Errorf(grpccodes.Unavailable, "service unavailable")
				return
			}
			handler := func(context.Context, any) (any, error) {
				return nil, nil
			}
			info := &grpc.UnaryServerInfo{
				FullMethod: "/private.v1.Service/Method",
			}
			_, err := interceptor.UnaryServer(ctx, nil, info, handler)
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.Internal))
			Expect(status.Message()).To(Equal("failed to check permissions"))
		})
	})

	Describe("Stream server interceptor", func() {
		var interceptor *GrpcExternalAuthInterceptor

		BeforeEach(func() {
			var err error
			interceptor, err = NewGrpcExternalAuthInterceptor().
				SetLogger(logger).
				SetAddress(address).
				SetCaPool(caPool).
				AddPublicMethodRegex(`^/public\..*$`).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		// makeDeniedResponse creates a denied response.
		var makeDeniedResponse = func() *envoyauthv3.CheckResponse {
			return &envoyauthv3.CheckResponse{
				Status: &status.Status{
					Code: int32(grpccodes.PermissionDenied),
				},
			}
		}

		It("Should deny when auth fails", func() {
			mock.Func = func(ctx context.Context,
				request *envoyauthv3.CheckRequest) (response *envoyauthv3.CheckResponse, err error) {
				response = makeDeniedResponse()
				return
			}
			handler := func(server any, stream grpc.ServerStream) error {
				return nil
			}
			info := &grpc.StreamServerInfo{
				FullMethod: "/private.v1.Service/StreamMethod",
			}
			stream := &mockServerStream{
				ctx: ctx,
			}
			err := interceptor.StreamServer(nil, stream, info, handler)
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
			Expect(status.Message()).To(Equal("permission denied"))
		})
	})
})
