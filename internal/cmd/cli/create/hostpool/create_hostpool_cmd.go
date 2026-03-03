/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package hostpool

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/config"
	"github.com/osac-project/fulfillment-service/internal/logging"
)

func Cmd() *cobra.Command {
	runner := &runnerContext{}
	result := &cobra.Command{
		Use:     "hostpool [flags]",
		Aliases: []string{string(proto.MessageName((*publicv1.HostPool)(nil)))},
		Short:   "Create a host pool",
		RunE:    runner.run,
	}
	flags := result.Flags()
	flags.StringVarP(
		&runner.args.name,
		"name",
		"n",
		"",
		"Name of the host pool.",
	)
	flags.StringArrayVarP(
		&runner.args.hostSets,
		"host-set",
		"s",
		[]string{},
		"Host set in the format 'name=host_class:value,size:value' (e.g., 'workers=host_class:worker-class,size:5').",
	)
	return result
}

type runnerContext struct {
	args struct {
		name     string
		hostSets []string
	}
	logger *slog.Logger
	client publicv1.HostPoolsClient
}

func (c *runnerContext) run(cmd *cobra.Command, args []string) error {
	var err error

	// Get the context:
	ctx := cmd.Context()

	// Get the logger:
	c.logger = logging.LoggerFromContext(ctx)

	// Get the configuration:
	cfg, err := config.Load(ctx)
	if err != nil {
		return err
	}
	if cfg == nil {
		return fmt.Errorf("there is no configuration, run the 'login' command")
	}

	// Check that we have at least one host set:
	if len(c.args.hostSets) == 0 {
		return fmt.Errorf("at least one host set is required, use --host-set flag in format 'name=host_class:value,size:value'")
	}

	// Create the gRPC connection from the configuration:
	conn, err := cfg.Connect(ctx, cmd.Flags())
	if err != nil {
		return fmt.Errorf("failed to create gRPC connection: %w", err)
	}
	defer conn.Close()

	// Create the gRPC client:
	c.client = publicv1.NewHostPoolsClient(conn)

	// Parse the host sets:
	hostSetsMap, err := c.parseHostSets()
	if err != nil {
		return fmt.Errorf("failed to parse host sets: %w", err)
	}

	// Prepare the host pool:
	hostPool := publicv1.HostPool_builder{
		Metadata: publicv1.Metadata_builder{
			Name: c.args.name,
		}.Build(),
		Spec: publicv1.HostPoolSpec_builder{
			HostSets: hostSetsMap,
		}.Build(),
	}.Build()

	// Create the host pool:
	response, err := c.client.Create(ctx, publicv1.HostPoolsCreateRequest_builder{
		Object: hostPool,
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to create host pool: %w", err)
	}

	// Display the result:
	createdHostPool := response.Object
	fmt.Printf("Created host pool '%s'.\n", createdHostPool.Id)

	return nil
}

// parseHostSets parses the --host-set flags into a map of host set name to HostPoolHostSet
func (c *runnerContext) parseHostSets() (map[string]*publicv1.HostPoolHostSet, error) {
	result := make(map[string]*publicv1.HostPoolHostSet)

	for _, hostSetFlag := range c.args.hostSets {
		// Split by '=' to get name and parameters
		parts := strings.SplitN(hostSetFlag, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid host set format '%s', expected 'name=host_class:value,size:value'", hostSetFlag)
		}

		hostSetName := strings.TrimSpace(parts[0])
		if hostSetName == "" {
			return nil, fmt.Errorf("host set name cannot be empty in '%s'", hostSetFlag)
		}

		// Check for duplicate host set names
		if _, exists := result[hostSetName]; exists {
			return nil, fmt.Errorf("duplicate host set name '%s' specified", hostSetName)
		}

		// Parse the parameters (host_class:value,size:value)
		paramStr := strings.TrimSpace(parts[1])
		hostSet, err := c.parseHostSetParameters(paramStr, hostSetFlag)
		if err != nil {
			return nil, err
		}

		result[hostSetName] = hostSet
	}

	return result, nil
}

// parseHostSetParameters parses the parameter portion of a host set specification
func (c *runnerContext) parseHostSetParameters(paramStr, originalFlag string) (*publicv1.HostPoolHostSet, error) {
	// Split by comma to get individual parameters
	params := strings.Split(paramStr, ",")
	if len(params) != 2 {
		return nil, fmt.Errorf("invalid parameters '%s' in '%s', expected 'host_class:value,size:value'", paramStr, originalFlag)
	}

	var hostClass string
	var size int32

	for _, param := range params {
		param = strings.TrimSpace(param)

		// Split each parameter by ':' to get key:value
		kvParts := strings.SplitN(param, ":", 2)
		if len(kvParts) != 2 {
			return nil, fmt.Errorf("invalid parameter '%s' in '%s', expected 'key:value' format", param, originalFlag)
		}

		key := strings.TrimSpace(kvParts[0])
		value := strings.TrimSpace(kvParts[1])

		switch key {
		case "host_class":
			if value == "" {
				return nil, fmt.Errorf("host_class value cannot be empty in '%s'", originalFlag)
			}
			hostClass = value
		case "size":
			if value == "" {
				return nil, fmt.Errorf("size value cannot be empty in '%s'", originalFlag)
			}
			sizeInt, err := strconv.ParseInt(value, 10, 32)
			if err != nil {
				return nil, fmt.Errorf("invalid size '%s' in '%s', must be a positive integer", value, originalFlag)
			}
			if sizeInt <= 0 {
				return nil, fmt.Errorf("size must be positive in '%s', got %d", originalFlag, sizeInt)
			}
			size = int32(sizeInt)
		default:
			return nil, fmt.Errorf("unknown parameter '%s' in '%s', expected 'host_class' or 'size'", key, originalFlag)
		}
	}

	// Verify both required parameters were provided
	if hostClass == "" {
		return nil, fmt.Errorf("missing required parameter 'host_class' in '%s'", originalFlag)
	}
	if size == 0 {
		return nil, fmt.Errorf("missing required parameter 'size' in '%s'", originalFlag)
	}

	return publicv1.HostPoolHostSet_builder{
		HostClass: hostClass,
		Size:      size,
	}.Build(), nil
}
