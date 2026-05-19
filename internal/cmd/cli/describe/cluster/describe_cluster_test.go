/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package cluster

import (
	"bytes"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
)

func formatCluster(cluster *publicv1.Cluster) string {
	var buf bytes.Buffer
	RenderCluster(&buf, cluster)
	return buf.String()
}

var _ = Describe("CEL filter construction", func() {
	It("should produce valid CEL for a plain name", func() {
		filter := buildFilter("my-cluster")
		Expect(filter).To(Equal(`this.id == "my-cluster" || this.metadata.name == "my-cluster"`))
	})
	It("should escape double quotes", func() {
		filter := buildFilter(`my"cluster`)
		Expect(filter).To(ContainSubstring(`"my\"cluster"`))
	})
	It("should escape backslashes", func() {
		filter := buildFilter(`my\cluster`)
		Expect(filter).To(ContainSubstring(`"my\\cluster"`))
	})
	It("should handle UUID-style IDs", func() {
		filter := buildFilter("550e8400-e29b-41d4-a716-446655440000")
		Expect(filter).To(Equal(`this.id == "550e8400-e29b-41d4-a716-446655440000" || this.metadata.name == "550e8400-e29b-41d4-a716-446655440000"`))
	})
	It("should pass through single quotes without escaping", func() {
		filter := buildFilter("my'cluster")
		Expect(filter).To(Equal(`this.id == "my'cluster" || this.metadata.name == "my'cluster"`))
	})
})

var _ = Describe("Rendering tests", func() {
	It("should display all fields when set", func() {
		cluster := publicv1.Cluster_builder{
			Id: "cluster-001",
			Spec: &publicv1.ClusterSpec{
				Template: "tpl-ha-001",
			},
			Status: &publicv1.ClusterStatus{
				State: publicv1.ClusterState_CLUSTER_STATE_READY,
			},
		}.Build()
		output := formatCluster(cluster)
		Expect(output).To(ContainSubstring("cluster-001"))
		Expect(output).To(ContainSubstring("tpl-ha-001"))
		Expect(output).To(ContainSubstring("READY"))
	})

	It("should show '-' for template when spec is nil", func() {
		cluster := publicv1.Cluster_builder{
			Id: "cluster-002",
		}.Build()
		output := formatCluster(cluster)
		Expect(output).To(MatchRegexp(`Template:\s+-`))
	})

	It("should show '-' for state when status is nil", func() {
		cluster := publicv1.Cluster_builder{
			Id: "cluster-003",
		}.Build()
		output := formatCluster(cluster)
		Expect(output).To(MatchRegexp(`State:\s+-`))
	})

	It("should strip CLUSTER_STATE_ prefix from state", func() {
		cluster := publicv1.Cluster_builder{
			Id: "cluster-004",
			Status: &publicv1.ClusterStatus{
				State: publicv1.ClusterState_CLUSTER_STATE_READY,
			},
		}.Build()
		output := formatCluster(cluster)
		Expect(output).To(ContainSubstring("READY"))
		Expect(output).NotTo(ContainSubstring("CLUSTER_STATE_"))
	})

	It("should strip CLUSTER_STATE_ prefix from PROGRESSING state", func() {
		cluster := publicv1.Cluster_builder{
			Id: "cluster-005",
			Status: &publicv1.ClusterStatus{
				State: publicv1.ClusterState_CLUSTER_STATE_PROGRESSING,
			},
		}.Build()
		output := formatCluster(cluster)
		Expect(output).To(ContainSubstring("PROGRESSING"))
		Expect(output).NotTo(ContainSubstring("CLUSTER_STATE_"))
	})
})

var _ = Describe("Multi-result guard", func() {
	It("should return nil when exactly one item found", func() {
		Expect(guardResult(1, "any-name")).To(BeNil())
	})
	It("should return not-found error when no items found", func() {
		err := guardResult(0, "missing-cluster")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("cluster not found"))
		Expect(err.Error()).To(ContainSubstring("missing-cluster"))
	})
	It("should return ambiguous error when multiple items found", func() {
		err := guardResult(2, "ambiguous-name")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("multiple clusters match"))
		Expect(err.Error()).To(ContainSubstring("ambiguous-name"))
		Expect(err.Error()).To(ContainSubstring("use the ID instead"))
	})
})
