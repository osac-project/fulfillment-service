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
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.yaml.in/yaml/v2"
)

var _ = Describe("CLI Output Formats", Label("cli", "output"), func() {
	var homeDir string

	BeforeEach(func() {
		var err error
		homeDir, err = tool.NewCLIHomeDir()
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			err := os.RemoveAll(homeDir)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	It("Table output contains expected headers", func(ctx context.Context) {
		_, _, exitCode := tool.LoginCLI(ctx, homeDir, adminUsername, adminsPassword)
		Expect(exitCode).To(Equal(0), "login should succeed")

		stdout, _, exitCode := tool.RunCLI(ctx, homeDir, "get", "computeinstance")
		Expect(exitCode).To(Equal(0), "get computeinstance should succeed")

		Expect(stdout).To(SatisfyAny(
			ContainSubstring("ID"),
			ContainSubstring("No matching"),
			ContainSubstring("no objects matching"),
		))
	})

	It("JSON output is valid", func(ctx context.Context) {
		_, _, exitCode := tool.LoginCLI(ctx, homeDir, adminUsername, adminsPassword)
		Expect(exitCode).To(Equal(0), "login should succeed")

		stdout, _, exitCode := tool.RunCLI(ctx, homeDir, "get", "computeinstance", "-o", "json")
		Expect(exitCode).To(Equal(0), "get computeinstance -o json should succeed")

		if stdout != "" {
			var parsed any
			err := json.Unmarshal([]byte(stdout), &parsed)
			Expect(err).ToNot(HaveOccurred(), "JSON output should be valid")
		}
	})

	It("YAML output is valid", func(ctx context.Context) {
		_, _, exitCode := tool.LoginCLI(ctx, homeDir, adminUsername, adminsPassword)
		Expect(exitCode).To(Equal(0), "login should succeed")

		stdout, _, exitCode := tool.RunCLI(ctx, homeDir, "get", "computeinstance", "-o", "yaml")
		Expect(exitCode).To(Equal(0), "get computeinstance -o yaml should succeed")

		if stdout != "" {
			var parsed any
			err := yaml.Unmarshal([]byte(stdout), &parsed)
			Expect(err).ToNot(HaveOccurred(), "YAML output should be valid")
		}
	})
})
