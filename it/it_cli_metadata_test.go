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
)

var _ = Describe("CLI Metadata", Label("cli", "metadata"), func() {
	var (
		homeDir        string
		networkClassId string
	)

	BeforeEach(func() {
		homeDir = setupCLIHomeDir()
		networkClassId = setupTestNetworkClass("CLI Metadata Test NC")
	})

	It("Label a resource", func(ctx context.Context) {
		mustLoginCLI(ctx, homeDir, adminUsername, adminsPassword)
		vnName := createCLIVirtualNetwork(ctx, homeDir, networkClassId, "10.110.0.0/16")

		_, _, exitCode := tool.RunCLI(ctx, homeDir,
			"label", "virtualnetwork", vnName, "env=test",
		)
		Expect(exitCode).To(Equal(0), "label should succeed")

		stdout, _, exitCode := tool.RunCLI(ctx, homeDir,
			"get", "virtualnetwork", vnName, "-o", "json",
		)
		Expect(exitCode).To(Equal(0), "get should succeed")

		var parsed map[string]any
		err := json.Unmarshal([]byte(stdout), &parsed)
		Expect(err).ToNot(HaveOccurred())
		metadata, ok := parsed["metadata"].(map[string]any)
		Expect(ok).To(BeTrue(), "JSON should contain metadata object")
		labels, ok := metadata["labels"].(map[string]any)
		Expect(ok).To(BeTrue(), "metadata should contain labels")
		Expect(labels).To(HaveKeyWithValue("env", "test"))
	})

	It("Annotate a resource", func(ctx context.Context) {
		mustLoginCLI(ctx, homeDir, adminUsername, adminsPassword)
		vnName := createCLIVirtualNetwork(ctx, homeDir, networkClassId, "10.111.0.0/16")

		_, _, exitCode := tool.RunCLI(ctx, homeDir,
			"annotate", "virtualnetwork", vnName, "note=testing",
		)
		Expect(exitCode).To(Equal(0), "annotate should succeed")

		stdout, _, exitCode := tool.RunCLI(ctx, homeDir,
			"get", "virtualnetwork", vnName, "-o", "json",
		)
		Expect(exitCode).To(Equal(0), "get should succeed")

		var parsed map[string]any
		err := json.Unmarshal([]byte(stdout), &parsed)
		Expect(err).ToNot(HaveOccurred())
		metadata, ok := parsed["metadata"].(map[string]any)
		Expect(ok).To(BeTrue(), "JSON should contain metadata object")
		annotations, ok := metadata["annotations"].(map[string]any)
		Expect(ok).To(BeTrue(), "metadata should contain annotations")
		Expect(annotations).To(HaveKeyWithValue("note", "testing"))
	})

	It("Remove a label", func(ctx context.Context) {
		mustLoginCLI(ctx, homeDir, adminUsername, adminsPassword)
		vnName := createCLIVirtualNetwork(ctx, homeDir, networkClassId, "10.112.0.0/16")

		_, _, exitCode := tool.RunCLI(ctx, homeDir,
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

		var parsed map[string]any
		err := json.Unmarshal([]byte(stdout), &parsed)
		Expect(err).ToNot(HaveOccurred())
		metadata, ok := parsed["metadata"].(map[string]any)
		Expect(ok).To(BeTrue(), "JSON should contain metadata object")
		if labels, hasLabels := metadata["labels"].(map[string]any); hasLabels {
			Expect(labels).ToNot(HaveKey("env"))
		}
	})
})
