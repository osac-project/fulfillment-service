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
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/net/websocket"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/console"
)

// consoleTypeMap maps URL console type segments to internal console type constants.
// Adding a new console type (e.g. serial) requires only one entry here.
var consoleTypeMap = map[string]string{
	"vnc": console.ConsoleTypeVNC,
}

// ConsoleWebSocketHandlerBuilder builds a ConsoleWebSocketHandler.
type ConsoleWebSocketHandlerBuilder struct {
	logger   *slog.Logger
	auth     auth.WebSocketAuthenticator
	resolver *ConsoleTargetResolver
	manager  *console.Manager
}

// ConsoleWebSocketHandler serves WebSocket console connections. It performs authentication,
// resource resolution, and backend connection before upgrading to WebSocket, so that failures
// return proper HTTP error responses instead of WebSocket close frames.
type ConsoleWebSocketHandler struct {
	logger   *slog.Logger
	auth     auth.WebSocketAuthenticator
	resolver *ConsoleTargetResolver
	manager  *console.Manager
}

// NewConsoleWebSocketHandler creates a new builder for the console WebSocket handler.
func NewConsoleWebSocketHandler() *ConsoleWebSocketHandlerBuilder {
	return &ConsoleWebSocketHandlerBuilder{}
}

func (b *ConsoleWebSocketHandlerBuilder) SetLogger(value *slog.Logger) *ConsoleWebSocketHandlerBuilder {
	b.logger = value
	return b
}

func (b *ConsoleWebSocketHandlerBuilder) SetAuthenticator(value auth.WebSocketAuthenticator) *ConsoleWebSocketHandlerBuilder {
	b.auth = value
	return b
}

func (b *ConsoleWebSocketHandlerBuilder) SetResolver(value *ConsoleTargetResolver) *ConsoleWebSocketHandlerBuilder {
	b.resolver = value
	return b
}

func (b *ConsoleWebSocketHandlerBuilder) SetManager(value *console.Manager) *ConsoleWebSocketHandlerBuilder {
	b.manager = value
	return b
}

func (b *ConsoleWebSocketHandlerBuilder) Build() (*ConsoleWebSocketHandler, error) {
	if b.logger == nil {
		return nil, errors.New("logger is mandatory")
	}
	if b.auth == nil {
		return nil, errors.New("authenticator is mandatory")
	}
	if b.resolver == nil {
		return nil, errors.New("resolver is mandatory")
	}
	if b.manager == nil {
		return nil, errors.New("manager is mandatory")
	}
	return &ConsoleWebSocketHandler{
		logger:   b.logger,
		auth:     b.auth,
		resolver: b.resolver,
		manager:  b.manager,
	}, nil
}

// ServeHTTP handles console WebSocket requests. All fallible operations (auth, resolve, backend
// connect) happen before the WebSocket upgrade so failures return proper HTTP error codes.
//
// URL pattern: /api/osac/public/v1/console/compute_instance/{resource_id}/connect/{console_type}
func (h *ConsoleWebSocketHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract path parameters. Go 1.22+ net/http pattern matching puts them in the request.
	resourceID := r.PathValue("resource_id")
	consoleTypeStr := r.PathValue("console_type")

	// Validate console type.
	internalConsoleType, ok := consoleTypeMap[consoleTypeStr]
	if !ok {
		http.Error(w, fmt.Sprintf("unsupported console type: %s", consoleTypeStr), http.StatusBadRequest)
		return
	}

	// Extract optional client ID from query params.
	clientID := r.URL.Query().Get("client_id")

	// Step 1: Authenticate.
	ctx, err := h.auth.Authenticate(ctx, r, resourceID)
	if err != nil {
		h.writeGrpcError(w, err)
		return
	}
	subject := auth.SubjectFromContext(ctx)

	// Step 2: Resolve the target.
	target, err := h.resolver.ResolveComputeInstance(ctx, resourceID)
	if err != nil {
		h.writeGrpcError(w, err)
		return
	}
	target.ConsoleType = internalConsoleType

	// Step 3: Open backend connection (reserves the session in the Manager).
	conn, err := h.manager.Connect(ctx, *target, subject.User, clientID)
	if err != nil {
		var sessionErr *console.ErrSessionExists
		if errors.As(err, &sessionErr) {
			h.writeGrpcError(w, status.Errorf(codes.FailedPrecondition, "%v", sessionErr))
			return
		}
		h.logger.ErrorContext(ctx, "Failed to open console backend connection",
			slog.String("resource_id", resourceID),
			slog.String("hub", target.HubID),
			slog.String("namespace", target.Namespace),
			slog.String("compute_instance", target.CRName),
			slog.Any("error", err),
		)
		h.writeGrpcError(w, status.Errorf(codes.Internal, "failed to connect: %v", err))
		return
	}

	// Wrap backend connection for ownership transfer. If the WebSocket upgrade fails,
	// the deferred CloseIfNotTaken() releases the session.
	pending := &pendingConsoleConn{conn: conn}
	defer pending.CloseIfNotTaken()

	// Step 4: Upgrade to WebSocket and relay.
	wsServer := websocket.Server{
		Handshake: func(config *websocket.Config, r *http.Request) error {
			// Accept the "binary" subprotocol (noVNC sends Sec-WebSocket-Protocol: binary).
			for _, proto := range config.Protocol {
				if strings.EqualFold(proto, "binary") {
					config.Protocol = []string{"binary"}
					return nil
				}
			}
			// Accept without subprotocol for generic clients.
			config.Protocol = nil
			return nil
		},
		Handler: func(ws *websocket.Conn) {
			// Take ownership of the backend connection.
			backendConn := pending.Take()
			defer backendConn.Close()

			ws.PayloadType = websocket.BinaryFrame
			h.relay(ws, backendConn)
		},
	}
	wsServer.ServeHTTP(w, r)
}

// relay copies data bidirectionally between the WebSocket and backend connections.
func (h *ConsoleWebSocketHandler) relay(ws *websocket.Conn, backend io.ReadWriteCloser) {
	errCh := make(chan error, 2)

	// Backend -> WebSocket.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := backend.Read(buf)
			if n > 0 {
				_, writeErr := ws.Write(buf[:n])
				if writeErr != nil {
					errCh <- fmt.Errorf("write to websocket: %w", writeErr)
					return
				}
			}
			if err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
					errCh <- nil
				} else {
					errCh <- fmt.Errorf("read from backend: %w", err)
				}
				return
			}
		}
	}()

	// WebSocket -> Backend.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ws.Read(buf)
			if n > 0 {
				_, writeErr := backend.Write(buf[:n])
				if writeErr != nil {
					if errors.Is(writeErr, net.ErrClosed) {
						errCh <- nil
					} else {
						errCh <- fmt.Errorf("write to backend: %w", writeErr)
					}
					return
				}
			}
			if err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
					errCh <- nil
				} else {
					errCh <- fmt.Errorf("read from websocket: %w", err)
				}
				return
			}
		}
	}()

	// Wait for either direction to finish.
	err := <-errCh
	if err != nil {
		h.logger.Warn("Console WebSocket relay error",
			slog.Any("error", err),
		)
	}
}

// writeGrpcError maps a gRPC status error to an HTTP response code and writes it.
func (h *ConsoleWebSocketHandler) writeGrpcError(w http.ResponseWriter, err error) {
	st, ok := status.FromError(err)
	if !ok {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Error(w, st.Message(), grpcStatusToHTTP(st.Code()))
}

// grpcStatusToHTTP maps gRPC status codes to HTTP status codes.
func grpcStatusToHTTP(code codes.Code) int {
	switch code {
	case codes.NotFound:
		return http.StatusNotFound
	case codes.PermissionDenied:
		return http.StatusForbidden
	case codes.Unauthenticated:
		return http.StatusUnauthorized
	case codes.FailedPrecondition:
		return http.StatusConflict
	case codes.AlreadyExists:
		return http.StatusConflict
	case codes.InvalidArgument:
		return http.StatusBadRequest
	case codes.Internal:
		return http.StatusInternalServerError
	case codes.Unavailable:
		return http.StatusServiceUnavailable
	case codes.DeadlineExceeded:
		return http.StatusGatewayTimeout
	default:
		return http.StatusBadGateway
	}
}

// pendingConsoleConn wraps a backend connection for explicit ownership transfer. If the
// WebSocket upgrade fails after Manager.Connect() succeeds, CloseIfNotTaken() releases
// the session to prevent stranding.
type pendingConsoleConn struct {
	conn  io.ReadWriteCloser
	taken bool
}

func (p *pendingConsoleConn) Take() io.ReadWriteCloser {
	p.taken = true
	return p.conn
}

func (p *pendingConsoleConn) CloseIfNotTaken() {
	if !p.taken && p.conn != nil {
		_ = p.conn.Close()
	}
}

// consolePanicRecovery wraps an http.Handler with panic recovery, logging the panic
// and returning HTTP 500. Same role as panicInterceptor in the gRPC chain.
func ConsolePanicRecovery(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("Console handler panicked",
					slog.Any("panic", rec),
					slog.String("stack", string(debug.Stack())),
				)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// consoleLogging wraps an http.Handler with structured request logging.
// It logs the sanitized path (without query string) to avoid token exposure.
func ConsoleLogging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Sanitize the path to strip query parameters (may contain token).
		cleanPath := r.URL.Path

		logger.Info("Console request started",
			slog.String("method", r.Method),
			slog.String("path", cleanPath),
			slog.String("remote_addr", r.RemoteAddr),
		)

		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)

		logger.Info("Console request completed",
			slog.String("method", r.Method),
			slog.String("path", cleanPath),
			slog.Int("status", sw.status),
			slog.Duration("duration", time.Since(start)),
		)
	})
}

// statusWriter captures the HTTP status code written by the handler.
type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.wroteHeader = true
	}
	return w.ResponseWriter.Write(b)
}

// consoleMetrics wraps an http.Handler with Prometheus connection metrics.
type consoleMetricsMiddleware struct {
	next          http.Handler
	connectTotal  *prometheus.CounterVec
	activeConns   *prometheus.GaugeVec
	connDuration  *prometheus.HistogramVec
}

// ConsoleMetrics creates a middleware that records console WebSocket connection metrics.
func ConsoleMetrics(registerer prometheus.Registerer, next http.Handler) http.Handler {
	connectTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: "console_websocket",
			Name:      "connections_total",
			Help:      "Total number of console WebSocket connections.",
		},
		[]string{"console_type", "status"},
	)
	activeConns := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: "console_websocket",
			Name:      "active_connections",
			Help:      "Number of active console WebSocket connections.",
		},
		[]string{"console_type"},
	)
	connDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: "console_websocket",
			Name:      "connection_duration_seconds",
			Help:      "Duration of console WebSocket connections.",
			Buckets:   prometheus.ExponentialBuckets(1, 2, 15),
		},
		[]string{"console_type"},
	)

	registerer.MustRegister(connectTotal, activeConns, connDuration)

	return &consoleMetricsMiddleware{
		next:         next,
		connectTotal: connectTotal,
		activeConns:  activeConns,
		connDuration: connDuration,
	}
}

func (m *consoleMetricsMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	consoleType := r.PathValue("console_type")
	if consoleType == "" {
		consoleType = "unknown"
	}

	start := time.Now()
	m.activeConns.WithLabelValues(consoleType).Inc()
	defer func() {
		m.activeConns.WithLabelValues(consoleType).Dec()
		m.connDuration.WithLabelValues(consoleType).Observe(time.Since(start).Seconds())
	}()

	sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
	m.next.ServeHTTP(sw, r)

	resultStatus := "success"
	if sw.status >= 400 {
		resultStatus = "error"
	}
	m.connectTotal.WithLabelValues(consoleType, resultStatus).Inc()
}
