/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package project

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/config"
	"github.com/osac-project/fulfillment-service/internal/terminal"
)

// Cmd creates the cobra command for creating projects.
func Cmd() *cobra.Command {
	runner := &runnerContext{}
	result := &cobra.Command{
		Use:     "project NAME...",
		Aliases: []string{string(proto.MessageName((*publicv1.Project)(nil)))},
		Short:   shortHelp,
		Long:    longHelp,
		Args:    cobra.MinimumNArgs(1),
		RunE:    runner.run,
	}
	flags := result.Flags()
	flags.StringVar(
		&runner.tenant,
		"tenant",
		"",
		tenantFlagHelp,
	)
	flags.StringVar(
		&runner.title,
		"title",
		"",
		titleFlagHelp,
	)
	flags.StringVar(
		&runner.description,
		"description",
		"",
		descriptionFlagHelp,
	)
	flags.BoolVarP(
		&runner.parents,
		"parents",
		"p",
		false,
		parentsFlagHelp,
	)
	return result
}

type runnerContext struct {
	console     *terminal.Console
	client      publicv1.ProjectsClient
	tenant      string
	title       string
	description string
	parents     bool
}

func (c *runnerContext) run(cmd *cobra.Command, args []string) error {
	// Get the context from the command:
	ctx := cmd.Context()

	// Get the console from the context:
	c.console = terminal.ConsoleFromContext(ctx)

	// Get the configuration from the context:
	cfg := config.SettingsFromContext(ctx)
	if !cfg.Armed() {
		return fmt.Errorf("there is no configuration, run the 'login' command")
	}

	// The title and description flags only make sense when creating a single project, as all projects would
	// get the same values otherwise.
	if len(args) > 1 && (c.title != "" || c.description != "") {
		return fmt.Errorf(
			"the '--title' and '--description' flags can only be used when creating a single project",
		)
	}

	// Create the gRPC connection from the configuration:
	conn, err := cfg.Connect(ctx, cmd.Flags())
	if err != nil {
		return fmt.Errorf("failed to create gRPC connection: %w", err)
	}
	defer conn.Close()

	// Create the client:
	c.client = publicv1.NewProjectsClient(conn)

	// Create the projects:
	for _, name := range args {
		if c.parents {
			err = c.createWithParents(ctx, name)
		} else {
			err = c.createProject(ctx, name, c.title, c.description)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

// createWithParents creates the given project and all its ancestor projects. For example, given
// "team-a.frontend.staging" it will ensure that "team-a", "team-a.frontend" and
// "team-a.frontend.staging" all exist. Parent projects that already exist are silently skipped.
// The title and description flags are only applied to the final (leaf) project.
func (c *runnerContext) createWithParents(ctx context.Context, name string) error {
	segments := strings.Split(name, ".")
	for i := range segments {
		current := strings.Join(segments[:i+1], ".")
		isLeaf := i == len(segments)-1

		var title, description string
		if isLeaf {
			title = c.title
			description = c.description
		}

		err := c.createProject(ctx, current, title, description)
		if err != nil {
			// When creating parents we expect that some of them may already exist, so we
			// silently skip those. For the leaf project we always report the error.
			if !isLeaf {
				status, ok := grpcstatus.FromError(err)
				if ok && status.Code() == grpccodes.AlreadyExists {
					continue
				}
			}
			return err
		}
	}
	return nil
}

// createProject sends a request to create a single project with the given name, title and
// description.
func (c *runnerContext) createProject(ctx context.Context, name, title, description string) error {
	specBuilder := publicv1.ProjectSpec_builder{
		Title: title,
	}
	if description != "" {
		specBuilder.Description = &description
	}
	project := publicv1.Project_builder{
		Metadata: publicv1.Metadata_builder{
			Name:   name,
			Tenant: c.tenant,
		}.Build(),
		Spec: specBuilder.Build(),
	}.Build()

	response, err := c.client.Create(ctx, publicv1.ProjectsCreateRequest_builder{
		Object: project,
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to create project '%s': %w", name, err)
	}

	project = response.GetObject()
	c.console.Infof(
		ctx,
		"Created project with name '%s' and identifier '%s'.\n",
		project.GetMetadata().GetName(), project.GetId(),
	)
	return nil
}

const shortHelp = `Create one or more projects`

const longHelp = `
Create one or more projects. Each argument is interpreted as the full name of the project to create.

Project names use a dot-separated hierarchy. For example, {{ bt }}team-a.frontend.staging{{ bt }} represents a project
nested three levels deep. The parent project is automatically derived from the name by removing the last segment.

To create a single project:

{{ bt 3 }}shell
{{ binary }} create project my-project
{{ bt 3 }}

To create multiple projects at once:

{{ bt 3 }}shell
{{ binary }} create project team-a team-a.frontend team-a.frontend.staging
{{ bt 3 }}

Use {{ bt }}--tenant{{ bt }} to assign the projects to a specific tenant:

{{ bt 3 }}shell
{{ binary }} create project --tenant my-tenant my-project
{{ bt 3 }}

Use {{ bt }}--parents{{ bt }} (or {{ bt }}-p{{ bt }}) to automatically create any missing ancestor projects. For
example, the following command will ensure that {{ bt }}team-a{{ bt }} and {{ bt }}team-a.frontend{{ bt }} exist before
creating {{ bt }}team-a.frontend.staging{{ bt }}:

{{ bt 3 }}shell
{{ binary }} create project -p team-a.frontend.staging
{{ bt 3 }}
`

const tenantFlagHelp = `
_NAME_ - Name of the tenant that the projects will be assigned to. By default this is the tenant of the authenticated
user, so there is no need to specify it unless the user has visibility of multiple tenants.
`

const titleFlagHelp = `
_TEXT_ - Short human-friendly title for the project, suitable for displaying in a single line. Can only be used when
creating a single project.
`

const descriptionFlagHelp = `
_TEXT_ - Longer description explaining the purpose or scope of the project. Can only be used when creating a single
project.
`

const parentsFlagHelp = `
_[BOOLEAN]_ - Automatically create any missing ancestor projects. For example, when creating
{{ bt }}team-a.frontend.staging{{ bt }}, this will first create {{ bt }}team-a{{ bt }} and {{ bt }}team-a.frontend{{ bt
}} if they do not already exist. The {{ bt }}--title{{ bt }} and {{ bt }}--description{{ bt }} flags are only applied to
the final project, not to the automatically created parents.
`
