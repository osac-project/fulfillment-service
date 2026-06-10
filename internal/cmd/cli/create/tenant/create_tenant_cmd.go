/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package tenant

import (
	"fmt"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/config"
	"github.com/osac-project/fulfillment-service/internal/terminal"
)

// Cmd creates the cobra command for creating tenants.
func Cmd() *cobra.Command {
	runner := &runnerContext{}
	result := &cobra.Command{
		Use:     "tenant NAME...",
		Aliases: []string{string(proto.MessageName((*privatev1.Organization)(nil)))},
		Short:   shortHelp,
		Long:    longHelp,
		Args:    cobra.MinimumNArgs(1),
		RunE:    runner.run,
	}
	return result
}

type runnerContext struct {
	console *terminal.Console
}

func (c *runnerContext) run(cmd *cobra.Command, args []string) error {
	// Get the context:
	ctx := cmd.Context()

	// Get the console:
	c.console = terminal.ConsoleFromContext(ctx)

	// Get the configuration:
	cfg := config.SettingsFromContext(ctx)
	if !cfg.Armed() {
		return fmt.Errorf("there is no configuration, run the 'login' command")
	}

	// Create the gRPC connection from the configuration:
	conn, err := cfg.Connect(ctx, cmd.Flags())
	if err != nil {
		return fmt.Errorf("failed to create gRPC connection: %w", err)
	}
	defer conn.Close()

	// Create the client:
	client := privatev1.NewOrganizationsClient(conn)

	// Create the tenants:
	for _, name := range args {
		// Prepare the tenant:
		tenant := privatev1.Organization_builder{
			Metadata: privatev1.Metadata_builder{
				Name: name,
			}.Build(),
		}.Build()

		// Send the request to create the tenant:
		response, err := client.Create(ctx, privatev1.OrganizationsCreateRequest_builder{
			Object: tenant,
		}.Build())
		if err != nil {
			return fmt.Errorf("failed to create tenant '%s': %w", name, err)
		}

		// Display the result:
		created := response.GetObject()
		c.console.Infof(
			ctx,
			"Created tenant with name '%s' and identifier '%s'.\n",
			created.GetMetadata().GetName(), created.GetId(),
		)
	}

	return nil
}

const shortHelp = `Create one or more tenants`

const longHelp = `
Create one or more tenants (organizations). Each argument is interpreted as the name of a tenant to create.

To create a single tenant:

{{ bt 3 }}shell
{{ binary }} create tenant my-tenant
{{ bt 3 }}

To create multiple tenants at once:

{{ bt 3 }}shell
{{ binary }} create tenant tenant-a tenant-b tenant-c
{{ bt 3 }}
`
