/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package console

import (
	"context"
	"log/slog"
	"time"

	"github.com/coder/websocket"
)

const (
	pingInterval = 30 * time.Second
	pongTimeout  = 10 * time.Second
)

// Ping sends periodic WebSocket pings to detect dead connections. It blocks
// until the context is cancelled or a ping fails, returning the cause.
func Ping(ctx context.Context, conn *websocket.Conn) error {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, pongTimeout)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return err
			}
		}
	}
}

// StartPing launches a background goroutine that sends periodic pings and logs
// failures at debug level. The goroutine exits when ctx is cancelled or a ping
// fails.
func StartPing(ctx context.Context, conn *websocket.Conn, logger *slog.Logger) {
	go func() {
		if err := Ping(ctx, conn); err != nil && ctx.Err() == nil {
			logger.DebugContext(ctx, "Ping timeout, tearing down connection",
				slog.Any("error", err),
			)
			_ = conn.CloseNow()
		}
	}()
}

// PingReceivedHandler returns an OnPingReceived callback that logs at debug
// level. The returned function has the signature expected by both
// websocket.AcceptOptions and websocket.DialOptions.
func PingReceivedHandler(logger *slog.Logger, ctx context.Context) func(context.Context, []byte) bool {
	return func(_ context.Context, payload []byte) bool {
		logger.DebugContext(ctx, "Ping received",
			slog.String("payload", string(payload)),
		)
		return true
	}
}

// PongReceivedHandler returns an OnPongReceived callback that logs at debug
// level.
func PongReceivedHandler(logger *slog.Logger, ctx context.Context) func(context.Context, []byte) {
	return func(_ context.Context, payload []byte) {
		logger.DebugContext(ctx, "Pong received",
			slog.String("payload", string(payload)),
		)
	}
}
