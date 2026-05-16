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
	"log/slog"

	"github.com/spf13/cobra"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/config"
	"github.com/osac-project/fulfillment-service/internal/logging"
	"github.com/osac-project/fulfillment-service/internal/terminal"
)

func Cmd() *cobra.Command {
	runner := &runnerContext{}
	result := &cobra.Command{
		Use:   "publicipattachment ATTACHMENT",
		Short: "Delete a public IP attachment",
		Long: "Delete a PublicIPAttachment to detach a public IP from its target. " +
			"The attachment is identified by its ID or name.",
		Example: `  # Delete an attachment by name
  osac delete publicipattachment my-attachment

  # Delete an attachment by ID
  osac delete publicipattachment pia-abc123`,
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

	client := publicv1.NewPublicIPAttachmentsClient(conn)

	// Resolve the attachment by name or ID:
	filter := fmt.Sprintf(`this.id == %[1]q || this.metadata.name == %[1]q`, args[0])
	listResponse, err := client.List(ctx, publicv1.PublicIPAttachmentsListRequest_builder{
		Filter: &filter,
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to resolve public IP attachment '%s': %w", args[0], err)
	}
	if len(listResponse.GetItems()) == 0 {
		return fmt.Errorf("public IP attachment not found: %s", args[0])
	}
	if len(listResponse.GetItems()) > 1 {
		return fmt.Errorf("multiple public IP attachments match '%s', use the ID instead", args[0])
	}

	attachment := listResponse.GetItems()[0]
	_, err = client.Delete(ctx, publicv1.PublicIPAttachmentsDeleteRequest_builder{
		Id: attachment.GetId(),
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to delete public IP attachment: %w", err)
	}

	c.console.Infof(ctx, "Deleted public IP attachment '%s'.\n", attachment.GetId())

	return nil
}
