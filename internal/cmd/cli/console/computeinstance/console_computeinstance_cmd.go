/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package computeinstance

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/logging"
	"github.com/osac-project/fulfillment-service/internal/terminal"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/osac-project/fulfillment-service/internal/config"
	"github.com/osac-project/fulfillment-service/internal/exit"
)

// Cmd returns the `console computeinstance` command.
func Cmd() *cobra.Command {
	runner := &runnerContext{}
	result := &cobra.Command{
		Use:   "computeinstance <name-or-id>",
		Short: "Access compute instance serial console",
		Long: `Open an interactive serial console session to a compute instance.

The console provides direct access to the compute instance's serial port,
allowing you to interact with it as if connected via a physical serial cable.

The instance can be specified by name or ID.

To disconnect: press Ctrl+] at any time, or type ~. after Enter.
The session continues running after you disconnect.

Login credentials:
  Cloud images (e.g., Fedora) require a password to be set via cloud-init
  at instance creation time. Without this, serial console login will be rejected.
  Example cloud-init config (base64-encoded):

    #cloud-config
    password: my-password
    chpasswd:
      expire: false

  Pass it as a template parameter when creating the instance:
    fulfillment-cli create computeinstance --template <template> \
      -p cloud_init_config=<base64-encoded-config>`,
		Args: cobra.ExactArgs(1),
		RunE: runner.run,
	}

	flags := result.Flags()
	flags.DurationVar(
		&runner.args.timeout,
		"timeout",
		30*time.Minute,
		"Session timeout.",
	)

	return result
}

type runnerContext struct {
	logger  *slog.Logger
	console *terminal.Console
	conn    *grpc.ClientConn
	args    struct {
		timeout time.Duration
	}
}

func (c *runnerContext) run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	key := args[0]

	// Get the logger and console.
	c.logger = logging.LoggerFromContext(ctx)
	c.console = terminal.ConsoleFromContext(ctx)

	// Load configuration.
	cfg, err := config.Load(ctx)
	if err != nil {
		return err
	}
	if cfg == nil {
		c.console.Errorf(ctx, "Not logged in. Run 'fulfillment-cli login' first.\n")
		return exit.Error(1)
	}

	// Create gRPC connection.
	c.conn, err = cfg.Connect(ctx, cmd.Flags())
	if err != nil {
		return fmt.Errorf("failed to create connection: %w", err)
	}
	defer c.conn.Close()

	// Resolve name or ID to instance ID.
	instanceID, err := c.resolveInstance(ctx, key)
	if err != nil {
		return err
	}

	// Apply client-side session timeout.
	if c.args.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.args.timeout)
		defer cancel()
	}

	// Run with auto-reconnect.
	err = c.connectWithRetry(ctx, instanceID)
	if ctx.Err() == context.DeadlineExceeded {
		c.console.Errorf(ctx, "\nSession timed out after %s.\n", c.args.timeout)
		return nil
	}
	return err
}

// resolveInstance resolves a name or ID to a compute instance ID.
func (c *runnerContext) resolveInstance(ctx context.Context, key string) (string, error) {
	client := publicv1.NewComputeInstancesClient(c.conn)
	listFilter := fmt.Sprintf(
		"this.id == %[1]q || this.metadata.name == %[1]q",
		key,
	)
	resp, err := client.List(ctx, publicv1.ComputeInstancesListRequest_builder{
		Filter: proto.String(listFilter),
		Limit:  proto.Int32(2),
	}.Build())
	if err != nil {
		return "", fmt.Errorf("failed to look up compute instance: %w", err)
	}

	items := resp.GetItems()
	switch len(items) {
	case 0:
		return "", fmt.Errorf("compute instance %q not found", key)
	case 1:
		return items[0].GetId(), nil
	default:
		return "", fmt.Errorf("multiple compute instances match %q; use the ID instead", key)
	}
}

// errConnectionLost is a sentinel indicating the session was established
// but the connection dropped. This resets the retry counter.
var errConnectionLost = errors.New("connection lost")

func (c *runnerContext) connectWithRetry(ctx context.Context, instanceID string) error {
	const maxConsecutiveRetries = 5
	consecutiveFailures := 0
	backoff := time.Second

	for {
		err := c.connectOnce(ctx, instanceID)
		if err == nil {
			// Clean disconnect (e.g., escape sequence). Done.
			return nil
		}

		// If the context expired (timeout or cancel), return immediately.
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Check if this is a permanent error (no retry).
		if isPermanentError(err) {
			return fmt.Errorf("%s", userFacingError(err))
		}

		// If we were connected and then lost the connection, reset the
		// retry counter — the server was reachable, this is a new failure
		// sequence.
		if errors.Is(err, errConnectionLost) {
			consecutiveFailures = 0
			backoff = time.Second
		}

		consecutiveFailures++
		if consecutiveFailures > maxConsecutiveRetries {
			c.console.Errorf(ctx, "\nGave up reconnecting after %d consecutive failures.\n",
				maxConsecutiveRetries)
			return fmt.Errorf("%s", userFacingError(err))
		}

		if consecutiveFailures == 1 {
			c.console.Errorf(ctx, "\nConnection lost. Reconnecting...\n")
		} else {
			c.console.Errorf(ctx, "\nConnection lost. Reconnecting (attempt %d/%d)...\n",
				consecutiveFailures, maxConsecutiveRetries)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff = min(backoff*2, 30*time.Second)
	}
}

func (c *runnerContext) connectOnce(ctx context.Context, instanceID string) error {
	client := publicv1.NewConsoleClient(c.conn)

	// Open bidirectional stream.
	stream, err := client.Connect(ctx)
	if err != nil {
		return fmt.Errorf("failed to open console stream: %w", err)
	}

	// Send init message.
	err = stream.Send(publicv1.ConsoleConnectRequest_builder{
		Init: publicv1.ConsoleConnectInit_builder{
			ResourceType: publicv1.ConsoleResourceType_CONSOLE_RESOURCE_TYPE_COMPUTE_INSTANCE,
			ResourceId:   instanceID,
			Type:         publicv1.ConsoleType_CONSOLE_TYPE_SERIAL,
		}.Build(),
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to send init: %w", err)
	}

	// Wait for connected status.
	if err := c.waitForConnected(ctx, stream, instanceID); err != nil {
		return err
	}

	// Set terminal to raw mode.
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		oldState, err := term.MakeRaw(fd)
		if err != nil {
			return fmt.Errorf("failed to set raw mode: %w", err)
		}
		defer term.Restore(fd, oldState)
	}

	err = c.proxyIO(ctx, stream)
	if err != nil {
		// We were connected and then lost the connection.
		return fmt.Errorf("%w: %v", errConnectionLost, err)
	}
	return nil
}

// waitForConnected receives status messages until the server reports CONNECTED.
func (c *runnerContext) waitForConnected(ctx context.Context,
	stream grpc.BidiStreamingClient[publicv1.ConsoleConnectRequest, publicv1.ConsoleConnectResponse], instanceID string) error {
	for {
		resp, err := stream.Recv()
		if err != nil {
			return err
		}

		if st := resp.GetStatus(); st != nil {
			switch st.GetState() {
			case publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_CONNECTED:
				c.console.Infof(ctx, "Connected to %s. Disconnect: Ctrl+] or Enter ~.\n", instanceID)
				return nil
			case publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_CONNECTING:
				c.console.Infof(ctx, "%s\n", st.GetMessage())
			case publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_ERROR:
				return fmt.Errorf("server error: %s", st.GetMessage())
			}
		}
	}
}

// proxyIO handles bidirectional I/O between the terminal and the gRPC stream.
func (c *runnerContext) proxyIO(ctx context.Context, stream grpc.BidiStreamingClient[publicv1.ConsoleConnectRequest, publicv1.ConsoleConnectResponse]) error {
	errCh := make(chan error, 2)

	// Server -> stdout.
	go func() {
		for {
			resp, err := stream.Recv()
			if err != nil {
				if err == io.EOF {
					errCh <- nil
				} else {
					errCh <- err
				}
				return
			}

			if output := resp.GetOutput(); output != nil {
				data := output.GetData()
				if len(data) > 0 {
					_, writeErr := os.Stdout.Write(data)
					if writeErr != nil {
						errCh <- writeErr
						return
					}
				}
			}

			if st := resp.GetStatus(); st != nil {
				switch st.GetState() {
				case publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_DISCONNECTED:
					c.console.Infof(ctx, "\n%s\n", st.GetMessage())
					errCh <- nil
					return
				case publicv1.ConsoleConnectionState_CONSOLE_CONNECTION_STATE_ERROR:
					errCh <- fmt.Errorf("server error: %s", st.GetMessage())
					return
				}
			}
		}
	}()

	// Stdin -> server (with escape sequence detection).
	go func() {
		buf := make([]byte, 256)
		escape := newEscapeDetector()

		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				data := buf[:n]

				// Check for escape sequence (~. after CR).
				if escape.feed(data) {
					c.console.Infof(ctx, "\nConnection closed.\n")
					errCh <- nil
					return
				}

				sendErr := stream.Send(publicv1.ConsoleConnectRequest_builder{
					Input: publicv1.ConsoleInput_builder{
						Data: append([]byte(nil), data...),
					}.Build(),
				}.Build())
				if sendErr != nil {
					errCh <- sendErr
					return
				}
			}
			if err != nil {
				if err == io.EOF {
					errCh <- nil
				} else {
					errCh <- err
				}
				return
			}
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return nil
	}
}

// escapeDetector detects the SSH-style escape sequence: CR ~ .
type escapeDetector struct {
	state int // 0 = normal, 1 = after CR, 2 = after CR + ~
}

func newEscapeDetector() *escapeDetector {
	return &escapeDetector{}
}

// feed processes input bytes and returns true if a disconnect sequence is detected.
// Supported sequences: CR/LF followed by ~. (SSH style), or Ctrl+] (0x1D, telnet style).
func (e *escapeDetector) feed(data []byte) bool {
	for _, b := range data {
		// Ctrl+] works at any time, no preceding Enter needed.
		if b == 0x1D {
			return true
		}
		switch e.state {
		case 0:
			if b == '\r' || b == '\n' {
				e.state = 1
			}
		case 1:
			if b == '~' {
				e.state = 2
			} else if b == '\r' || b == '\n' {
				e.state = 1
			} else {
				e.state = 0
			}
		case 2:
			if b == '.' {
				return true
			}
			e.state = 0
		}
	}
	return false
}

// userFacingError extracts a clean message from gRPC status errors.
func userFacingError(err error) string {
	if st, ok := status.FromError(err); ok {
		return st.Message()
	}
	return err.Error()
}

// isPermanentError returns true for errors that should not be retried.
func isPermanentError(err error) bool {
	st, ok := status.FromError(err)
	if !ok {
		return false
	}
	switch st.Code() {
	case codes.PermissionDenied, codes.NotFound, codes.Unauthenticated,
		codes.FailedPrecondition, codes.InvalidArgument, codes.Unimplemented:
		return true
	}
	return false
}
