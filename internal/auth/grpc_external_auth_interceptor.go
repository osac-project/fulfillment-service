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
	"errors"
	"log/slog"
	"net/http"
	"regexp"

	envoyauthv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

// GrpcExternalAuthType is the name of the external authentication type.
const GrpcExternalAuthType = "external"

// GrpcExternalAuthInterceptorBuilder contains the data and logic needed to build an interceptor that performs
// authentication and authorization by calling an external service using the Envoy ext_authz gRPC protocol.
type GrpcExternalAuthInterceptorBuilder struct {
	logger        *slog.Logger
	grpcClient    *grpc.ClientConn
	publicMethods []string
	checker       *ExternalAuthChecker
}

// GrpcExternalAuthInterceptor is an interceptor that performs authentication and authorization by calling an external
// service using the Envoy ext_authz gRPC protocol. This interceptor does not require a prior authentication
// interceptor in the chain. Instead, the external auth service is expected to provide subject details (user, groups)
// in its response headers when authorization is granted. These details are then added to the context for use by
// downstream handlers. The interceptor also has access to the request message, allowing it to include object-specific
// details (like identifiers and tenants) in the authorization request.
type GrpcExternalAuthInterceptor struct {
	logger        *slog.Logger
	checker       *ExternalAuthChecker
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

// SetGrpcClient sets the gRPC client that will be used to communicate with the external auth service. This is
// mandatory.
func (b *GrpcExternalAuthInterceptorBuilder) SetGrpcClient(value *grpc.ClientConn) *GrpcExternalAuthInterceptorBuilder {
	b.grpcClient = value
	return b
}

// SetChecker sets a pre-built ExternalAuthChecker. When set, the interceptor will use it instead of creating one
// from the gRPC client. This allows sharing a single checker instance between the gRPC interceptor and other
// authentication paths (e.g. WebSocket).
func (b *GrpcExternalAuthInterceptorBuilder) SetChecker(value *ExternalAuthChecker) *GrpcExternalAuthInterceptorBuilder {
	b.checker = value
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

// Build uses the data stored in the builder to create and configure a new interceptor.
func (b *GrpcExternalAuthInterceptorBuilder) Build() (result *GrpcExternalAuthInterceptor, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.grpcClient == nil && b.checker == nil {
		err = errors.New("gRPC client is mandatory")
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

	// Create or reuse the external auth checker:
	checker := b.checker
	if checker == nil {
		checker, err = NewExternalAuthChecker().
			SetLogger(b.logger).
			SetAuthClient(envoyauthv3.NewAuthorizationClient(b.grpcClient)).
			Build()
		if err != nil {
			return
		}
	}

	// Create and populate the object:
	result = &GrpcExternalAuthInterceptor{
		logger:        b.logger,
		checker:       checker,
		publicMethods: publicMethods,
	}
	return
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
	ctx, err = i.check(ctx, info.FullMethod, request)
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
	ctx, err := i.check(stream.Context(), info.FullMethod, nil)
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
// returns a new context containing the subject details extracted from the response. The grpcRequest parameter is the
// incoming gRPC request message, which may be nil for streaming RPCs.
func (i *GrpcExternalAuthInterceptor) check(ctx context.Context, method string,
	grpcRequest any) (context.Context, error) {
	// Extract headers from the incoming gRPC metadata:
	headers := map[string]string{}
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		for key, values := range md {
			if len(values) > 0 {
				headers[key] = values[0]
			}
		}
	}

	// Try to extract the object identifier from the request so that the external authorization service can use
	// it to make fine grained authorization decisions. The identifier is sent in the context extensions of the
	// check request. For example, Authorino exposes it to OPA policies as `input.context.context_extensions.id`.
	extensions := map[string]string{}
	if grpcRequest != nil {
		id := i.extractId(ctx, grpcRequest)
		if id != "" {
			extensions["id"] = id
		}
	}

	// Delegate the actual ext-auth check to the shared checker:
	return i.checker.Check(ctx, CheckParams{
		Method:            http.MethodPost,
		Path:              method,
		Headers:           headers,
		ContextExtensions: extensions,
	})
}

// extractId tries to extract the identifier of the object from the incoming request message. For get and delete
// requests, the identifier is directly available via the 'GetId' method. For update requests, the identifier is inside
// the 'object' field, which is accessed via protobuf reflection.
func (i *GrpcExternalAuthInterceptor) extractId(ctx context.Context, request any) string {
	// First try to get the identifier directly from the request. This works for any request message that has a
	// 'GetId' method, including get and delete requests.
	type idGetter interface {
		GetId() string
	}
	getter, ok := request.(idGetter)
	if ok {
		return getter.GetId()
	}

	// If the request doesn't have a direct identifier, try to extract it from the nested 'object' field using
	// protobuf reflection. This is necessary for update requests, for example, where the identifier is inside
	// the object.
	message, ok := request.(proto.Message)
	if !ok {
		return ""
	}
	reflect := message.ProtoReflect()
	field := reflect.Descriptor().Fields().ByName("object")
	if field == nil {
		return ""
	}
	if !reflect.Has(field) {
		return ""
	}
	value := reflect.Get(field)
	getter, ok = value.Message().Interface().(idGetter)
	if !ok {
		return ""
	}
	return getter.GetId()
}

func (i *GrpcExternalAuthInterceptor) isPublicMethod(method string) bool {
	for _, publicMethod := range i.publicMethods {
		if publicMethod.MatchString(method) {
			return true
		}
	}
	return false
}
