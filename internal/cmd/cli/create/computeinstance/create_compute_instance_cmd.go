/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package computeinstance

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/config"
	"github.com/osac-project/fulfillment-service/internal/exit"
	"github.com/osac-project/fulfillment-service/internal/logging"
	"github.com/osac-project/fulfillment-service/internal/reflection"
	"github.com/osac-project/fulfillment-service/internal/terminal"
)

//go:embed templates
var templatesFS embed.FS

func Cmd() *cobra.Command {
	runner := &runnerContext{}
	result := &cobra.Command{
		Use:     "computeinstance [flags]",
		Aliases: []string{string(proto.MessageName((*publicv1.ComputeInstance)(nil)))},
		Short:   "Create a compute instance",
		RunE:    runner.run,
	}
	flags := result.Flags()
	flags.StringVarP(
		&runner.args.name,
		"name",
		"n",
		"",
		"Name of the compute instance.",
	)
	flags.StringVarP(
		&runner.args.template,
		"template",
		"t",
		"",
		"Template identifier or name",
	)
	flags.StringSliceVarP(
		&runner.args.templateParameterValues,
		"template-parameter",
		"p",
		[]string{},
		"Template parameter in the format 'name=value'.",
	)
	flags.StringSliceVarP(
		&runner.args.templateParameterFiles,
		"template-parameter-file",
		"f",
		[]string{},
		"Template parameter from file in the format 'name=filename'.",
	)
	flags.Int32Var(
		&runner.args.cores,
		"cores",
		0,
		"Number of CPU cores.",
	)
	flags.Int32Var(
		&runner.args.memoryGiB,
		"memory-gib",
		0,
		"Memory size in GiB.",
	)
	flags.StringVar(
		&runner.args.imageSourceRef,
		"image",
		"",
		"Image reference (e.g. OCI image URL).",
	)
	flags.StringVar(
		&runner.args.imageSourceType,
		"image-source-type",
		"registry",
		"Image source type.",
	)
	flags.StringVar(
		&runner.args.sshKey,
		"ssh-key",
		"",
		"SSH public key.",
	)
	flags.Int32Var(
		&runner.args.bootDiskSizeGiB,
		"boot-disk-size",
		0,
		"Boot disk size in GiB.",
	)
	flags.StringSliceVar(
		&runner.args.additionalDisks,
		"additional-disk",
		[]string{},
		"Additional disk size in GiB (e.g. '100'). Repeatable.",
	)
	flags.StringVar(
		&runner.args.runStrategy,
		"run-strategy",
		"",
		"Run strategy (e.g. 'Always' or 'Halted').",
	)
	flags.StringVar(
		&runner.args.userData,
		"user-data",
		"",
		"User data for the compute instance (e.g. cloud-init, ignition).",
	)
	return result
}

type runnerContext struct {
	args struct {
		name                    string
		template                string
		templateParameterValues []string
		templateParameterFiles  []string
		cores                   int32
		memoryGiB               int32
		imageSourceRef          string
		imageSourceType         string
		sshKey                  string
		bootDiskSizeGiB         int32
		additionalDisks         []string
		runStrategy             string
		userData                string
	}
	logger                 *slog.Logger
	console                *terminal.Console
	templatesClient        publicv1.ComputeInstanceTemplatesClient
	computeInstancesClient publicv1.ComputeInstancesClient
}

func (c *runnerContext) run(cmd *cobra.Command, args []string) error {
	var err error

	// Get the context:
	ctx := cmd.Context()

	// Get the logger and console:
	c.logger = logging.LoggerFromContext(ctx)
	c.console = terminal.ConsoleFromContext(ctx)

	// Add the templates file system to the console:
	err = c.console.AddTemplates(templatesFS, "templates")
	if err != nil {
		return fmt.Errorf("failed to load templates: %w", err)
	}

	// Get the configuration:
	cfg, err := config.Load(ctx)
	if err != nil {
		return err
	}
	if cfg.Address == "" {
		return fmt.Errorf("there is no configuration, run the 'login' command")
	}

	// Check that we have a template:
	if c.args.template == "" {
		return fmt.Errorf("template identifier or name is required")
	}

	// Create the gRPC connection from the configuration:
	conn, err := cfg.Connect(ctx, cmd.Flags())
	if err != nil {
		return fmt.Errorf("failed to create gRPC connection: %w", err)
	}
	defer conn.Close()

	// Create the reflection helper:
	helper, err := reflection.NewHelper().
		SetLogger(c.logger).
		SetConnection(conn).
		AddPackages(cfg.Packages()).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create reflection tool: %w", err)
	}
	c.console.SetHelper(helper)

	// Create the gRPC clients:
	c.templatesClient = publicv1.NewComputeInstanceTemplatesClient(conn)
	c.computeInstancesClient = publicv1.NewComputeInstancesClient(conn)

	// Fetch the compute instance template:
	template, err := c.findTemplate(ctx)
	if err != nil {
		return err
	}
	if template == nil {
		return exit.Error(1)
	}

	// Parse the template parameters:
	templateParameterValues, templateParameterIssues := c.parseTemplateParameters(ctx, template)
	if len(templateParameterIssues) > 0 {
		validTemplateParameters := c.validTemplateParameters(template)
		c.console.Render(ctx, "template_parameter_issues.txt", map[string]any{
			"Template":   c.args.template,
			"Parameters": validTemplateParameters,
			"Issues":     templateParameterIssues,
		})
		return exit.Error(1)
	}

	// Build the spec:
	spec, err := c.buildSpec(template.GetId(), templateParameterValues)
	if err != nil {
		return err
	}

	// Prepare the compute instance:
	computeInstance := publicv1.ComputeInstance_builder{
		Metadata: publicv1.Metadata_builder{
			Name: c.args.name,
		}.Build(),
		Spec: spec,
	}.Build()

	// Create the compute instance:
	response, err := c.computeInstancesClient.Create(ctx, publicv1.ComputeInstancesCreateRequest_builder{
		Object: computeInstance,
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to create compute instance: %w", err)
	}

	// Display the result:
	computeInstance = response.Object
	c.console.Infof(ctx, "Created compute instance '%s'.\n", computeInstance.Id)

	return nil
}

// findTemplate finds a compute instance template by identifier or name. It tries to find by identifier or name using a
// server-side filter. If there is exactly one match it returns it. If there are multiple matches it displays them to
// the user and returns an error. If there are no matches it displays available templates and returns an error.
func (c *runnerContext) findTemplate(ctx context.Context) (result *publicv1.ComputeInstanceTemplate, err error) {
	// Try to find the template by identifier or name using a filter:
	filter := fmt.Sprintf(
		"this.id == %[1]q || this.metadata.name == %[1]q",
		c.args.template,
	)
	response, err := c.templatesClient.List(ctx, publicv1.ComputeInstanceTemplatesListRequest_builder{
		Filter: proto.String(filter),
		Limit:  proto.Int32(10),
	}.Build())
	if err != nil {
		return nil, fmt.Errorf("failed to list templates: %w", err)
	}
	total := response.GetTotal()
	matches := response.GetItems()

	// If there is exactly one match, use it:
	if len(matches) == 1 {
		result = matches[0]
		return
	}

	// If there are multiple matches, display them and advise to use the identifier:
	if len(matches) > 1 {
		c.console.Render(ctx, "template_conflict.txt", map[string]any{
			"Matches": matches,
			"Ref":     c.args.template,
			"Total":   total,
		})
		err = exit.Error(1)
		return
	}

	// If we are here then no matches were found, we will show to the user some of the available templates:
	response, err = c.templatesClient.List(ctx, publicv1.ComputeInstanceTemplatesListRequest_builder{
		Limit: proto.Int32(10),
	}.Build())
	if err != nil {
		return nil, fmt.Errorf("failed to list templates: %w", err)
	}
	examples := response.GetItems()
	c.console.Render(ctx, "template_not_found.txt", map[string]any{
		"Examples": examples,
		"Ref":      c.args.template,
	})
	err = exit.Error(1)
	return
}

// parseTemplateParameters parses the '--template-parameter' and '--template-parameter-file' flags into a map of
// parameter name to value, and a list of issues found. The issues are intended for display to the user.
func (c *runnerContext) parseTemplateParameters(ctx context.Context,
	template *publicv1.ComputeInstanceTemplate) (result map[string]*anypb.Any, issues []string) {
	// Prepare empty results and issues:
	result = map[string]*anypb.Any{}

	// Make a map of parameter definitions indexed by name for quick lookup:
	definitions := map[string]*publicv1.ComputeInstanceTemplateParameterDefinition{}
	for _, definition := range template.GetParameters() {
		definitions[definition.GetName()] = definition
	}

	// Parse '--template-parameter' flags:
	for _, flag := range c.args.templateParameterValues {
		parts := strings.SplitN(flag, "=", 2)
		if len(parts) != 2 {
			name := strings.TrimSpace(flag)
			definition := definitions[name]
			if definition == nil {
				issues = append(
					issues,
					fmt.Sprintf(
						"In '%s' parameter '%s' doesn't exist, and if it existed the value "+
							"would be missing",
						flag, name,
					),
				)
			} else {
				issues = append(
					issues,
					fmt.Sprintf(
						"In '%s' parameter value is missing",
						flag,
					),
				)
			}
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name == "" {
			issues = append(
				issues,
				fmt.Sprintf(
					"In '%s' parameter name is missing",
					flag,
				),
			)
			continue
		}
		definition := definitions[name]
		if definition == nil {
			issues = append(
				issues,
				fmt.Sprintf(
					"In '%s' parameter '%s' doesn't exist",
					flag, name,
				),
			)
			continue
		}
		text := strings.TrimSpace(parts[1])
		value, issue := c.convertTextToTemplateParameterValue(ctx, text, definition.GetType())
		if issue != "" {
			issues = append(issues, fmt.Sprintf("In '%s' %s", flag, issue))
			continue
		}
		result[name] = value
	}

	// Parse '--template-parameter-file' flags:
	for _, flag := range c.args.templateParameterFiles {
		parts := strings.SplitN(flag, "=", 2)
		if len(parts) != 2 {
			name := strings.TrimSpace(flag)
			definition := definitions[name]
			if definition == nil {
				issues = append(issues, fmt.Sprintf(
					"In '%s' parameter '%s' doesn't exist, and if existed the file would be "+
						"missing",
					flag, name,
				))
			} else {
				issues = append(
					issues,
					fmt.Sprintf(
						"In '%s' file is missing",
						flag,
					))
			}
			continue
		}
		name := strings.TrimSpace(parts[0])
		if name == "" {
			issues = append(
				issues,
				fmt.Sprintf(
					"In '%s' parameter name is missing",
					flag,
				),
			)
			continue
		}
		definition := definitions[name]
		if definition == nil {
			issues = append(
				issues,
				fmt.Sprintf(
					"In '%s' parameter '%s' doesn't exist",
					flag, name,
				),
			)
			continue
		}
		file := strings.TrimSpace(parts[1])
		if file == "" {
			issues = append(
				issues,
				fmt.Sprintf(
					"In '%s' file is missing",
					flag,
				),
			)
			continue
		}
		data, err := os.ReadFile(file)
		if errors.Is(err, os.ErrNotExist) {
			issues = append(
				issues, fmt.Sprintf(
					"In '%s' file '%s' doesn't exist",
					flag, file,
				),
			)
			continue
		}
		if err != nil {
			issues = append(
				issues,
				fmt.Sprintf(
					"In '%s' failed to read file '%s': %v",
					flag, file, err,
				),
			)
			continue
		}
		text := string(data)
		value, issue := c.convertTextToTemplateParameterValue(ctx, text, definition.GetType())
		if issue != "" {
			issues = append(
				issues,
				fmt.Sprintf("In '%s' %s'", flag, issue),
			)
			continue
		}
		result[name] = value
	}

	// Add issues for missing required parameters, at the end of the list and sorted by parameter name:
	var missing []*publicv1.ComputeInstanceTemplateParameterDefinition
	for _, definition := range template.GetParameters() {
		if definition.GetRequired() && result[definition.GetName()] == nil {
			missing = append(missing, definition)
		}
	}
	sort.Slice(missing, func(i, j int) bool {
		return missing[i].GetName() < missing[j].GetName()
	})
	for _, definition := range missing {
		issues = append(
			issues,
			fmt.Sprintf("Parameter '%s' is required", definition.GetName()),
		)
	}

	return
}

// convertTextToTemplateParameterValue converts a string value to the appropriate protobuf type based on the kind. It
// returns the value and a string descibing the issue if the conversion fails.
func (c *runnerContext) convertTextToTemplateParameterValue(ctx context.Context, text,
	kind string) (result *anypb.Any, issue string) {
	var wrapper proto.Message
	switch kind {
	case "type.googleapis.com/google.protobuf.StringValue":
		wrapper = &wrapperspb.StringValue{Value: text}
	case "type.googleapis.com/google.protobuf.BoolValue":
		text = strings.TrimSpace(text)
		value, err := strconv.ParseBool(text)
		if err != nil {
			c.logger.DebugContext(
				ctx,
				"Failed to parse boolean",
				slog.String("text", text),
				slog.Any("error", err),
			)
			issue = fmt.Sprintf(
				"value '%s' isn't a valid boolean, valid values are 'true' and 'false'",
				text,
			)
			return
		}
		wrapper = &wrapperspb.BoolValue{Value: value}
	case "type.googleapis.com/google.protobuf.Int32Value":
		text = strings.TrimSpace(text)
		var value int64
		value, err := strconv.ParseInt(text, 10, 32)
		if err != nil {
			c.logger.DebugContext(
				ctx,
				"Failed to parse 32-bit integer number",
				slog.String("text", text),
				slog.Any("error", err),
			)
			issue = fmt.Sprintf("value '%s' isn't a valid 32-bit integer", text)
			return
		}
		wrapper = &wrapperspb.Int32Value{Value: int32(value)}
	case "type.googleapis.com/google.protobuf.Int64Value":
		text = strings.TrimSpace(text)
		var value int64
		value, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			c.logger.DebugContext(
				ctx,
				"Failed to parse 64-bit integer number",
				slog.String("text", text),
				slog.Any("error", err),
			)
			issue = fmt.Sprintf("value '%s' isn't a valid 64-bit integer", text)
			return
		}
		wrapper = &wrapperspb.Int64Value{Value: value}
	case "type.googleapis.com/google.protobuf.FloatValue":
		text = strings.TrimSpace(text)
		var value float64
		value, err := strconv.ParseFloat(text, 32)
		if err != nil {
			c.logger.DebugContext(
				ctx,
				"Failed to parse 32-bit floating point number",
				slog.String("text", text),
				slog.Any("error", err),
			)
			issue = fmt.Sprintf("value '%s' isn't a valid 32-bit floating point number", text)
			return
		}
		wrapper = &wrapperspb.FloatValue{Value: float32(value)}
	case "type.googleapis.com/google.protobuf.DoubleValue":
		text = strings.TrimSpace(text)
		var value float64
		value, err := strconv.ParseFloat(text, 64)
		if err != nil {
			c.logger.DebugContext(
				ctx,
				"Failed to parse 64-bit floating point number",
				slog.String("text", text),
				slog.Any("error", err),
			)
			issue = fmt.Sprintf("value '%s' isn't a valid 64-bit floating point numberw", text)
			return
		}
		wrapper = &wrapperspb.DoubleValue{Value: value}
	case "type.googleapis.com/google.protobuf.BytesValue":
		wrapper = &wrapperspb.BytesValue{Value: []byte(text)}
	case "type.googleapis.com/google.protobuf.Timestamp":
		text = strings.TrimSpace(text)
		var value time.Time
		value, err := time.Parse(time.RFC3339, text)
		if err != nil {
			c.logger.DebugContext(
				ctx,
				"Failed to parse RFC3339 timestamp",
				slog.String("text", text),
				slog.Any("error", err),
			)
			issue = fmt.Sprintf("value '%s' isn't a valid RFC3339 timestamp", text)
			return
		}
		wrapper = timestamppb.New(value)
	case "type.googleapis.com/google.protobuf.Duration":
		var value time.Duration
		value, err := time.ParseDuration(text)
		if err != nil {
			c.logger.DebugContext(
				ctx,
				"Failed to parse duration",
				slog.String("text", text),
				slog.Any("error", err),
			)
			issue = fmt.Sprintf("value '%s' isn't a valid duration", text)
			return
		}
		wrapper = durationpb.New(value)
	default:
		issue = fmt.Sprintf("flag has is of an unsupported type '%s'", kind)
		return
	}
	if issue != "" {
		return
	}
	result, err := anypb.New(wrapper)
	if err != nil {
		c.logger.DebugContext(
			ctx,
			"Failed to create protobuf value for template parameter",
			slog.String("text", text),
			slog.String("kind", kind),
			slog.Any("error", err),
		)
		issue = fmt.Sprintf("Failed to create protobuf value for template parameter: %v", err)
		return
	}
	return
}

// buildSpec constructs the ComputeInstanceSpec from template info and CLI flags.
func (c *runnerContext) buildSpec(templateID string,
	templateParams map[string]*anypb.Any) (*publicv1.ComputeInstanceSpec, error) {
	spec := publicv1.ComputeInstanceSpec_builder{
		Template:           templateID,
		TemplateParameters: templateParams,
	}
	if c.args.imageSourceRef != "" {
		spec.Image = publicv1.ComputeInstanceImage_builder{
			SourceType: c.args.imageSourceType,
			SourceRef:  c.args.imageSourceRef,
		}.Build()
	}
	if c.args.cores > 0 {
		spec.Cores = proto.Int32(c.args.cores)
	}
	if c.args.memoryGiB > 0 {
		spec.MemoryGib = proto.Int32(c.args.memoryGiB)
	}
	if c.args.sshKey != "" {
		spec.SshKey = proto.String(c.args.sshKey)
	}
	if c.args.bootDiskSizeGiB > 0 {
		spec.BootDisk = publicv1.ComputeInstanceDisk_builder{
			SizeGib: c.args.bootDiskSizeGiB,
		}.Build()
	}
	if len(c.args.additionalDisks) > 0 {
		disks, err := parseAdditionalDisks(c.args.additionalDisks)
		if err != nil {
			return nil, err
		}
		spec.AdditionalDisks = disks
	}
	if c.args.runStrategy != "" {
		spec.RunStrategy = proto.String(c.args.runStrategy)
	}
	if c.args.userData != "" {
		spec.UserData = proto.String(c.args.userData)
	}
	return spec.Build(), nil
}

// parseAdditionalDisks parses disk sizes in GiB.
// Example: "100"
func parseAdditionalDisks(diskArgs []string) ([]*publicv1.ComputeInstanceDisk, error) {
	disks := make([]*publicv1.ComputeInstanceDisk, 0, len(diskArgs))
	for _, arg := range diskArgs {
		sizeGiB, err := strconv.ParseInt(arg, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid disk size '%s': expected an integer number of GiB", arg)
		}
		disks = append(disks, publicv1.ComputeInstanceDisk_builder{
			SizeGib: int32(sizeGiB),
		}.Build())
	}
	return disks, nil
}

// validTemplateParameter contains the information about a valid template parameter, for use in the error messages that
// display them.
type validTemplateParameter struct {
	// Name is the name of the parameter.
	Name string

	// Type is the type of the parameter.
	Type string

	// Title is the title of the parameter.
	Title string
}

// validTemplateParameters returns the list of valid template parameters for the given template.
func (c *runnerContext) validTemplateParameters(template *publicv1.ComputeInstanceTemplate) []validTemplateParameter {
	// Prepare the results:
	results := []validTemplateParameter{}
	for _, parameter := range template.GetParameters() {
		result := validTemplateParameter{
			Name:  parameter.GetName(),
			Title: parameter.GetTitle(),
		}
		switch parameter.GetType() {
		case "type.googleapis.com/google.protobuf.StringValue":
			result.Type = "string"
		case "type.googleapis.com/google.protobuf.BoolValue":
			result.Type = "boolean"
		case "type.googleapis.com/google.protobuf.Int32Value":
			result.Type = "int32"
		case "type.googleapis.com/google.protobuf.Int64Value":
			result.Type = "int64"
		case "type.googleapis.com/google.protobuf.FloatValue":
			result.Type = "float"
		case "type.googleapis.com/google.protobuf.DoubleValue":
			result.Type = "double"
		case "type.googleapis.com/google.protobuf.BytesValue":
			result.Type = "bytes"
		case "type.googleapis.com/google.protobuf.Timestamp":
			result.Type = "timestamp"
		case "type.googleapis.com/google.protobuf.Duration":
			result.Type = "duration"
		default:
			result.Type = "unknown"
		}
		results = append(results, result)
	}

	// Sort the result by name so that the output will be predictable:
	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})

	return results
}
