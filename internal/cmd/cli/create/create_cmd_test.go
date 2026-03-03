/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package create

import (
	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/ginkgo/v2/dsl/table"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/cmd/cli/create/cluster"
	"github.com/osac-project/fulfillment-service/internal/cmd/cli/create/computeinstance"
	"github.com/osac-project/fulfillment-service/internal/cmd/cli/create/hostpool"
	"github.com/osac-project/fulfillment-service/internal/cmd/cli/create/hub"
)

var _ = Describe("Create command", func() {
	DescribeTable("Subcommand aliases",
		func(cmdFunc func() *cobra.Command, protoMsg proto.Message) {
			cmd := cmdFunc()
			expectedAlias := string(proto.MessageName(protoMsg))
			Expect(cmd.Aliases).To(ContainElement(expectedAlias))
		},
		Entry("cluster", cluster.Cmd, (*publicv1.Cluster)(nil)),
		Entry("computeinstance", computeinstance.Cmd, (*publicv1.ComputeInstance)(nil)),
		Entry("hostpool", hostpool.Cmd, (*publicv1.HostPool)(nil)),
		Entry("hub", hub.Cmd, (*privatev1.Hub)(nil)),
	)

	Describe("Subcommands", func() {
		It("should have all expected subcommands", func() {
			cmd := Cmd()
			subcommands := cmd.Commands()

			var subcommandNames []string
			for _, subcmd := range subcommands {
				subcommandNames = append(subcommandNames, subcmd.Name())
			}

			Expect(subcommandNames).To(ContainElements("cluster", "computeinstance", "hostpool", "hub"))
		})
	})
})
