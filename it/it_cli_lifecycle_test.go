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

var _ = Describe("CLI Resource Lifecycle", Label("cli", "lifecycle"), func() {
	var (
		homeDir        string
		networkClassId string
	)

	BeforeEach(func() {
		var err error
		homeDir, err = tool.NewCLIHomeDir()
		Expect(err).ToNot(HaveOccurred())

		ncClient := privatev1.NewNetworkClassesClient(tool.InternalView().AdminConn())
		ncResp, err := ncClient.Create(context.Background(), privatev1.NetworkClassesCreateRequest_builder{
			Object: privatev1.NetworkClass_builder{
				Title:                  "CLI Lifecycle Test NC",
				ImplementationStrategy: "cudn",
				FabricManager:          "netris",
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		networkClassId = ncResp.GetObject().GetId()

		DeferCleanup(func() {
			ncClient := privatev1.NewNetworkClassesClient(tool.InternalView().AdminConn())
			ncClient.Delete(context.Background(), privatev1.NetworkClassesDeleteRequest_builder{
				Id: networkClassId,
			}.Build())
			os.RemoveAll(homeDir)
		})
	})

	It("Create virtual network via CLI", func(ctx context.Context) {
		_, _, exitCode := tool.LoginCLI(ctx, homeDir, adminUsername, adminsPassword)
		Expect(exitCode).To(Equal(0), "login should succeed")

		vnName := fmt.Sprintf("cli-create-%s", uuid.New())
		stdout, stderr, exitCode := tool.RunCLI(ctx, homeDir,
			"create", "virtualnetwork",
			"--name", vnName,
			"--network-class", networkClassId,
			"--ipv4-cidr", "10.100.0.0/16",
		)
		Expect(exitCode).To(Equal(0), "create should succeed, stdout=%s, stderr=%s", stdout, stderr)
		Expect(stdout).To(ContainSubstring("Created"))

		DeferCleanup(func() {
			tool.RunCLI(ctx, homeDir, "delete", "virtualnetwork", vnName)
		})
	})

	It("Full create-get-describe-delete lifecycle", func(ctx context.Context) {
		_, _, exitCode := tool.LoginCLI(ctx, homeDir, adminUsername, adminsPassword)
		Expect(exitCode).To(Equal(0), "login should succeed")

		vnName := fmt.Sprintf("cli-lifecycle-%s", uuid.New())

		// Create
		stdout, stderr, exitCode := tool.RunCLI(ctx, homeDir,
			"create", "virtualnetwork",
			"--name", vnName,
			"--network-class", networkClassId,
			"--ipv4-cidr", "10.101.0.0/16",
		)
		Expect(exitCode).To(Equal(0), "create should succeed, stderr=%s", stderr)
		Expect(stdout).To(ContainSubstring("Created"))

		// Get by name
		stdout, _, exitCode = tool.RunCLI(ctx, homeDir, "get", "virtualnetwork", vnName)
		Expect(exitCode).To(Equal(0), "get by name should succeed")
		Expect(stdout).To(ContainSubstring(vnName))

		// Describe
		stdout, _, exitCode = tool.RunCLI(ctx, homeDir, "describe", "virtualnetwork", vnName)
		Expect(exitCode).To(Equal(0), "describe should succeed")
		Expect(stdout).To(ContainSubstring("ID:"))

		// Delete by name
		_, _, exitCode = tool.RunCLI(ctx, homeDir, "delete", "virtualnetwork", vnName)
		Expect(exitCode).To(Equal(0), "delete should succeed")
	})
})
