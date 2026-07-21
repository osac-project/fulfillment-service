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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/osac-project/fulfillment-service/internal/uuid"
)

var _ = Describe("CLI Resource Lifecycle", Label("cli", "lifecycle"), func() {
	var (
		homeDir        string
		networkClassId string
	)

	BeforeEach(func() {
		homeDir = setupCLIHomeDir()
		networkClassId = setupTestNetworkClass("CLI Lifecycle Test NC")
	})

	It("Create virtual network via CLI", func(ctx context.Context) {
		mustLoginCLI(ctx, homeDir, adminUsername, adminsPassword)

		vnName := createCLIVirtualNetwork(ctx, homeDir, networkClassId, "10.100.0.0/16")

		stdout, _, exitCode := tool.RunCLI(ctx, homeDir, "get", "virtualnetwork", vnName)
		Expect(exitCode).To(Equal(0), "get after create should succeed")
		Expect(stdout).To(ContainSubstring(vnName))
	})

	It("Full create-get-describe-delete lifecycle", func(ctx context.Context) {
		mustLoginCLI(ctx, homeDir, adminUsername, adminsPassword)

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
		Expect(stdout).To(ContainSubstring(vnName))

		// Get by name
		stdout, _, exitCode = tool.RunCLI(ctx, homeDir, "get", "virtualnetwork", vnName)
		Expect(exitCode).To(Equal(0), "get by name should succeed")
		Expect(stdout).To(ContainSubstring(vnName))

		// Describe
		stdout, _, exitCode = tool.RunCLI(ctx, homeDir, "describe", "virtualnetwork", vnName)
		Expect(exitCode).To(Equal(0), "describe should succeed")
		Expect(stdout).To(ContainSubstring("ID:"))
		Expect(stdout).To(ContainSubstring(vnName))

		// Delete by name
		stdout, _, exitCode = tool.RunCLI(ctx, homeDir, "delete", "virtualnetwork", vnName)
		Expect(exitCode).To(Equal(0), "delete should succeed")
		Expect(stdout).To(ContainSubstring("Deleted"))

		// Confirm soft-delete: resource remains visible with DELETING=Yes until finalizers complete
		stdout, _, exitCode = tool.RunCLI(ctx, homeDir, "get", "virtualnetwork", vnName)
		Expect(exitCode).To(Equal(0), "get after delete should succeed")
		Expect(stdout).To(ContainSubstring(vnName))
		Expect(stdout).To(MatchRegexp(`(?m)^\S+\s+Yes\s+.*%s`, vnName),
			"DELETING column should be Yes after delete")
	})
})
