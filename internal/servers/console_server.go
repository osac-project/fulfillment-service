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
	"io"
	"log/slog"
	"net"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/console"
	"github.com/osac-project/fulfillment-service/internal/database"
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

// ConsoleServerBuilder builds a ConsoleServer.
type ConsoleServerBuilder struct {
	logger           *slog.Logger
	manager          *console.Manager
	resolver         *ConsoleTargetResolver
	ciServer         privatev1.ComputeInstancesServer
	hubServer        privatev1.HubsServer
	txManager        database.TxManager
	hubClientFactory HubClientFactory
	scheme           *runtime.Scheme
}

// consoleServer implements the Console gRPC service.
type consoleServer struct {
	publicv1.UnimplementedConsoleServer
	logger   *slog.Logger
	manager  *console.Manager
	resolver *ConsoleTargetResolver
}

// NewConsoleServer creates a new builder for the console server.
func NewConsoleServer() *ConsoleServerBuilder {
	return &ConsoleServerBuilder{}
}

func (b *ConsoleServerBuilder) SetLogger(value *slog.Logger) *ConsoleServerBuilder {
	b.logger = value
	return b
}

func (b *ConsoleServerBuilder) SetManager(value *console.Manager) *ConsoleServerBuilder {
	b.manager = value
	return b
}

func (b *ConsoleServerBuilder) SetResolver(value *ConsoleTargetResolver) *ConsoleServerBuilder {
	b.resolver = value
	return b
}

func (b *ConsoleServerBuilder) SetComputeInstancesServer(value privatev1.ComputeInstancesServer) *ConsoleServerBuilder {
	b.ciServer = value
	return b
}

func (b *ConsoleServerBuilder) SetHubServer(value privatev1.HubsServer) *ConsoleServerBuilder {
	b.hubServer = value
	return b
}

func (b *ConsoleServerBuilder) SetHubClientFactory(value HubClientFactory) *ConsoleServerBuilder {
	b.hubClientFactory = value
	return b
}

func (b *ConsoleServerBuilder) SetTxManager(value database.TxManager) *ConsoleServerBuilder {
	b.txManager = value
	return b
}

func (b *ConsoleServerBuilder) SetScheme(value *runtime.Scheme) *ConsoleServerBuilder {
	b.scheme = value
	return b
}

func (b *ConsoleServerBuilder) Build() (publicv1.ConsoleServer, error) {
	if b.logger == nil {
		return nil, errors.New("logger is mandatory")
	}
	if b.manager == nil {
		return nil, errors.New("manager is mandatory")
	}

	// If a resolver is provided, use it directly. Otherwise, build one from the
	// individual dependencies (backwards compatible with existing callers).
	resolver := b.resolver
	if resolver == nil {
		if b.ciServer == nil {
			return nil, errors.New("compute instances server is mandatory")
		}
		if b.hubServer == nil {
			return nil, errors.New("hubs server is mandatory")
		}
		if b.txManager == nil {
			return nil, errors.New("transaction manager is mandatory")
		}
		if b.scheme == nil {
			return nil, errors.New("scheme is mandatory")
		}
		hubClientFactory := b.hubClientFactory
		if hubClientFactory == nil {
			hubClientFactory = NewDefaultHubClientFactory(b.scheme)
		}
		var err error
		resolver, err = NewConsoleTargetResolver().
			SetLogger(b.logger).
			SetComputeInstanceLookup(NewPrivateServerCILookup(b.ciServer)).
			SetHubLookup(NewPrivateServerHubLookup(b.hubServer)).
			SetHubClientFactory(hubClientFactory).
			SetTxManager(b.txManager).
			Build()
		if err != nil {
			return nil, fmt.Errorf("failed to build console target resolver: %w", err)
		}
	}

	return &consoleServer{
		logger:   b.logger,
		manager:  b.manager,
		resolver: resolver,
	}, nil
}

// Connect handles bidirectional console streaming.
func (s *consoleServer) Connect(stream publicv1.Console_ConnectServer) error {
	ctx := stream.Context()

	// Receive the init message.
	req, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "failed to receive init message: %v", err)
	}

	init := req.GetInit()
	if init == nil {
		return status.Error(codes.InvalidArgument, "first message must be ConsoleConnectInit")
	}

	resourceType := init.GetResourceType()
	resourceID := init.GetResourceId()
	clientID := init.GetClientId()
	consoleType := init.GetType()

	var targetConsoleType string
	switch consoleType {
	case publicv1.ConsoleType_CONSOLE_TYPE_SERIAL:
		targetConsoleType = console.ConsoleTypeSerial
	case publicv1.ConsoleType_CONSOLE_TYPE_VNC:
		targetConsoleType = console.ConsoleTypeVNC
	default:
		return status.Errorf(codes.InvalidArgument, "unsupported console type: %s", consoleType.String())
	}

	s.logger.InfoContext(ctx, "Console connect request",
		slog.String("resource_type", resourceType.String()),
		slog.String("resource_id", resourceID),
		slog.String("console_type", consoleType.String()),
		slog.String("client_id", clientID),
	)

	// Resolve the resource to a target.
	target, err := s.resolveTarget(ctx, resourceType, resourceID)
	if err != nil {
		return err
	}
	target.ConsoleType = targetConsoleType

	// Get user identity for session tracking and audit.
	subject := auth.SubjectFromContext(ctx)
	user := subject.User

	// Send connecting status.
	err = stream.Send(publicv1.ConsoleConnectResponse_builder{
		Status: publicv1.ConsoleStatus_builder{
			State:   publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_CONNECTING,
			Message: fmt.Sprintf("Connecting to %s...", resourceID),
		}.Build(),
	}.Build())
	if err != nil {
		return status.Errorf(codes.Internal, "failed to send status: %v", err)
	}

	// Open the backend connection.
	conn, err := s.manager.Connect(ctx, *target, user, clientID)
	if err != nil {
		var sessionErr *console.ErrSessionExists
		if errors.As(err, &sessionErr) {
			return status.Errorf(codes.FailedPrecondition, "%v", sessionErr)
		}
		s.logger.ErrorContext(ctx, "Failed to open console backend connection",
			slog.String("resource_type", resourceType.String()),
			slog.String("resource_id", resourceID),
			slog.String("hub", target.HubID),
			slog.String("namespace", target.Namespace),
			slog.String("compute_instance", target.CRName),
			slog.Any("error", err),
		)
		return status.Errorf(codes.Internal, "failed to connect: %v", err)
	}
	defer conn.Close()

	// Send connected status.
	err = stream.Send(publicv1.ConsoleConnectResponse_builder{
		Status: publicv1.ConsoleStatus_builder{
			State:   publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_CONNECTED,
			Message: fmt.Sprintf("Connected to %s", resourceID),
		}.Build(),
	}.Build())
	if err != nil {
		return status.Errorf(codes.Internal, "failed to send status: %v", err)
	}

	// Proxy bidirectionally.
	return s.proxy(ctx, stream, conn)
}

// proxy handles bidirectional data transfer between the gRPC stream and the backend connection.
func (s *consoleServer) proxy(ctx context.Context, stream publicv1.Console_ConnectServer, conn io.ReadWriteCloser) error {
	errCh := make(chan error, 2)

	// Backend -> client: read from backend, send to gRPC stream.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				sendErr := stream.Send(publicv1.ConsoleConnectResponse_builder{
					Output: publicv1.ConsoleOutput_builder{
						Data: append([]byte(nil), buf[:n]...),
					}.Build(),
				}.Build())
				if sendErr != nil {
					errCh <- fmt.Errorf("send to client: %w", sendErr)
					return
				}
			}
			if err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled) {
					errCh <- nil
				} else {
					errCh <- fmt.Errorf("read from backend: %w", err)
				}
				return
			}
		}
	}()

	// Client -> backend: read from gRPC stream, write to backend.
	go func() {
		for {
			req, err := stream.Recv()
			if err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || errors.Is(err, context.Canceled) {
					errCh <- nil
				} else {
					errCh <- fmt.Errorf("recv from client: %w", err)
				}
				return
			}

			if input := req.GetInput(); input != nil {
				data := input.GetData()
				if len(data) > 0 {
					_, writeErr := conn.Write(data)
					if writeErr != nil {
						if errors.Is(writeErr, net.ErrClosed) {
							errCh <- nil
						} else {
							errCh <- fmt.Errorf("write to backend: %w", writeErr)
						}
						return
					}
				}
			}
			// ConsoleResize is a no-op for serial console.
		}
	}()

	// Close the backend connection when context expires (e.g., session timeout)
	// or when the proxy returns. This unblocks the read goroutine which would
	// otherwise hang forever.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()

	// Wait for either direction to finish.
	select {
	case err := <-errCh:
		if err != nil && ctx.Err() == nil {
			s.logger.WarnContext(ctx, "Console proxy error",
				slog.Any("error", err),
			)
			return err
		}
		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				s.logger.InfoContext(ctx, "Console session timed out")
				// Send a status message before returning, best-effort.
				_ = stream.Send(publicv1.ConsoleConnectResponse_builder{
					Status: publicv1.ConsoleStatus_builder{
						State:   publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_DISCONNECTED,
						Message: "Session timed out",
					}.Build(),
				}.Build())
			} else {
				s.logger.InfoContext(ctx, "Console session ended",
					slog.String("reason", ctx.Err().Error()),
				)
			}
		}
		return nil
	}
}

// resolveTarget resolves a resource type and ID to a console.Target.
func (s *consoleServer) resolveTarget(ctx context.Context, resourceType publicv1.ConsoleResourceType, resourceID string) (*console.Target, error) {
	switch resourceType {
	case publicv1.ConsoleResourceType_CONSOLE_RESOURCE_TYPE_COMPUTE_INSTANCE:
		return s.resolver.ResolveComputeInstance(ctx, resourceID)
	default:
		return nil, status.Errorf(codes.Unimplemented, "unsupported resource type %q", resourceType.String())
	}
}

// GetAccess checks console availability for a resource.
func (s *consoleServer) GetAccess(ctx context.Context, req *publicv1.ConsoleGetAccessRequest) (*publicv1.ConsoleGetAccessResponse, error) {
	resourceType := req.GetResourceType()
	resourceID := req.GetResourceId()

	switch resourceType {
	case publicv1.ConsoleResourceType_CONSOLE_RESOURCE_TYPE_COMPUTE_INSTANCE:
		_, err := s.resolver.ResolveComputeInstance(ctx, resourceID)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				return publicv1.ConsoleGetAccessResponse_builder{
					Available: false,
					Reason:    st.Message(),
				}.Build(), nil
			}
			return nil, err
		}
		return publicv1.ConsoleGetAccessResponse_builder{
			Available: true,
			SupportedTypes: []publicv1.ConsoleType{
				publicv1.ConsoleType_CONSOLE_TYPE_SERIAL,
				publicv1.ConsoleType_CONSOLE_TYPE_VNC,
			},
		}.Build(), nil
	default:
		return publicv1.ConsoleGetAccessResponse_builder{
			Available: false,
			Reason:    fmt.Sprintf("unsupported resource type: %s", resourceType.String()),
		}.Build(), nil
	}
}
