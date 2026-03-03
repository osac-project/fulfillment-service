/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package describe

import (
	"github.com/spf13/cobra"

	"github.com/osac-project/fulfillment-service/internal/cmd/cli/describe/cluster"
	"github.com/osac-project/fulfillment-service/internal/cmd/cli/describe/computeinstance"
	"github.com/osac-project/fulfillment-service/internal/cmd/cli/describe/host"
	"github.com/osac-project/fulfillment-service/internal/cmd/cli/describe/hostpool"
)

func Cmd() *cobra.Command {
	result := &cobra.Command{
		Use:   "describe",
		Short: "Describe a resource",
	}
	result.AddCommand(cluster.Cmd())
	result.AddCommand(computeinstance.Cmd())
	result.AddCommand(host.Cmd())
	result.AddCommand(hostpool.Cmd())
	return result
}
