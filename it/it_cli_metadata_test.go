/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package it

import (
	"context"
	"fmt"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/uuid"
)

var _ = Describe("CLI Metadata", Label("cli", "metadata"), func() {
	var (
		homeDir        string
		networkClassId string
		vnName         string
	)

	BeforeEach(func() {
		var err error
		homeDir, err = tool.NewCLIHomeDir()
		Expect(err).ToNot(HaveOccurred())

		ncClient := privatev1.NewNetworkClassesClient(tool.InternalView().AdminConn())
		ncResp, err := ncClient.Create(context.Background(), privatev1.NetworkClassesCreateRequest_builder{
			Object: privatev1.NetworkClass_builder{
				Title:                  "CLI Metadata Test NC",
				ImplementationStrategy: "cudn",
				FabricManager:          "netris",
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		networkClassId = ncResp.GetObject().GetId()

		DeferCleanup(func() {
			ctx := context.Background()
			if vnName != "" {
				tool.RunCLI(ctx, homeDir, "delete", "virtualnetwork", vnName)
			}
			ncClient := privatev1.NewNetworkClassesClient(tool.InternalView().AdminConn())
			ncClient.Delete(ctx, privatev1.NetworkClassesDeleteRequest_builder{
				Id: networkClassId,
			}.Build())
			os.RemoveAll(homeDir)
		})
	})

	It("Label a resource", func(ctx context.Context) {
		_, _, exitCode := tool.LoginCLI(ctx, homeDir, adminUsername, adminsPassword)
		Expect(exitCode).To(Equal(0))

		vnName = fmt.Sprintf("cli-label-%s", uuid.New())
		_, stderr, exitCode := tool.RunCLI(ctx, homeDir,
			"create", "virtualnetwork",
			"--name", vnName,
			"--network-class", networkClassId,
			"--ipv4-cidr", "10.110.0.0/16",
		)
		Expect(exitCode).To(Equal(0), "create should succeed, stderr=%s", stderr)

		_, _, exitCode = tool.RunCLI(ctx, homeDir,
			"label", "virtualnetwork", vnName, "env=test",
		)
		Expect(exitCode).To(Equal(0), "label should succeed")

		stdout, _, exitCode := tool.RunCLI(ctx, homeDir,
			"get", "virtualnetwork", vnName, "-o", "json",
		)
		Expect(exitCode).To(Equal(0), "get should succeed")
		Expect(stdout).To(ContainSubstring("env"))
	})

	It("Annotate a resource", func(ctx context.Context) {
		_, _, exitCode := tool.LoginCLI(ctx, homeDir, adminUsername, adminsPassword)
		Expect(exitCode).To(Equal(0))

		vnName = fmt.Sprintf("cli-annotate-%s", uuid.New())
		_, stderr, exitCode := tool.RunCLI(ctx, homeDir,
			"create", "virtualnetwork",
			"--name", vnName,
			"--network-class", networkClassId,
			"--ipv4-cidr", "10.111.0.0/16",
		)
		Expect(exitCode).To(Equal(0), "create should succeed, stderr=%s", stderr)

		_, _, exitCode = tool.RunCLI(ctx, homeDir,
			"annotate", "virtualnetwork", vnName, "note=testing",
		)
		Expect(exitCode).To(Equal(0), "annotate should succeed")

		stdout, _, exitCode := tool.RunCLI(ctx, homeDir,
			"get", "virtualnetwork", vnName, "-o", "json",
		)
		Expect(exitCode).To(Equal(0), "get should succeed")
		Expect(stdout).To(ContainSubstring("note"))
	})

	It("Remove a label", func(ctx context.Context) {
		_, _, exitCode := tool.LoginCLI(ctx, homeDir, adminUsername, adminsPassword)
		Expect(exitCode).To(Equal(0))

		vnName = fmt.Sprintf("cli-rmlabel-%s", uuid.New())
		_, stderr, exitCode := tool.RunCLI(ctx, homeDir,
			"create", "virtualnetwork",
			"--name", vnName,
			"--network-class", networkClassId,
			"--ipv4-cidr", "10.112.0.0/16",
		)
		Expect(exitCode).To(Equal(0), "create should succeed, stderr=%s", stderr)

		_, _, exitCode = tool.RunCLI(ctx, homeDir,
			"label", "virtualnetwork", vnName, "env=test",
		)
		Expect(exitCode).To(Equal(0), "add label should succeed")

		_, _, exitCode = tool.RunCLI(ctx, homeDir,
			"label", "virtualnetwork", vnName, "env-",
		)
		Expect(exitCode).To(Equal(0), "remove label should succeed")

		stdout, _, exitCode := tool.RunCLI(ctx, homeDir,
			"get", "virtualnetwork", vnName, "-o", "json",
		)
		Expect(exitCode).To(Equal(0), "get should succeed")
		Expect(stdout).ToNot(ContainSubstring(`"env"`))
	})
})
