/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package computeinstance

import (
	"bytes"
	"time"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/types/known/timestamppb"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
)

func formatComputeInstance(ci *publicv1.ComputeInstance) string {
	var buf bytes.Buffer
	renderComputeInstance(&buf, ci)
	return buf.String()
}

var _ = Describe("Describe Compute Instance", func() {
	It("should display all fields when set", func() {
		ci := &publicv1.ComputeInstance{
			Id: "ci-001",
			Spec: &publicv1.ComputeInstanceSpec{
				Template: "tpl-small-001",
			},
			Status: &publicv1.ComputeInstanceStatus{
				State: publicv1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING,
			},
		}
		output := formatComputeInstance(ci)
		Expect(output).To(ContainSubstring("ci-001"))
		Expect(output).To(MatchRegexp(`Catalog Item:\s+-`))
		Expect(output).NotTo(ContainSubstring("Template:"))
		Expect(output).To(ContainSubstring("RUNNING"))
	})

	It("should strip COMPUTE_INSTANCE_STATE_ prefix from state", func() {
		ci := &publicv1.ComputeInstance{
			Id: "ci-002",
			Status: &publicv1.ComputeInstanceStatus{
				State: publicv1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING,
			},
		}
		output := formatComputeInstance(ci)
		Expect(output).To(ContainSubstring("RUNNING"))
		Expect(output).NotTo(ContainSubstring("COMPUTE_INSTANCE_STATE_"))
	})

	It("should show '-' for catalog item when spec is nil", func() {
		ci := &publicv1.ComputeInstance{
			Id: "ci-003",
		}
		output := formatComputeInstance(ci)
		Expect(output).To(MatchRegexp(`Catalog Item:\s+-`))
	})

	It("should display last_restarted_at when set", func() {
		restartTime := time.Date(2026, 3, 15, 10, 30, 0, 0, time.UTC)
		ci := &publicv1.ComputeInstance{
			Id: "ci-test-001",
			Spec: &publicv1.ComputeInstanceSpec{
				Template: "tpl-small-001",
			},
			Status: &publicv1.ComputeInstanceStatus{
				State:           publicv1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING,
				LastRestartedAt: timestamppb.New(restartTime),
			},
		}

		output := formatComputeInstance(ci)
		Expect(output).NotTo(ContainSubstring("Template:"))
		Expect(output).To(ContainSubstring("Catalog Item:"))
		Expect(output).To(ContainSubstring("Last Restarted At:"))
		Expect(output).To(ContainSubstring("2026-03-15T10:30:00Z"))
	})

	It("should omit last_restarted_at when not set", func() {
		ci := &publicv1.ComputeInstance{
			Id: "ci-test-002",
			Spec: &publicv1.ComputeInstanceSpec{
				Template: "tpl-small-001",
			},
			Status: &publicv1.ComputeInstanceStatus{
				State: publicv1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING,
			},
		}

		output := formatComputeInstance(ci)
		Expect(output).NotTo(ContainSubstring("Template:"))
		Expect(output).To(ContainSubstring("Catalog Item:"))
		Expect(output).NotTo(ContainSubstring("Last Restarted At:"))
	})

	It("should omit last_restarted_at when status is nil", func() {
		ci := &publicv1.ComputeInstance{
			Id: "ci-test-003",
			Spec: &publicv1.ComputeInstanceSpec{
				Template: "tpl-small-001",
			},
		}

		output := formatComputeInstance(ci)
		Expect(output).NotTo(ContainSubstring("Template:"))
		Expect(output).To(ContainSubstring("Catalog Item:"))
		Expect(output).To(MatchRegexp(`State:\s+-`))
		Expect(output).NotTo(ContainSubstring("Last Restarted At:"))
	})

	It("should NOT show Template: row even when template is set on spec", func() {
		ci := &publicv1.ComputeInstance{
			Id: "ci-010",
			Spec: &publicv1.ComputeInstanceSpec{
				Template: "tpl-small-001",
			},
		}
		output := formatComputeInstance(ci)
		Expect(output).NotTo(ContainSubstring("Template:"))
		Expect(output).To(ContainSubstring("Catalog Item:"))
	})

	// TODO(OSAC-704): Add test for "should show catalog item ID when set"
	// once ComputeInstanceSpec.GetCatalogItem() exists from Phase 4.
})

var _ = Describe("CEL filter construction", func() {
	It("should produce valid CEL with == operator and quoted value for a plain name", func() {
		filter := buildFilter("my-instance")
		Expect(filter).To(Equal(`this.id == "my-instance" || this.metadata.name == "my-instance"`))
	})

	It("should escape double quotes in the reference value", func() {
		filter := buildFilter(`my"instance`)
		Expect(filter).To(ContainSubstring(`"my\"instance"`))
	})

	It("should escape backslashes in the reference value", func() {
		filter := buildFilter(`my\instance`)
		Expect(filter).To(ContainSubstring(`"my\\instance"`))
	})

	It("should produce valid CEL for a UUID-style ID", func() {
		filter := buildFilter("550e8400-e29b-41d4-a716-446655440000")
		Expect(filter).To(Equal(`this.id == "550e8400-e29b-41d4-a716-446655440000" || this.metadata.name == "550e8400-e29b-41d4-a716-446655440000"`))
	})
	It("should pass through single quotes without escaping", func() {
		filter := buildFilter("my'instance")
		Expect(filter).To(Equal(`this.id == "my'instance" || this.metadata.name == "my'instance"`))
	})
})

var _ = Describe("Multi-result guard", func() {
	It("should return nil when exactly one item found", func() {
		Expect(guardResult(1, "any-name")).To(BeNil())
	})
	It("should return not-found error when no items found", func() {
		err := guardResult(0, "missing-instance")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("compute instance not found"))
		Expect(err.Error()).To(ContainSubstring("missing-instance"))
	})
	It("should return ambiguous error when multiple items found", func() {
		err := guardResult(2, "ambiguous-name")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("multiple compute instances match"))
		Expect(err.Error()).To(ContainSubstring("ambiguous-name"))
		Expect(err.Error()).To(ContainSubstring("use the ID instead"))
	})
})
