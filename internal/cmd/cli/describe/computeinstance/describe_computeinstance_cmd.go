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
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/config"
	"github.com/osac-project/fulfillment-service/internal/terminal"
)

// Cmd creates the command to describe a compute instance.
func Cmd() *cobra.Command {
	runner := &runnerContext{}
	result := &cobra.Command{
		Use:     "computeinstance [flags] ID_OR_NAME",
		Aliases: []string{"computeinstances"},
		Short:   "Describe a compute instance",
		Args:    cobra.ExactArgs(1),
		RunE:    runner.run,
	}
	return result
}

type runnerContext struct {
	console *terminal.Console
}

func (c *runnerContext) run(cmd *cobra.Command, args []string) error {
	ref := args[0]

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

	client := publicv1.NewComputeInstancesClient(conn)

	filter := buildFilter(ref)
	listResponse, err := client.List(ctx, publicv1.ComputeInstancesListRequest_builder{
		Filter: &filter,
		Limit:  proto.Int32(2),
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to describe compute instance: %w", err)
	}
	if err := guardResult(len(listResponse.GetItems()), ref); err != nil {
		return err
	}

	renderComputeInstance(c.console, listResponse.GetItems()[0])

	return nil
}

func guardResult(items int, ref string) error {
	if items == 0 {
		return fmt.Errorf("compute instance not found: %s", ref)
	}
	if items > 1 {
		return fmt.Errorf("multiple compute instances match '%s', use the ID instead", ref)
	}
	return nil
}

func buildFilter(ref string) string {
	return fmt.Sprintf(`this.id == %[1]q || this.metadata.name == %[1]q`, ref)
}

func renderComputeInstance(w io.Writer, ci *publicv1.ComputeInstance) {
	writer := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	catalogItem := "-"
	if ci.Spec != nil {
		if catalogItemID := ci.Spec.GetCatalogItem(); catalogItemID != "" {
			catalogItem = catalogItemID
		}
	}
	state := "-"
	if ci.Status != nil {
		state = ci.Status.State.String()
		state = strings.TrimPrefix(state, "COMPUTE_INSTANCE_STATE_")
	}
	fmt.Fprintf(writer, "ID:\t%s\n", ci.Id)
	fmt.Fprintf(writer, "Catalog Item:\t%s\n", catalogItem)
	fmt.Fprintf(writer, "State:\t%s\n", state)
	if ci.Status != nil && ci.Status.GetLastRestartedAt() != nil {
		fmt.Fprintf(writer, "Last Restarted At:\t%s\n", ci.Status.GetLastRestartedAt().AsTime().Format(time.RFC3339))
	}
	writer.Flush()
}
