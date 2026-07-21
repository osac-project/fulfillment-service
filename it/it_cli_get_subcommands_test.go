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
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/uuid"
)

var _ = Describe("CLI Get Subcommands", Label("cli", "get"), func() {
	var homeDir string

	BeforeEach(func() {
		homeDir = setupCLIHomeDir()
	})

	It("Get token returns a JWT", func(ctx context.Context) {
		mustLoginCLI(ctx, homeDir, adminUsername, adminsPassword)

		stdout, _, exitCode := tool.RunCLI(ctx, homeDir, "get", "token")
		Expect(exitCode).To(Equal(0), "get token should succeed")
		Expect(stdout).ToNot(BeEmpty(), "token should not be empty")

		// A JWT has three base64-encoded segments separated by dots.
		token := strings.TrimSpace(stdout)
		parts := strings.Split(token, ".")
		Expect(parts).To(HaveLen(3), "token should be a valid JWT with 3 parts")
	})

	It("Get token --header returns valid JSON", func(ctx context.Context) {
		mustLoginCLI(ctx, homeDir, adminUsername, adminsPassword)

		stdout, _, exitCode := tool.RunCLI(ctx, homeDir, "get", "token", "--header")
		Expect(exitCode).To(Equal(0), "get token --header should succeed")
		Expect(stdout).ToNot(BeEmpty())

		var parsed map[string]any
		err := json.Unmarshal([]byte(stdout), &parsed)
		Expect(err).ToNot(HaveOccurred(), "token header should be valid JSON")
		Expect(parsed).To(HaveKey("alg"), "JWT header should contain 'alg' field")
	})

	It("Get publicippool lists a created pool", func(ctx context.Context) {
		mustLoginCLI(ctx, homeDir, adminUsername, adminsPassword)

		poolName := fmt.Sprintf("cli-pool-%s", uuid.New())
		poolClient := privatev1.NewPublicIPPoolsClient(tool.InternalView().AdminConn())
		createResp, err := poolClient.Create(ctx, privatev1.PublicIPPoolsCreateRequest_builder{
			Object: privatev1.PublicIPPool_builder{
				Metadata: privatev1.Metadata_builder{
					Name: poolName,
				}.Build(),
				Spec: privatev1.PublicIPPoolSpec_builder{
					Cidrs:    []string{uniqueCIDR()},
					IpFamily: privatev1.IPFamily_IP_FAMILY_IPV4,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		pool := createResp.GetObject()
		pool.GetStatus().SetState(privatev1.PublicIPPoolState_PUBLIC_IP_POOL_STATE_READY)
		pool.GetStatus().SetAvailable(256)
		_, err = poolClient.Update(ctx, privatev1.PublicIPPoolsUpdateRequest_builder{
			Object: pool,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			_, _ = poolClient.Delete(context.Background(), privatev1.PublicIPPoolsDeleteRequest_builder{
				Id: pool.GetId(),
			}.Build())
		})

		stdout, _, exitCode := tool.RunCLI(ctx, homeDir, "get", "publicippool")
		Expect(exitCode).To(Equal(0), "get publicippool should succeed")
		Expect(stdout).To(ContainSubstring("ID"))
		Expect(stdout).To(ContainSubstring(poolName), "table output should include the created pool name")
	})
})
