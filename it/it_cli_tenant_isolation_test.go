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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CLI Tenant Isolation", Label("cli", "tenant", "isolation"), func() {
	var (
		homeDirA       string
		homeDirB       string
		networkClassId string
	)

	BeforeEach(func() {
		homeDirA = setupCLIHomeDir()
		homeDirB = setupCLIHomeDir()
		networkClassId = setupTestNetworkClass("CLI Isolation Test NC")
	})

	It("Tenant user creates and lists own resources", func(ctx context.Context) {
		// adam belongs to the "engineering" tenant
		mustLoginCLI(ctx, homeDirA, "adam", usersPassword)
		vnName := createCLIVirtualNetwork(ctx, homeDirA, networkClassId, "10.120.0.0/16")

		stdout, _, exitCode := tool.RunCLI(ctx, homeDirA, "get", "virtualnetwork")
		Expect(exitCode).To(Equal(0), "list should succeed")
		Expect(stdout).To(ContainSubstring(vnName))
	})

	It("Cross-tenant isolation prevents visibility", func(ctx context.Context) {
		// adam (engineering) creates a resource
		mustLoginCLI(ctx, homeDirA, "adam", usersPassword)
		vnName := createCLIVirtualNetwork(ctx, homeDirA, networkClassId, "10.121.0.0/16")

		// ben (development) should NOT see adam's resource
		mustLoginCLI(ctx, homeDirB, "ben", usersPassword)

		stdout, _, exitCode := tool.RunCLI(ctx, homeDirB, "get", "virtualnetwork")
		Expect(exitCode).To(Equal(0), "ben list should succeed")
		visible := strings.Contains(stdout, vnName)
		Expect(visible).To(BeFalse(), "ben should not see adam's resource")
	})

	It("Tenant user deletes own resource", func(ctx context.Context) {
		mustLoginCLI(ctx, homeDirA, "adam", usersPassword)

		vnName := createCLIVirtualNetwork(ctx, homeDirA, networkClassId, "10.122.0.0/16")

		stdout, _, exitCode := tool.RunCLI(ctx, homeDirA, "delete", "virtualnetwork", vnName)
		Expect(exitCode).To(Equal(0), "adam should delete own resource")
		Expect(stdout).To(ContainSubstring("Deleted"))

		// Confirm soft-delete: resource remains visible with DELETING=Yes until finalizers complete
		stdout, _, exitCode = tool.RunCLI(ctx, homeDirA, "get", "virtualnetwork", vnName)
		Expect(exitCode).To(Equal(0), "get after delete should succeed")
		Expect(stdout).To(ContainSubstring(vnName))
		Expect(stdout).To(MatchRegexp(`(?m)^\S+\s+Yes\s+.*%s`, vnName),
			"DELETING column should be Yes after delete")
	})
})
