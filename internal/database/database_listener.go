/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// ListenerPayloadCallback is a function that will be called by the listener when a notification arrives.
type ListenerPayloadCallback func(ctx context.Context, payload proto.Message) error

// ListenerReadyCallback is a function that will be called by the listener when it is actually listening for
// notifications.
type ListenerReadyCallback func(ctx context.Context) error

// ListenerBuilder contains the data and logic needed to build a listener.
type ListenerBuilder struct {
	logger           *slog.Logger
	url              string
	channel          string
	readyCallbacks   []ListenerReadyCallback
	payloadCallbacks []ListenerPayloadCallback
	waitTimeout      time.Duration
	retryInterval    time.Duration
}

// Listener knows how to liste for notifications using PostgreSQL's `listen` mechanism.
type Listener struct {
	logger           *slog.Logger
	url              string
	channel          string
	readyCallbacks   []ListenerReadyCallback
	payloadCallbacks []ListenerPayloadCallback
	waitTimeout      time.Duration
	retryInterval    time.Duration
	conn             *pgx.Conn
}

// NewListener uses the information stored in the builder to create a new listener.
func NewListener() *ListenerBuilder {
	return &ListenerBuilder{
		waitTimeout:   5 * time.Minute,
		retryInterval: 5 * time.Second,
	}
}

// SetLogger sets the logger for the listener. This is mandatory.
func (b *ListenerBuilder) SetLogger(logger *slog.Logger) *ListenerBuilder {
	b.logger = logger
	return b
}

// SetChannel sets the channel name for the listener. This is mandatory.
func (b *ListenerBuilder) SetChannel(value string) *ListenerBuilder {
	b.channel = value
	return b
}

// SetUrl sets the database connection URL. This is mandatory.
func (b *ListenerBuilder) SetUrl(value string) *ListenerBuilder {
	b.url = value
	return b
}

// AddReadyCallback adds a function that will be called by the listener when it is actually listening for notifications.
// Errors returned by these functions will be logged, but the listener will continue working. This is optional.
func (b *ListenerBuilder) AddReadyCallback(value ListenerReadyCallback) *ListenerBuilder {
	b.readyCallbacks = append(b.readyCallbacks, value)
	return b
}

// AddPayloadCallback adds a function that will be called by the listener when a notification arrives. Errors returned
// by these functions will be logged, but the listener will continue working. At least one of these functions is
// mandatory.
func (b *ListenerBuilder) AddPayloadCallback(value ListenerPayloadCallback) *ListenerBuilder {
	b.payloadCallbacks = append(b.payloadCallbacks, value)
	return b
}

// SetWaitTimeout sets the maximum time that the listener will wait for a notification. After that it will close the
// connection and open it again. This is intended to automatically recover from situations where the server or the
// connection malfunction and stop sending the notifications. This is optional and the default is five minutes.
func (b *ListenerBuilder) SetWaitTimeout(value time.Duration) *ListenerBuilder {
	b.waitTimeout = value
	return b
}

// SetRetryInterval sets the time that the listener will wait before trying to open a new connectio and start listening
// after a failure. This is optional and the default is five seconds.
func (b *ListenerBuilder) SetRetryInterval(value time.Duration) *ListenerBuilder {
	b.retryInterval = value
	return b
}

// Build constructs a listener instance using the configured parameters.
func (b *ListenerBuilder) Build() (result *Listener, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.url == "" {
		err = errors.New("database connection URL is mandatory")
		return
	}
	if b.channel == "" {
		err = errors.New("channel is mandatory")
		return
	}
	if len(b.payloadCallbacks) == 0 {
		err = errors.New("at least one payload callback is mandatory")
		return
	}
	if b.waitTimeout <= 0 {
		err = fmt.Errorf("wait timeout should be positive, but it is %s", b.waitTimeout)
		return
	}
	if b.retryInterval <= 0 {
		err = fmt.Errorf("retry interval should be positive, but it is %s", b.retryInterval)
		return
	}

	// Create and populate the object:
	logger := b.logger.With(
		slog.String("channel", b.channel),
	)
	result = &Listener{
		logger:           logger,
		url:              b.url,
		channel:          b.channel,
		readyCallbacks:   slices.Clone(b.readyCallbacks),
		payloadCallbacks: slices.Clone(b.payloadCallbacks),
		waitTimeout:      b.waitTimeout,
		retryInterval:    b.retryInterval,
	}
	return
}

// Listen waits for notifications in the configured channel, decodes the payload and runs the callbacks to process it.
func (l *Listener) Listen(ctx context.Context) error {
	// Always remember to close the database connection:
	defer func() {
		if l.conn != nil {
			err := l.conn.Close(ctx)
			if err != nil {
				l.logger.ErrorContext(
					ctx,
					"Failed to close connection",
					slog.Any("error", err),
				)
			}
		}
	}()

	// Start listening, and call the ready callbacks when we succeed, but only the first time:
	err := l.listenLoop(ctx)
	if err != nil {
		return err
	}
	l.runReadyCallbacks(ctx)

	// Run the loop that waits for notifications and creates new connections in case of failure or timeout:
	for {
		err := l.waitLoop(ctx)
		if err == nil {
			l.logger.DebugContext(ctx, "Wait finishied")
			continue
		}
		if errors.Is(err, context.DeadlineExceeded) {
			l.logger.InfoContext(
				ctx,
				"Wait timeout exceeded",
				slog.Duration("timeout", l.waitTimeout),
			)
			continue
		}
		if errors.Is(err, context.Canceled) {
			l.logger.DebugContext(ctx, "Wait canceled")
			return err
		}
		l.logger.ErrorContext(
			ctx,
			"Wait failed",
			slog.Any("error", err),
		)
		err = l.sleepBeforeRetry(ctx)
		if err != nil {
			return err
		}
		err = l.listenLoop(ctx)
		if err != nil {
			return err
		}
	}
}

// listenLopp runs the `listen ...` SQL statement till it succeeds, dropping and creating the connection again when it
// fails.
func (l *Listener) listenLoop(ctx context.Context) error {
	for {
		err := l.listen(ctx)
		if err == nil {
			l.logger.DebugContext(ctx, "Listen succeeded")
			return nil
		}
		l.logger.ErrorContext(
			ctx,
			"Failed to listen",
			slog.Any("error", err),
		)
		err = l.sleepBeforeRetry(ctx)
		if err != nil {
			return err
		}
	}
}

// listen runs the `listen ...` SQL statement always with a freshly created connection, and closing the old one if it
// is still open.
func (l *Listener) listen(ctx context.Context) error {
	if l.conn != nil {
		err := l.conn.Close(ctx)
		if err != nil {
			l.logger.InfoContext(
				ctx,
				"Failed to close connection",
				slog.Any("error", err),
			)
		}
		l.conn = nil
	}
	conn, err := pgx.Connect(ctx, l.url)
	if err != nil {
		return err
	}
	l.conn = conn
	_, err = l.conn.Exec(ctx, fmt.Sprintf("listen %s", l.channel))
	return err
}

// waitLoop waits for notifications in a loop, while the connection is healthy. It returns when the connection fails or
// the context is canceled.
func (l *Listener) waitLoop(ctx context.Context) error {
	for {
		err := l.wait(ctx)
		if err != nil {
			return err
		}
	}
}

// wait waits for one notification and processes it.
func (l *Listener) wait(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, l.waitTimeout)
	defer cancel()
	notification, err := l.conn.WaitForNotification(ctx)
	if err != nil {
		return err
	}
	l.processNotification(ctx, notification)
	return nil
}

func (l *Listener) processNotification(ctx context.Context, notification *pgconn.Notification) {
	// Write the details of the notification to the log:
	l.logger.DebugContext(
		ctx,
		"Received notification",
		slog.Uint64("pid", uint64(notification.PID)),
		slog.String("payload", notification.Payload),
	)

	// Get the payload from the database:
	id := notification.Payload
	row := l.conn.QueryRow(ctx, "select payload from notifications where id = $1", id)
	var data []byte
	err := row.Scan(&data)
	if err != nil {
		l.logger.ErrorContext(
			ctx,
			"Failed to get payload",
			slog.String("id", id),
			slog.Any("error", err),
		)
		return
	}

	// Unwrap the payload:
	wrapper := &anypb.Any{}
	err = proto.Unmarshal(data, wrapper)
	if err != nil {
		l.logger.ErrorContext(
			ctx,
			"Failed to unmarshal payload",
			slog.String("payload", notification.Payload),
			slog.Any("error", err),
		)
		return
	}
	payload, err := wrapper.UnmarshalNew()
	if err != nil {
		l.logger.ErrorContext(
			ctx,
			"Failed to unwrap payload",
			slog.String("payload", notification.Payload),
			slog.Any("error", err),
		)
		return
	}
	if l.logger.Enabled(ctx, slog.LevelDebug) {
		data, err := protojson.Marshal(wrapper)
		if err != nil {
			l.logger.ErrorContext(
				ctx,
				"Failed to marshal payload",
				slog.Any("error", err),
			)
		}
		var object any
		_ = json.Unmarshal(data, &object)

	}
	if l.logger.Enabled(ctx, slog.LevelDebug) {
		l.logger.DebugContext(
			ctx,
			"Decoded payload",
			slog.Any("payload", payload),
		)
	}

	// Run the callbacks:
	l.runPayloadCallbacks(ctx, payload)
}

func (l *Listener) runReadyCallbacks(ctx context.Context) {
	errors := 0
	for i, readyCallback := range l.readyCallbacks {
		err := readyCallback(ctx)
		if err != nil {
			l.logger.InfoContext(
				ctx,
				"Ready callback failed",
				slog.Int("index", i),
				slog.Any("error", err),
			)
			errors++
		}
	}
	l.logger.DebugContext(
		ctx,
		"Ready callbacks completed",
		slog.Int("total", len(l.readyCallbacks)),
		slog.Int("errors", errors),
	)
}

func (l *Listener) runPayloadCallbacks(ctx context.Context, payload proto.Message) {
	errors := 0
	for i, payloadCallback := range l.payloadCallbacks {
		err := payloadCallback(ctx, payload)
		if err != nil {
			l.logger.ErrorContext(
				ctx,
				"Payload callback failed",
				slog.Any("index", i),
				slog.Any("error", err),
			)
			errors++
		}
	}
	l.logger.DebugContext(
		ctx,
		"Payload callbacks completed",
		slog.Int("total", len(l.payloadCallbacks)),
		slog.Int("errors", errors),
	)
}

// sleepBeforeRetry waits till the retry interval passes, or till the context is cancelled.
func (l *Listener) sleepBeforeRetry(ctx context.Context) error {
	l.logger.DebugContext(
		ctx,
		"Sleeping before retry",
		slog.Duration("interval", l.retryInterval),
	)
	select {
	case <-ctx.Done():
		return context.Canceled
	case <-time.After(l.retryInterval):
		return nil
	}
}
