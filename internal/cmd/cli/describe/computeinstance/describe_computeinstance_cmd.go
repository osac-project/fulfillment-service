/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package computeinstance

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/config"
	"github.com/osac-project/fulfillment-service/internal/logging"
	"github.com/osac-project/fulfillment-service/internal/terminal"
)

// Cmd creates the command to describe a compute instance.
func Cmd() *cobra.Command {
	runner := &runnerContext{}
	result := &cobra.Command{
		Use:   "computeinstance [flags] ID",
		Short: "Describe a compute instance",
		RunE:  runner.run,
	}
	return result
}

type runnerContext struct {
	logger  *slog.Logger
	console *terminal.Console
}

func (c *runnerContext) run(cmd *cobra.Command, args []string) error {
	// Check that there is exactly one compute instance ID specified
	if len(args) != 1 {
		fmt.Fprintf(
			os.Stderr,
			"Expected exactly one compute instance ID\n",
		)
		os.Exit(1)
	}
	id := args[0]

	// Get the context:
	ctx := cmd.Context()

	// Get the logger and console:
	c.logger = logging.LoggerFromContext(ctx)
	c.console = terminal.ConsoleFromContext(ctx)

	// Get the configuration:
	cfg, err := config.Load(ctx)
	if err != nil {
		return err
	}
	if cfg.Address == "" {
		return fmt.Errorf("there is no configuration, run the 'login' command")
	}

	// Create the gRPC connection from the configuration:
	conn, err := cfg.Connect(ctx, cmd.Flags())
	if err != nil {
		return fmt.Errorf("failed to create gRPC connection: %w", err)
	}
	defer conn.Close()

	// Create the client for the compute instances service:
	client := publicv1.NewComputeInstancesClient(conn)

	// Look up the compute instance by ID or name using a CEL filter:
	filter := fmt.Sprintf("this.id in ['%s'] || this.metadata.name in ['%s']", id, id)
	listResponse, err := client.List(ctx, publicv1.ComputeInstancesListRequest_builder{
		Filter: &filter,
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to describe compute instance: %w", err)
	}
	if len(listResponse.GetItems()) == 0 {
		return fmt.Errorf("compute instance not found: %s", id)
	}

	// Get the full object using the resolved UUID:
	response, err := client.Get(ctx, publicv1.ComputeInstancesGetRequest_builder{
		Id: listResponse.GetItems()[0].GetId(),
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to describe compute instance: %w", err)
	}

	// Display the compute instance:
	writer := tabwriter.NewWriter(c.console, 0, 0, 2, ' ', 0)
	ci := response.Object
	template := "-"
	if ci.Spec != nil {
		template = ci.Spec.Template
	}
	state := "-"
	if ci.Status != nil {
		state = ci.Status.State.String()
		state = strings.Replace(state, "COMPUTE_INSTANCE_STATE_", "", -1)
	}
	fmt.Fprintf(writer, "ID:\t%s\n", ci.Id)
	fmt.Fprintf(writer, "Template:\t%s\n", template)
	fmt.Fprintf(writer, "State:\t%s\n", state)
	writer.Flush()

	return nil
}
