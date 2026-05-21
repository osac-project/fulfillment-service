/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package publicippool

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/cmd/cli/lookup"
	"github.com/osac-project/fulfillment-service/internal/config"
	"github.com/osac-project/fulfillment-service/internal/terminal"
)

// Cmd creates the command to list or get public IP pools.
func Cmd() *cobra.Command {
	runner := &runnerContext{}
	result := &cobra.Command{
		Use:     "publicippool [ID_OR_NAME]",
		Aliases: []string{"publicippools"},
		Short:   "List or get public IP pools",
		Long:    "List all available public IP pools, or display details for a specific pool by ID or name.",
		Example: `  # List all available pools
  osac get publicippool

  # Get a specific pool by name
  osac get publicippool pool-ipv4-prod

  # Get a specific pool by ID
  osac get publicippool pool-abc123`,
		Args: cobra.MaximumNArgs(1),
		RunE: runner.run,
	}
	return result
}

type runnerContext struct {
	console *terminal.Console
}

func (c *runnerContext) run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

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

	client := publicv1.NewPublicIPPoolsClient(conn)

	if len(args) == 0 {
		resp, err := client.List(ctx, publicv1.PublicIPPoolsListRequest_builder{}.Build())
		if err != nil {
			return fmt.Errorf("failed to list public IP pools: %w", err)
		}
		if len(resp.GetItems()) == 0 {
			c.console.Infof(ctx, "No public IP pools found.\n")
			return nil
		}
		renderPoolTable(c.console, resp.GetItems())
		return nil
	}

	ref := args[0]
	pool, err := lookup.Find(ref, "public IP pool", func(filter string, limit int32) ([]*publicv1.PublicIPPool, error) {
		resp, err := client.List(ctx, publicv1.PublicIPPoolsListRequest_builder{
			Filter: proto.String(filter),
			Limit:  proto.Int32(limit),
		}.Build())
		if err != nil {
			return nil, fmt.Errorf("failed to list public IP pools: %w", err)
		}
		return resp.GetItems(), nil
	})
	if err != nil {
		return err
	}

	renderPoolDetail(c.console, pool)
	return nil
}

// renderPoolTable writes a compact table of pools — used when listing all pools.
func renderPoolTable(w *terminal.Console, pools []*publicv1.PublicIPPool) {
	writer := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(writer, "NAME\tCIDRS\tIP-FAMILY")
	for _, p := range pools {
		name := p.GetMetadata().GetName()
		if name == "" {
			name = p.GetId()
		}
		cidrs := strings.Join(p.GetSpec().GetCidrs(), ", ")
		if cidrs == "" {
			cidrs = "-"
		}
		ipFamily := strings.TrimPrefix(p.GetSpec().GetIpFamily().String(), "IP_FAMILY_")
		fmt.Fprintf(writer, "%s\t%s\t%s\n", name, cidrs, ipFamily)
	}
	writer.Flush()
}

// renderPoolDetail writes a detailed key-value view of a single pool — used when getting by name/id.
func renderPoolDetail(w *terminal.Console, p *publicv1.PublicIPPool) {
	writer := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	name := "-"
	if v := p.GetMetadata().GetName(); v != "" {
		name = v
	}

	cidrs := "-"
	if c := p.GetSpec().GetCidrs(); len(c) > 0 {
		cidrs = strings.Join(c, ", ")
	}

	ipFamily := strings.TrimPrefix(p.GetSpec().GetIpFamily().String(), "IP_FAMILY_")

	fmt.Fprintf(writer, "ID:\t%s\n", p.GetId())
	fmt.Fprintf(writer, "Name:\t%s\n", name)
	fmt.Fprintf(writer, "CIDRs:\t%s\n", cidrs)
	fmt.Fprintf(writer, "IP Family:\t%s\n", ipFamily)
	writer.Flush()
}
