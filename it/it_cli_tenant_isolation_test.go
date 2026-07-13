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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/uuid"
)

var _ = Describe("CLI Tenant Isolation", Label("cli", "tenant", "isolation"), func() {
	var (
		homeDirA       string
		homeDirB       string
		networkClassId string
	)

	BeforeEach(func() {
		var err error
		homeDirA, err = tool.NewCLIHomeDir()
		Expect(err).ToNot(HaveOccurred())
		homeDirB, err = tool.NewCLIHomeDir()
		Expect(err).ToNot(HaveOccurred())

		ncClient := privatev1.NewNetworkClassesClient(tool.InternalView().AdminConn())
		ncResp, err := ncClient.Create(context.Background(), privatev1.NetworkClassesCreateRequest_builder{
			Object: privatev1.NetworkClass_builder{
				Title:                  "CLI Isolation Test NC",
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
			os.RemoveAll(homeDirA)
			os.RemoveAll(homeDirB)
		})
	})

	It("Tenant user creates and lists own resources", func(ctx context.Context) {
		// adam belongs to the "engineering" tenant
		_, _, exitCode := tool.LoginCLI(ctx, homeDirA, "adam", usersPassword)
		Expect(exitCode).To(Equal(0), "adam login should succeed")

		vnName := fmt.Sprintf("adam-vn-%s", uuid.New())
		stdout, stderr, exitCode := tool.RunCLI(ctx, homeDirA,
			"create", "virtualnetwork",
			"--name", vnName,
			"--network-class", networkClassId,
			"--ipv4-cidr", "10.120.0.0/16",
		)
		Expect(exitCode).To(Equal(0), "adam should create a VN, stdout=%s, stderr=%s", stdout, stderr)
		Expect(stdout).To(ContainSubstring("Created"))

		DeferCleanup(func() {
			tool.RunCLI(ctx, homeDirA, "delete", "virtualnetwork", vnName)
		})

		stdout, _, exitCode = tool.RunCLI(ctx, homeDirA, "get", "virtualnetwork")
		Expect(exitCode).To(Equal(0), "list should succeed")
		Expect(stdout).To(ContainSubstring(vnName))
	})

	It("Cross-tenant isolation prevents visibility", func(ctx context.Context) {
		// adam (engineering) creates a resource
		_, _, exitCode := tool.LoginCLI(ctx, homeDirA, "adam", usersPassword)
		Expect(exitCode).To(Equal(0), "adam login should succeed")

		vnName := fmt.Sprintf("isolated-%s", uuid.New())
		_, stderr, exitCode := tool.RunCLI(ctx, homeDirA,
			"create", "virtualnetwork",
			"--name", vnName,
			"--network-class", networkClassId,
			"--ipv4-cidr", "10.121.0.0/16",
		)
		Expect(exitCode).To(Equal(0), "adam create should succeed, stderr=%s", stderr)

		DeferCleanup(func() {
			tool.RunCLI(ctx, homeDirA, "delete", "virtualnetwork", vnName)
		})

		// ben (development) should NOT see adam's resource
		_, _, exitCode = tool.LoginCLI(ctx, homeDirB, "ben", usersPassword)
		Expect(exitCode).To(Equal(0), "ben login should succeed")

		stdout, _, exitCode := tool.RunCLI(ctx, homeDirB, "get", "virtualnetwork")
		Expect(exitCode).To(Equal(0), "ben list should succeed")
		visible := strings.Contains(stdout, vnName)
		Expect(visible).To(BeFalse(), "ben should not see adam's resource")
	})

	It("Tenant user deletes own resource", func(ctx context.Context) {
		_, _, exitCode := tool.LoginCLI(ctx, homeDirA, "adam", usersPassword)
		Expect(exitCode).To(Equal(0), "adam login should succeed")

		vnName := fmt.Sprintf("delete-me-%s", uuid.New())
		_, stderr, exitCode := tool.RunCLI(ctx, homeDirA,
			"create", "virtualnetwork",
			"--name", vnName,
			"--network-class", networkClassId,
			"--ipv4-cidr", "10.122.0.0/16",
		)
		Expect(exitCode).To(Equal(0), "create should succeed, stderr=%s", stderr)

		_, _, exitCode = tool.RunCLI(ctx, homeDirA, "delete", "virtualnetwork", vnName)
		Expect(exitCode).To(Equal(0), "adam should delete own resource")
	})
})
