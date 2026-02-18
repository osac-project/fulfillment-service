/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/osac-project/fulfillment-common/network"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/osac-project/fulfillment-service/internal"
	"github.com/osac-project/fulfillment-service/internal/version"
)

func NewProbeGrpcServerCommand() *cobra.Command {
	runner := &probeGrpcServerCommandRunner{}
	command := &cobra.Command{
		Use:   "grpc-server",
		Short: "Checks the health of the gRPC server",
		Long: "Checks the health of the gRPC server using the gRPC health checking protocol. " +
			"This command is intended to be used as a liveness or readiness probe in Kubernetes. " +
			"It exits with code 0 if the server is healthy, or a non-zero code otherwise.",
		Args: cobra.NoArgs,
		RunE: runner.run,
	}
	flags := command.Flags()
	network.AddGrpcClientFlags(flags, network.GrpcClientName, network.DefaultGrpcAddress)
	flags.StringSliceVar(
		&runner.args.caFiles,
		"ca-file",
		[]string{},
		"Files or directories containing trusted CA certificates in PEM format.",
	)
	flags.DurationVar(
		&runner.args.timeout,
		"grpc-server-timeout",
		time.Second,
		"Timeout for the gRPC server health check request.",
	)
	return command
}

type probeGrpcServerCommandRunner struct {
	logger *slog.Logger
	flags  *pflag.FlagSet
	args   struct {
		caFiles []string
		timeout time.Duration
	}
}

func (c *probeGrpcServerCommandRunner) run(cmd *cobra.Command, argv []string) error {
	// Get the context:
	ctx := cmd.Context()

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

	// Calculate the user agent:
	userAgent := fmt.Sprintf("%s/%s", grpcProbeUserAgent, version.Get())

	// Create the gRPC client connection:
	conn, err := network.NewGrpcClient().
		SetLogger(c.logger).
		SetFlags(c.flags, network.GrpcClientName).
		SetCaPool(caPool).
		SetUserAgent(userAgent).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create gRPC client: %w", err)
	}
	defer func() {
		err := conn.Close()
		if err != nil {
			c.logger.ErrorContext(
				ctx,
				"Failed to close gRPC connection",
				slog.Any("error", err),
			)
		}
	}()

	// Create a context with timeout:
	checkCtx, cancel := context.WithTimeout(ctx, c.args.timeout)
	defer cancel()

	// Check the health:
	err = c.checkHealth(checkCtx, conn)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	return nil
}

func (c *probeGrpcServerCommandRunner) checkHealth(ctx context.Context, conn *grpc.ClientConn) error {
	client := healthv1.NewHealthClient(conn)
	response, err := client.Check(ctx, &healthv1.HealthCheckRequest{})
	if err != nil {
		return fmt.Errorf("health check request failed: %w", err)
	}
	if response.Status != healthv1.HealthCheckResponse_SERVING {
		return fmt.Errorf("service is not serving, status: %s", response.Status.String())
	}
	c.logger.InfoContext(
		ctx,
		"Health check passed",
		slog.String("status", response.Status.String()),
	)
	return nil
}

// grpcProbeUserAgent is the user agent string for the gRPC probe.
const grpcProbeUserAgent = "fulfillment-grpc-probe"
