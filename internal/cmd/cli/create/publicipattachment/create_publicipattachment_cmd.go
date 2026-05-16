/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package publicipattachment

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/config"
	"github.com/osac-project/fulfillment-service/internal/logging"
	"github.com/osac-project/fulfillment-service/internal/terminal"
)

func Cmd() *cobra.Command {
	runner := &runnerContext{}
	result := &cobra.Command{
		Use:   "publicipattachment [flags]",
		Short: "Attach a public IP to a compute instance",
		Long: "Create a PublicIPAttachment to bind a public IP to a compute instance. " +
			"Both --publicip and --compute-instance flags are required.",
		Example: `  # Attach a public IP to a compute instance
  osac create publicipattachment --publicip my-ip --compute-instance my-vm

  # Attach using IDs
  osac create publicipattachment --publicip pip-abc123 --compute-instance ci-xyz789`,
		Args: cobra.NoArgs,
		RunE: runner.run,
	}
	flags := result.Flags()
	flags.StringVar(
		&runner.args.publicIP,
		"publicip",
		"",
		"ID or name of the public IP to attach.",
	)
	flags.StringVar(
		&runner.args.computeInstance,
		"compute-instance",
		"",
		"ID or name of the compute instance to attach the public IP to.",
	)
	result.MarkFlagRequired("publicip")         //nolint:errcheck
	result.MarkFlagRequired("compute-instance") //nolint:errcheck
	return result
}

type runnerContext struct {
	args struct {
		publicIP        string
		computeInstance string
	}
	logger  *slog.Logger
	console *terminal.Console
}

func (c *runnerContext) run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	c.logger = logging.LoggerFromContext(ctx)
	c.console = terminal.ConsoleFromContext(ctx)

	cfg, err := config.Load(ctx)
	if err != nil {
		return err
	}
	if cfg.Address == "" {
		return fmt.Errorf("there is no configuration, run the 'login' command")
	}

	conn, err := cfg.Connect(ctx, cmd.Flags())
	if err != nil {
		return fmt.Errorf("failed to create gRPC connection: %w", err)
	}
	defer conn.Close()

	// Resolve the PublicIP by name or ID:
	publicIPsClient := publicv1.NewPublicIPsClient(conn)
	filter := fmt.Sprintf(`this.id == %[1]q || this.metadata.name == %[1]q`, c.args.publicIP)
	listResponse, err := publicIPsClient.List(ctx, publicv1.PublicIPsListRequest_builder{
		Filter: &filter,
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to resolve public IP '%s': %w", c.args.publicIP, err)
	}
	if len(listResponse.GetItems()) == 0 {
		return fmt.Errorf("public IP not found: %s", c.args.publicIP)
	}
	if len(listResponse.GetItems()) > 1 {
		return fmt.Errorf("multiple public IPs match '%s', use the ID instead", c.args.publicIP)
	}
	pipID := listResponse.GetItems()[0].GetId()

	// Create the PublicIPAttachment:
	attachClient := publicv1.NewPublicIPAttachmentsClient(conn)
	attachment := publicv1.PublicIPAttachment_builder{
		Spec: publicv1.PublicIPAttachmentSpec_builder{
			PublicIp:        pipID,
			ComputeInstance: &c.args.computeInstance,
		}.Build(),
	}.Build()

	response, err := attachClient.Create(ctx, publicv1.PublicIPAttachmentsCreateRequest_builder{
		Object: attachment,
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to create public IP attachment: %w", err)
	}

	c.console.Infof(ctx, "Created public IP attachment '%s' (public IP '%s' -> compute instance '%s').\n",
		response.GetObject().GetId(), pipID, c.args.computeInstance)

	return nil
}
