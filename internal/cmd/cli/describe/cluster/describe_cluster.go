/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package cluster

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/config"
	"github.com/osac-project/fulfillment-service/internal/logging"
	"github.com/osac-project/fulfillment-service/internal/terminal"
)

// Cmd creates the command to describe a cluster.
func Cmd() *cobra.Command {
	runner := &runnerContext{}
	result := &cobra.Command{
		Use:     "cluster [flags] ID_OR_NAME",
		Aliases: []string{"clusters"},
		Short:   "Describe a cluster",
		Long:    "Display detailed information about a cluster, identified by ID or name.",
		Example: `  # Describe a cluster by ID
  osac describe cluster cluster-abc123

  # Describe a cluster by name
  osac describe cluster my-cluster`,
		Args: cobra.ExactArgs(1),
		RunE: runner.run,
	}
	return result
}

type runnerContext struct {
	logger  *slog.Logger
	console *terminal.Console
}

func (c *runnerContext) run(cmd *cobra.Command, args []string) error {
	ref := args[0]

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
	defer func() { _ = conn.Close() }()

	client := publicv1.NewClustersClient(conn)

	filter := buildFilter(ref)
	listResponse, err := client.List(ctx, publicv1.ClustersListRequest_builder{
		Filter: &filter,
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to describe cluster: %w", err)
	}
	if err := guardResult(len(listResponse.GetItems()), ref); err != nil {
		return err
	}

	response, err := client.Get(ctx, publicv1.ClustersGetRequest_builder{
		Id: listResponse.GetItems()[0].GetId(),
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to describe cluster: %w", err)
	}

	RenderCluster(c.console, response.Object)

	return nil
}

func guardResult(items int, ref string) error {
	if items == 0 {
		return fmt.Errorf("cluster not found: %s", ref)
	}
	if items > 1 {
		return fmt.Errorf("multiple clusters match '%s', use the ID instead", ref)
	}
	return nil
}

func buildFilter(ref string) string {
	return fmt.Sprintf(`this.id == %[1]q || this.metadata.name == %[1]q`, ref)
}

// RenderCluster writes a formatted description of cluster to w.
func RenderCluster(w io.Writer, cluster *publicv1.Cluster) {
	writer := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	template := "-"
	if cluster.GetSpec() != nil {
		template = cluster.GetSpec().GetTemplate()
	}
	state := "-"
	if cluster.GetStatus() != nil {
		state = strings.TrimPrefix(cluster.GetStatus().GetState().String(), "CLUSTER_STATE_")
	}
	_, _ = fmt.Fprintf(writer, "ID:\t%s\n", cluster.GetId())
	_, _ = fmt.Fprintf(writer, "Template:\t%s\n", template)
	_, _ = fmt.Fprintf(writer, "State:\t%s\n", state)
	_ = writer.Flush()
}
