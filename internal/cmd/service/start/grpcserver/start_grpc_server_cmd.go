/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package grpcserver

import (
	"context"
	"crypto/x509"
	"fmt"
	"log/slog"
	"math"
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
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
	"k8s.io/klog/v2"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/console"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/idp"
	"github.com/osac-project/fulfillment-service/internal/idp/keycloak"
	hubscheme "github.com/osac-project/fulfillment-service/internal/kubernetes/scheme"
	"github.com/osac-project/fulfillment-service/internal/logging"
	"github.com/osac-project/fulfillment-service/internal/metrics"
	"github.com/osac-project/fulfillment-service/internal/network"
	"github.com/osac-project/fulfillment-service/internal/oauth"
	"github.com/osac-project/fulfillment-service/internal/recovery"
	"github.com/osac-project/fulfillment-service/internal/servers"
	shtdwn "github.com/osac-project/fulfillment-service/internal/shutdown"
	"github.com/osac-project/fulfillment-service/internal/version"
)

// Cmd creates and returns the `start grpc-server` command.
func Cmd() *cobra.Command {
	runner := &runnerContext{}
	command := &cobra.Command{
		Use:                   "grpc-server [FLAG...]",
		Short:                 shortHelp,
		Long:                  longHelp,
		DisableFlagsInUseLine: true,
		Args:                  cobra.NoArgs,
		RunE:                  runner.run,
	}
	flags := command.Flags()
	network.AddListenerFlags(flags, network.GrpcListenerName, network.DefaultGrpcAddress)
	network.AddListenerFlags(flags, network.MetricsListenerName, network.DefaultMetricsAddress)
	database.AddFlags(flags)
	flags.StringVar(
		&runner.args.authType,
		"grpc-authn-type",
		auth.GrpcGuestAuthType,
		grpcAuthnTypeFlagHelp,
	)
	flags.StringVar(
		&runner.args.externalAuthAddress,
		"grpc-authn-external-address",
		"",
		grpcAuthnExternalAddressFlagHelp,
	)
	flags.StringSliceVar(
		&runner.args.caFiles,
		"ca-file",
		[]string{},
		caFileFlagHelp,
	)
	flags.StringSliceVar(
		&runner.args.trustedTokenIssuers,
		"grpc-authn-trusted-token-issuers",
		[]string{},
		grpcAuthnTrustedTokenIssuersFlagHelp,
	)
	flags.StringVar(
		&runner.args.tenancyLogic,
		"tenancy-logic",
		"default",
		tenancyLogicFlagHelp,
	)
	flags.StringVar(
		&runner.args.idpProvider,
		"idp-provider",
		"keycloak",
		idpProviderFlagHelp,
	)
	flags.StringVar(
		&runner.args.idpURL,
		"idp-url",
		"",
		idpURLFlagHelp,
	)
	flags.StringVar(
		&runner.args.idpClientID,
		"idp-client-id",
		"",
		idpClientIDFlagHelp,
	)
	flags.StringVar(
		&runner.args.idpClientSecretFile,
		"idp-client-secret-file",
		"",
		idpClientSecretFileFlagHelp,
	)
	return command
}

// runnerContext contains the data and logic needed to run the `start grpc-server` command.
type runnerContext struct {
	logger *slog.Logger
	flags  *pflag.FlagSet
	args   struct {
		caFiles             []string
		authType            string
		externalAuthAddress string
		trustedTokenIssuers []string
		tenancyLogic        string
		idpProvider         string
		idpURL              string
		idpClientID         string
		idpClientSecretFile string
	}
}

// run runs the `start grpc-server` command.
func (c *runnerContext) run(cmd *cobra.Command, argv []string) error {
	// Get the context and create a cancellable version:
	ctx, cancel := context.WithCancel(cmd.Context())

	// Get the dependencies from the context:
	c.logger = logging.LoggerFromContext(ctx)

	// Configure the Kubernetes libraries to use the logger:
	logrLogger := logr.FromSlogHandler(c.logger.Handler())
	crlog.SetLogger(logrLogger)
	klog.SetLogger(logrLogger)

	// Save the flags:
	c.flags = cmd.Flags()

	// Prepare the metrics registerer:
	metricsRegisterer := prometheus.DefaultRegisterer

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

	// Load the trusted CA certificates:
	caPool, err := network.NewCertPool().
		SetLogger(c.logger).
		AddFiles(c.args.caFiles...).
		Build()
	if err != nil {
		return fmt.Errorf("failed to load trusted CA certificates: %w", err)
	}

	// Wait till the database is available:
	dbTool, err := database.NewTool().
		SetLogger(c.logger).
		SetFlags(c.flags).
		Build()
	if err != nil {
		return err
	}
	c.logger.InfoContext(ctx, "Waiting for database")
	err = dbTool.Wait(ctx)
	if err != nil {
		return err
	}

	// Run the migrations:
	c.logger.InfoContext(ctx, "Running database migrations")
	err = dbTool.Migrate(ctx, math.MaxUint)
	if err != nil {
		return err
	}

	// Create the database connection pool:
	c.logger.InfoContext(ctx, "Creating database connection pool")
	dbPool, err := dbTool.Pool(ctx)
	if err != nil {
		return err
	}
	shutdown.AddDatabasePool("database", 0, dbPool)

	// Create the network listener:
	listener, err := network.NewListener().
		SetLogger(c.logger).
		SetFlags(c.flags, network.GrpcListenerName).
		Build()
	if err != nil {
		return err
	}

	// Prepare the logging interceptor:
	c.logger.InfoContext(ctx, "Creating logging interceptor")
	loggingInterceptor, err := logging.NewInterceptor().
		SetLogger(c.logger).
		SetFlags(c.flags).
		Build()
	if err != nil {
		return err
	}

	// Calculate the user agent:
	userAgent := fmt.Sprintf("%s/%s", grpcServerUserAgent, version.Get())

	// Prepare the auth interceptor:
	c.logger.InfoContext(
		ctx,
		"Creating auth interceptor",
		slog.String("type", c.args.authType),
	)
	var authUnaryInterceptor grpc.UnaryServerInterceptor
	var authStreamInterceptor grpc.StreamServerInterceptor
	switch strings.ToLower(c.args.authType) {
	case auth.GrpcGuestAuthType:
		guestAuthInterceptor, err := auth.NewGrpcGuestAuthInterceptor().
			SetLogger(c.logger).
			Build()
		if err != nil {
			return fmt.Errorf("failed to create guest auth interceptor: %w", err)
		}
		authUnaryInterceptor = guestAuthInterceptor.UnaryServer
		authStreamInterceptor = guestAuthInterceptor.StreamServer
	case auth.GrpcExternalAuthType:
		if c.args.externalAuthAddress == "" {
			return fmt.Errorf(
				"external auth address is required when auth type is '%s'",
				auth.GrpcExternalAuthType,
			)
		}
		externalAuthClient, err := network.NewGrpcClient().
			SetLogger(c.logger).
			SetAddress(c.args.externalAuthAddress).
			SetCaPool(caPool).
			SetUserAgent(userAgent).
			SetMetricsSubsystem("outbound").
			SetMetricsRegisterer(metricsRegisterer).
			Build()
		if err != nil {
			return fmt.Errorf("failed to create external auth client: %w", err)
		}
		externalAuthInterceptor, err := auth.NewGrpcExternalAuthInterceptor().
			SetLogger(c.logger).
			SetGrpcClient(externalAuthClient).
			AddPublicMethodRegex(publicMethodRegex).
			Build()
		if err != nil {
			return fmt.Errorf("failed to create external auth interceptor: %w", err)
		}
		authUnaryInterceptor = externalAuthInterceptor.UnaryServer
		authStreamInterceptor = externalAuthInterceptor.StreamServer
	default:
		return fmt.Errorf(
			"unknown auth type '%s', valid values are '%s' and '%s'",
			c.args.authType, auth.GrpcGuestAuthType, auth.GrpcExternalAuthType,
		)
	}

	// Prepare the panic interceptor:
	c.logger.InfoContext(ctx, "Creating panic interceptor")
	panicInterceptor, err := recovery.NewGrpcPanicInterceptor().
		SetLogger(c.logger).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create panic interceptor: %w", err)
	}

	// Prepare the metrics interceptor:
	c.logger.InfoContext(ctx, "Creating metrics interceptor")
	metricsInterceptor, err := metrics.NewGrpcInterceptor().
		SetSubsystem("inbound").
		SetRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create metrics interceptor: %w", err)
	}

	// Prepare the transactions interceptor:
	c.logger.InfoContext(ctx, "Creating transactions interceptor")
	txManager, err := database.NewTxManager().
		SetLogger(c.logger).
		SetPool(dbPool).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create transactions manager: %w", err)
	}
	txInterceptor, err := database.NewTxInterceptor().
		SetLogger(c.logger).
		SetManager(txManager).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create transactions interceptor: %w", err)
	}

	// Create the gRPC server:
	c.logger.InfoContext(ctx, "Creating gRPC server")
	grpcServer := grpc.NewServer(
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    15 * time.Second,
			Timeout: 10 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.ChainUnaryInterceptor(
			panicInterceptor.UnaryServer,
			metricsInterceptor.UnaryServer,
			loggingInterceptor.UnaryServer,
			authUnaryInterceptor,
			txInterceptor.UnaryServer,
		),
		grpc.ChainStreamInterceptor(
			panicInterceptor.StreamServer,
			metricsInterceptor.StreamServer,
			loggingInterceptor.StreamServer,
			authStreamInterceptor,
		),
	)
	shutdown.AddGrpcServer(network.GrpcListenerName, 0, grpcServer)

	// Register the reflection server:
	c.logger.InfoContext(ctx, "Registering gRPC reflection server")
	reflection.RegisterV1(grpcServer)

	// Register the health server:
	c.logger.InfoContext(ctx, "Registering gRPC health server")
	healthServer := health.NewServer()
	healthv1.RegisterHealthServer(grpcServer, healthServer)

	// Create the notifier:
	c.logger.InfoContext(ctx, "Creating notifier")
	notifier, err := database.NewNotifier().
		SetLogger(c.logger).
		SetChannel("events").
		SetPool(dbPool).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create notifier: %w", err)
	}
	err = notifier.Start(ctx)
	if err != nil {
		return fmt.Errorf("failed to start notifier: %w", err)
	}

	// Create the public attribution logic:
	c.logger.InfoContext(ctx, "Creating public attribution logic")
	publicAttributionLogic, err := auth.NewDefaultAttributionLogic().
		SetLogger(c.logger).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create public attribution logic: %w", err)
	}

	// Create the tenancy logic:
	c.logger.InfoContext(
		ctx,
		"Creating tenancy logic",
		slog.String("type", c.args.tenancyLogic),
	)
	var tenancyLogic auth.TenancyLogic
	switch strings.ToLower(c.args.tenancyLogic) {
	case "default":
		tenancyLogic, err = auth.NewDefaultTenancyLogic().
			SetLogger(c.logger).
			Build()
		if err != nil {
			return fmt.Errorf("failed to create default tenancy logic: %w", err)
		}
	case "guest":
		tenancyLogic, err = auth.NewGuestTenancyLogic().
			SetLogger(c.logger).
			Build()
		if err != nil {
			return fmt.Errorf("failed to create guest tenancy logic: %w", err)
		}
	default:
		return fmt.Errorf(
			"unknown tenancy logic '%s', valid values are 'default' and 'guest'",
			c.args.tenancyLogic,
		)
	}

	// Create the private attribution logic:
	c.logger.InfoContext(ctx, "Creating private attribution logic")
	privateAttributionLogic, err := auth.NewSystemAttributionLogic().
		SetLogger(c.logger).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create system attribution logic: %w", err)
	}

	// Create the capabilities servers:
	c.logger.InfoContext(ctx, "Creating capabilities servers")
	capabilitiesServer, err := servers.NewCapabilitiesServer().
		SetLogger(c.logger).
		AddAutnTrustedTokenIssuers(c.args.trustedTokenIssuers...).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create public capabilities server: %w", err)
	}
	publicv1.RegisterCapabilitiesServer(grpcServer, capabilitiesServer)
	privateCapabilitiesServer, err := servers.NewPrivateCapabilitiesServer().
		SetLogger(c.logger).
		AddAuthnTrustedTokenIssuers(c.args.trustedTokenIssuers...).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create private capabilities server: %w", err)
	}
	privatev1.RegisterCapabilitiesServer(grpcServer, privateCapabilitiesServer)

	// Create the cluster templates server:
	c.logger.InfoContext(ctx, "Creating cluster templates server")
	clusterTemplatesServer, err := servers.NewClusterTemplatesServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(publicAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create cluster templates server: %w", err)
	}
	publicv1.RegisterClusterTemplatesServer(grpcServer, clusterTemplatesServer)

	// Create the cluster catalog items server:
	c.logger.InfoContext(ctx, "Creating cluster catalog items server")
	clusterCatalogItemsServer, err := servers.NewClusterCatalogItemsServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(publicAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create cluster catalog items server: %w", err)
	}
	publicv1.RegisterClusterCatalogItemsServer(grpcServer, clusterCatalogItemsServer)

	// Create the compute instance catalog items server:
	c.logger.InfoContext(ctx, "Creating compute instance catalog items server")
	computeInstanceCatalogItemsServer, err := servers.NewComputeInstanceCatalogItemsServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(publicAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create compute instance catalog items server: %w", err)
	}
	publicv1.RegisterComputeInstanceCatalogItemsServer(grpcServer, computeInstanceCatalogItemsServer)

	// Create the private cluster templates server:
	c.logger.InfoContext(ctx, "Creating private cluster templates server")
	privateClusterTemplatesServer, err := servers.NewPrivateClusterTemplatesServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(privateAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create private cluster templates server: %w", err)
	}
	privatev1.RegisterClusterTemplatesServer(grpcServer, privateClusterTemplatesServer)

	// Create the private cluster catalog items server:
	c.logger.InfoContext(ctx, "Creating private cluster catalog items server")
	privateClusterCatalogItemsServer, err := servers.NewPrivateClusterCatalogItemsServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(privateAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create private cluster catalog items server: %w", err)
	}
	privatev1.RegisterClusterCatalogItemsServer(grpcServer, privateClusterCatalogItemsServer)

	// Create the private compute instance catalog items server:
	c.logger.InfoContext(ctx, "Creating private compute instance catalog items server")
	privateComputeInstanceCatalogItemsServer, err := servers.NewPrivateComputeInstanceCatalogItemsServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(privateAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create private compute instance catalog items server: %w", err)
	}
	privatev1.RegisterComputeInstanceCatalogItemsServer(grpcServer, privateComputeInstanceCatalogItemsServer)

	// Create the runtime scheme for typed OSAC API objects:
	hubScheme, err := hubscheme.NewHub()
	if err != nil {
		return fmt.Errorf("failed to create hub scheme: %w", err)
	}

	// Create the clusters server:
	c.logger.InfoContext(ctx, "Creating clusters server")
	clustersServer, err := servers.NewClustersServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(publicAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		SetScheme(hubScheme).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create clusters server: %w", err)
	}
	publicv1.RegisterClustersServer(grpcServer, clustersServer)

	// Create the private clusters server:
	c.logger.InfoContext(ctx, "Creating private clusters server")
	privateClustersServer, err := servers.NewPrivateClustersServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(privateAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create private clusters server: %w", err)
	}
	privatev1.RegisterClustersServer(grpcServer, privateClustersServer)

	// Create the host types server:
	c.logger.InfoContext(ctx, "Creating host types server")
	hostTypesServer, err := servers.NewHostTypesServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(publicAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create host types server: %w", err)
	}
	publicv1.RegisterHostTypesServer(grpcServer, hostTypesServer)

	// Create the private host types server:
	c.logger.InfoContext(ctx, "Creating private host types server")
	privateHostTypesServer, err := servers.NewPrivateHostTypesServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(privateAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create private host types server: %w", err)
	}
	privatev1.RegisterHostTypesServer(grpcServer, privateHostTypesServer)

	// Create the compute instance templates server:
	c.logger.InfoContext(ctx, "Creating compute instance templates server")
	computeInstanceTemplatesServer, err := servers.NewComputeInstanceTemplatesServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(publicAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create compute instance templates server: %w", err)
	}
	publicv1.RegisterComputeInstanceTemplatesServer(grpcServer, computeInstanceTemplatesServer)

	// Create the private compute instance templates server:
	c.logger.InfoContext(ctx, "Creating private compute instance templates server")
	privateComputeInstanceTemplatesServer, err := servers.NewPrivateComputeInstanceTemplatesServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(privateAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create private compute instance templates server: %w", err)
	}
	privatev1.RegisterComputeInstanceTemplatesServer(grpcServer, privateComputeInstanceTemplatesServer)

	// Create the compute instances server:
	c.logger.InfoContext(ctx, "Creating compute instances server")
	computeInstancesServer, err := servers.NewComputeInstancesServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(publicAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create compute instances server: %w", err)
	}
	publicv1.RegisterComputeInstancesServer(grpcServer, computeInstancesServer)

	// Create the private compute instances server:
	c.logger.InfoContext(ctx, "Creating private compute instances server")
	privateComputeInstancesServer, err := servers.NewPrivateComputeInstancesServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(privateAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create private compute instances server: %w", err)
	}
	privatev1.RegisterComputeInstancesServer(grpcServer, privateComputeInstancesServer)

	// Create the private hubs server:
	c.logger.InfoContext(ctx, "Creating hubs server")
	privateHubsServer, err := servers.NewPrivateHubsServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(privateAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create hubs server: %w", err)
	}
	privatev1.RegisterHubsServer(grpcServer, privateHubsServer)

	// Create the virtual networks server:
	c.logger.InfoContext(ctx, "Creating virtual networks server")
	virtualNetworksServer, err := servers.NewVirtualNetworksServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(publicAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create virtual networks server: %w", err)
	}
	publicv1.RegisterVirtualNetworksServer(grpcServer, virtualNetworksServer)

	// Create the private virtual networks server:
	c.logger.InfoContext(ctx, "Creating private virtual networks server")
	privateVirtualNetworksServer, err := servers.NewPrivateVirtualNetworksServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(privateAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create private virtual networks server: %w", err)
	}
	privatev1.RegisterVirtualNetworksServer(grpcServer, privateVirtualNetworksServer)

	// Create the subnets server:
	c.logger.InfoContext(ctx, "Creating subnets server")
	subnetsServer, err := servers.NewSubnetsServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(publicAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create subnets server: %w", err)
	}
	publicv1.RegisterSubnetsServer(grpcServer, subnetsServer)

	// Create the private subnets server:
	c.logger.InfoContext(ctx, "Creating private subnets server")
	privateSubnetsServer, err := servers.NewPrivateSubnetsServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(privateAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create private subnets server: %w", err)
	}
	privatev1.RegisterSubnetsServer(grpcServer, privateSubnetsServer)

	// Create the security groups server:
	c.logger.InfoContext(ctx, "Creating security groups server")
	securityGroupsServer, err := servers.NewSecurityGroupsServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(publicAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create security groups server: %w", err)
	}
	publicv1.RegisterSecurityGroupsServer(grpcServer, securityGroupsServer)

	// Create the private security groups server:
	c.logger.InfoContext(ctx, "Creating private security groups server")
	privateSecurityGroupsServer, err := servers.NewPrivateSecurityGroupsServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(privateAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create private security groups server: %w", err)
	}
	privatev1.RegisterSecurityGroupsServer(grpcServer, privateSecurityGroupsServer)

	// Create the network classes server:
	c.logger.InfoContext(ctx, "Creating network classes server")
	networkClassesServer, err := servers.NewNetworkClassesServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(publicAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create network classes server: %w", err)
	}
	publicv1.RegisterNetworkClassesServer(grpcServer, networkClassesServer)

	// Create the private network classes server:
	c.logger.InfoContext(ctx, "Creating private network classes server")
	privateNetworkClassesServer, err := servers.NewPrivateNetworkClassesServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(privateAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create private network classes server: %w", err)
	}
	privatev1.RegisterNetworkClassesServer(grpcServer, privateNetworkClassesServer)

	// Create the roles server:
	c.logger.InfoContext(ctx, "Creating roles server")
	rolesServer, err := servers.NewRolesServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(publicAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create roles server: %w", err)
	}
	publicv1.RegisterRolesServer(grpcServer, rolesServer)

	// Create the private roles server:
	c.logger.InfoContext(ctx, "Creating private roles server")
	privateRolesServer, err := servers.NewPrivateRolesServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(privateAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create private roles server: %w", err)
	}
	privatev1.RegisterRolesServer(grpcServer, privateRolesServer)

	// Create the role bindings server:
	c.logger.InfoContext(ctx, "Creating role bindings server")
	roleBindingsServer, err := servers.NewRoleBindingsServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(publicAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create role bindings server: %w", err)
	}
	publicv1.RegisterRoleBindingsServer(grpcServer, roleBindingsServer)

	// Create the private role bindings server:
	c.logger.InfoContext(ctx, "Creating private role bindings server")
	privateRoleBindingsServer, err := servers.NewPrivateRoleBindingsServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(privateAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create private role bindings server: %w", err)
	}
	privatev1.RegisterRoleBindingsServer(grpcServer, privateRoleBindingsServer)

	// Create the public IPs server:
	c.logger.InfoContext(ctx, "Creating public IPs server")
	publicIPsServer, err := servers.NewPublicIPsServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(publicAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create public IPs server: %w", err)
	}
	publicv1.RegisterPublicIPsServer(grpcServer, publicIPsServer)

	// Create the private public IP pools server:
	c.logger.InfoContext(ctx, "Creating private public IP pools server")
	privatePublicIPPoolsServer, err := servers.NewPrivatePublicIPPoolsServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(privateAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create private public IP pools server: %w", err)
	}
	privatev1.RegisterPublicIPPoolsServer(grpcServer, privatePublicIPPoolsServer)

	// Create the private public IPs server:
	c.logger.InfoContext(ctx, "Creating private public IPs server")
	privatePublicIPsServer, err := servers.NewPrivatePublicIPsServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(privateAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create private public IPs server: %w", err)
	}
	privatev1.RegisterPublicIPsServer(grpcServer, privatePublicIPsServer)

	// Create the private public IP attachments server:
	c.logger.InfoContext(ctx, "Creating private public IP attachments server")
	privatePublicIPAttachmentsServer, err := servers.NewPrivatePublicIPAttachmentsServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(privateAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create private public IP attachments server: %w", err)
	}
	privatev1.RegisterPublicIPAttachmentsServer(grpcServer, privatePublicIPAttachmentsServer)

	// Create the public public IP attachments server:
	c.logger.InfoContext(ctx, "Creating public public IP attachments server")
	publicIPAttachmentsServer, err := servers.NewPublicIPAttachmentsServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(publicAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create public IP attachments server: %w", err)
	}
	publicv1.RegisterPublicIPAttachmentsServer(grpcServer, publicIPAttachmentsServer)

	// Create the IDP client (optional - only needed for GrantAccess/RevokeAccess):
	var idpClient idp.Client
	if c.args.idpURL != "" {
		c.logger.InfoContext(ctx, "Creating IDP client")
		idpClient, err = c.createIdpClient(ctx, caPool)
		if err != nil {
			return fmt.Errorf("failed to create IDP client: %w", err)
		}
	} else {
		c.logger.WarnContext(ctx, "IDP client not configured - GrantAccess/RevokeAccess will not work")
	}

	// Create the public organizations server:
	c.logger.InfoContext(ctx, "Creating public organizations server")
	publicOrganizationsServer, err := servers.NewOrganizationsServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(publicAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create public organizations server: %w", err)
	}
	publicv1.RegisterOrganizationsServer(grpcServer, publicOrganizationsServer)

	// Create the private organizations server:
	c.logger.InfoContext(ctx, "Creating private organizations server")
	privateOrganizationsServer, err := servers.NewPrivateOrganizationsServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(privateAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create private organizations server: %w", err)
	}
	privatev1.RegisterOrganizationsServer(grpcServer, privateOrganizationsServer)

	// Create the public projects server:
	c.logger.InfoContext(ctx, "Creating public projects server")
	publicProjectsServer, err := servers.NewProjectsServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(publicAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		SetIdpClient(idpClient).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create public projects server: %w", err)
	}
	publicv1.RegisterProjectsServer(grpcServer, publicProjectsServer)

	// Create the private projects server:
	c.logger.InfoContext(ctx, "Creating private projects server")
	privateProjectsServer, err := servers.NewPrivateProjectsServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(privateAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create private projects server: %w", err)
	}
	privatev1.RegisterProjectsServer(grpcServer, privateProjectsServer)

	// Create the public users server:
	c.logger.InfoContext(ctx, "Creating public users server")
	publicUsersServer, err := servers.NewUsersServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(publicAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create public users server: %w", err)
	}
	publicv1.RegisterUsersServer(grpcServer, publicUsersServer)

	// Create the private users server:
	c.logger.InfoContext(ctx, "Creating private users server")
	privateUsersServer, err := servers.NewPrivateUsersServer().
		SetLogger(c.logger).
		SetNotifier(notifier).
		SetAttributionLogic(privateAttributionLogic).
		SetTenancyLogic(tenancyLogic).
		SetMetricsRegisterer(metricsRegisterer).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create private users server: %w", err)
	}
	privatev1.RegisterUsersServer(grpcServer, privateUsersServer)

	// Create the console manager and server:
	c.logger.InfoContext(ctx, "Creating console server")
	hubConfigProvider := console.HubConfigProviderFromKubeconfigs(
		func(ctx context.Context, id string) ([]byte, error) {
			tx, err := txManager.Begin(ctx)
			if err != nil {
				return nil, err
			}
			defer txManager.End(ctx, tx)
			txCtx := database.TxIntoContext(ctx, tx)
			resp, err := privateHubsServer.Get(txCtx, privatev1.HubsGetRequest_builder{
				Id: id,
			}.Build())
			if err != nil {
				return nil, err
			}
			return resp.GetObject().GetSpec().GetKubeconfig(), nil
		},
	)
	kvBackend, err := console.NewKubeVirtBackend().
		SetLogger(c.logger).
		SetHubConfigProvider(hubConfigProvider).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create kubevirt backend: %w", err)
	}
	consoleManager, err := console.NewManager().
		SetLogger(c.logger).
		AddBackend("compute_instance", kvBackend).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create console manager: %w", err)
	}
	consoleServer, err := servers.NewConsoleServer().
		SetLogger(c.logger).
		SetManager(consoleManager).
		SetComputeInstancesServer(privateComputeInstancesServer).
		SetHubServer(privateHubsServer).
		SetTxManager(txManager).
		SetScheme(hubScheme).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create console server: %w", err)
	}
	publicv1.RegisterConsoleServer(grpcServer, consoleServer)

	// Create the events server:
	c.logger.InfoContext(ctx, "Creating events server")
	eventsListener, err := database.NewListener().
		SetLogger(c.logger).
		SetUrl(dbTool.URL()).
		SetChannel("events").
		Build()
	if err != nil {
		return fmt.Errorf("failed to create events listener: %w", err)
	}
	eventsServer, err := servers.NewEventsServer().
		SetLogger(c.logger).
		SetListener(eventsListener).
		SetTenancyLogic(tenancyLogic).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create events server: %w", err)
	}
	go func() {
		err := eventsServer.Start(ctx)
		if err == nil || errors.Is(err, context.Canceled) {
			c.logger.InfoContext(ctx, "Events server finished")
		} else {
			c.logger.ErrorContext(
				ctx,
				"Events server finished",
				slog.Any("error", err),
			)
		}
	}()
	publicv1.RegisterEventsServer(grpcServer, eventsServer)

	// Create the private events server:
	c.logger.InfoContext(ctx, "Creating private events server")
	privateEventsListener, err := database.NewListener().
		SetLogger(c.logger).
		SetUrl(dbTool.URL()).
		SetChannel("events").
		Build()
	if err != nil {
		return fmt.Errorf("failed to create private events listener: %w", err)
	}
	privateEventsServer, err := servers.NewPrivateEventsServer().
		SetLogger(c.logger).
		SetListener(privateEventsListener).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create private events server: %w", err)
	}
	go func() {
		err := privateEventsServer.Start(ctx)
		if err == nil || errors.Is(err, context.Canceled) {
			c.logger.InfoContext(ctx, "Private events server finished")
		} else {
			c.logger.ErrorContext(
				ctx,
				"Private events server finished",
				slog.Any("error", err),
			)
		}
	}()
	privatev1.RegisterEventsServer(grpcServer, privateEventsServer)

	// Create the metrics listener:
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
	shutdown.AddHttpServer(network.MetricsListenerName, 0, metricsServer)
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

	// Start serving:
	c.logger.InfoContext(
		ctx,
		"Start serving",
		slog.String("address", listener.Addr().String()),
	)
	go func() {
		err := grpcServer.Serve(listener)
		if err != nil {
			c.logger.ErrorContext(
				ctx,
				"gRPC server failed",
				slog.Any("error", err),
			)
		}
	}()

	// Keep running till the shutdown sequence finishes:
	c.logger.InfoContext(ctx, "Waiting for shutdown to sequence to complete")
	return shutdown.Wait()
}

// createIdpClient creates the IDP client for managing authorization resources.
// This should only be called after checking that c.args.idpURL is set.
func (c *runnerContext) createIdpClient(ctx context.Context, caPool *x509.CertPool) (idp.Client, error) {
	// Validate required IDP configuration
	if c.args.idpClientID == "" {
		return nil, errors.New("IDP client ID is required (--idp-client-id)")
	}
	if c.args.idpClientSecretFile == "" {
		return nil, errors.New("IDP client secret file is required (--idp-client-secret-file)")
	}

	// Read the client secret
	clientSecret, err := c.readTrimmedFile(c.args.idpClientSecretFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read IDP client secret: %w", err)
	}

	c.logger.InfoContext(ctx, "Creating IDP token source",
		slog.String("url", c.args.idpURL),
		slog.String("client_id", c.args.idpClientID),
	)

	// Create a token store that saves the token in memory
	idpTokenStore, err := auth.NewMemoryTokenStore().
		SetLogger(c.logger).
		Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create IDP token store: %w", err)
	}

	// Create OAuth token source for IDP authentication
	idpTokenSource, err := oauth.NewTokenSource().
		SetLogger(c.logger).
		SetStore(idpTokenStore).
		SetCaPool(caPool).
		SetIssuer(c.args.idpURL).
		SetFlow(oauth.CredentialsFlow).
		SetClientId(c.args.idpClientID).
		SetClientSecret(clientSecret).
		Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create IDP token source: %w", err)
	}

	// Create IDP client based on provider type
	c.logger.InfoContext(ctx, "Creating IDP client",
		slog.String("provider", c.args.idpProvider),
	)

	var idpClient idp.Client
	switch c.args.idpProvider {
	case idp.ProviderKeycloak:
		idpClient, err = keycloak.NewClient().
			SetLogger(c.logger).
			SetBaseURL(c.args.idpURL).
			SetTokenSource(idpTokenSource).
			SetCaPool(caPool).
			Build()
		if err != nil {
			return nil, fmt.Errorf("failed to create Keycloak client: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported IDP provider: %s (supported: %s)", c.args.idpProvider, strings.Join(idp.ValidProviders, ", "))
	}

	c.logger.InfoContext(ctx, "IDP client created successfully")
	return idpClient, nil
}

// readTrimmedFile reads the content of the given file and returns it with all leading and trailing whitespace removed.
func (c *runnerContext) readTrimmedFile(file string) (result string, err error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return
	}
	result = strings.TrimSpace(string(data))
	return
}

// publicMethodRegex is regular expression for the methods that are considered public, including the capabilities, and
// reflection, health methods. These will skip authentication and authorization.
const publicMethodRegex = `^/(osac\.public\.v1\.Capabilities/|grpc\.(reflection|health)\.).*$`

// grpcServerUserAgent is the user agent string for the gRPC server.
const grpcServerUserAgent = "fulfillment-grpc-server"

const shortHelp = `Starts the gRPC server`

const longHelp = `
Starts the gRPC server.
`

const grpcAuthnTypeFlagHelp = `
_TYPE_ - Type of authentication. Valid values are {{ bt }}guest{{ bt }}
and {{ bt }}external{{ bt }}.
`

const grpcAuthnExternalAddressFlagHelp = `
_ADDRESS_ - Address of the external auth service using the
Envoy ext_authz gRPC protocol. Required when {{ bt }}--grpc-authn-type{{ bt }}
is set to {{ bt }}external{{ bt }}.
`

const caFileFlagHelp = `
_FILE_ - Files or directories containing trusted CA certificates in PEM
format. Used for TLS connections to the external auth service.
`

const grpcAuthnTrustedTokenIssuersFlagHelp = `
_ISSUERS_ - Comma separated list of token issuers that
are advertised as trusted by the gRPC server.
`

const tenancyLogicFlagHelp = `
_LOGIC_ - Type of tenancy logic to use. Valid values are
{{ bt }}default{{ bt }} and {{ bt }}guest{{ bt }}.
`

const idpProviderFlagHelp = `
_PROVIDER_ - Identity provider type. Valid values are
{{ bt }}keycloak{{ bt }}.
`

const idpURLFlagHelp = `
_URL_ - Base URL of the identity provider (e.g., https://keycloak.example.com/realms/osac).
`

const idpClientIDFlagHelp = `
_CLIENT_ID_ - Client ID for authenticating to the identity provider.
`

const idpClientSecretFileFlagHelp = `
_FILE_ - File containing the client secret for authenticating to the identity provider.
`
