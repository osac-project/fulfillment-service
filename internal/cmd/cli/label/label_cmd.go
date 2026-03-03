/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package label

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"strings"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.com/osac-project/fulfillment-service/internal/config"
	"github.com/osac-project/fulfillment-service/internal/logging"
	"github.com/osac-project/fulfillment-service/internal/reflection"
	"github.com/osac-project/fulfillment-service/internal/terminal"
)

//go:embed templates
var templatesFS embed.FS

// Cmd creates and returns the command that adds or removes labels.
func Cmd() *cobra.Command {
	runner := &runnerContext{}
	result := &cobra.Command{
		Use:   "label OBJECT ID|NAME LABEL...",
		Short: "Add or remove labels from objects",
		RunE:  runner.run,
	}
	return result
}

type runnerContext struct {
	logger  *slog.Logger
	console *terminal.Console
	conn    *grpc.ClientConn
	helper  *reflection.ObjectHelper
}

func (c *runnerContext) run(cmd *cobra.Command, args []string) error {
	var err error

	// Get the context:
	ctx := cmd.Context()

	// Get the logger and the console:
	c.logger = logging.LoggerFromContext(ctx)
	c.console = terminal.ConsoleFromContext(ctx)

	// Load the templates for the console messages:
	err = c.console.AddTemplates(templatesFS, "templates")
	if err != nil {
		return fmt.Errorf("failed to load templates: %w", err)
	}

	// Get the configuration:
	cfg, err := config.Load(ctx)
	if err != nil {
		return err
	}
	if cfg == nil {
		return fmt.Errorf("there is no configuration, run the 'login' command")
	}

	// Create the gRPC connection from the configuration:
	c.conn, err = cfg.Connect(ctx, cmd.Flags())
	if err != nil {
		return fmt.Errorf("failed to create gRPC connection: %w", err)
	}
	defer c.conn.Close()

	// Create the reflection helper:
	helper, err := reflection.NewHelper().
		SetLogger(c.logger).
		SetConnection(c.conn).
		AddPackages(cfg.Packages()).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create reflection tool: %w", err)
	}
	c.console.SetHelper(helper)

	// Check that the object type has been specified:
	if len(args) == 0 {
		c.console.Render(ctx, "no_object.txt", map[string]any{
			"Helper": helper,
		})
		return nil
	}

	// Get the information about the object type:
	c.helper = helper.Lookup(args[0])
	if c.helper == nil {
		c.console.Render(ctx, "wrong_object.txt", map[string]any{
			"Helper": helper,
			"Object": args[0],
		})
		return nil
	}

	// Check that the object identifier or name has been specified:
	if len(args) < 2 {
		c.console.Render(ctx, "no_id.txt", map[string]any{})
		return nil
	}
	ref := args[1]

	// Check that at least one label operation has been specified:
	if len(args) < 3 {
		c.console.Render(ctx, "no_labels.txt", map[string]any{})
		return nil
	}

	operations, err := c.parseLabelOperations(args[2:])
	if err != nil {
		return err
	}

	// Find the object by identifier or name:
	object, err := c.findObject(ctx, ref)
	if err != nil {
		return err
	}
	if object == nil {
		return nil
	}

	// Apply the label operations:
	metadata := c.helper.GetMetadata(object)
	c.applyLabelOperations(metadata, operations)

	// Save the result:
	_, err = c.helper.Update(ctx, object)
	if err != nil {
		return err
	}

	return nil
}

// findObject tries to find an object by identifier or name. It uses the list method with a filter that matches
// either the identifier or the name. Returns an error if no match is found or if multiple matches are found.
func (c *runnerContext) findObject(ctx context.Context, ref string) (result proto.Message, err error) {
	// Find the objects matching the reference (identifier or name):
	filter := fmt.Sprintf(`this.id == %[1]q || this.metadata.name == %[1]q`, ref)
	response, err := c.helper.List(ctx, reflection.ListOptions{
		Filter: filter,
		Limit:  10,
	})
	if err != nil {
		err = fmt.Errorf("failed to find object of type '%s' with identifier or name '%s': %w", c.helper, ref, err)
		return
	}
	items := response.Items
	total := response.Total

	// Prepare the response based on the number of objects found:
	switch len(items) {
	case 0:
		c.console.Render(ctx, "no_matches.txt", map[string]any{
			"Object": c.helper.Singular(),
			"Ref":    ref,
		})
		return
	case 1:
		result = items[0]
		return
	default:
		c.console.Render(ctx, "multiple_matches.txt", map[string]any{
			"Matches": items,
			"Object":  c.helper.Singular(),
			"Ref":     ref,
			"Total":   total,
		})
		return
	}
}

type labelOperation struct {
	label  string
	value  *string
	remove bool
}

func (c *runnerContext) parseLabelOperations(values []string) (result []labelOperation, err error) {
	for _, value := range values {
		var operation labelOperation
		operation, err = c.parseLabelOperation(value)
		if err != nil {
			return
		}
		result = append(result, operation)
	}
	return
}

func (c *runnerContext) parseLabelOperation(text string) (operation labelOperation, err error) {
	label, value, ok := strings.Cut(text, "=")
	if ok {
		operation = labelOperation{
			label: label,
			value: &value,
		}
		return
	}
	if strings.HasSuffix(text, "-") {
		label := strings.TrimSuffix(text, "-")
		if label == "" {
			err = fmt.Errorf("label name can't be empty in %q", text)
			return
		}
		operation = labelOperation{
			label:  label,
			remove: true,
		}
		return
	}
	err = fmt.Errorf("invalid label specification %q, expected 'label=value' or 'label-'", text)
	return
}

func (c *runnerContext) applyLabelOperations(metadata reflection.Metadata, operations []labelOperation) {
	labels := metadata.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	for _, operation := range operations {
		if operation.remove {
			delete(labels, operation.label)
			continue
		}
		labels[operation.label] = *operation.value
	}
	if len(labels) > 0 || metadata.GetLabels() != nil {
		metadata.SetLabels(labels)
	}
}
