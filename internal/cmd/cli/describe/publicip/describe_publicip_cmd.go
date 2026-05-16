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

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
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

	client := publicv1.NewPublicIPsClient(conn)

	filter := fmt.Sprintf(`this.id == %[1]q || this.metadata.name == %[1]q`, ref)
	listResponse, err := client.List(ctx, publicv1.PublicIPsListRequest_builder{
		Filter: &filter,
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to describe public IP: %w", err)
	}
	if len(listResponse.GetItems()) == 0 {
		return fmt.Errorf("public IP not found: %s", ref)
	}
	if len(listResponse.GetItems()) > 1 {
		return fmt.Errorf("multiple public IPs match '%s', use the ID instead", ref)
	}

	response, err := client.Get(ctx, publicv1.PublicIPsGetRequest_builder{
		Id: listResponse.GetItems()[0].GetId(),
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to describe public IP: %w", err)
	}

	RenderPublicIP(c.console, response.Object)

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
