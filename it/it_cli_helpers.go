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

// setupCLIHomeDir creates an isolated CLI HOME directory and registers cleanup.
func setupCLIHomeDir() string {
	homeDir, err := tool.NewCLIHomeDir()
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(func() {
		Expect(os.RemoveAll(homeDir)).To(Succeed())
	})
	return homeDir
}

// mustLoginCLI logs in with the given credentials and fails the test on non-zero exit.
func mustLoginCLI(ctx context.Context, homeDir, user, password string) {
	_, _, exitCode := tool.LoginCLI(ctx, homeDir, user, password)
	Expect(exitCode).To(Equal(0), "login as %s should succeed", user)
}

// setupTestNetworkClass creates a NetworkClass for CLI tests and registers cleanup.
func setupTestNetworkClass(title string) string {
	ncClient := privatev1.NewNetworkClassesClient(tool.InternalView().AdminConn())
	ncResp, err := ncClient.Create(context.Background(), privatev1.NetworkClassesCreateRequest_builder{
		Object: privatev1.NetworkClass_builder{
			Title:                  title,
			ImplementationStrategy: "cudn",
			FabricManager:          "netris",
		}.Build(),
	}.Build())
	Expect(err).ToNot(HaveOccurred())
	networkClassId := ncResp.GetObject().GetId()

	DeferCleanup(func(ctx context.Context) {
		ncClient := privatev1.NewNetworkClassesClient(tool.InternalView().AdminConn())
		// Best-effort cleanup; ignore not-found / already-deleted.
		_, _ = ncClient.Delete(ctx, privatev1.NetworkClassesDeleteRequest_builder{
			Id: networkClassId,
		}.Build())
	})
	return networkClassId
}

// createCLIVirtualNetwork creates a VirtualNetwork via the CLI, registers cleanup, and
// returns the resource name.
func createCLIVirtualNetwork(ctx context.Context, homeDir, networkClassId, cidr string) string {
	vnName := fmt.Sprintf("cli-vn-%s", uuid.New())
	stdout, stderr, exitCode := tool.RunCLI(ctx, homeDir,
		"create", "virtualnetwork",
		"--name", vnName,
		"--network-class", networkClassId,
		"--ipv4-cidr", cidr,
	)
	Expect(exitCode).To(Equal(0), "create virtualnetwork should succeed, stdout=%s, stderr=%s", stdout, stderr)
	Expect(stdout).To(ContainSubstring("Created"))
	DeferCleanup(func(ctx context.Context) {
		tool.RunCLI(ctx, homeDir, "delete", "virtualnetwork", vnName)
	})
	return vnName
}

// expectCLISoftDeletedVirtualNetwork asserts the resource is still listed after delete
// with DELETING=Yes (soft-delete until finalizers complete).
func expectCLISoftDeletedVirtualNetwork(ctx context.Context, homeDir, vnName string) {
	stdout, _, exitCode := tool.RunCLI(ctx, homeDir, "get", "virtualnetwork", vnName)
	Expect(exitCode).To(Equal(0), "get after delete should succeed")
	Expect(stdout).To(ContainSubstring(vnName))
	Expect(stdout).To(MatchRegexp(`(?m)^\S+\s+Yes\s+.*%s`, vnName),
		"DELETING column should be Yes after delete")
}
