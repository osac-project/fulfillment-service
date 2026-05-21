/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package controller

import (
	"context"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"k8s.io/klog/v2"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/controllers"
	"github.com/osac-project/fulfillment-service/internal/controllers/cluster"
	"github.com/osac-project/fulfillment-service/internal/controllers/computeinstance"
	"github.com/osac-project/fulfillment-service/internal/controllers/organization"
	"github.com/osac-project/fulfillment-service/internal/controllers/project"
	"github.com/osac-project/fulfillment-service/internal/controllers/publicip"
	"github.com/osac-project/fulfillment-service/internal/controllers/publicippool"
	"github.com/osac-project/fulfillment-service/internal/controllers/role"
	"github.com/osac-project/fulfillment-service/internal/controllers/rolebinding"
	"github.com/osac-project/fulfillment-service/internal/controllers/securitygroup"
	"github.com/osac-project/fulfillment-service/internal/controllers/subnet"
	"github.com/osac-project/fulfillment-service/internal/controllers/virtualnetwork"
	internalhealth "github.com/osac-project/fulfillment-service/internal/health"
	"github.com/osac-project/fulfillment-service/internal/idp"
	"github.com/osac-project/fulfillment-service/internal/idp/keycloak"
	hubscheme "github.com/osac-project/fulfillment-service/internal/kubernetes/scheme"
	"github.com/osac-project/fulfillment-service/internal/logging"
	"github.com/osac-project/fulfillment-service/internal/network"
	"github.com/osac-project/fulfillment-service/internal/oauth"
	shtdwn "github.com/osac-project/fulfillment-service/internal/shutdown"
	"github.com/osac-project/fulfillment-service/internal/version"
)

// Cmd creates and returns the `start controllers` command.
func Cmd() *cobra.Command {
	runner := &runnerContext{}
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
		&runner.args.apiClientId,
		"api-client-id",
		"",
		"OAuth client identifier for authentication with the API. Mutually exclusive with "+
			"'--api-client-id-file'.",
	)
	flags.StringVar(
		&runner.args.apiClientIdFile,
		"api-client-id-file",
		"",
		"File containing the OAuth client identifier for authentication with the API. Mutually exclusive with "+
			"'--api-client-id'.",
	)
	flags.StringVar(
		&runner.args.apiClientSecret,
		"api-client-secret",
		"",
		"OAuth client secret for authentication with the API. Mutually exclusive with "+
			"'--api-client-secret-file'.",
	)
	flags.StringVar(
		&runner.args.apiClientSecretFile,
		"api-client-secret-file",
		"",
		"File containing the OAuth client secret for authentication with the API. Mutually exclusive with "+
			"'--api-client-secret'.",
	)
	flags.StringVar(
		&runner.args.keycloakUrl,
		"keycloak-url",
		"",
		"Base URL of the Keycloak instance.",
	)
	flags.StringVar(
		&runner.args.keycloakClientIdFile,
		"keycloak-client-id-file",
		"",
		"File containing the OAuth client identifier for Keycloak authentication. "+
			"Mutually exclusive with '--keycloak-client-id'.",
	)
	flags.StringVar(
		&runner.args.keycloakClientId,
		"keycloak-client-id",
		"",
		"OAuth client identifier for Keycloak authentication. "+
			"Mutually exclusive with '--keycloak-client-id-file'.",
	)
	flags.StringVar(
		&runner.args.keycloakClientSecretFile,
		"keycloak-client-secret-file",
		"",
		"File containing the OAuth client secret for Keycloak authentication. "+
			"Mutually exclusive with '--keycloak-client-secret'.",
	)
	flags.StringVar(
		&runner.args.keycloakClientSecret,
		"keycloak-client-secret",
		"",
		"OAuth client secret for Keycloak authentication. "+
			"Mutually exclusive with '--keycloak-client-secret-file'.",
	)
	network.AddGrpcClientFlags(flags, network.APIClientName, network.DefaultGrpcAddress)
	network.AddListenerFlags(flags, network.GrpcListenerName, network.DefaultGrpcAddress)
	network.AddListenerFlags(flags, network.MetricsListenerName, network.DefaultMetricsAddress)
	return command
}

// runnerContext contains the data and logic needed to run the `start controllers` command.
type runnerContext struct {
	logger *slog.Logger
	flags  *pflag.FlagSet
	args   struct {
		caFiles                  []string
		apiClientId              string
		apiClientIdFile          string
		apiClientSecret          string
		apiClientSecretFile      string
		keycloakUrl              string
		keycloakClientId         string
		keycloakClientIdFile     string
		keycloakClientSecret     string
		keycloakClientSecretFile string
	}
	client *grpc.ClientConn
}

// run runs the `start controllers` command.
func (r *runnerContext) run(cmd *cobra.Command, argv []string) error {
	var err error

	// Get the context:
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	// Get the dependencies from the context:
	r.logger = logging.LoggerFromContext(ctx)

	// Configure the Kubernetes libraries to use the logger:
	logrLogger := logr.FromSlogHandler(r.logger.Handler())
	crlog.SetLogger(logrLogger)
	klog.SetLogger(logrLogger)

	// Save the flags:
	r.flags = cmd.Flags()

	// Check the flags:
	if r.args.apiClientId != "" && r.args.apiClientIdFile != "" {
		return fmt.Errorf("flags '--api-client-id' and '--api-client-id-file' are mutually exclusive")
	}
	if r.args.apiClientSecret != "" && r.args.apiClientSecretFile != "" {
		return fmt.Errorf("flags '--api-client-secret' and '--api-client-secret-file' are mutually exclusive")
	}
	if r.args.keycloakClientId != "" && r.args.keycloakClientIdFile != "" {
		return fmt.Errorf("flags '--keycloak-client-id' and '--keycloak-client-id-file' are mutually exclusive")
	}
	if r.args.keycloakClientSecret != "" && r.args.keycloakClientSecretFile != "" {
		return fmt.Errorf("flags '--keycloak-client-secret' and '--keycloak-client-secret-file' are mutually exclusive")
	}
	if r.args.keycloakUrl == "" {
		return fmt.Errorf("flag '--keycloak-url' is required")
	}
	if r.args.apiClientId == "" && r.args.apiClientIdFile == "" {
		return fmt.Errorf("flag '--api-client-id' or '--api-client-id-file' is required")
	}
	if r.args.apiClientSecret == "" && r.args.apiClientSecretFile == "" {
		return fmt.Errorf("flag '--api-client-secret' or '--api-client-secret-file' is required")
	}

	// Sanitize the Keycloak URL:
	for strings.HasSuffix(r.args.keycloakUrl, "/") {
		r.args.keycloakUrl = strings.TrimSuffix(r.args.keycloakUrl, "/")
	}

	// Prepare the metrics registerer:
	metricsRegisterer := prometheus.DefaultRegisterer

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
	tokenSource, err := r.createTokenSource(ctx, caPool)
	if err != nil {
		return err
	}

	// Calculate the user agent:
	r.logger.InfoContext(ctx, "Calculating user agent")
	userAgent := fmt.Sprintf("%s/%s", controllerUserAgent, version.Get())

	// Create the gRPC client:
	r.logger.InfoContext(ctx, "Creating gRPC client")
	r.client, err = network.NewGrpcClient().
		SetLogger(r.logger).
		SetFlags(r.flags, network.APIClientName).
		SetCaPool(caPool).
		SetTokenSource(tokenSource).
		SetUserAgent(userAgent).
		SetMetricsSubsystem("outbound").
		SetMetricsRegisterer(metricsRegisterer).
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

	// Create scheme for typed OSAC CRD access on hub clusters:
	hubScheme, err := hubscheme.NewHub()
	if err != nil {
		return fmt.Errorf("failed to create hub scheme: %w", err)
	}

	// Create the hub cache:
	r.logger.InfoContext(ctx, "Creating hub cache")
	hubCache, err := controllers.NewHubCache().
		SetLogger(r.logger).
		SetConnection(r.client).
		SetScheme(hubScheme).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create hub cache: %w", err)
	}

	// Create the IDP manager:
	idpManager, err := r.createIDPManager(ctx, caPool)
	if err != nil {
		return err
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

	// Create the subnet reconciler:
	r.logger.InfoContext(ctx, "Creating subnet reconciler")
	subnetReconcilerFunction, err := subnet.NewFunction().
		SetLogger(r.logger).
		SetConnection(r.client).
		SetHubCache(hubCache).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create subnet reconciler function: %w", err)
	}
	subnetReconciler, err := controllers.NewReconciler[*privatev1.Subnet]().
		SetLogger(r.logger).
		SetName("subnet").
		SetClient(r.client).
		SetFunction(subnetReconcilerFunction).
		SetEventFilter("has(event.subnet) || (has(event.hub) && event.type == EVENT_TYPE_OBJECT_CREATED)").
		SetHealthReporter(healthAggregator).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create subnet reconciler: %w", err)
	}

	// Start the subnet reconciler:
	r.logger.InfoContext(ctx, "Starting subnet reconciler")
	go func() {
		err := subnetReconciler.Start(ctx)
		if err == nil || errors.Is(err, context.Canceled) {
			r.logger.InfoContext(ctx, "Subnet reconciler finished")
		} else {
			r.logger.InfoContext(
				ctx,
				"Subnet reconciler failed",
				slog.Any("error", err),
			)
		}
	}()

	// Create the virtual network reconciler:
	r.logger.InfoContext(ctx, "Creating virtual network reconciler")
	virtualNetworkReconcilerFunction, err := virtualnetwork.NewFunction().
		SetLogger(r.logger).
		SetConnection(r.client).
		SetHubCache(hubCache).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create virtual network reconciler function: %w", err)
	}
	virtualNetworkReconciler, err := controllers.NewReconciler[*privatev1.VirtualNetwork]().
		SetLogger(r.logger).
		SetName("virtual_network").
		SetClient(r.client).
		SetFunction(virtualNetworkReconcilerFunction).
		SetEventFilter("has(event.virtual_network) || (has(event.hub) && event.type == EVENT_TYPE_OBJECT_CREATED)").
		SetHealthReporter(healthAggregator).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create virtual network reconciler: %w", err)
	}

	// Start the virtual network reconciler:
	r.logger.InfoContext(ctx, "Starting virtual network reconciler")
	go func() {
		err := virtualNetworkReconciler.Start(ctx)
		if err == nil || errors.Is(err, context.Canceled) {
			r.logger.InfoContext(ctx, "Virtual network reconciler finished")
		} else {
			r.logger.InfoContext(
				ctx,
				"Virtual network reconciler failed",
				slog.Any("error", err),
			)
		}
	}()

	// Create the security group reconciler:
	r.logger.InfoContext(ctx, "Creating security group reconciler")
	securityGroupReconcilerFunction, err := securitygroup.NewFunction().
		SetLogger(r.logger).
		SetConnection(r.client).
		SetHubCache(hubCache).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create security group reconciler function: %w", err)
	}
	securityGroupReconciler, err := controllers.NewReconciler[*privatev1.SecurityGroup]().
		SetLogger(r.logger).
		SetName("security_group").
		SetClient(r.client).
		SetFunction(securityGroupReconcilerFunction).
		SetEventFilter("has(event.security_group) || (has(event.hub) && event.type == EVENT_TYPE_OBJECT_CREATED)").
		SetHealthReporter(healthAggregator).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create security group reconciler: %w", err)
	}

	// Start the security group reconciler:
	r.logger.InfoContext(ctx, "Starting security group reconciler")
	go func() {
		err := securityGroupReconciler.Start(ctx)
		if err == nil || errors.Is(err, context.Canceled) {
			r.logger.InfoContext(ctx, "Security group reconciler finished")
		} else {
			r.logger.InfoContext(
				ctx,
				"Security group reconciler failed",
				slog.Any("error", err),
			)
		}
	}()

	// Create the public IP pool reconciler:
	r.logger.InfoContext(ctx, "Creating public IP pool reconciler")
	publicIPPoolReconcilerFunction, err := publicippool.NewFunction().
		SetLogger(r.logger).
		SetConnection(r.client).
		SetHubCache(hubCache).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create public IP pool reconciler function: %w", err)
	}
	publicIPPoolReconciler, err := controllers.NewReconciler[*privatev1.PublicIPPool]().
		SetLogger(r.logger).
		SetName("public_ip_pool").
		SetClient(r.client).
		SetFunction(publicIPPoolReconcilerFunction).
		SetEventFilter("has(event.public_ip_pool) || (has(event.hub) && event.type == EVENT_TYPE_OBJECT_CREATED)").
		SetHealthReporter(healthAggregator).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create public IP pool reconciler: %w", err)
	}

	// Start the public IP pool reconciler:
	r.logger.InfoContext(ctx, "Starting public IP pool reconciler")
	go func() {
		err := publicIPPoolReconciler.Start(ctx)
		if err == nil || errors.Is(err, context.Canceled) {
			r.logger.InfoContext(ctx, "Public IP pool reconciler finished")
		} else {
			r.logger.InfoContext(
				ctx,
				"Public IP pool reconciler failed",
				slog.Any("error", err),
			)
		}
	}()

	// Create the public IP reconciler:
	r.logger.InfoContext(ctx, "Creating public IP reconciler")
	publicIPReconcilerFunction, err := publicip.NewFunction().
		SetLogger(r.logger).
		SetConnection(r.client).
		SetHubCache(hubCache).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create public IP reconciler function: %w", err)
	}
	publicIPReconciler, err := controllers.NewReconciler[*privatev1.PublicIP]().
		SetLogger(r.logger).
		SetName("public_ip").
		SetClient(r.client).
		SetFunction(publicIPReconcilerFunction).
		SetEventFilter("has(event.public_ip) || (has(event.hub) && event.type == EVENT_TYPE_OBJECT_CREATED)").
		SetHealthReporter(healthAggregator).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create public IP reconciler: %w", err)
	}

	// Start the public IP reconciler:
	r.logger.InfoContext(ctx, "Starting public IP reconciler")
	go func() {
		err := publicIPReconciler.Start(ctx)
		if err == nil || errors.Is(err, context.Canceled) {
			r.logger.InfoContext(ctx, "Public IP reconciler finished")
		} else {
			r.logger.InfoContext(
				ctx,
				"Public IP reconciler failed",
				slog.Any("error", err),
			)
		}
	}()

	// Create the role reconciler:
	r.logger.InfoContext(ctx, "Creating role reconciler")
	roleReconcilerFunction, err := role.NewFunction().
		SetLogger(r.logger).
		SetConnection(r.client).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create role reconciler function: %w", err)
	}
	roleReconciler, err := controllers.NewReconciler[*privatev1.Role]().
		SetLogger(r.logger).
		SetName("role").
		SetClient(r.client).
		SetFunction(roleReconcilerFunction.Run).
		SetEventFilter("has(event.role)").
		SetHealthReporter(healthAggregator).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create role reconciler: %w", err)
	}

	// Start the role reconciler:
	r.logger.InfoContext(ctx, "Starting role reconciler")
	go func() {
		err := roleReconciler.Start(ctx)
		if err == nil || errors.Is(err, context.Canceled) {
			r.logger.InfoContext(ctx, "Role reconciler finished")
		} else {
			r.logger.InfoContext(
				ctx,
				"Role reconciler failed",
				slog.Any("error", err),
			)
		}
	}()

	// Create the role binding reconciler:
	r.logger.InfoContext(ctx, "Creating role binding reconciler")
	roleBindingReconcilerFunction, err := rolebinding.NewFunction().
		SetLogger(r.logger).
		SetConnection(r.client).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create role binding reconciler function: %w", err)
	}
	roleBindingReconciler, err := controllers.NewReconciler[*privatev1.RoleBinding]().
		SetLogger(r.logger).
		SetName("role_binding").
		SetClient(r.client).
		SetFunction(roleBindingReconcilerFunction.Run).
		SetEventFilter("has(event.role_binding)").
		SetHealthReporter(healthAggregator).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create role binding reconciler: %w", err)
	}

	// Start the role binding reconciler:
	r.logger.InfoContext(ctx, "Starting role binding reconciler")
	go func() {
		err := roleBindingReconciler.Start(ctx)
		if err == nil || errors.Is(err, context.Canceled) {
			r.logger.InfoContext(ctx, "Role binding reconciler finished")
		} else {
			r.logger.InfoContext(
				ctx,
				"Role binding reconciler failed",
				slog.Any("error", err),
			)
		}
	}()

	// Create the organization reconciler:
	r.logger.InfoContext(ctx, "Creating organization reconciler")
	organizationReconcilerFunction, err := organization.NewFunction().
		SetLogger(r.logger).
		SetConnection(r.client).
		SetIdpManager(idpManager).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create organization reconciler function: %w", err)
	}
	organizationReconciler, err := controllers.NewReconciler[*privatev1.Organization]().
		SetLogger(r.logger).
		SetName("organization").
		SetClient(r.client).
		SetFunction(organizationReconcilerFunction.Run).
		SetEventFilter("has(event.organization)").
		SetHealthReporter(healthAggregator).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create organization reconciler: %w", err)
	}

	// Start the organization reconciler:
	r.logger.InfoContext(ctx, "Starting organization reconciler")
	go func() {
		err := organizationReconciler.Start(ctx)
		if err == nil || errors.Is(err, context.Canceled) {
			r.logger.InfoContext(ctx, "Organization reconciler finished")
		} else {
			r.logger.InfoContext(
				ctx,
				"Organization reconciler failed",
				slog.Any("error", err),
			)
		}
	}()

	// Create the project reconciler:
	r.logger.InfoContext(ctx, "Creating project reconciler")
	projectReconcilerFunction, err := project.NewFunction().
		SetLogger(r.logger).
		SetConnection(r.client).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create project reconciler function: %w", err)
	}
	projectReconciler, err := controllers.NewReconciler[*privatev1.Project]().
		SetLogger(r.logger).
		SetName("project").
		SetClient(r.client).
		SetFunction(projectReconcilerFunction.Run).
		SetEventFilter("has(event.project)").
		SetHealthReporter(healthAggregator).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create project reconciler: %w", err)
	}

	// Start the project reconciler:
	r.logger.InfoContext(ctx, "Starting project reconciler")
	go func() {
		err := projectReconciler.Start(ctx)
		if err == nil || errors.Is(err, context.Canceled) {
			r.logger.InfoContext(ctx, "Project reconciler finished")
		} else {
			r.logger.InfoContext(
				ctx,
				"Project reconciler failed",
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
func (r *runnerContext) waitForServer(ctx context.Context) error {
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

// createTokenSource creates the token source used to authenticate the controller when it acts as a client of other
// services.
func (r *runnerContext) createTokenSource(ctx context.Context, caPool *x509.CertPool) (result auth.TokenSource,
	err error) {
	issuerUrl := fmt.Sprintf("%s/realms/%s", r.args.keycloakUrl, keycloakRealm)
	clientId := r.args.apiClientId
	if clientId == "" && r.args.apiClientIdFile != "" {
		clientId, err = r.readTrimmedFile(r.args.apiClientIdFile)
		if err != nil {
			err = fmt.Errorf(
				"failed to read API client identifier from file '%s': %w",
				r.args.apiClientIdFile, err,
			)
			return
		}
	}
	clientSecret := r.args.apiClientSecret
	if clientSecret == "" && r.args.apiClientSecretFile != "" {
		clientSecret, err = r.readTrimmedFile(r.args.apiClientSecretFile)
		if err != nil {
			err = fmt.Errorf(
				"failed to read API client secret from file '%s': %w",
				r.args.apiClientSecretFile, err,
			)
			return
		}
	}
	r.logger.DebugContext(
		ctx,
		"Credentials from flags",
		slog.String("issuer_url", issuerUrl),
		slog.String("!client_id", clientId),
		slog.String("!client_secret", clientSecret),
	)

	// Create a token store that saves the token in memory:
	tokenStore, err := auth.NewMemoryTokenStore().
		SetLogger(r.logger).
		Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create token store: %w", err)
	}

	// Create an token source:
	tokenSource, err := oauth.NewTokenSource().
		SetLogger(r.logger).
		SetStore(tokenStore).
		SetCaPool(caPool).
		SetIssuer(issuerUrl).
		SetFlow(oauth.CredentialsFlow).
		SetClientId(clientId).
		SetClientSecret(clientSecret).
		Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create token source: %w", err)
	}

	// Return the token source:
	result = tokenSource
	return
}

// createIDPManager creates the Keycloak client and organization manager.
func (r *runnerContext) createIDPManager(ctx context.Context, caPool *x509.CertPool) (*idp.OrganizationManager, error) {
	if r.args.keycloakClientId == "" && r.args.keycloakClientIdFile == "" {
		return nil, fmt.Errorf("flag '--keycloak-client-id' or '--keycloak-client-id-file' is required")
	}
	if r.args.keycloakClientSecret == "" && r.args.keycloakClientSecretFile == "" {
		return nil, fmt.Errorf("flag '--keycloak-client-secret' or '--keycloak-client-secret-file' is required")
	}

	keycloakClientId := r.args.keycloakClientId
	if keycloakClientId == "" && r.args.keycloakClientIdFile != "" {
		var err error
		keycloakClientId, err = r.readTrimmedFile(r.args.keycloakClientIdFile)
		if err != nil {
			return nil, fmt.Errorf(
				"failed to read Keycloak client identifier from file '%s': %w",
				r.args.keycloakClientIdFile, err,
			)
		}
	}

	keycloakClientSecret := r.args.keycloakClientSecret
	if keycloakClientSecret == "" && r.args.keycloakClientSecretFile != "" {
		var err error
		keycloakClientSecret, err = r.readTrimmedFile(r.args.keycloakClientSecretFile)
		if err != nil {
			return nil, fmt.Errorf(
				"failed to read Keycloak client secret from file '%s': %w",
				r.args.keycloakClientSecretFile, err,
			)
		}
	}

	// Calcualte the issuer URL:
	issuerUrl := fmt.Sprintf("%s/realms/%s", r.args.keycloakUrl, keycloakRealm)
	r.logger.DebugContext(
		ctx,
		"Keycloak credentials from flags",
		slog.String("issuer_url", issuerUrl),
		slog.String("!client_id", keycloakClientId),
		slog.String("!client_secret", keycloakClientSecret),
	)

	keycloakTokenStore, err := auth.NewMemoryTokenStore().
		SetLogger(r.logger).
		Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create Keycloak token store: %w", err)
	}

	r.logger.InfoContext(ctx, "Creating Keycloak token source")
	keycloakTokenSource, err := oauth.NewTokenSource().
		SetLogger(r.logger).
		SetStore(keycloakTokenStore).
		SetCaPool(caPool).
		SetIssuer(issuerUrl).
		SetFlow(oauth.CredentialsFlow).
		SetClientId(keycloakClientId).
		SetClientSecret(keycloakClientSecret).
		Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create Keycloak token source: %w", err)
	}

	r.logger.InfoContext(ctx, "Creating Keycloak client")
	keycloakClient, err := keycloak.NewClient().
		SetLogger(r.logger).
		SetBaseURL(r.args.keycloakUrl).
		SetTokenSource(keycloakTokenSource).
		SetCaPool(caPool).
		Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create Keycloak client: %w", err)
	}

	r.logger.InfoContext(ctx, "Creating organization manager")
	idpManager, err := idp.NewOrganizationManager().
		SetLogger(r.logger).
		SetClient(keycloakClient).
		Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create organization manager: %w", err)
	}

	r.logger.InfoContext(ctx, "Organization manager created successfully")
	return idpManager, nil
}

// readTrimmedFile reads the content of the given file and returns it with all leading and trailing whitespace removed.
func (r *runnerContext) readTrimmedFile(file string) (result string, err error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return
	}
	result = strings.TrimSpace(string(data))
	return
}

// controllerUserAgent is the user agent string for the controller.
const controllerUserAgent = "fulfillment-controller"

// keycloakRealm is the name of the Keycloak realm used by the system.
const keycloakRealm = "osac"
