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
		Use:                   "publicipattachment [FLAG...] ID|NAME",
		Aliases:               []string{"publicipattachments"},
		Short:                 shortHelp,
		Long:                  longHelp,
		DisableFlagsInUseLine: true,
		Args:                  cobra.ExactArgs(1),
		RunE:                  runner.run,
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

	client := publicv1.NewPublicIPAttachmentsClient(conn)

	matched, err := lookup.Find(ref, "public IP attachment", func(filter string, limit int32) ([]*publicv1.PublicIPAttachment, error) {
		resp, err := client.List(ctx, publicv1.PublicIPAttachmentsListRequest_builder{
			Filter: proto.String(filter),
			Limit:  proto.Int32(limit),
		}.Build())
		if err != nil {
			return nil, fmt.Errorf("failed to describe public IP attachment: %w", err)
		}
		return resp.GetItems(), nil
	})
	if err != nil {
		return err
	}

	RenderPublicIPAttachment(c.console, matched)

	return nil
}

func RenderPublicIPAttachment(w io.Writer, a *publicv1.PublicIPAttachment) {
	writer := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	name := "-"
	if v := a.GetMetadata().GetName(); v != "" {
		name = v
	}

	publicIP := "-"
	if v := a.GetSpec().GetPublicIp(); v != "" {
		publicIP = v
	}

	computeInstance := "-"
	if v := a.GetSpec().GetComputeInstance(); v != "" {
		computeInstance = v
	}

	state := "-"
	publicIPAddress := "-"
	message := "-"
	if a.GetStatus() != nil {
		state = strings.TrimPrefix(a.GetStatus().GetState().String(), "PUBLIC_IP_ATTACHMENT_STATE_")
		if v := a.GetStatus().GetPublicIpAddress(); v != "" {
			publicIPAddress = v
		}
		if v := a.GetStatus().GetMessage(); v != "" {
			message = v
		}
	}

	fmt.Fprintf(writer, "ID:\t%s\n", a.GetId())
	fmt.Fprintf(writer, "Name:\t%s\n", name)
	fmt.Fprintf(writer, "Public IP:\t%s\n", publicIP)
	fmt.Fprintf(writer, "Compute Instance:\t%s\n", computeInstance)
	fmt.Fprintf(writer, "Public IP Address:\t%s\n", publicIPAddress)
	fmt.Fprintf(writer, "State:\t%s\n", state)
	fmt.Fprintf(writer, "Message:\t%s\n", message)
	writer.Flush()
}

const shortHelp = `Describe a public IP attachment`

const longHelp = `
Display detailed information about a public IP attachment, referenced by identifier or name.

Examples:

{{ bt 3 }}shell
# Describe a public IP attachment by identifier:
{{ binary }} describe publicipattachment 019e5fee-0742-78b7-8c4a-e2501f44783a
{{ bt 3 }}

{{ bt 3 }}shell
# Describe a public IP attachment by name:
{{ binary }} describe publicipattachment my-attachment
{{ bt 3 }}
`
