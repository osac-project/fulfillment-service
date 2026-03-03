/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package host

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

func Cmd() *cobra.Command {
	runner := &runnerContext{}
	result := &cobra.Command{
		Use:     "host [flags] ID",
		Aliases: []string{"hosts"},
		Short:   "Describe a host",
		RunE:    runner.run,
	}
	return result
}

type runnerContext struct {
	logger  *slog.Logger
	console *terminal.Console
}

func (c *runnerContext) run(cmd *cobra.Command, args []string) error {
	// Check that there is exactly one host ID specified
	if len(args) != 1 {
		fmt.Fprintf(
			os.Stderr,
			"Expected exactly one host ID\n",
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

	// Create the client for the hosts service:
	client := publicv1.NewHostsClient(conn)

	// Get the host:
	response, err := client.Get(ctx, publicv1.HostsGetRequest_builder{
		Id: id,
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to describe host: %w", err)
	}

	// Display the host:
	writer := tabwriter.NewWriter(c.console, 0, 0, 2, ' ', 0)
	host := response.Object

	specPowerState := "-"
	if host.Spec != nil {
		specPowerState = formatPowerState(host.Spec.PowerState)
	}

	statusPowerState := "-"
	if host.Status != nil {
		statusPowerState = formatPowerState(host.Status.PowerState)
	}

	fmt.Fprintf(writer, "ID:\t%s\n", host.Id)
	fmt.Fprintf(writer, "Spec Power State:\t%s\n", specPowerState)
	fmt.Fprintf(writer, "Status Power State:\t%s\n", statusPowerState)
	writer.Flush()

	return nil
}

// formatPowerState converts the power state enum to a human-readable string
func formatPowerState(state publicv1.HostPowerState) string {
	stateStr := state.String()
	// Remove the common prefix to make it more readable
	stateStr = strings.Replace(stateStr, "HOST_POWER_STATE_", "", 1)
	return stateStr
}
