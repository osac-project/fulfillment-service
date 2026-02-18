/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package dev

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"

	"github.com/osac-project/fulfillment-common/auth"
	"github.com/osac-project/fulfillment-common/network"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"google.golang.org/protobuf/proto"

	"github.com/osac-project/fulfillment-service/internal"
	eventsv1 "github.com/osac-project/fulfillment-service/internal/api/events/v1"
	"github.com/osac-project/fulfillment-service/internal/version"
)

// NewWatchCommand creates and returns the `listen` command.
func NewWatchCommand() *cobra.Command {
	runner := &watchCommandRunner{}
	command := &cobra.Command{
		Use:   "watch",
		Short: "watches events",
		Args:  cobra.NoArgs,
		RunE:  runner.run,
	}
	flags := command.Flags()
	network.AddGrpcClientFlags(flags, network.GrpcClientName, network.DefaultGrpcAddress)
	flags.StringVar(
		&runner.args.filter,
		"filter",
		"",
		"Event filter",
	)
	flags.StringArrayVar(
		&runner.args.caFiles,
		"ca-file",
		[]string{},
		"File or directory containing trusted CA certificates.",
	)
	flags.StringVar(
		&runner.args.tokenFile,
		"token-file",
		"",
		"File containing the token to use for authentication.",
	)
	return command
}

// watchCommandRunner contains the data and logic needed to run the `listen` command.
type watchCommandRunner struct {
	logger *slog.Logger
	flags  *pflag.FlagSet
	args   struct {
		caFiles   []string
		tokenFile string
		filter    string
	}
}

// run runs the `listen` command.
func (c *watchCommandRunner) run(cmd *cobra.Command, argv []string) error {
	// Get the context:
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	// Get the dependencies from the context:
	c.logger = internal.LoggerFromContext(ctx)

	// Save the flags:
	c.flags = cmd.Flags()

	// Load the trusted CA certificates:
	caPool, err := network.NewCertPool().
		SetLogger(c.logger).
		AddFiles(c.args.caFiles...).
		Build()
	if err != nil {
		return fmt.Errorf("failed to load trusted CA certificates: %w", err)
	}

	// Create the token source:
	tokenSource, err := auth.NewFileTokenSource().
		SetLogger(c.logger).
		SetFile(c.args.tokenFile).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create token source: %w", err)
	}

	// Calculate the user agent:
	userAgent := fmt.Sprintf("%s/%s", watchUserAgent, version.Get())

	// Create the grpcClient:
	grpcClient, err := network.NewGrpcClient().
		SetLogger(c.logger).
		SetFlags(c.flags, network.GrpcClientName).
		SetCaPool(caPool).
		SetTokenSource(tokenSource).
		SetUserAgent(userAgent).
		Build()
	if err != nil {
		return err
	}
	defer func() {
		err := grpcClient.Close()
		if err != nil {
			c.logger.InfoContext(
				ctx,
				"Failed to close client",
				slog.Any("error", err),
			)
		}
	}()

	// Start watching events:
	eventsClient := eventsv1.NewEventsClient(grpcClient)
	eventsStream, err := eventsClient.Watch(ctx, &eventsv1.EventsWatchRequest{
		Filter: proto.String(c.args.filter),
	})
	if err != nil {
		return fmt.Errorf("failed to start stream: %w", err)
	}
	for {
		response, err := eventsStream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("stream failed: %w", err)
		}
		c.logger.InfoContext(
			ctx,
			"Received event",
			slog.Any("event", response.Event),
		)
	}

	// Wait for a signal:
	c.logger.InfoContext(ctx, "Waiting for signal")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	c.logger.InfoContext(ctx, "Signal received, shutting down")
	return nil
}

// watchUserAgent is the user agent string for the watch command.
const watchUserAgent = "fulfillment-watch"
