/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package publicip

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/config"
	"github.com/osac-project/fulfillment-service/internal/logging"
	"github.com/osac-project/fulfillment-service/internal/terminal"
)

func Cmd() *cobra.Command {
	runner := &runnerContext{}
	result := &cobra.Command{
		Use:     "publicip [flags]",
		Aliases: []string{string(proto.MessageName((*publicv1.PublicIP)(nil)))},
		Short:   "Create a public IP",
		Long:    "Allocate a public IP address from an existing PublicIPPool.",
		Example: `  # Create a public IP from a pool
  osac create publicip --name my-ip --pool pool-abc123`,
		Args: cobra.NoArgs,
		RunE: runner.run,
	}
	flags := result.Flags()
	flags.StringVarP(
		&runner.args.name,
		"name",
		"n",
		"",
		"Name of the public IP.",
	)
	flags.StringVar(
		&runner.args.pool,
		"pool",
		"",
		"ID of the parent PublicIPPool to allocate the address from.",
	)
	result.MarkFlagRequired("pool") //nolint:errcheck
	// Note: attaching a compute instance at creation time (via --compute-instance flag) is future
	// scope. To attach a public IP to a compute instance, use 'osac create publicipattachment'.
	return result
}

type runnerContext struct {
	args struct {
		name string
		pool string
	}
	logger  *slog.Logger
	console *terminal.Console
}

func (c *runnerContext) run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	c.logger = logging.LoggerFromContext(ctx)
	c.console = terminal.ConsoleFromContext(ctx)

	cfg := config.SettingsFromContext(ctx)
	if !cfg.Armed() {
		return fmt.Errorf("there is no configuration, run the 'login' command")
	}

	conn, err := cfg.Connect(ctx, cmd.Flags())
	if err != nil {
		return fmt.Errorf("failed to create gRPC connection: %w", err)
	}
	defer conn.Close()

	client := publicv1.NewPublicIPsClient(conn)

	spec := publicv1.PublicIPSpec_builder{
		Pool: c.args.pool,
	}
	publicIP := publicv1.PublicIP_builder{
		Metadata: publicv1.Metadata_builder{Name: c.args.name}.Build(),
		Spec:     spec.Build(),
	}.Build()

	response, err := client.Create(ctx, publicv1.PublicIPsCreateRequest_builder{Object: publicIP}.Build())
	if err != nil {
		return fmt.Errorf("failed to create public IP: %w", err)
	}

	c.console.Infof(ctx, "Created public IP '%s' (ID: %s).\n", response.Object.GetMetadata().GetName(), response.Object.GetId())

	return nil
}
