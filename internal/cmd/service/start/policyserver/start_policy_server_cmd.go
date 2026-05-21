/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package policyserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/logging"
	"github.com/osac-project/fulfillment-service/internal/network"
	shtdwn "github.com/osac-project/fulfillment-service/internal/shutdown"
)

// Cmd creates and returns the `start policy-server` command.
func Cmd() *cobra.Command {
	runner := &runnerContext{}
	command := &cobra.Command{
		Use:   "policy-server",
		Short: "Starts the policy server",
		Args:  cobra.NoArgs,
		RunE:  runner.run,
	}
	flags := command.Flags()
	network.AddListenerFlags(flags, network.HttpListenerName, network.DefaultHttpAddress)
	network.AddListenerFlags(flags, network.GrpcListenerName, network.DefaultGrpcAddress)
	flags.StringArrayVar(
		&runner.args.policyValues,
		"policy-value",
		nil,
		"Key-value pair passed as template data to the policy templates, in the form key=value. "+
			"Can be specified multiple times "+
			"(e.g., --policy-value key1=value1 --policy-value key2=value2).",
	)
	return command
}

// runnerContext contains the data and logic needed to run the `start policy-server` command.
type runnerContext struct {
	logger *slog.Logger
	flags  *pflag.FlagSet
	args   struct {
		policyValues []string
	}
}

// run runs the `start policy-server` command.
func (c *runnerContext) run(cmd *cobra.Command, argv []string) error {
	// Get the context and create a cancellable version:
	ctx, cancel := context.WithCancel(cmd.Context())

	// Get the dependencies from the context:
	c.logger = logging.LoggerFromContext(ctx)

	// Save the flags:
	c.flags = cmd.Flags()

	// Create the shutdown sequence triggered by typical stop signals:
	c.logger.InfoContext(ctx, "Creating shutdown sequence")
	shutdown, err := shtdwn.NewSequence().
		SetLogger(c.logger).
		AddSignals(syscall.SIGTERM, syscall.SIGINT).
		AddContext("context", 0, cancel).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create shutdown sequence: %w", err)
	}

	// Load the policy values from all sources. Values files are loaded first, then inline
	// documents, then individual value files, then inline key=value pairs, so that more specific
	// sources take precedence.
	policyValues := map[string]any{}
	for _, pair := range c.args.policyValues {
		key, value, err := c.parseKeyValue(pair)
		if err != nil {
			return fmt.Errorf("failed to parse --policy-value '%s': %w", pair, err)
		}
		policyValues[key] = value
	}

	// Create the policy server:
	c.logger.InfoContext(ctx, "Creating policy server")
	policyServer, err := auth.NewPolicyServer().
		SetLogger(c.logger).
		SetValues(policyValues).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create policy server: %w", err)
	}

	// Create the HTTP listener:
	c.logger.InfoContext(ctx, "Creating HTTP listener")
	httpListener, err := network.NewListener().
		SetLogger(c.logger).
		SetFlags(c.flags, network.HttpListenerName).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create HTTP listener: %w", err)
	}

	// Create the HTTP server:
	c.logger.InfoContext(ctx, "Creating HTTP server")
	httpServer := &http.Server{
		Addr:              httpListener.Addr().String(),
		Handler:           policyServer,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	shutdown.AddHttpServer(network.HttpListenerName, 0, httpServer)

	// Create the gRPC listener:
	c.logger.InfoContext(ctx, "Creating gRPC listener")
	grpcListener, err := network.NewListener().
		SetLogger(c.logger).
		SetFlags(c.flags, network.GrpcListenerName).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create gRPC listener: %w", err)
	}

	// Start serving HTTP:
	c.logger.InfoContext(
		ctx,
		"Starting HTTP server",
		slog.String("address", httpListener.Addr().String()),
	)
	go func() {
		err := httpServer.Serve(httpListener)
		if err != nil {
			c.logger.ErrorContext(
				ctx,
				"HTTP server failed",
				slog.Any("error", err),
			)
		}
	}()

	// Create the gRPC server with reflection and health services:
	grpcServer := grpc.NewServer()
	shutdown.AddGrpcServer(network.GrpcListenerName, 0, grpcServer)
	c.logger.InfoContext(ctx, "Registering gRPC reflection server")
	reflection.RegisterV1(grpcServer)
	c.logger.InfoContext(ctx, "Registering gRPC health server")
	healthServer := health.NewServer()
	healthv1.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", healthv1.HealthCheckResponse_SERVING)

	// Start the gRPC server:
	c.logger.InfoContext(
		ctx,
		"Starting gRPC server",
		slog.String("address", grpcListener.Addr().String()),
	)
	go func() {
		err := grpcServer.Serve(grpcListener)
		if err != nil {
			c.logger.ErrorContext(
				ctx,
				"gRPC server failed",
				slog.Any("error", err),
			)
		}
	}()

	// Keep running till the shutdown sequence finishes:
	c.logger.InfoContext(ctx, "Waiting for shutdown sequence to complete")
	return shutdown.Wait()
}

// parseKeyValue splits a string of the form 'key=value' into its key and value parts. The split is done on the
// first equals sign, so the value may contain additional equals signs.
func (c *runnerContext) parseKeyValue(pair string) (key, value string, err error) {
	key, value, ok := strings.Cut(pair, "=")
	if !ok {
		err = fmt.Errorf("expected key=value format, but no '=' found in '%s'", pair)
		return
	}
	if key == "" {
		err = fmt.Errorf("key must not be empty in '%s'", pair)
	}
	return
}
