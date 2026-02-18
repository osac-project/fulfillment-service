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
	"net/http"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"github.com/osac-project/fulfillment-common/auth"
	"github.com/osac-project/fulfillment-common/network"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"k8s.io/klog/v2"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/osac-project/fulfillment-service/internal"
	privatev1 "github.com/osac-project/fulfillment-service/internal/api/private/v1"
	"github.com/osac-project/fulfillment-service/internal/controllers"
	"github.com/osac-project/fulfillment-service/internal/controllers/cluster"
	"github.com/osac-project/fulfillment-service/internal/controllers/computeinstance"
	"github.com/osac-project/fulfillment-service/internal/controllers/host"
	"github.com/osac-project/fulfillment-service/internal/controllers/hostpool"
	internalhealth "github.com/osac-project/fulfillment-service/internal/health"
	shtdwn "github.com/osac-project/fulfillment-service/internal/shutdown"
	"github.com/osac-project/fulfillment-service/internal/version"
)

// NewStartControllerCommand creates and returns the `start controllers` command.
func NewStartControllerCommand() *cobra.Command {
	runner := &startControllerRunner{}
	command := &cobra.Command{
		Use:   "controller",
		Short: "Starts the controller",
		Args:  cobra.NoArgs,
		RunE:  runner.run,
	}
	flags := command.Flags()
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
	network.AddGrpcClientFlags(flags, network.GrpcClientName, network.DefaultGrpcAddress)
	network.AddListenerFlags(flags, network.GrpcListenerName, network.DefaultGrpcAddress)
	network.AddListenerFlags(flags, network.MetricsListenerName, network.DefaultMetricsAddress)
	return command
}

// startControllerRunner contains the data and logic needed to run the `start controllers` command.
type startControllerRunner struct {
	logger *slog.Logger
	flags  *pflag.FlagSet
	args   struct {
		caFiles   []string
		tokenFile string
	}
	client *grpc.ClientConn
}

// run runs the `start controllers` command.
func (r *startControllerRunner) run(cmd *cobra.Command, argv []string) error {
	var err error

	// Get the context:
	ctx, cancel := context.WithCancel(cmd.Context())

	// Get the dependencies from the context:
	r.logger = internal.LoggerFromContext(ctx)

	// Configure the Kubernetes libraries to use the logger:
	logrLogger := logr.FromSlogHandler(r.logger.Handler())
	crlog.SetLogger(logrLogger)
	klog.SetLogger(logrLogger)

	// Save the flags:
	r.flags = cmd.Flags()

	// Create the shutdown sequence:
	r.logger.InfoContext(ctx, "Creating shutdown sequence")
	shutdown, err := shtdwn.NewSequence().
		SetLogger(r.logger).
		AddSignals(syscall.SIGTERM, syscall.SIGINT).
		AddContext("context", 0, cancel).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create shutdown sequence: %w", err)
	}

	// Load the trusted CA certificates:
	r.logger.InfoContext(ctx, "Loading trusted CA certificates")
	caPool, err := network.NewCertPool().
		SetLogger(r.logger).
		AddFiles(r.args.caFiles...).
		Build()
	if err != nil {
		return fmt.Errorf("failed to load trusted CA certificates: %w", err)
	}

	// Create the token source:
	r.logger.InfoContext(ctx, "Creating token source")
	tokenSource, err := auth.NewFileTokenSource().
		SetLogger(r.logger).
		SetFile(r.args.tokenFile).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create token source: %w", err)
	}

	// Calculate the user agent:
	r.logger.InfoContext(ctx, "Calculating user agent")
	userAgent := fmt.Sprintf("%s/%s", controllerUserAgent, version.Get())

	// Create the gRPC client:
	r.logger.InfoContext(ctx, "Creating gRPC client")
	r.client, err = network.NewGrpcClient().
		SetLogger(r.logger).
		SetFlags(r.flags, network.GrpcClientName).
		SetCaPool(caPool).
		SetTokenSource(tokenSource).
		SetUserAgent(userAgent).
		SetMetricsSubsystem("outbound").
		Build()
	if err != nil {
		return fmt.Errorf("failed to create gRPC client: %w", err)
	}

	// Create the gRPC server:
	r.logger.InfoContext(ctx, "Creating gRPC listener")
	grpcListener, err := network.NewListener().
		SetLogger(r.logger).
		SetFlags(r.flags, network.GrpcListenerName).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}
	grpcServer := grpc.NewServer()
	shutdown.AddGrpcServer(network.GrpcListenerName, 0, grpcServer)

	// Register the reflection server:
	r.logger.InfoContext(ctx, "Registering gRPC reflection server")
	reflection.RegisterV1(grpcServer)

	// Register the health server:
	r.logger.InfoContext(ctx, "Registering gRPC health server")
	healthServer := health.NewServer()
	healthv1.RegisterHealthServer(grpcServer, healthServer)

	// Create the health aggregator:
	r.logger.InfoContext(ctx, "Creating health aggregator")
	healthAggregator, err := internalhealth.NewAggregator().
		SetLogger(r.logger).
		SetServer(healthServer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create health aggregator: %w", err)
	}

	// Start the gRPC server:
	r.logger.InfoContext(
		ctx,
		"Starting gRPC server",
		slog.String("address", grpcListener.Addr().String()),
	)
	go func() {
		err := grpcServer.Serve(grpcListener)
		if err != nil {
			r.logger.ErrorContext(
				ctx,
				"gRPC server failed",
				slog.Any("error", err),
			)
		}
	}()

	// Wait for the server to be ready:
	r.logger.InfoContext(ctx, "Waiting for server to be ready")
	err = r.waitForServer(ctx)
	if err != nil {
		return fmt.Errorf("failed to wait for server to be ready: %w", err)
	}

	// Create the hub cache:
	r.logger.InfoContext(ctx, "Creating hub cache")
	hubCache, err := controllers.NewHubCache().
		SetLogger(r.logger).
		SetConnection(r.client).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create hub cache: %w", err)
	}

	// Create the cluster reconciler:
	r.logger.InfoContext(ctx, "Creating cluster reconciler")
	clusterReconcilerFunction, err := cluster.NewFunction().
		SetLogger(r.logger).
		SetConnection(r.client).
		SetHubCache(hubCache).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create cluster reconciler function: %w", err)
	}
	clusterReconciler, err := controllers.NewReconciler[*privatev1.Cluster]().
		SetLogger(r.logger).
		SetName("cluster").
		SetClient(r.client).
		SetFunction(clusterReconcilerFunction).
		SetEventFilter("has(event.cluster) || (has(event.hub) && event.type == EVENT_TYPE_OBJECT_CREATED)").
		SetHealthReporter(healthAggregator).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create cluster reconciler: %w", err)
	}

	// Start the cluster reconciler:
	r.logger.InfoContext(ctx, "Starting cluster reconciler")
	go func() {
		err := clusterReconciler.Start(ctx)
		if err == nil || errors.Is(err, context.Canceled) {
			r.logger.InfoContext(ctx, "Cluster reconciler finished")
		} else {
			r.logger.InfoContext(
				ctx,
				"Cluster reconciler failed",
				slog.Any("error", err),
			)
		}
	}()

	// Create the compute instance reconciler:
	r.logger.InfoContext(ctx, "Creating compute instance reconciler")
	computeInstanceReconcilerFunction, err := computeinstance.NewFunction().
		SetLogger(r.logger).
		SetConnection(r.client).
		SetHubCache(hubCache).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create compute instance reconciler function: %w", err)
	}
	computeInstanceReconciler, err := controllers.NewReconciler[*privatev1.ComputeInstance]().
		SetLogger(r.logger).
		SetName("compute_instance").
		SetClient(r.client).
		SetFunction(computeInstanceReconcilerFunction).
		SetEventFilter("has(event.compute_instance) || (has(event.hub) && event.type == EVENT_TYPE_OBJECT_CREATED)").
		SetHealthReporter(healthAggregator).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create compute instance reconciler: %w", err)
	}

	// Start the compute instance reconciler:
	r.logger.InfoContext(ctx, "Starting compute instance reconciler")
	go func() {
		err := computeInstanceReconciler.Start(ctx)
		if err == nil || errors.Is(err, context.Canceled) {
			r.logger.InfoContext(ctx, "Compute instance reconciler finished")
		} else {
			r.logger.InfoContext(
				ctx,
				"Compute instance reconciler failed",
				slog.Any("error", err),
			)
		}
	}()

	// Create the host reconciler:
	r.logger.InfoContext(ctx, "Creating host reconciler")
	hostReconcilerFunction, err := host.NewFunction().
		SetLogger(r.logger).
		SetConnection(r.client).
		SetHubCache(hubCache).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create host reconciler function: %w", err)
	}
	hostReconciler, err := controllers.NewReconciler[*privatev1.Host]().
		SetLogger(r.logger).
		SetName("host").
		SetClient(r.client).
		SetFunction(hostReconcilerFunction).
		SetEventFilter("has(event.host) || (has(event.hub) && event.type == EVENT_TYPE_OBJECT_CREATED)").
		SetHealthReporter(healthAggregator).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create host reconciler: %w", err)
	}

	// Start the host reconciler:
	r.logger.InfoContext(ctx, "Starting host reconciler")
	go func() {
		err := hostReconciler.Start(ctx)
		if err == nil || errors.Is(err, context.Canceled) {
			r.logger.InfoContext(ctx, "Host reconciler finished")
		} else {
			r.logger.InfoContext(
				ctx,
				"Host reconciler failed",
				slog.Any("error", err),
			)
		}
	}()

	// Create the host pool reconciler:
	r.logger.InfoContext(ctx, "Creating host pool reconciler")
	hostPoolReconcilerFunction, err := hostpool.NewFunction().
		SetLogger(r.logger).
		SetConnection(r.client).
		SetHubCache(hubCache).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create host pool reconciler function: %w", err)
	}
	hostPoolReconciler, err := controllers.NewReconciler[*privatev1.HostPool]().
		SetLogger(r.logger).
		SetName("host_pool").
		SetClient(r.client).
		SetFunction(hostPoolReconcilerFunction).
		SetEventFilter("has(event.host_pool) || (has(event.hub) && event.type == EVENT_TYPE_OBJECT_CREATED)").
		SetHealthReporter(healthAggregator).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create host pool reconciler: %w", err)
	}

	// Start the host pool reconciler:
	r.logger.InfoContext(ctx, "Starting host pool reconciler")
	go func() {
		err := hostPoolReconciler.Start(ctx)
		if err == nil || errors.Is(err, context.Canceled) {
			r.logger.InfoContext(ctx, "Host pool reconciler finished")
		} else {
			r.logger.InfoContext(
				ctx,
				"Host pool reconciler failed",
				slog.Any("error", err),
			)
		}
	}()

	// Create the metrics listener:
	r.logger.InfoContext(ctx, "Creating metrics listener")
	metricsListener, err := network.NewListener().
		SetLogger(r.logger).
		SetFlags(r.flags, network.MetricsListenerName).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create metrics listener: %w", err)
	}

	// Start the metrics server:
	r.logger.InfoContext(
		ctx,
		"Starting metrics server",
		slog.String("address", metricsListener.Addr().String()),
	)
	metricsServer := &http.Server{
		Handler: promhttp.Handler(),
	}
	go func() {
		err := metricsServer.Serve(metricsListener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			r.logger.ErrorContext(
				ctx,
				"Metrics server failed",
				slog.Any("error", err),
			)
		}
	}()

	// Wait for the shutdown sequence to complete:
	r.logger.InfoContext(ctx, "Waiting for shutdown sequence to complete")
	return shutdown.Wait()
}

// waitForServer waits for the server to be ready using the health service.
func (r *startControllerRunner) waitForServer(ctx context.Context) error {
	client := healthv1.NewHealthClient(r.client)
	request := &healthv1.HealthCheckRequest{}
	const max = time.Minute
	const interval = time.Second
	start := time.Now()
	for {
		response, err := client.Check(ctx, request)
		if err == nil && response.Status == healthv1.HealthCheckResponse_SERVING {
			r.logger.InfoContext(ctx, "Server is ready")
			return nil
		}
		if time.Since(start) >= max {
			return fmt.Errorf("server did not become ready after waiting for %s: %w", max, err)
		}
		r.logger.InfoContext(
			ctx,
			"Server not yet ready",
			slog.Duration("elapsed", time.Since(start)),
			slog.Any("error", err),
		)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// controllerUserAgent is the user agent string for the controller.
const controllerUserAgent = "fulfillment-controller"
