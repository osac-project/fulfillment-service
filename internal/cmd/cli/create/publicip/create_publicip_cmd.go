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
	"log/slog"
	"strings"

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
		Use:                   "publicip [FLAG...]",
		Aliases:               []string{string(proto.MessageName((*publicv1.PublicIP)(nil)))},
		Short:                 shortHelp,
		Long:                  longHelp,
		DisableFlagsInUseLine: true,
		Args:                  cobra.NoArgs,
		RunE:                  runner.run,
	}
	flags := result.Flags()
	flags.StringVarP(
		&runner.args.name,
		"name",
		"n",
		"",
		nameFlagHelp,
	)
	flags.StringVar(
		&runner.args.pool,
		"pool",
		"",
		poolFlagHelp,
	)
	flags.StringVar(
		&runner.args.ipFamily,
		"ip-family",
		"",
		ipFamilyFlagHelp,
	)
	result.MarkFlagsMutuallyExclusive("pool", "ip-family")
	// Note: attaching a compute instance at creation time (via --compute-instance flag) is future
	// scope. To attach a public IP to a compute instance, use 'osac create publicipattachment'.
	return result
}

type runnerContext struct {
	args struct {
		name     string
		pool     string
		ipFamily string
	}
	logger   *slog.Logger
	console  *terminal.Console
	settings *config.Settings
}

func (c *runnerContext) run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	c.logger = logging.LoggerFromContext(ctx)
	c.console = terminal.ConsoleFromContext(ctx)

	c.settings = config.SettingsFromContext(ctx)
	if !c.settings.Armed() {
		return fmt.Errorf("there is no configuration, run the 'login' command")
	}

	conn, err := c.settings.Connect(ctx, cmd.Flags())
	if err != nil {
		return fmt.Errorf("failed to create gRPC connection: %w", err)
	}
	defer conn.Close()

	spec := publicv1.PublicIPSpec_builder{}

	if c.args.pool != "" {
		poolClient := publicv1.NewPublicIPPoolsClient(conn)
		pool, err := lookup.Find(c.args.pool, "public IP pool", func(filter string, limit int32) ([]*publicv1.PublicIPPool, error) {
			resp, err := poolClient.List(ctx, publicv1.PublicIPPoolsListRequest_builder{
				Filter: proto.String(filter),
				Limit:  proto.Int32(limit),
			}.Build())
			if err != nil {
				return nil, fmt.Errorf("failed to resolve public IP pool %q: %w", c.args.pool, err)
			}
			return resp.GetItems(), nil
		})
		if err != nil {
			return err
		}
		spec.Pool = pool.GetId()
	} else if c.args.ipFamily != "" {
		family, err := parseIPFamily(c.args.ipFamily)
		if err != nil {
			return err
		}
		spec.IpFamily = family
	}

	client := publicv1.NewPublicIPsClient(conn)

	publicIP := publicv1.PublicIP_builder{
		Metadata: publicv1.Metadata_builder{
			Name:   c.args.name,
			Tenant: c.settings.Tenant(),
		}.Build(),
		Spec: spec.Build(),
	}.Build()

	response, err := client.Create(ctx, publicv1.PublicIPsCreateRequest_builder{Object: publicIP}.Build())
	if err != nil {
		return fmt.Errorf("failed to create public IP: %w", err)
	}

	c.console.Infof(ctx, "Created public IP '%s' (ID: %s).\n", response.Object.GetMetadata().GetName(), response.Object.GetId())

	return nil
}

func parseIPFamily(value string) (publicv1.IPFamily, error) {
	switch strings.ToLower(value) {
	case "ipv4":
		return publicv1.IPFamily_IP_FAMILY_IPV4, nil
	case "ipv6":
		return publicv1.IPFamily_IP_FAMILY_IPV6, nil
	default:
		return publicv1.IPFamily_IP_FAMILY_UNSPECIFIED,
			fmt.Errorf("invalid IP family %q: must be 'IPv4' or 'IPv6'", value)
	}
}

const shortHelp = `Create a public IP.`

const longHelp = `
Allocate a public IP address from a PublicIPPool.

Specify either {{ bt }}--pool{{ bt }} to select a specific pool, or {{ bt }}--ip-family{{ bt }}
to let the system auto-select a READY pool with available capacity. If neither is provided, the
system defaults to auto-selecting an IPv4 pool. The two flags are mutually exclusive.

Examples:

{{ bt 3 }}shell
# Create a public IP from a specific pool
{{ binary }} create publicip --name my-ip --pool pool-abc123

# Create a public IP with automatic IPv6 pool selection
{{ binary }} create publicip --name my-ip --ip-family IPv6

# Create a public IP with default IPv4 auto-selection
{{ binary }} create publicip --name my-ip
{{ bt 3 }}
`

const nameFlagHelp = `
_NAME_ - Name of the public IP.
`

const poolFlagHelp = `
_ID|NAME_ - ID or name of the parent PublicIPPool to allocate the address from.
Mutually exclusive with {{ bt }}--ip-family{{ bt }}.
`

const ipFamilyFlagHelp = `
_IPv4|IPv6_ - IP address family preference for automatic pool selection.
Mutually exclusive with {{ bt }}--pool{{ bt }}.
`
