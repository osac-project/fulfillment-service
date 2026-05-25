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
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"

	envoycorev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoyauthv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/osac-project/fulfillment-service/internal/collections"
	. "github.com/osac-project/fulfillment-service/internal/testing"
)

var _ = Describe("GuestWebSocketAuth", func() {
	var (
		ctx  context.Context
		auth *GuestWebSocketAuth
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		auth, err = NewGuestWebSocketAuth().
			SetLogger(logger).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("Build", func() {
		It("Should fail if logger is not set", func() {
			_, err := NewGuestWebSocketAuth().
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("logger is mandatory"))
		})

		It("Should succeed with all required parameters", func() {
			result, err := NewGuestWebSocketAuth().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(result).ToNot(BeNil())
		})
	})

	Describe("Authenticate", func() {
		It("Should succeed with no Origin header", func() {
			r := &http.Request{
				Host:   "api.example.com",
				Header: http.Header{},
			}
			result, err := auth.Authenticate(ctx, r, "my-instance-id")
			Expect(err).ToNot(HaveOccurred())
			subject := SubjectFromContext(result)
			Expect(subject).To(BeIdenticalTo(Guest))
		})

		It("Should succeed when Origin matches Host", func() {
			r := &http.Request{
				Host: "api.example.com",
				Header: http.Header{
					"Origin": []string{"https://api.example.com"},
				},
			}
			result, err := auth.Authenticate(ctx, r, "my-instance-id")
			Expect(err).ToNot(HaveOccurred())
			subject := SubjectFromContext(result)
			Expect(subject).To(BeIdenticalTo(Guest))
		})

		It("Should succeed when Origin matches Host with http scheme", func() {
			r := &http.Request{
				Host: "localhost:8080",
				Header: http.Header{
					"Origin": []string{"http://localhost:8080"},
				},
			}
			result, err := auth.Authenticate(ctx, r, "my-instance-id")
			Expect(err).ToNot(HaveOccurred())
			subject := SubjectFromContext(result)
			Expect(subject).To(BeIdenticalTo(Guest))
		})

		It("Should reject when Origin does not match Host", func() {
			r := &http.Request{
				Host: "api.example.com",
				Header: http.Header{
					"Origin": []string{"https://evil.example.com"},
				},
			}
			_, err := auth.Authenticate(ctx, r, "my-instance-id")
			Expect(err).To(HaveOccurred())
			st, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(st.Code()).To(Equal(grpccodes.PermissionDenied))
			Expect(st.Message()).To(ContainSubstring("cross-origin"))
		})

		It("Should reject when Origin host differs in port", func() {
			r := &http.Request{
				Host: "api.example.com:443",
				Header: http.Header{
					"Origin": []string{"https://api.example.com:8443"},
				},
			}
			_, err := auth.Authenticate(ctx, r, "my-instance-id")
			Expect(err).To(HaveOccurred())
			st, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(st.Code()).To(Equal(grpccodes.PermissionDenied))
		})
	})
})

var _ = Describe("ExternalWebSocketAuth", func() {
	var (
		ctx  context.Context
		auth *ExternalWebSocketAuth
		mock *GrpcExternalAuthMock
	)

	BeforeEach(func() {
		var err error
		ctx = context.Background()

		// Create the mock:
		mock = &GrpcExternalAuthMock{}

		// Get the test certificate files:
		crtFile, keyFile, caFile := LocalhostCertificateFiles()
		DeferCleanup(func() {
			_ = os.Remove(crtFile)
			_ = os.Remove(keyFile)
			_ = os.Remove(caFile)
		})
		caData, err := os.ReadFile(caFile)
		Expect(err).ToNot(HaveOccurred())

		// Create the CA pool:
		caPool, err := x509.SystemCertPool()
		Expect(err).ToNot(HaveOccurred())
		ok := caPool.AppendCertsFromPEM(caData)
		Expect(ok).To(BeTrue())

		// Create the listener:
		crt, err := tls.LoadX509KeyPair(crtFile, keyFile)
		Expect(err).ToNot(HaveOccurred())
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).ToNot(HaveOccurred())
		listener = tls.NewListener(listener, &tls.Config{
			RootCAs: caPool,
			Certificates: []tls.Certificate{
				crt,
			},
			NextProtos: []string{"h2"},
		})
		address := listener.Addr().String()

		// Create the gRPC client:
		endpoint := fmt.Sprintf("dns:///%s", address)
		creds := credentials.NewTLS(&tls.Config{
			RootCAs: caPool,
		})
		options := []grpc.DialOption{
			grpc.WithTransportCredentials(creds),
		}
		client, err := grpc.NewClient(endpoint, options...)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(client.Close)

		// Create the server:
		server := grpc.NewServer()
		DeferCleanup(server.Stop)
		envoyauthv3.RegisterAuthorizationServer(server, mock)
		go server.Serve(listener)

		// Build the checker:
		checker, err := NewExternalAuthChecker().
			SetLogger(logger).
			SetAuthClient(envoyauthv3.NewAuthorizationClient(client)).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Build the authenticator:
		auth, err = NewExternalWebSocketAuth().
			SetLogger(logger).
			SetChecker(checker).
			Build()
		Expect(err).ToNot(HaveOccurred())
	})

	// makeOkResponse creates an OK response with a subject header.
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

	Describe("Build", func() {
		It("Should fail if logger is not set", func() {
			_, err := NewExternalWebSocketAuth().
				SetChecker(&ExternalAuthChecker{}).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("logger is mandatory"))
		})

		It("Should fail if checker is not set", func() {
			_, err := NewExternalWebSocketAuth().
				SetLogger(logger).
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("checker is mandatory"))
		})
	})

	Describe("Authenticate", func() {
		It("Should succeed with Authorization header", func() {
			mock.Func = func(ctx context.Context,
				request *envoyauthv3.CheckRequest) (*envoyauthv3.CheckResponse, error) {
				headers := request.GetAttributes().GetRequest().GetHttp().GetHeaders()
				Expect(headers).To(HaveKeyWithValue("authorization", "Bearer my-token"))

				// Verify the method is POST and path is the Console/Connect method:
				httpReq := request.GetAttributes().GetRequest().GetHttp()
				Expect(httpReq.GetMethod()).To(Equal("POST"))
				Expect(httpReq.GetPath()).To(Equal(consoleConnectMethod))

				return makeOkResponse(&Subject{
					User:    "my-user",
					Tenants: collections.NewSet("my-tenant"),
				}), nil
			}

			r := &http.Request{
				Header: http.Header{
					"Authorization": []string{"Bearer my-token"},
				},
			}
			result, err := auth.Authenticate(ctx, r, "instance-123")
			Expect(err).ToNot(HaveOccurred())
			subject := SubjectFromContext(result)
			Expect(subject).ToNot(BeNil())
			Expect(subject.User).To(Equal("my-user"))
			Expect(subject.Tenants.Contains("my-tenant")).To(BeTrue())
		})

		It("Should succeed with token query parameter", func() {
			mock.Func = func(ctx context.Context,
				request *envoyauthv3.CheckRequest) (*envoyauthv3.CheckResponse, error) {
				headers := request.GetAttributes().GetRequest().GetHttp().GetHeaders()
				Expect(headers).To(HaveKeyWithValue("authorization", "Bearer query-token"))
				return makeOkResponse(&Subject{
					User: "my-user",
				}), nil
			}

			r, err := http.NewRequest(http.MethodGet,
				"https://api.example.com/console?token=query-token", nil)
			Expect(err).ToNot(HaveOccurred())
			result, err := auth.Authenticate(ctx, r, "instance-123")
			Expect(err).ToNot(HaveOccurred())
			subject := SubjectFromContext(result)
			Expect(subject).ToNot(BeNil())
			Expect(subject.User).To(Equal("my-user"))
		})

		It("Should prefer Authorization header over query parameter", func() {
			mock.Func = func(ctx context.Context,
				request *envoyauthv3.CheckRequest) (*envoyauthv3.CheckResponse, error) {
				headers := request.GetAttributes().GetRequest().GetHttp().GetHeaders()
				Expect(headers).To(HaveKeyWithValue("authorization", "Bearer header-token"))
				return makeOkResponse(&Subject{
					User: "my-user",
				}), nil
			}

			r, err := http.NewRequest(http.MethodGet,
				"https://api.example.com/console?token=query-token", nil)
			Expect(err).ToNot(HaveOccurred())
			r.Header.Set("Authorization", "Bearer header-token")
			result, err := auth.Authenticate(ctx, r, "instance-123")
			Expect(err).ToNot(HaveOccurred())
			subject := SubjectFromContext(result)
			Expect(subject).ToNot(BeNil())
			Expect(subject.User).To(Equal("my-user"))
		})

		It("Should return Unauthenticated when no token is provided", func() {
			r := &http.Request{
				Header: http.Header{},
			}
			_, err := auth.Authenticate(ctx, r, "instance-123")
			Expect(err).To(HaveOccurred())
			st, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(st.Code()).To(Equal(grpccodes.Unauthenticated))
			Expect(st.Message()).To(ContainSubstring("missing authentication token"))
		})

		It("Should include resource ID in context extensions", func() {
			mock.Func = func(ctx context.Context,
				request *envoyauthv3.CheckRequest) (*envoyauthv3.CheckResponse, error) {
				extensions := request.GetAttributes().GetContextExtensions()
				Expect(extensions).To(HaveKeyWithValue("id", "instance-456"))
				return makeOkResponse(&Subject{
					User: "my-user",
				}), nil
			}

			r := &http.Request{
				Header: http.Header{
					"Authorization": []string{"Bearer my-token"},
				},
			}
			_, err := auth.Authenticate(ctx, r, "instance-456")
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should return PermissionDenied when auth service denies access", func() {
			mock.Func = func(ctx context.Context,
				request *envoyauthv3.CheckRequest) (*envoyauthv3.CheckResponse, error) {
				return &envoyauthv3.CheckResponse{
					Status: &status.Status{
						Code: int32(grpccodes.PermissionDenied),
					},
				}, nil
			}

			r := &http.Request{
				Header: http.Header{
					"Authorization": []string{"Bearer bad-token"},
				},
			}
			_, err := auth.Authenticate(ctx, r, "instance-123")
			Expect(err).To(HaveOccurred())
			st, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(st.Code()).To(Equal(grpccodes.PermissionDenied))
		})

		It("Should return Internal when auth service is unavailable", func() {
			mock.Func = func(ctx context.Context,
				request *envoyauthv3.CheckRequest) (*envoyauthv3.CheckResponse, error) {
				return nil, errors.New("connection refused")
			}

			r := &http.Request{
				Header: http.Header{
					"Authorization": []string{"Bearer my-token"},
				},
			}
			_, err := auth.Authenticate(ctx, r, "instance-123")
			Expect(err).To(HaveOccurred())
			st, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(st.Code()).To(Equal(grpccodes.Internal))
			Expect(st.Message()).To(Equal("failed to check permissions"))
		})

		It("Should normalize bare token from query param to Bearer format", func() {
			mock.Func = func(ctx context.Context,
				request *envoyauthv3.CheckRequest) (*envoyauthv3.CheckResponse, error) {
				headers := request.GetAttributes().GetRequest().GetHttp().GetHeaders()
				Expect(headers).To(HaveKeyWithValue("authorization", "Bearer raw-jwt-value"))
				return makeOkResponse(&Subject{
					User: "my-user",
				}), nil
			}

			r, err := http.NewRequest(http.MethodGet,
				"https://api.example.com/console?token=raw-jwt-value", nil)
			Expect(err).ToNot(HaveOccurred())
			_, err = auth.Authenticate(ctx, r, "instance-123")
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should pass through Bearer-prefixed query param as-is", func() {
			mock.Func = func(ctx context.Context,
				request *envoyauthv3.CheckRequest) (*envoyauthv3.CheckResponse, error) {
				headers := request.GetAttributes().GetRequest().GetHttp().GetHeaders()
				Expect(headers).To(HaveKeyWithValue("authorization", "Bearer already-prefixed"))
				return makeOkResponse(&Subject{
					User: "my-user",
				}), nil
			}

			r, err := http.NewRequest(http.MethodGet,
				"https://api.example.com/console?token=Bearer+already-prefixed", nil)
			Expect(err).ToNot(HaveOccurred())
			_, err = auth.Authenticate(ctx, r, "instance-123")
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
