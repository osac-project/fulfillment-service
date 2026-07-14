/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package publicip

import (
	"bytes"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
)

func formatPublicIP(pip *publicv1.PublicIP) string {
	var buf bytes.Buffer
	RenderPublicIP(&buf, pip)
	return buf.String()
}

var _ = Describe("Describe PublicIP", func() {
	Describe("Rendering tests", func() {
		It("should display all fields when set", func() {
			msg := "IP allocated"
			pip := publicv1.PublicIP_builder{
				Id: "pip-001",
				Metadata: publicv1.Metadata_builder{
					Name: "my-ip",
				}.Build(),
				Spec: publicv1.PublicIPSpec_builder{
					Pool: "pool-abc123",
				}.Build(),
				Status: publicv1.PublicIPStatus_builder{
					State:    publicv1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED,
					Address:  "203.0.113.42",
					Message:  &msg,
					Attached: true,
				}.Build(),
			}.Build()

			output := formatPublicIP(pip)
			Expect(output).To(ContainSubstring("pip-001"))
			Expect(output).To(ContainSubstring("my-ip"))
			Expect(output).To(ContainSubstring("pool-abc123"))
			Expect(output).To(ContainSubstring("203.0.113.42"))
			Expect(output).To(ContainSubstring("ALLOCATED"))
			Expect(output).To(ContainSubstring("IP allocated"))
			Expect(output).To(MatchRegexp(`Attached:\s+true`))
		})

		It("should show '-' for optional fields when status is nil", func() {
			pip := publicv1.PublicIP_builder{
				Id: "pip-002",
				Spec: publicv1.PublicIPSpec_builder{
					Pool: "pool-abc123",
				}.Build(),
			}.Build()

			output := formatPublicIP(pip)
			Expect(output).To(MatchRegexp(`Name:\s+-`))
			Expect(output).To(MatchRegexp(`Attached:\s+false`))
			Expect(output).To(MatchRegexp(`Address:\s+-`))
			Expect(output).To(MatchRegexp(`State:\s+-`))
			Expect(output).To(MatchRegexp(`Message:\s+-`))
		})

		It("should show '-' for compute instance when not set", func() {
			pip := publicv1.PublicIP_builder{
				Id: "pip-003",
				Spec: publicv1.PublicIPSpec_builder{
					Pool: "pool-abc123",
				}.Build(),
				Status: publicv1.PublicIPStatus_builder{
					State:   publicv1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED,
					Address: "203.0.113.10",
				}.Build(),
			}.Build()

			output := formatPublicIP(pip)
			Expect(output).To(MatchRegexp(`Attached:\s+false`))
			Expect(output).To(ContainSubstring("203.0.113.10"))
		})

		It("should strip PUBLIC_IP_STATE_ prefix from state", func() {
			pip := publicv1.PublicIP_builder{
				Id: "pip-004",
				Spec: publicv1.PublicIPSpec_builder{
					Pool: "pool-abc123",
				}.Build(),
				Status: publicv1.PublicIPStatus_builder{
					State: publicv1.PublicIPState_PUBLIC_IP_STATE_PENDING,
				}.Build(),
			}.Build()

			output := formatPublicIP(pip)
			Expect(output).To(ContainSubstring("PENDING"))
			Expect(output).NotTo(ContainSubstring("PUBLIC_IP_STATE_"))
		})

		It("should show '-' for message when status has no message", func() {
			pip := publicv1.PublicIP_builder{
				Id: "pip-005",
				Spec: publicv1.PublicIPSpec_builder{
					Pool: "pool-abc123",
				}.Build(),
				Status: publicv1.PublicIPStatus_builder{
					State: publicv1.PublicIPState_PUBLIC_IP_STATE_ALLOCATED,
				}.Build(),
			}.Build()

			output := formatPublicIP(pip)
			Expect(output).To(MatchRegexp(`Message:\s+-`))
		})

		It("should show '-' for address when not yet allocated", func() {
			pip := publicv1.PublicIP_builder{
				Id: "pip-006",
				Spec: publicv1.PublicIPSpec_builder{
					Pool: "pool-abc123",
				}.Build(),
				Status: publicv1.PublicIPStatus_builder{
					State: publicv1.PublicIPState_PUBLIC_IP_STATE_PENDING,
				}.Build(),
			}.Build()

			output := formatPublicIP(pip)
			Expect(output).To(MatchRegexp(`Address:\s+-`))
		})
	})
})
