/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package annotate

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

// Cmd creates and returns the command that adds or removes annotations.
func Cmd() *cobra.Command {
	runner := &runnerContext{}
	result := &cobra.Command{
		Use:   "annotate OBJECT ID|NAME ANNOTATION...",
		Short: "Add or remove annotations from objects",
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

	// Check that at least one annotation operation has been specified:
	if len(args) < 3 {
		c.console.Render(ctx, "no_annotations.txt", map[string]any{})
		return nil
	}

	// Parse the annotation operations:
	operations, err := c.parseAnnotationOperations(args[2:])
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

	// Apply the annotation operations:
	metadata := c.helper.GetMetadata(object)
	c.applyAnnotationOperations(metadata, operations)

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
	filter := fmt.Sprintf(`this.id == %[1]q || this.metadata.name == %[1]q`, ref)
	response, err := c.helper.List(ctx, reflection.ListOptions{
		Filter: filter,
		Limit:  10,
	})
	if err != nil {
		err = fmt.Errorf(
			"failed to find object of type '%s' with identifier or name '%s': %w",
			c.helper, ref, err,
		)
		return
	}
	items := response.Items
	total := response.Total

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

// annotationOperation represents a single annotation set or remove operation.
type annotationOperation struct {
	key    string
	value  *string
	remove bool
}

func (c *runnerContext) parseAnnotationOperations(values []string) (result []annotationOperation, err error) {
	for _, value := range values {
		var operation annotationOperation
		operation, err = c.parseAnnotationOperation(value)
		if err != nil {
			return
		}
		result = append(result, operation)
	}
	return
}

func (c *runnerContext) parseAnnotationOperation(text string) (operation annotationOperation, err error) {
	key, value, ok := strings.Cut(text, "=")
	if ok {
		if key == "" {
			err = fmt.Errorf("annotation name can't be empty in %q", text)
			return
		}
		operation = annotationOperation{
			key:   key,
			value: &value,
		}
		return
	}
	if strings.HasSuffix(text, "-") {
		key := strings.TrimSuffix(text, "-")
		if key == "" {
			err = fmt.Errorf("annotation name can't be empty in %q", text)
			return
		}
		operation = annotationOperation{
			key:    key,
			remove: true,
		}
		return
	}
	err = fmt.Errorf("invalid annotation specification %q, expected 'annotation=value' or 'annotation-'", text)
	return
}

func (c *runnerContext) applyAnnotationOperations(metadata reflection.Metadata, operations []annotationOperation) {
	annotations := metadata.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	for _, operation := range operations {
		if operation.remove {
			delete(annotations, operation.key)
			continue
		}
		annotations[operation.key] = *operation.value
	}
	if len(annotations) > 0 || metadata.GetAnnotations() != nil {
		metadata.SetAnnotations(annotations)
	}
}
