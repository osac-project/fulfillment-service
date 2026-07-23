/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package seedcatalogitems

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/logging"
	"github.com/osac-project/fulfillment-service/internal/network"
	"github.com/osac-project/fulfillment-service/internal/terminal"
)

func Cmd() *cobra.Command {
	runner := &runnerContext{}
	result := &cobra.Command{
		Use:                   "seed-catalog-items [FLAG...]",
		Short:                 shortHelp,
		Long:                  longHelp,
		DisableFlagsInUseLine: true,
		Args:                  cobra.NoArgs,
		RunE:                  runner.run,
	}
	flags := result.Flags()
	flags.StringVar(
		&runner.args.apiUrl,
		"api-url",
		"",
		apiUrlFlagHelp,
	)
	flags.StringVar(
		&runner.args.token,
		"token",
		"",
		tokenFlagHelp,
	)
	flags.BoolVar(
		&runner.args.insecure,
		"insecure",
		false,
		insecureFlagHelp,
	)
	flags.BoolVar(
		&runner.args.dryRun,
		"dry-run",
		false,
		dryRunFlagHelp,
	)
	return result
}

type runnerContext struct {
	args struct {
		apiUrl   string
		token    string
		insecure bool
		dryRun   bool
	}
	logger  *slog.Logger
	console *terminal.Console
}

func (c *runnerContext) run(cmd *cobra.Command, args []string) error {
	var err error

	// Get the context with a timeout to avoid blocking indefinitely on stalled RPCs:
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()

	// Get the logger and the console:
	c.logger = logging.LoggerFromContext(ctx)
	c.console = terminal.ConsoleFromContext(ctx)

	// Check that the API URL is provided:
	if c.args.apiUrl == "" {
		return fmt.Errorf("API URL is required")
	}

	// Check that the token is provided:
	if c.args.token == "" {
		return fmt.Errorf("token is required")
	}

	// Create the token source:
	token := &auth.Token{
		Access: c.args.token,
	}
	tokenSource, err := auth.NewStaticTokenSource().
		SetLogger(c.logger).
		SetToken(token).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create token source: %w", err)
	}

	// Create the gRPC client connection:
	conn, err := network.NewGrpcClient().
		SetLogger(c.logger).
		SetAddress(c.args.apiUrl).
		SetInsecure(c.args.insecure).
		SetTokenSource(tokenSource).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create gRPC client: %w", err)
	}
	defer func() {
		err := conn.Close()
		if err != nil {
			c.logger.WarnContext(
				ctx,
				"Failed to close gRPC connection",
				slog.Any("error", err),
			)
		}
	}()

	// Create the clients:
	clusterClient := privatev1.NewClusterCatalogItemsClient(conn)
	computeClient := privatev1.NewComputeInstanceCatalogItemsClient(conn)

	// Seed cluster catalog items:
	c.console.Infof(ctx, "→ Cluster Catalog Items\n")
	created, skipped, err := c.seedClusterCatalogItems(ctx, clusterClient)
	if err != nil {
		return err
	}
	totalCreated := created
	totalSkipped := skipped

	c.console.Infof(ctx, "\n")

	// Seed compute instance catalog items:
	c.console.Infof(ctx, "→ Compute Instance Catalog Items\n")
	created, skipped, err = c.seedComputeCatalogItems(ctx, computeClient)
	if err != nil {
		return err
	}
	totalCreated += created
	totalSkipped += skipped

	c.console.Infof(ctx, "\n")
	if c.args.dryRun {
		c.console.Infof(ctx, "Dry run: Would create: %d | Skip: %d\n", totalCreated, totalSkipped)
	} else {
		c.console.Infof(ctx, "Seeding complete. Created: %d | Skipped: %d\n", totalCreated, totalSkipped)
	}

	return nil
}

// validationSchema marshals a JSON Schema map to a string. The input contains only primitive types
// (strings and ints), so json.Marshal cannot fail here.
func validationSchema(schema map[string]any) string {
	bytes, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal validation schema: %v", err))
	}
	return string(bytes)
}

func cidrValidationSchema() string {
	return validationSchema(map[string]any{
		"type":    "string",
		"pattern": `^([0-9]{1,3}\.){3}[0-9]{1,3}/[0-9]{1,2}$`,
	})
}

// vmFieldDefinitions returns common field definitions for VM catalog items.
func vmFieldDefinitions(defaultInstanceType, defaultImage string) []*privatev1.FieldDefinition {
	return []*privatev1.FieldDefinition{
		{
			Path:        "instance_type",
			DisplayName: "Instance Type",
			Editable:    true,
			Default:     structpb.NewStringValue(defaultInstanceType),
			ValidationSchema: validationSchema(map[string]any{
				"type":      "string",
				"minLength": 1,
			}),
		},
		{
			Path:        "boot_disk.size_gib",
			DisplayName: "Boot Disk Size (GiB)",
			Editable:    true,
			Default:     structpb.NewNumberValue(120),
			ValidationSchema: validationSchema(map[string]any{
				"type":    "integer",
				"minimum": 10,
				"maximum": 1024,
			}),
		},
		{
			Path:        "image.source_ref",
			DisplayName: "Container Disk Image",
			Editable:    true,
			Default:     structpb.NewStringValue(defaultImage),
			ValidationSchema: validationSchema(map[string]any{
				"type":    "string",
				"pattern": `^[a-z0-9./-]+:[a-z0-9._-]+$`,
			}),
		},
		{
			Path:        "image.source_type",
			DisplayName: "Image Source Type",
			Editable:    false,
			Default:     structpb.NewStringValue("registry"),
			ValidationSchema: validationSchema(map[string]any{
				"type": "string",
			}),
		},
		{
			Path:        "run_strategy",
			DisplayName: "Run Strategy",
			Editable:    true,
			Default:     structpb.NewStringValue("Always"),
			ValidationSchema: validationSchema(map[string]any{
				"type": "string",
				"enum": []string{"Always", "Halted"},
			}),
		},
		{
			Path:        "user_data",
			DisplayName: "Cloud Init User Data",
			Editable:    true,
			ValidationSchema: validationSchema(map[string]any{
				"type": "string",
			}),
		},
	}
}

func (c *runnerContext) seedClusterCatalogItems(ctx context.Context, client privatev1.ClusterCatalogItemsClient) (created, skipped int, err error) {
	cidrSchema := cidrValidationSchema()

	items := []struct {
		name string
		item *privatev1.ClusterCatalogItem
	}{
		{
			name: "simple-ocp-4-17-cluster",
			item: &privatev1.ClusterCatalogItem{
				Metadata: &privatev1.Metadata{
					Name: "simple-ocp-4-17-cluster",
				},
				Title:       "Simple OpenShift 4.17 Cluster",
				Description: "A small OpenShift 4.17 cluster with 2 worker nodes on fc430 hardware. Suitable for development and testing workloads.",
				Template:    "osac.templates.ocp_4_17_small",
				Published:   true,
				FieldDefinitions: []*privatev1.FieldDefinition{
					{
						Path:             "spec.network.pod_cidr",
						DisplayName:      "Pod CIDR",
						Editable:         true,
						ValidationSchema: cidrSchema,
					},
					{
						Path:             "spec.network.service_cidr",
						DisplayName:      "Service CIDR",
						Editable:         true,
						ValidationSchema: cidrSchema,
					},
				},
			},
		},
		{
			name: "ocp-4-20-nico-baremetal-cluster",
			item: &privatev1.ClusterCatalogItem{
				Metadata: &privatev1.Metadata{
					Name: "ocp-4-20-nico-baremetal-cluster",
				},
				Title:       "OpenShift 4.20 Cluster (NICo Bare Metal)",
				Description: "An OpenShift 4.20 cluster on NICo bare metal infrastructure with DGX nodes. Optimized for GPU workloads and high-performance computing.",
				Template:    "osac.templates.ocp_4_20_small_nico",
				Published:   true,
				FieldDefinitions: []*privatev1.FieldDefinition{
					{
						Path:             "spec.network.pod_cidr",
						DisplayName:      "Pod CIDR",
						Editable:         true,
						ValidationSchema: cidrSchema,
					},
					{
						Path:             "spec.network.service_cidr",
						DisplayName:      "Service CIDR",
						Editable:         true,
						ValidationSchema: cidrSchema,
					},
				},
			},
		},
	}

	for _, item := range items {
		itemCreated, itemSkipped, itemErr := c.createClusterItemIfMissing(ctx, client, item.name, item.item)
		if itemErr != nil {
			return created, skipped, itemErr
		}
		created += itemCreated
		skipped += itemSkipped
	}

	return created, skipped, nil
}

func (c *runnerContext) seedComputeCatalogItems(ctx context.Context, client privatev1.ComputeInstanceCatalogItemsClient) (created, skipped int, err error) {
	items := []struct {
		name string
		item *privatev1.ComputeInstanceCatalogItem
	}{
		{
			name: "linux-vm",
			item: &privatev1.ComputeInstanceCatalogItem{
				Metadata: &privatev1.Metadata{
					Name: "linux-vm",
				},
				Title:            "Linux Virtual Machine",
				Description:      "A general-purpose Linux virtual machine with customizable CPU, memory, and disk resources.",
				Template:         "osac.templates.ocp_virt_vm",
				Published:        true,
				FieldDefinitions: vmFieldDefinitions("cx1.2xlarge", "quay.io/containerdisks/fedora:latest"),
			},
		},
		{
			name: "windows-vm",
			item: &privatev1.ComputeInstanceCatalogItem{
				Metadata: &privatev1.Metadata{
					Name: "windows-vm",
				},
				Title:            "Windows Virtual Machine",
				Description:      "A Windows virtual machine with customizable CPU and memory resources.",
				Template:         "osac.templates.ocp_virt_vm",
				Published:        true,
				FieldDefinitions: vmFieldDefinitions("cx1.2xlarge", "quay.io/containerdisks/windows-server:latest"),
			},
		},
	}

	for _, item := range items {
		itemCreated, itemSkipped, itemErr := c.createComputeItemIfMissing(ctx, client, item.name, item.item)
		if itemErr != nil {
			return created, skipped, itemErr
		}
		created += itemCreated
		skipped += itemSkipped
	}

	return created, skipped, nil
}

func (c *runnerContext) createClusterItemIfMissing(ctx context.Context, client privatev1.ClusterCatalogItemsClient, name string, item *privatev1.ClusterCatalogItem) (created, skipped int, err error) {
	listResp, err := client.List(ctx, &privatev1.ClusterCatalogItemsListRequest{})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list cluster catalog items: %w", err)
	}

	for _, existing := range listResp.Items {
		if existing.Metadata != nil && existing.Metadata.Name == name {
			c.console.Infof(ctx, "  Already exists: %s\n", name)
			c.logger.InfoContext(ctx, "Catalog item already exists",
				slog.String("name", name),
				slog.String("type", "cluster"))
			return 0, 1, nil
		}
	}

	if c.args.dryRun {
		c.console.Infof(ctx, "  Would create: %s\n", name)
		c.logger.InfoContext(ctx, "Dry run: would create catalog item",
			slog.String("name", name),
			slog.String("type", "cluster"))
		return 1, 0, nil
	}

	_, err = client.Create(ctx, &privatev1.ClusterCatalogItemsCreateRequest{
		Object: item,
	})
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.AlreadyExists {
			c.console.Infof(ctx, "  Already exists: %s\n", name)
			c.logger.InfoContext(ctx, "Catalog item already exists (race)",
				slog.String("name", name),
				slog.String("type", "cluster"))
			return 0, 1, nil
		}
		return 0, 0, fmt.Errorf("failed to create %s: %w", name, err)
	}

	c.console.Infof(ctx, "  Created: %s\n", name)
	c.logger.InfoContext(ctx, "Created catalog item",
		slog.String("name", name),
		slog.String("type", "cluster"))
	return 1, 0, nil
}

func (c *runnerContext) createComputeItemIfMissing(ctx context.Context, client privatev1.ComputeInstanceCatalogItemsClient, name string, item *privatev1.ComputeInstanceCatalogItem) (created, skipped int, err error) {
	listResp, err := client.List(ctx, &privatev1.ComputeInstanceCatalogItemsListRequest{})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list compute instance catalog items: %w", err)
	}

	for _, existing := range listResp.Items {
		if existing.Metadata != nil && existing.Metadata.Name == name {
			c.console.Infof(ctx, "  Already exists: %s\n", name)
			c.logger.InfoContext(ctx, "Catalog item already exists",
				slog.String("name", name),
				slog.String("type", "compute"))
			return 0, 1, nil
		}
	}

	if c.args.dryRun {
		c.console.Infof(ctx, "  Would create: %s\n", name)
		c.logger.InfoContext(ctx, "Dry run: would create catalog item",
			slog.String("name", name),
			slog.String("type", "compute"))
		return 1, 0, nil
	}

	_, err = client.Create(ctx, &privatev1.ComputeInstanceCatalogItemsCreateRequest{
		Object: item,
	})
	if err != nil {
		if st, ok := status.FromError(err); ok && st.Code() == codes.AlreadyExists {
			c.console.Infof(ctx, "  Already exists: %s\n", name)
			c.logger.InfoContext(ctx, "Catalog item already exists (race)",
				slog.String("name", name),
				slog.String("type", "compute"))
			return 0, 1, nil
		}
		return 0, 0, fmt.Errorf("failed to create %s: %w", name, err)
	}

	c.console.Infof(ctx, "  Created: %s\n", name)
	c.logger.InfoContext(ctx, "Created catalog item",
		slog.String("name", name),
		slog.String("type", "compute"))
	return 1, 0, nil
}

const shortHelp = "Seed default catalog items into the OSAC fulfillment service"

const longHelp = `
Seed default catalog items into the OSAC fulfillment service.

This command is idempotent and safe to run multiple times. It will skip catalog items that already exist.
`

const apiUrlFlagHelp = `
_URL_ - The fulfillment service API URL (e.g., {{ bt }}fulfillment-api.osac.svc.cluster.local:443{{ bt }}).
`

const tokenFlagHelp = `
_TOKEN_ - Bearer token for authentication.
`

const insecureFlagHelp = `
_[BOOLEAN]_ - Skip TLS certificate verification (for development only).
`

const dryRunFlagHelp = `
_[BOOLEAN]_ - Show what would be done without creating catalog items.
`
