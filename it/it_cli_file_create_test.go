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
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/uuid"
)

var _ = Describe("CLI File Create", Label("cli", "create"), func() {
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
				Title:                  "CLI File Create Test NC",
				ImplementationStrategy: "cudn",
				FabricManager:          "netris",
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		networkClassId = ncResp.GetObject().GetId()

		DeferCleanup(func() {
			ctx := context.Background()
			ncClient := privatev1.NewNetworkClassesClient(tool.InternalView().AdminConn())
			ncClient.Delete(ctx, privatev1.NetworkClassesDeleteRequest_builder{
				Id: networkClassId,
			}.Build())
			os.RemoveAll(homeDir)
		})
	})

	It("Create from JSON file", func(ctx context.Context) {
		_, _, exitCode := tool.LoginCLI(ctx, homeDir, adminUsername, adminsPassword)
		Expect(exitCode).To(Equal(0), "login should succeed")

		name := fmt.Sprintf("json-create-%s", uuid.New())
		jsonContent := fmt.Sprintf(`{
			"@type": "type.googleapis.com/osac.public.v1.VirtualNetwork",
			"metadata": {
				"name": "%s"
			},
			"spec": {
				"network_class": "%s",
				"ipv4_cidr": "10.130.0.0/16"
			}
		}`, name, networkClassId)

		filePath := filepath.Join(homeDir, "resource.json")
		err := os.WriteFile(filePath, []byte(jsonContent), 0644)
		Expect(err).ToNot(HaveOccurred())

		stdout, stderr, exitCode := tool.RunCLI(ctx, homeDir, "create", "-f", filePath)
		Expect(exitCode).To(Equal(0), "create -f JSON should succeed, stdout=%s, stderr=%s", stdout, stderr)
		Expect(stdout).To(ContainSubstring("Created"))

		DeferCleanup(func() {
			tool.RunCLI(ctx, homeDir, "delete", "virtualnetwork", name)
		})
	})

	It("Create from YAML file", func(ctx context.Context) {
		_, _, exitCode := tool.LoginCLI(ctx, homeDir, adminUsername, adminsPassword)
		Expect(exitCode).To(Equal(0), "login should succeed")

		name := fmt.Sprintf("yaml-create-%s", uuid.New())
		yamlContent := fmt.Sprintf(`"@type": "type.googleapis.com/osac.public.v1.VirtualNetwork"
metadata:
  name: "%s"
spec:
  network_class: "%s"
  ipv4_cidr: "10.131.0.0/16"
`, name, networkClassId)

		filePath := filepath.Join(homeDir, "resource.yaml")
		err := os.WriteFile(filePath, []byte(yamlContent), 0644)
		Expect(err).ToNot(HaveOccurred())

		stdout, stderr, exitCode := tool.RunCLI(ctx, homeDir, "create", "-f", filePath)
		Expect(exitCode).To(Equal(0), "create -f YAML should succeed, stdout=%s, stderr=%s", stdout, stderr)
		Expect(stdout).To(ContainSubstring("Created"))

		DeferCleanup(func() {
			tool.RunCLI(ctx, homeDir, "delete", "virtualnetwork", name)
		})
	})

	It("Create from invalid file fails gracefully", func(ctx context.Context) {
		_, _, exitCode := tool.LoginCLI(ctx, homeDir, adminUsername, adminsPassword)
		Expect(exitCode).To(Equal(0), "login should succeed")

		filePath := filepath.Join(homeDir, "bad.yaml")
		err := os.WriteFile(filePath, []byte("this is not valid proto"), 0644)
		Expect(err).ToNot(HaveOccurred())

		stdout, stderr, exitCode := tool.RunCLI(ctx, homeDir, "create", "-f", filePath)
		Expect(exitCode).ToNot(Equal(0), "create -f with invalid content should fail")
		combinedOutput := stdout + stderr
		Expect(combinedOutput).ToNot(ContainSubstring("runtime error"), "should not panic")
		Expect(combinedOutput).ToNot(ContainSubstring("goroutine"), "should not dump stack trace")
	})
})
