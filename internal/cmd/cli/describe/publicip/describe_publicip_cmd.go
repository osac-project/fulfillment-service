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
	"io"
	"log/slog"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/cmd/cli/lookup"
	"github.com/osac-project/fulfillment-service/internal/config"
	"github.com/osac-project/fulfillment-service/internal/logging"
	"github.com/osac-project/fulfillment-service/internal/terminal"
)

func Cmd() *cobra.Command {
	runner := &runnerContext{}
	result := &cobra.Command{
		Use:     "publicip [flags] ID_OR_NAME",
		Aliases: []string{"publicips"},
		Short:   "Describe a public IP",
		Long:    "Display detailed information about a public IP, identified by ID or name.",
		Example: `  # Describe a public IP by ID
  osac describe publicip pip-abc123

  # Describe a public IP by name
  osac describe publicip my-ip`,
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

	matched, err := lookup.Find(ref, "public IP", func(filter string, limit int32) ([]*publicv1.PublicIP, error) {
		resp, err := client.List(ctx, publicv1.PublicIPsListRequest_builder{
			Filter: proto.String(filter),
			Limit:  proto.Int32(limit),
		}.Build())
		if err != nil {
			return nil, fmt.Errorf("failed to describe public IP: %w", err)
		}
		return resp.GetItems(), nil
	})
	if err != nil {
		return err
	}

	RenderPublicIP(c.console, matched)

	return nil
}

func RenderPublicIP(w io.Writer, pip *publicv1.PublicIP) {
	writer := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	name := "-"
	if v := pip.GetMetadata().GetName(); v != "" {
		name = v
	}

	pool := "-"
	if v := pip.GetSpec().GetPool(); v != "" {
		pool = v
	}

	attached := "false"
	if pip.GetStatus() != nil && pip.GetStatus().GetAttached() {
		attached = "true"
	}

	address := "-"
	state := "-"
	message := "-"
	if pip.GetStatus() != nil {
		state = strings.TrimPrefix(pip.GetStatus().GetState().String(), "PUBLIC_IP_STATE_")
		if v := pip.GetStatus().GetAddress(); v != "" {
			address = v
		}
		if v := pip.GetStatus().GetMessage(); v != "" {
			message = v
		}
	}

	fmt.Fprintf(writer, "ID:\t%s\n", pip.GetId())
	fmt.Fprintf(writer, "Name:\t%s\n", name)
	fmt.Fprintf(writer, "Pool:\t%s\n", pool)
	fmt.Fprintf(writer, "Attached:\t%s\n", attached)
	fmt.Fprintf(writer, "Address:\t%s\n", address)
	fmt.Fprintf(writer, "State:\t%s\n", state)
	fmt.Fprintf(writer, "Message:\t%s\n", message)
	writer.Flush()
}
