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
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	envoycorev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoyauthv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	"github.com/osac-project/fulfillment-common/network"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
)

// GrpcExternalAuthType is the name of the external authentication type.
const GrpcExternalAuthType = "external"

// GrpcExternalAuthInterceptorBuilder contains the data and logic needed to build an interceptor that performs
// authentication and authorization by calling an external service using the Envoy ext_authz gRPC protocol.
type GrpcExternalAuthInterceptorBuilder struct {
	logger            *slog.Logger
	address           string
	caPool            *x509.CertPool
	insecure          bool
	publicMethods     []string
	userAgent         string
	metricsSubsystem  string
	metricsRegisterer prometheus.Registerer
}

// GrpcExternalAuthInterceptor is an interceptor that performs authentication and authorization by calling an external
// service using the Envoy ext_authz gRPC protocol. This interceptor does not require a prior authentication
// interceptor in the chain. Instead, the external auth service is expected to provide subject details (user, groups)
// in its response headers when authorization is granted. These details are then added to the context for use by
// downstream handlers. The interceptor also has access to the request message, allowing it to include object-specific
// details (like identifiers and tenants) in the authorization request.
type GrpcExternalAuthInterceptor struct {
	logger        *slog.Logger
	grpcClient    *grpc.ClientConn
	authClient    envoyauthv3.AuthorizationClient
	publicMethods []*regexp.Regexp
}

// NewGrpcExternalAuthInterceptor creates a builder that can then be used to configure and create an authentication
// and authorization interceptor.
func NewGrpcExternalAuthInterceptor() *GrpcExternalAuthInterceptorBuilder {
	return &GrpcExternalAuthInterceptorBuilder{}
}

// SetLogger sets the logger that will be used to write to the log. This is mandatory.
func (b *GrpcExternalAuthInterceptorBuilder) SetLogger(value *slog.Logger) *GrpcExternalAuthInterceptorBuilder {
	b.logger = value
	return b
}

// SetAddress sets the address of the external auth service. This is mandatory.
func (b *GrpcExternalAuthInterceptorBuilder) SetAddress(value string) *GrpcExternalAuthInterceptorBuilder {
	b.address = value
	return b
}

// SetCaPool sets the CA certificate pool to use for TLS connections to the external auth service. This is optional
// and only used when insecure is false.
func (b *GrpcExternalAuthInterceptorBuilder) SetCaPool(value *x509.CertPool) *GrpcExternalAuthInterceptorBuilder {
	b.caPool = value
	return b
}

// SetInsecure sets whether to use an insecure (plaintext) connection to the external auth service. If true, the
// connection will not use TLS. Defaults to false.
func (b *GrpcExternalAuthInterceptorBuilder) SetInsecure(value bool) *GrpcExternalAuthInterceptorBuilder {
	b.insecure = value
	return b
}

// AddPublicMethodRegex adds a regular expression that describes a set of methods that are considered public, and
// therefore require no authentication or authorization. The regular expression will be matched against the full gRPC
// method name, including the leading slash. For example, to consider public all the methods of the
// 'example.v1.Products' service the regular expression could be '^/example\.v1\.Products/.*$'.
//
// This method may be called multiple times to add multiple regular expressions. A method will be considered public if
// it matches at least one of them.
func (b *GrpcExternalAuthInterceptorBuilder) AddPublicMethodRegex(value string) *GrpcExternalAuthInterceptorBuilder {
	b.publicMethods = append(b.publicMethods, value)
	return b
}

// SetUserAgent sets the user agent string to use when making gRPC calls to the external auth service. This is optional.
func (b *GrpcExternalAuthInterceptorBuilder) SetUserAgent(value string) *GrpcExternalAuthInterceptorBuilder {
	b.userAgent = value
	return b
}

// SetMetricsSubsystem sets the subsystem that will be used for metrics. This is optional, if not specified then no
// metrics will be collected.
func (b *GrpcExternalAuthInterceptorBuilder) SetMetricsSubsystem(value string) *GrpcExternalAuthInterceptorBuilder {
	b.metricsSubsystem = value
	return b
}

// SetMetricsRegisterer sets the metrics registry that will be used for metrics. This is optional, if not specified then
// the default metrics registry will be used.
func (b *GrpcExternalAuthInterceptorBuilder) SetMetricsRegisterer(value prometheus.Registerer) *GrpcExternalAuthInterceptorBuilder {
	b.metricsRegisterer = value
	return b
}

// Build uses the data stored in the builder to create and configure a new interceptor.
func (b *GrpcExternalAuthInterceptorBuilder) Build() (result *GrpcExternalAuthInterceptor, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.address == "" {
		err = errors.New("address is mandatory")
		return
	}

	// Try to compile the regular expressions that define the set of public methods:
	publicMethods := make([]*regexp.Regexp, len(b.publicMethods))
	for i, expr := range b.publicMethods {
		publicMethods[i], err = regexp.Compile(expr)
		if err != nil {
			return
		}
	}

	// Create the gRPC connection:
	grpcClient, err := network.NewGrpcClient().
		SetLogger(b.logger).
		SetAddress(b.address).
		SetCaPool(b.caPool).
		SetInsecure(b.insecure).
		SetUserAgent(b.userAgent).
		SetMetricsSubsystem(b.metricsSubsystem).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		err = fmt.Errorf(
			"failed to create gRPC connection to external service with address '%s': %w",
			b.address, err,
		)
		return
	}

	// Create the auth authClient:
	authClient := envoyauthv3.NewAuthorizationClient(grpcClient)

	// Create and populate the object:
	result = &GrpcExternalAuthInterceptor{
		logger:        b.logger,
		grpcClient:    grpcClient,
		authClient:    authClient,
		publicMethods: publicMethods,
	}
	return
}

// Close closes the gRPC connection to the external auth service.
func (i *GrpcExternalAuthInterceptor) Close() error {
	if i.grpcClient != nil {
		return i.grpcClient.Close()
	}
	return nil
}

// UnaryServer is the unary server interceptor function.
func (i *GrpcExternalAuthInterceptor) UnaryServer(ctx context.Context, request any, info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler) (response any, err error) {
	// Skip auth for public methods:
	if i.isPublicMethod(info.FullMethod) {
		ctx = ContextWithSubject(ctx, Guest)
		return handler(ctx, request)
	}

	// Call the external auth service:
	ctx, err = i.check(ctx, info.FullMethod)
	if err != nil {
		return
	}

	// Continue with the handler:
	return handler(ctx, request)
}

// StreamServer is the stream server interceptor function.
func (i *GrpcExternalAuthInterceptor) StreamServer(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo,
	handler grpc.StreamHandler) error {
	// Skip auth for public methods:
	if i.isPublicMethod(info.FullMethod) {
		ctx := ContextWithSubject(stream.Context(), Guest)
		stream = &grpcExternalAuthInterceptorStream{
			context: ctx,
			stream:  stream,
		}
		return handler(server, stream)
	}

	// For streaming RPCs, we check auth based on the method only because there is no request body available yet.
	ctx, err := i.check(stream.Context(), info.FullMethod)
	if err != nil {
		return err
	}

	// Wrap the stream with the new context containing the subject:
	stream = &grpcExternalAuthInterceptorStream{
		context: ctx,
		stream:  stream,
	}
	return handler(server, stream)
}

// grpcExternalAuthInterceptorStream wraps a gRPC server stream with a modified context.
type grpcExternalAuthInterceptorStream struct {
	context context.Context
	stream  grpc.ServerStream
}

func (s *grpcExternalAuthInterceptorStream) Context() context.Context {
	return s.context
}

func (s *grpcExternalAuthInterceptorStream) RecvMsg(message any) error {
	return s.stream.RecvMsg(message)
}

func (s *grpcExternalAuthInterceptorStream) SendHeader(md metadata.MD) error {
	return s.stream.SendHeader(md)
}

func (s *grpcExternalAuthInterceptorStream) SendMsg(message any) error {
	return s.stream.SendMsg(message)
}

func (s *grpcExternalAuthInterceptorStream) SetHeader(md metadata.MD) error {
	return s.stream.SetHeader(md)
}

func (s *grpcExternalAuthInterceptorStream) SetTrailer(md metadata.MD) {
	s.stream.SetTrailer(md)
}

// check calls the external auth service to check if the request is authenticated and authorized. If granted, it
// returns a new context containing the subject details extracted from the response.
func (i *GrpcExternalAuthInterceptor) check(ctx context.Context, method string) (result context.Context,
	err error) {
	// Add some details to the logger:
	logger := i.logger.With(
		slog.String("method", method),
	)

	// Build the check request:
	request, err := i.buildCheckRequest(ctx, method)
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to build check request",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to build check request")
		return
	}
	logger = logger.With(
		slog.Any("request", request),
	)

	// Call the external service:
	logger.DebugContext(
		ctx,
		"Sending check request to external service",
	)
	response, err := i.authClient.Check(ctx, request)
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed sending check request to external service",
			slog.Any("error", err),
		)
		err = grpcExternalAuthInternalError
		return
	}
	logger = logger.With(
		slog.Any("response", response),
	)
	logger.DebugContext(
		ctx,
		"Received check response from external service",
	)

	// Check the response and extract subject details:
	result, err = i.handleCheckResponse(ctx, method, response)
	return
}

func (i *GrpcExternalAuthInterceptor) buildCheckRequest(ctx context.Context,
	method string) (result *envoyauthv3.CheckRequest, err error) {
	// Extract headers from the incoming context:
	headers := map[string]string{}
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		for key, values := range md {
			if len(values) > 0 {
				headers[key] = values[0]
			}
		}
	}

	// Build the check request:
	result = &envoyauthv3.CheckRequest{
		Attributes: &envoyauthv3.AttributeContext{
			Request: &envoyauthv3.AttributeContext_Request{
				Http: &envoyauthv3.AttributeContext_HttpRequest{
					Method:  http.MethodPost,
					Path:    method,
					Headers: headers,
				},
			},
		},
	}
	return
}

// handleCheckResponse processes the response from the external auth service. If granted, it extracts subject details
// from the response headers and returns a new context containing the subject.
func (i *GrpcExternalAuthInterceptor) handleCheckResponse(ctx context.Context, method string,
	response *envoyauthv3.CheckResponse) (result context.Context, err error) {
	// Add some details to the logger:
	logger := i.logger.With(
		slog.String("method", method),
	)

	// Deny access if there is no response or no status:
	if response == nil {
		logger.ErrorContext(
			ctx,
			"Permission denied because the response is nil",
		)
		err = grpcExternalAuthInternalError
		return
	}
	status := response.GetStatus()
	if status == nil {
		logger.ErrorContext(
			ctx,
			"Permission denined because there is no status in the response",
		)
		err = grpcExternalAuthInternalError
		return
	}

	// Deny access if the external service denied it:
	code := grpccodes.Code(status.GetCode())
	logger = logger.With(
		slog.String("code", code.String()),
		slog.String("message", status.GetMessage()),
		slog.Any("details", status.GetDetails()),
	)
	if code != grpccodes.OK {
		logger.DebugContext(ctx, "Permission denied by external service")
		err = grpcstatus.Errorf(code, "permission denied")
		return
	}

	// Try to extract the subject from the resonse. If this fails then deny access.
	subject, err := i.subjectFromResponse(response)
	if err != nil {
		logger.ErrorContext(
			ctx,
			"Failed to extract subject from response",
			slog.Any("error", err),
		)
		err = grpcExternalAuthInternalError
		return
	}

	// If we are here, then the external server granted access:
	logger.DebugContext(ctx, "Permission granted by external service")
	result = ContextWithSubject(ctx, subject)
	return
}

func (i *GrpcExternalAuthInterceptor) subjectFromResponse(response *envoyauthv3.CheckResponse) (result *Subject,
	err error) {
	accepted := response.GetOkResponse()
	if accepted == nil {
		err = errors.New("response doesn't contain headers")
		return
	}
	var subject *Subject
	for _, header := range accepted.GetHeaders() {
		entry := header.GetHeader()
		if entry == nil {
			continue
		}
		if strings.EqualFold(entry.GetKey(), SubjectHeader) {
			subject, err = i.subjectFromHeader(entry)
			if err != nil {
				return
			}
		}
	}
	if subject == nil {
		err = fmt.Errorf("response doesn't contain the '%s' header", SubjectHeader)
		return
	}
	result = subject
	return
}

func (i *GrpcExternalAuthInterceptor) subjectFromHeader(header *envoycorev3.HeaderValue) (result *Subject, err error) {
	key := header.GetKey()
	value := header.GetValue()
	subject := &Subject{}
	err = json.Unmarshal([]byte(value), subject)
	if err != nil {
		err = fmt.Errorf("failed to unmarshal subject from header '%s' with value '%s': %w", key, value, err)
		return
	}
	if subject.Source == "" {
		err = fmt.Errorf("header '%s' is missing the 'source' field", key)
		return
	}
	if subject.User == "" {
		err = fmt.Errorf("header '%s' is missing the 'user' field", key)
		return
	}
	result = subject
	return
}

func (i *GrpcExternalAuthInterceptor) isPublicMethod(method string) bool {
	for _, publicMethod := range i.publicMethods {
		if publicMethod.MatchString(method) {
			return true
		}
	}
	return false
}

// grpcExternalAuthInternalError is the error returned an internal error is detected that pevents completing the
// authenentication and authorization process. Note that this intentially hides the details of the error to avoid
// pontentially sesnsitive information. The details will be written to the log.
var grpcExternalAuthInternalError = grpcstatus.Errorf(grpccodes.Internal, "failed to check permissions")
