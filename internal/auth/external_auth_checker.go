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
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	envoycorev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoyauthv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

// CheckParams are the transport-independent inputs to an ext-auth check. Both the gRPC interceptor
// and the WebSocket authenticator build these from their respective request formats, then delegate
// to ExternalAuthChecker.Check.
type CheckParams struct {
	// Method is the HTTP method to present to the auth service. Always POST for gRPC-style checks.
	Method string

	// Path is the gRPC method path, e.g. "/osac.public.v1.Console/Connect".
	Path string

	// Headers are the request headers to forward (e.g. Authorization).
	Headers map[string]string

	// ContextExtensions are additional key-value pairs for the auth check
	// (e.g. "id" -> resourceID).
	ContextExtensions map[string]string
}

// ExternalAuthCheckerBuilder builds an ExternalAuthChecker.
type ExternalAuthCheckerBuilder struct {
	logger     *slog.Logger
	authClient envoyauthv3.AuthorizationClient
}

// ExternalAuthChecker performs ext_authz checks against an authorization service. It encapsulates
// the full check flow: building the CheckRequest, calling authClient.Check(), and parsing the
// response into a Subject. Both the gRPC interceptor and the WebSocket authenticator use this
// to avoid duplicating the request construction logic.
type ExternalAuthChecker struct {
	logger     *slog.Logger
	authClient envoyauthv3.AuthorizationClient
}

// NewExternalAuthChecker creates a new builder for the external auth checker.
func NewExternalAuthChecker() *ExternalAuthCheckerBuilder {
	return &ExternalAuthCheckerBuilder{}
}

func (b *ExternalAuthCheckerBuilder) SetLogger(value *slog.Logger) *ExternalAuthCheckerBuilder {
	b.logger = value
	return b
}

func (b *ExternalAuthCheckerBuilder) SetAuthClient(value envoyauthv3.AuthorizationClient) *ExternalAuthCheckerBuilder {
	b.authClient = value
	return b
}

func (b *ExternalAuthCheckerBuilder) Build() (*ExternalAuthChecker, error) {
	if b.logger == nil {
		return nil, errors.New("logger is mandatory")
	}
	if b.authClient == nil {
		return nil, errors.New("auth client is mandatory")
	}
	return &ExternalAuthChecker{
		logger:     b.logger,
		authClient: b.authClient,
	}, nil
}

// Check performs the ext-auth check and returns a context with the Subject injected, or an error.
// The returned errors are gRPC status errors: PermissionDenied when the auth service denies access,
// Internal when the check cannot be completed.
func (c *ExternalAuthChecker) Check(ctx context.Context, params CheckParams) (context.Context, error) {
	logger := c.logger.With(
		slog.String("method", params.Path),
	)

	// Build the check request:
	request := c.buildCheckRequest(params)
	logger = logger.With(
		slog.Any("request", request),
	)

	// Call the external service:
	logger.DebugContext(ctx, "Sending check request to external service")
	response, err := c.authClient.Check(ctx, request)
	if err != nil {
		logger.ErrorContext(ctx, "Failed sending check request to external service",
			slog.Any("error", err),
		)
		return ctx, externalAuthInternalError
	}
	logger = logger.With(
		slog.Any("response", response),
	)
	logger.DebugContext(ctx, "Received check response from external service")

	// Check the response and extract subject details:
	return c.handleCheckResponse(ctx, logger, response)
}

func (c *ExternalAuthChecker) buildCheckRequest(params CheckParams) *envoyauthv3.CheckRequest {
	return &envoyauthv3.CheckRequest{
		Attributes: &envoyauthv3.AttributeContext{
			Request: &envoyauthv3.AttributeContext_Request{
				Http: &envoyauthv3.AttributeContext_HttpRequest{
					Method:  params.Method,
					Path:    params.Path,
					Headers: params.Headers,
				},
			},
			ContextExtensions: params.ContextExtensions,
		},
	}
}

// handleCheckResponse processes the response from the external auth service. If granted, it
// extracts subject details from the response headers and returns a new context containing
// the subject.
func (c *ExternalAuthChecker) handleCheckResponse(ctx context.Context, logger *slog.Logger,
	response *envoyauthv3.CheckResponse) (context.Context, error) {
	// Deny access if there is no response or no status:
	if response == nil {
		logger.ErrorContext(ctx, "Permission denied because the response is nil")
		return ctx, externalAuthInternalError
	}
	status := response.GetStatus()
	if status == nil {
		logger.ErrorContext(ctx, "Permission denied because there is no status in the response")
		return ctx, externalAuthInternalError
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
		return ctx, grpcstatus.Errorf(code, "permission denied")
	}

	// Try to extract the subject from the response. If this fails then deny access.
	subject, err := subjectFromCheckResponse(response)
	if err != nil {
		logger.ErrorContext(ctx, "Failed to extract subject from response",
			slog.Any("error", err),
		)
		return ctx, externalAuthInternalError
	}

	logger.DebugContext(ctx, "Permission granted by external service")
	return ContextWithSubject(ctx, subject), nil
}

// subjectFromCheckResponse extracts the Subject from an OK check response's headers.
func subjectFromCheckResponse(response *envoyauthv3.CheckResponse) (*Subject, error) {
	accepted := response.GetOkResponse()
	if accepted == nil {
		return nil, errors.New("response doesn't contain headers")
	}
	var subject *Subject
	for _, header := range accepted.GetHeaders() {
		entry := header.GetHeader()
		if entry == nil {
			continue
		}
		if strings.EqualFold(entry.GetKey(), SubjectHeader) {
			var err error
			subject, err = subjectFromCheckHeader(entry)
			if err != nil {
				return nil, err
			}
		}
	}
	if subject == nil {
		return nil, fmt.Errorf("response doesn't contain the '%s' header", SubjectHeader)
	}
	return subject, nil
}

// subjectFromCheckHeader unmarshals a Subject from an ext-auth response header value.
func subjectFromCheckHeader(header *envoycorev3.HeaderValue) (*Subject, error) {
	key := header.GetKey()
	value := header.GetValue()
	subject := &Subject{}
	err := json.Unmarshal([]byte(value), subject)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal subject from header '%s' with value '%s': %w", key, value, err)
	}
	if subject.User == "" {
		return nil, fmt.Errorf("header '%s' is missing the 'user' field", key)
	}
	return subject, nil
}

// externalAuthInternalError is the error returned when an internal error prevents completing the
// authentication and authorization process. This intentionally hides the details of the error to
// avoid potentially sensitive information. The details are written to the log.
var externalAuthInternalError = grpcstatus.Errorf(grpccodes.Internal, "failed to check permissions")
