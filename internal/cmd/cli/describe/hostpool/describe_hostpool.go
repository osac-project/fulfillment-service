/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package hostpool

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
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
		Use:     "hostpool [flags] ID",
		Aliases: []string{"hostpools"},
		Short:   "Describe a host pool",
		RunE:    runner.run,
	}
	return result
}

type runnerContext struct {
	logger  *slog.Logger
	console *terminal.Console
}

func (c *runnerContext) run(cmd *cobra.Command, args []string) error {
	// Check that there is exactly one host pool ID specified
	if len(args) != 1 {
		fmt.Fprintf(
			os.Stderr,
			"Expected exactly one host pool ID\n",
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

	// Create the client for the host pools service:
	client := publicv1.NewHostPoolsClient(conn)

	// Get the host pool:
	response, err := client.Get(ctx, publicv1.HostPoolsGetRequest_builder{
		Id: id,
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to describe host pool: %w", err)
	}

	// Display the host pool:
	writer := tabwriter.NewWriter(c.console, 0, 0, 2, ' ', 0)
	hostPool := response.Object

	state := "-"
	allocatedHosts := 0
	if hostPool.Status != nil {
		state = formatPoolState(hostPool.Status.State)
		allocatedHosts = len(hostPool.Status.Hosts)
	}

	specHostSets := 0
	if hostPool.Spec != nil {
		specHostSets = len(hostPool.Spec.HostSets)
	}

	fmt.Fprintf(writer, "ID:\t%s\n", hostPool.Id)
	fmt.Fprintf(writer, "State:\t%s\n", state)
	fmt.Fprintf(writer, "Host Sets (Spec):\t%d\n", specHostSets)
	fmt.Fprintf(writer, "Allocated Hosts:\t%d\n", allocatedHosts)

	// Display host sets details if available
	if hostPool.Spec != nil && len(hostPool.Spec.HostSets) > 0 {
		fmt.Fprintf(writer, "\nHost Sets:\n")

		// Sort host classes for consistent output
		hostSets := make([]string, 0, len(hostPool.Spec.HostSets))
		for hostSet := range hostPool.Spec.HostSets {
			hostSets = append(hostSets, hostSet)
		}
		sort.Strings(hostSets)

		for _, hostSetName := range hostSets {
			hostSet := hostPool.Spec.HostSets[hostSetName]
			fmt.Fprintf(writer, "  %s:\t%d %s hosts\n", hostSetName, hostSet.Size, hostSet.HostClass)
		}
	}

	// Display allocated hosts if available
	if hostPool.Status != nil && len(hostPool.Status.Hosts) > 0 {
		fmt.Fprintf(writer, "\nAllocated Hosts:\n")

		// Sort hosts for consistent output
		hosts := make([]string, len(hostPool.Status.Hosts))
		copy(hosts, hostPool.Status.Hosts)
		sort.Strings(hosts)

		for _, host := range hosts {
			fmt.Fprintf(writer, "  %s\n", host)
		}
	}

	writer.Flush()

	return nil
}

// formatPoolState converts the pool state enum to a human-readable string
func formatPoolState(state publicv1.HostPoolState) string {
	stateStr := state.String()
	// Remove the common prefix to make it more readable
	stateStr = strings.Replace(stateStr, "HOST_POOL_STATE_", "", 1)
	return stateStr
}
