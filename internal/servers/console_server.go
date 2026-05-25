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
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/console"
)

// HubClientFactory creates a Kubernetes client from raw kubeconfig bytes.
type HubClientFactory func(kubeconfig []byte) (clnt.Client, error)

// NewDefaultHubClientFactory creates a HubClientFactory that uses the given scheme
// to create real Kubernetes clients from kubeconfig bytes.
func NewDefaultHubClientFactory(scheme *runtime.Scheme) HubClientFactory {
	return func(kubeconfig []byte) (clnt.Client, error) {
		config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
		}
		return clnt.New(config, clnt.Options{Scheme: scheme})
	}
}

// ConsoleServerBuilder builds a ConsoleSessionsServer.
type ConsoleServerBuilder struct {
	logger   *slog.Logger
	sealer   *console.TicketSealer
	resolver *ConsoleTargetResolver
}

// consoleSessionsServer implements the ConsoleSessions gRPC service.
type consoleSessionsServer struct {
	publicv1.UnimplementedConsoleSessionsServer
	logger   *slog.Logger
	sealer   *console.TicketSealer
	resolver *ConsoleTargetResolver
}

// NewConsoleServer creates a new builder for the console sessions server.
func NewConsoleServer() *ConsoleServerBuilder {
	return &ConsoleServerBuilder{}
}

func (b *ConsoleServerBuilder) SetLogger(value *slog.Logger) *ConsoleServerBuilder {
	b.logger = value
	return b
}

func (b *ConsoleServerBuilder) SetSealer(value *console.TicketSealer) *ConsoleServerBuilder {
	b.sealer = value
	return b
}

func (b *ConsoleServerBuilder) SetResolver(value *ConsoleTargetResolver) *ConsoleServerBuilder {
	b.resolver = value
	return b
}

func (b *ConsoleServerBuilder) Build() (publicv1.ConsoleSessionsServer, error) {
	if b.logger == nil {
		return nil, errors.New("logger is mandatory")
	}
	if b.sealer == nil {
		return nil, errors.New("sealer is mandatory")
	}

	if b.resolver == nil {
		return nil, errors.New("resolver is mandatory")
	}

	return &consoleSessionsServer{
		logger:   b.logger,
		sealer:   b.sealer,
		resolver: b.resolver,
	}, nil
}

// defaultTicketTTL is the TTL for console session tickets.
const defaultTicketTTL = 30 * time.Second

// Create creates a signed JWT ticket for console access. Resolves the
// compute instance, fetches the hub kubeconfig, and embeds the pre-computed
// backend WebSocket URL and token in the encrypted ticket.
func (s *consoleSessionsServer) Create(ctx context.Context,
	req *publicv1.ConsoleSessionsCreateRequest) (response *publicv1.ConsoleSessionsCreateResponse, err error) {
	resourceType := req.GetResourceType()
	resourceID := req.GetResourceId()

	if resourceID == "" {
		err = status.Errorf(codes.InvalidArgument, "field 'resource_id' is mandatory")
		return
	}

	if resourceType != publicv1.ConsoleResourceType_CONSOLE_RESOURCE_TYPE_COMPUTE_INSTANCE {
		err = status.Errorf(codes.Unimplemented, "unsupported resource type: %s", resourceType.String())
		return
	}

	consoleType := mapConsoleType(req.GetType())
	if consoleType == "" {
		err = status.Errorf(codes.InvalidArgument, "unsupported console type: %s", req.GetType().String())
		return
	}

	clientID := req.GetClientId()
	if clientID != "" {
		if _, err = uuid.Parse(clientID); err != nil {
			err = status.Errorf(codes.InvalidArgument, "client_id must be a valid UUID or empty")
			return
		}
	}

	// Full resolution: CI exists, running, hub assigned, CR found on hub,
	// backend URI and token pre-computed.
	target, resolveErr := s.resolver.ResolveComputeInstance(ctx, resourceID, consoleType)
	if resolveErr != nil {
		err = resolveErr
		return
	}

	subject := auth.SubjectFromContext(ctx)

	ticket := &console.Ticket{
		TargetURI:   target.BackendURI,
		TargetToken: target.BackendToken,
	}
	tokenString, expiresAt, signErr := s.sealer.Seal(ticket, subject.User, clientID, consoleType, defaultTicketTTL)
	if signErr != nil {
		err = status.Errorf(codes.Internal, "failed to seal ticket: %v", signErr)
		return
	}

	s.logger.InfoContext(ctx, "Console session ticket created",
		slog.String("resource_id", resourceID),
		slog.String("user", subject.User),
		slog.String("console_type", consoleType),
	)

	response = publicv1.ConsoleSessionsCreateResponse_builder{
		Ticket:    tokenString,
		ExpiresAt: timestamppb.New(expiresAt),
	}.Build()
	return
}

// mapConsoleType maps proto ConsoleType to internal console type string.
func mapConsoleType(ct publicv1.ConsoleType) string {
	switch ct {
	case publicv1.ConsoleType_CONSOLE_TYPE_SERIAL:
		return console.ConsoleTypeSerial
	case publicv1.ConsoleType_CONSOLE_TYPE_VNC:
		return console.ConsoleTypeVNC
	default:
		return ""
	}
}
