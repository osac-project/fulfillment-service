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

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/osac-project/fulfillment-common/network"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/osac-project/fulfillment-service/internal"
	api "github.com/osac-project/fulfillment-service/internal/api/fulfillment/v1"
	privateapi "github.com/osac-project/fulfillment-service/internal/api/private/v1"
	shtdwn "github.com/osac-project/fulfillment-service/internal/shutdown"
	"github.com/osac-project/fulfillment-service/internal/version"
)

// NewStartRestGatewayCommand creates and returns the `start rest-gateway` command.
func NewStartRestGatewayCommand() *cobra.Command {
	runner := &startRestGatewayCommandRunner{}
	command := &cobra.Command{
		Use:   "rest-gateway",
		Short: "Starts the REST gateway",
		Args:  cobra.NoArgs,
		RunE:  runner.run,
	}
	flags := command.Flags()
	network.AddListenerFlags(flags, network.HttpListenerName, network.DefaultHttpAddress)
	network.AddListenerFlags(flags, network.MetricsListenerName, network.DefaultMetricsAddress)
	network.AddCorsFlags(flags, network.HttpListenerName)
	network.AddGrpcClientFlags(flags, network.GrpcClientName, network.DefaultGrpcAddress)
	flags.StringArrayVar(
		&runner.args.caFiles,
		"ca-file",
		[]string{},
		"File or directory containing trusted CA certificates.",
	)
	return command
}

// startRestGatewayCommandRunner contains the data and logic needed to run the `start rest-gateway` command.
type startRestGatewayCommandRunner struct {
	logger       *slog.Logger
	flags        *pflag.FlagSet
	grpcClient   *grpc.ClientConn
	healthClient healthv1.HealthClient
	args         struct {
		caFiles   []string
		tokenFile string
	}
}

// run runs the `start rest-gateway` command.
func (c *startRestGatewayCommandRunner) run(cmd *cobra.Command, argv []string) error {
	// Get the context:
	ctx, cancel := context.WithCancel(cmd.Context())

	// Get the dependencies from the context:
	c.logger = internal.LoggerFromContext(ctx)

	// Save the flags:
	c.flags = cmd.Flags()

	// Create the shutdown sequence:
	shutdown, err := shtdwn.NewSequence().
		SetLogger(c.logger).
		AddSignals(syscall.SIGTERM, syscall.SIGINT).
		AddContext("context", 0, cancel).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create shutdown sequence: %w", err)
	}

	// Create the network listener:
	c.logger.InfoContext(ctx, "Creating REST gateway listener")
	gwListener, err := network.NewListener().
		SetLogger(c.logger).
		SetFlags(c.flags, network.HttpListenerName).
		Build()
	if err != nil {
		return err
	}

	// Load the trusted CA certificates:
	c.logger.InfoContext(ctx, "Loading trusted CA certificates")
	caPool, err := network.NewCertPool().
		SetLogger(c.logger).
		AddFiles(c.args.caFiles...).
		Build()
	if err != nil {
		return fmt.Errorf("failed to load trusted CA certificates: %w", err)
	}

	// Calculate the user agent:
	c.logger.InfoContext(ctx, "Calculating user agent")
	userAgent := fmt.Sprintf("%s/%s", restGatewayUserAgent, version.Get())

	// Create the gRPC client:
	c.logger.InfoContext(ctx, "Creating gRPC client")
	c.grpcClient, err = network.NewGrpcClient().
		SetLogger(c.logger).
		SetFlags(c.flags, network.GrpcClientName).
		SetCaPool(caPool).
		SetUserAgent(userAgent).
		SetMetricsSubsystem("outbound").
		Build()
	if err != nil {
		return err
	}

	// Create the gateway multiplexer:
	c.logger.InfoContext(ctx, "Creating REST gateway server")
	gatewayMarshaller := &runtime.JSONPb{
		MarshalOptions: protojson.MarshalOptions{
			UseProtoNames: true,
		},
	}
	gatewayMux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, gatewayMarshaller),
	)

	// Register the service handlers:
	err = api.RegisterClusterTemplatesHandler(ctx, gatewayMux, c.grpcClient)
	if err != nil {
		return err
	}
	err = api.RegisterClustersHandler(ctx, gatewayMux, c.grpcClient)
	if err != nil {
		return err
	}
	err = api.RegisterHostClassesHandler(ctx, gatewayMux, c.grpcClient)
	if err != nil {
		return err
	}
	err = api.RegisterHostsHandler(ctx, gatewayMux, c.grpcClient)
	if err != nil {
		return err
	}
	err = api.RegisterHostPoolsHandler(ctx, gatewayMux, c.grpcClient)
	if err != nil {
		return err
	}
	err = api.RegisterComputeInstanceTemplatesHandler(ctx, gatewayMux, c.grpcClient)
	if err != nil {
		return err
	}
	err = api.RegisterComputeInstancesHandler(ctx, gatewayMux, c.grpcClient)
	if err != nil {
		return err
	}

	// Register the private API service handlers:
	err = privateapi.RegisterClusterTemplatesHandler(ctx, gatewayMux, c.grpcClient)
	if err != nil {
		return err
	}
	err = privateapi.RegisterClustersHandler(ctx, gatewayMux, c.grpcClient)
	if err != nil {
		return err
	}
	err = privateapi.RegisterEventsHandler(ctx, gatewayMux, c.grpcClient)
	if err != nil {
		return err
	}
	err = privateapi.RegisterHostClassesHandler(ctx, gatewayMux, c.grpcClient)
	if err != nil {
		return err
	}
	err = privateapi.RegisterHostPoolsHandler(ctx, gatewayMux, c.grpcClient)
	if err != nil {
		return err
	}
	err = privateapi.RegisterHostsHandler(ctx, gatewayMux, c.grpcClient)
	if err != nil {
		return err
	}
	err = privateapi.RegisterHubsHandler(ctx, gatewayMux, c.grpcClient)
	if err != nil {
		return err
	}
	err = privateapi.RegisterComputeInstanceTemplatesHandler(ctx, gatewayMux, c.grpcClient)
	if err != nil {
		return err
	}
	err = privateapi.RegisterComputeInstancesHandler(ctx, gatewayMux, c.grpcClient)
	if err != nil {
		return err
	}

	// Add the health endpoint:
	c.logger.InfoContext(ctx, "Adding health endpoint")
	c.healthClient = healthv1.NewHealthClient(c.grpcClient)
	err = gatewayMux.HandlePath(http.MethodGet, "/healthz", c.handleHealth)
	if err != nil {
		return fmt.Errorf("failed to register health endpoint: %w", err)
	}

	// Add the CORS support:
	corsMiddleware, err := network.NewCorsMiddleware().
		SetLogger(c.logger).
		SetFlags(c.flags, network.HttpListenerName).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create CORS middleware: %w", err)
	}
	handler := corsMiddleware(gatewayMux)

	// Create the metrics server:
	c.logger.InfoContext(ctx, "Creating metrics listener")
	metricsListener, err := network.NewListener().
		SetLogger(c.logger).
		SetFlags(c.flags, network.MetricsListenerName).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create metrics listener: %w", err)
	}

	// Start the metrics server:
	c.logger.InfoContext(
		ctx,
		"Starting metrics server",
		slog.String("address", metricsListener.Addr().String()),
	)
	metricsServer := &http.Server{
		Addr:    metricsListener.Addr().String(),
		Handler: promhttp.Handler(),
	}
	go func() {
		err := metricsServer.Serve(metricsListener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			c.logger.ErrorContext(
				ctx,
				"Metrics server failed",
				slog.Any("error", err),
			)
		}
	}()
	shutdown.AddHttpServer(network.MetricsListenerName, 0, metricsServer)

	// Start serving:
	c.logger.InfoContext(
		ctx,
		"Start serving",
		slog.String("address", gwListener.Addr().String()),
	)
	http2Server := &http2.Server{}
	http1Server := &http.Server{
		Addr:    gwListener.Addr().String(),
		Handler: h2c.NewHandler(handler, http2Server),
	}
	go func() {
		err := http1Server.Serve(gwListener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			c.logger.ErrorContext(
				ctx,
				"REST gateway server failed",
				slog.Any("error", err),
			)
		}
	}()
	shutdown.AddHttpServer(network.HttpListenerName, 0, http1Server)

	// Keep running till the shutdown sequence cancels the context:
	c.logger.InfoContext(ctx, "Waiting for shutdown to sequence to complete")
	return shutdown.Wait()
}

func (c *startRestGatewayCommandRunner) handleHealth(
	w http.ResponseWriter, r *http.Request, p map[string]string) {
	response, err := c.healthClient.Check(r.Context(), &healthv1.HealthCheckRequest{})
	if err != nil {
		c.logger.ErrorContext(
			r.Context(),
			"Health check failed",
			slog.Any("error", err),
		)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if response.Status != healthv1.HealthCheckResponse_SERVING {
		c.logger.WarnContext(
			r.Context(),
			"Server is not serving",
			slog.String("status", response.Status.String()),
		)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// restGatewayUserAgent is the user agent string for the REST gateway.
const restGatewayUserAgent = "fulfillment-rest-gateway"
