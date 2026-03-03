/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package edit

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"

	"github.com/osac-project/fulfillment-service/internal/config"
	"github.com/osac-project/fulfillment-service/internal/logging"
	"github.com/osac-project/fulfillment-service/internal/reflection"
	"github.com/osac-project/fulfillment-service/internal/terminal"
)

//go:embed templates
var templatesFS embed.FS

// Possible output formats:
const (
	outputFormatJson = "json"
	outputFormatYaml = "yaml"
)

func Cmd() *cobra.Command {
	runner := &runnerContext{
		marshalOptions: protojson.MarshalOptions{
			UseProtoNames: true,
		},
	}
	result := &cobra.Command{
		Use:   "edit OBJECT ID|NAME",
		Short: "Edit objects",
		RunE:  runner.run,
	}
	flags := result.Flags()
	flags.StringVarP(
		&runner.format,
		"output",
		"o",
		outputFormatYaml,
		fmt.Sprintf(
			"Output format, one of '%s' or '%s'.",
			outputFormatJson, outputFormatYaml,
		),
	)
	return result
}

type runnerContext struct {
	logger         *slog.Logger
	console        *terminal.Console
	format         string
	conn           *grpc.ClientConn
	marshalOptions protojson.MarshalOptions
	helper         *reflection.ObjectHelper
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

	// Check the flags:
	if c.format != outputFormatJson && c.format != outputFormatYaml {
		return fmt.Errorf(
			"unknown output format '%s', should be '%s' or '%s'",
			c.format, outputFormatJson, outputFormatYaml,
		)
	}

	// Check that the object identifier or name has been specified:
	if len(args) < 2 {
		c.console.Render(ctx, "no_id.txt", map[string]any{})
		return nil
	}
	key := args[1]

	// Find the object by identifier or name:
	object, err := c.findObject(ctx, key)
	if err != nil {
		return err
	}
	if object == nil {
		return nil
	}

	// Render the object:
	var render func(proto.Message) ([]byte, error)
	switch c.format {
	case outputFormatJson:
		render = c.renderJson
	default:
		render = c.renderYaml
	}
	data, err := render(object)
	if err != nil {
		return err
	}

	// Write the rendered object to a temporary file:
	tmpDir, err := os.MkdirTemp("", "")
	if err != nil {
		return err
	}
	defer func() {
		err := os.RemoveAll(tmpDir)
		if err != nil {
			c.logger.ErrorContext(
				ctx,
				"Failed to remove temporary directory",
				slog.String("dir", tmpDir),
				slog.Any("error", err),
			)
		}
	}()
	objectId := c.helper.GetId(object)
	tmpFile := filepath.Join(tmpDir, fmt.Sprintf("%s-%s.%s", c.helper, objectId, c.format))
	err = os.WriteFile(tmpFile, data, 0600)
	if err != nil {
		return fmt.Errorf("failed to create temporary file '%s': %w", tmpFile, err)
	}

	// Run the editor:
	editorName := c.findEditor(ctx)
	editorPath, err := exec.LookPath(editorName)
	if err != nil {
		return fmt.Errorf("failed to find editor command '%s': %w", editorName, err)
	}
	editorCmd := &exec.Cmd{
		Path: editorPath,
		Args: []string{
			editorName,
			tmpFile,
		},
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	err = editorCmd.Run()
	if err != nil {
		return fmt.Errorf("failed to edit: %w", err)
	}

	// Load the potentiall modified file:
	data, err = os.ReadFile(tmpFile)
	if err != nil {
		return fmt.Errorf("failed to read back temporary file '%s': %w", tmpFile, err)
	}

	// Parse the result:
	var parse func([]byte) (proto.Message, error)
	switch c.format {
	case outputFormatJson:
		parse = c.parseJson
	default:
		parse = c.parseYaml
	}
	object, err = parse(data)
	if err != nil {
		return fmt.Errorf("failed to parse modified object: %w", err)
	}

	// Save the result:
	updated, err := c.update(ctx, object)
	if err != nil {
		return err
	}

	c.showWatchSuggestion(ctx, updated)

	return nil
}

// findEditor tries to find the name of the editor command. It will first try with the content of the `EDITOR` and
// `VISUAL` environment variables, and if those are empty it defaults to `vi`.
func (c *runnerContext) findEditor(ctx context.Context) string {
	for _, editorEnvVar := range editorEnvVars {
		value, ok := os.LookupEnv(editorEnvVar)
		if ok && value != "" {
			c.logger.DebugContext(
				ctx,
				"Found editor using environment variable",
				slog.String("var", editorEnvVar),
				slog.String("value", value),
			)
			return value
		}
	}
	c.logger.InfoContext(
		ctx,
		"Didn't find a editor in the environment, will use the default",
		slog.Any("vars", editorEnvVars),
		slog.String("default", defaultEditor),
	)
	return defaultEditor
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

func (c *runnerContext) update(ctx context.Context, object proto.Message) (result proto.Message, err error) {
	result, err = c.helper.Update(ctx, object)
	return
}

func (c *runnerContext) showWatchSuggestion(ctx context.Context, object proto.Message) {
	objectId := c.helper.GetId(object)
	c.console.Render(ctx, "watch_suggestion.txt", map[string]any{
		"Object": c.helper.Singular(),
		"Id":     objectId,
	})
}

func (c *runnerContext) renderJson(object proto.Message) (result []byte, err error) {
	result, err = c.marshalOptions.Marshal(object)
	return
}

func (c *runnerContext) renderYaml(object proto.Message) (result []byte, err error) {
	data, err := c.renderJson(object)
	if err != nil {
		return
	}
	var value any
	err = json.Unmarshal(data, &value)
	if err != nil {
		return
	}
	buffer := &bytes.Buffer{}
	encoder := yaml.NewEncoder(buffer)
	encoder.SetIndent(2)
	err = encoder.Encode(value)
	if err != nil {
		return
	}
	result = buffer.Bytes()
	return
}

func (c *runnerContext) parseJson(data []byte) (result proto.Message, err error) {
	object := c.helper.Instance()
	err = protojson.Unmarshal(data, object)
	if err != nil {
		return
	}
	result = object
	return
}

func (c *runnerContext) parseYaml(data []byte) (result proto.Message, err error) {
	var value any
	err = yaml.Unmarshal(data, &value)
	if err != nil {
		return
	}
	data, err = json.Marshal(value)
	if err != nil {
		return
	}
	result, err = c.parseJson(data)
	return
}

// editorEnvVars is the list of environment variables that will be used to obtain the name of the editor command.
var editorEnvVars = []string{
	"EDITOR",
	"VISUAL",
}

// defualtEditor is the editor used when the environment variables don't indicate any other editor.
const defaultEditor = "vi"
