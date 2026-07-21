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

var _ = Describe("CLI Output Formats", Label("cli", "output"), func() {
	var (
		homeDir        string
		networkClassId string
	)

	BeforeEach(func() {
		homeDir = setupCLIHomeDir()
		networkClassId = setupTestNetworkClass("CLI Output Test NC")
	})

	It("Table output contains headers and resource data", func(ctx context.Context) {
		mustLoginCLI(ctx, homeDir, adminUsername, adminsPassword)
		vnName := createCLIVirtualNetwork(ctx, homeDir, networkClassId, "10.140.0.0/16")

		stdout, _, exitCode := tool.RunCLI(ctx, homeDir, "get", "virtualnetwork")
		Expect(exitCode).To(Equal(0), "get virtualnetwork should succeed")
		Expect(stdout).To(ContainSubstring("ID"))
		Expect(stdout).To(ContainSubstring(vnName), "table output should include the created resource name")
	})

	It("JSON output is valid and contains resource data", func(ctx context.Context) {
		mustLoginCLI(ctx, homeDir, adminUsername, adminsPassword)
		vnName := createCLIVirtualNetwork(ctx, homeDir, networkClassId, "10.141.0.0/16")

		stdout, _, exitCode := tool.RunCLI(ctx, homeDir, "get", "virtualnetwork", vnName, "-o", "json")
		Expect(exitCode).To(Equal(0), "get virtualnetwork -o json should succeed")
		Expect(stdout).ToNot(BeEmpty())

		var parsed map[string]any
		err := json.Unmarshal([]byte(stdout), &parsed)
		Expect(err).ToNot(HaveOccurred(), "JSON output should be valid")
		metadata, ok := parsed["metadata"].(map[string]any)
		Expect(ok).To(BeTrue(), "JSON should contain metadata object")
		Expect(metadata).To(HaveKeyWithValue("name", vnName))
	})

	It("YAML output is valid and contains resource data", func(ctx context.Context) {
		mustLoginCLI(ctx, homeDir, adminUsername, adminsPassword)
		vnName := createCLIVirtualNetwork(ctx, homeDir, networkClassId, "10.142.0.0/16")

		stdout, _, exitCode := tool.RunCLI(ctx, homeDir, "get", "virtualnetwork", vnName, "-o", "yaml")
		Expect(exitCode).To(Equal(0), "get virtualnetwork -o yaml should succeed")
		Expect(stdout).ToNot(BeEmpty())

		var parsed map[any]any
		err := yaml.Unmarshal([]byte(stdout), &parsed)
		Expect(err).ToNot(HaveOccurred(), "YAML output should be valid")
		metadata, ok := parsed["metadata"].(map[any]any)
		Expect(ok).To(BeTrue(), "YAML should contain metadata object")
		Expect(metadata["name"]).To(Equal(vnName))
	})
})
