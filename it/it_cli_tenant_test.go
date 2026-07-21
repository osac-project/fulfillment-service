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
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.yaml.in/yaml/v2"
)

var _ = Describe("CLI Tenant", Label("cli", "tenant"), func() {
	var homeDir string

	BeforeEach(func() {
		homeDir = setupCLIHomeDir()
	})

	It("Set tenant scopes subsequent commands", func(ctx context.Context) {
		mustLoginCLI(ctx, homeDir, adminUsername, adminsPassword)

		stdout, _, exitCode := tool.RunCLI(ctx, homeDir, "tenant", usersGroup)
		Expect(exitCode).To(Equal(0), "setting tenant should succeed")
		Expect(stdout).To(ContainSubstring("saved"))
		Expect(stdout).To(ContainSubstring(usersGroup))

		stdout, _, exitCode = tool.RunCLI(ctx, homeDir, "tenant")
		Expect(exitCode).To(Equal(0), "showing tenant should succeed")
		Expect(stdout).To(ContainSubstring(usersGroup))
	})

	It("Show tenant without one set hints at usage", func(ctx context.Context) {
		mustLoginCLI(ctx, homeDir, adminUsername, adminsPassword)

		stdout, _, exitCode := tool.RunCLI(ctx, homeDir, "tenant")
		Expect(exitCode).To(Equal(0), "showing tenant should succeed even with none set")
		Expect(stdout).To(ContainSubstring("No tenant is currently set"))
	})

	It("Clear tenant removes saved scope", func(ctx context.Context) {
		mustLoginCLI(ctx, homeDir, adminUsername, adminsPassword)

		_, _, exitCode := tool.RunCLI(ctx, homeDir, "tenant", usersGroup)
		Expect(exitCode).To(Equal(0), "setting tenant should succeed")

		stdout, _, exitCode := tool.RunCLI(ctx, homeDir, "tenant", "--clear")
		Expect(exitCode).To(Equal(0), "clearing tenant should succeed")
		Expect(stdout).To(ContainSubstring("cleared"))

		stdout, _, exitCode = tool.RunCLI(ctx, homeDir, "tenant")
		Expect(exitCode).To(Equal(0))
		Expect(stdout).To(ContainSubstring("No tenant is currently set"))
		Expect(stdout).ToNot(ContainSubstring(usersGroup))
	})

	It("Tenant JSON output is valid", func(ctx context.Context) {
		mustLoginCLI(ctx, homeDir, adminUsername, adminsPassword)

		_, _, exitCode := tool.RunCLI(ctx, homeDir, "tenant", usersGroup)
		Expect(exitCode).To(Equal(0), "setting tenant should succeed")

		stdout, _, exitCode := tool.RunCLI(ctx, homeDir, "tenant", "-o", "json")
		Expect(exitCode).To(Equal(0), "tenant -o json should succeed")
		Expect(stdout).ToNot(BeEmpty())

		var parsed map[string]any
		err := json.Unmarshal([]byte(stdout), &parsed)
		Expect(err).ToNot(HaveOccurred(), "tenant JSON output should be valid")
		Expect(stdout).To(ContainSubstring(usersGroup), "JSON output should include the tenant name")
	})

	It("Tenant YAML output is valid", func(ctx context.Context) {
		mustLoginCLI(ctx, homeDir, adminUsername, adminsPassword)

		_, _, exitCode := tool.RunCLI(ctx, homeDir, "tenant", usersGroup)
		Expect(exitCode).To(Equal(0), "setting tenant should succeed")

		stdout, _, exitCode := tool.RunCLI(ctx, homeDir, "tenant", "-o", "yaml")
		Expect(exitCode).To(Equal(0), "tenant -o yaml should succeed")
		Expect(stdout).ToNot(BeEmpty())

		var parsed any
		err := yaml.Unmarshal([]byte(stdout), &parsed)
		Expect(err).ToNot(HaveOccurred(), "tenant YAML output should be valid")
		Expect(stdout).To(ContainSubstring(usersGroup), "YAML output should include the tenant name")
	})
})
