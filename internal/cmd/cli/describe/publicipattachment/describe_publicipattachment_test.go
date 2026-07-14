/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package publicipattachment

import (
	"bytes"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/proto"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
)

func formatAttachment(a *publicv1.PublicIPAttachment) string {
	var buf bytes.Buffer
	RenderPublicIPAttachment(&buf, a)
	return buf.String()
}

var _ = Describe("Describe PublicIPAttachment", func() {
	Describe("Rendering tests", func() {
		It("should display all fields when set", func() {
			msg := "Binding address to target"
			a := publicv1.PublicIPAttachment_builder{
				Id: "att-001",
				Metadata: publicv1.Metadata_builder{
					Name: "my-attachment",
				}.Build(),
				Spec: publicv1.PublicIPAttachmentSpec_builder{
					PublicIp:        "pip-abc123",
					ComputeInstance: proto.String("ci-def456"),
				}.Build(),
				Status: publicv1.PublicIPAttachmentStatus_builder{
					State:           publicv1.PublicIPAttachmentState_PUBLIC_IP_ATTACHMENT_STATE_PENDING,
					PublicIpAddress: "203.0.113.42",
					Message:         &msg,
				}.Build(),
			}.Build()

			output := formatAttachment(a)
			Expect(output).To(ContainSubstring("att-001"))
			Expect(output).To(ContainSubstring("my-attachment"))
			Expect(output).To(ContainSubstring("pip-abc123"))
			Expect(output).To(ContainSubstring("ci-def456"))
			Expect(output).To(ContainSubstring("203.0.113.42"))
			Expect(output).To(ContainSubstring("PENDING"))
			Expect(output).To(ContainSubstring("Binding address to target"))
		})

		It("should show '-' for optional fields when status is nil", func() {
			a := publicv1.PublicIPAttachment_builder{
				Id: "att-002",
				Spec: publicv1.PublicIPAttachmentSpec_builder{
					PublicIp:        "pip-abc123",
					ComputeInstance: proto.String("ci-def456"),
				}.Build(),
			}.Build()

			output := formatAttachment(a)
			Expect(output).To(MatchRegexp(`Name:\s+-`))
			Expect(output).To(MatchRegexp(`Public IP Address:\s+-`))
			Expect(output).To(MatchRegexp(`State:\s+-`))
			Expect(output).To(MatchRegexp(`Message:\s+-`))
		})

		It("should show '-' for compute instance when not set", func() {
			a := publicv1.PublicIPAttachment_builder{
				Id: "att-003",
				Spec: publicv1.PublicIPAttachmentSpec_builder{
					PublicIp: "pip-abc123",
				}.Build(),
			}.Build()

			output := formatAttachment(a)
			Expect(output).To(MatchRegexp(`Compute Instance:\s+-`))
		})

		It("should strip PUBLIC_IP_ATTACHMENT_STATE_ prefix from state", func() {
			a := publicv1.PublicIPAttachment_builder{
				Id: "att-004",
				Spec: publicv1.PublicIPAttachmentSpec_builder{
					PublicIp:        "pip-abc123",
					ComputeInstance: proto.String("ci-def456"),
				}.Build(),
				Status: publicv1.PublicIPAttachmentStatus_builder{
					State: publicv1.PublicIPAttachmentState_PUBLIC_IP_ATTACHMENT_STATE_PENDING,
				}.Build(),
			}.Build()

			output := formatAttachment(a)
			Expect(output).To(ContainSubstring("PENDING"))
			Expect(output).NotTo(ContainSubstring("PUBLIC_IP_ATTACHMENT_STATE_"))
		})

		It("should show '-' for message when status has no message", func() {
			a := publicv1.PublicIPAttachment_builder{
				Id: "att-005",
				Spec: publicv1.PublicIPAttachmentSpec_builder{
					PublicIp:        "pip-abc123",
					ComputeInstance: proto.String("ci-def456"),
				}.Build(),
				Status: publicv1.PublicIPAttachmentStatus_builder{
					State: publicv1.PublicIPAttachmentState_PUBLIC_IP_ATTACHMENT_STATE_READY,
				}.Build(),
			}.Build()

			output := formatAttachment(a)
			Expect(output).To(MatchRegexp(`Message:\s+-`))
		})

		It("should display FAILED state with error message", func() {
			msg := "target compute instance not found"
			a := publicv1.PublicIPAttachment_builder{
				Id: "att-007",
				Spec: publicv1.PublicIPAttachmentSpec_builder{
					PublicIp:        "pip-abc123",
					ComputeInstance: proto.String("ci-def456"),
				}.Build(),
				Status: publicv1.PublicIPAttachmentStatus_builder{
					State:   publicv1.PublicIPAttachmentState_PUBLIC_IP_ATTACHMENT_STATE_FAILED,
					Message: &msg,
				}.Build(),
			}.Build()

			output := formatAttachment(a)
			Expect(output).To(ContainSubstring("FAILED"))
			Expect(output).NotTo(ContainSubstring("PUBLIC_IP_ATTACHMENT_STATE_"))
			Expect(output).To(ContainSubstring("target compute instance not found"))
			Expect(output).To(MatchRegexp(`Public IP Address:\s+-`))
		})

		It("should show '-' for public IP address when not yet ready", func() {
			a := publicv1.PublicIPAttachment_builder{
				Id: "att-006",
				Spec: publicv1.PublicIPAttachmentSpec_builder{
					PublicIp:        "pip-abc123",
					ComputeInstance: proto.String("ci-def456"),
				}.Build(),
				Status: publicv1.PublicIPAttachmentStatus_builder{
					State: publicv1.PublicIPAttachmentState_PUBLIC_IP_ATTACHMENT_STATE_PENDING,
				}.Build(),
			}.Build()

			output := formatAttachment(a)
			Expect(output).To(MatchRegexp(`Public IP Address:\s+-`))
		})
	})
})
