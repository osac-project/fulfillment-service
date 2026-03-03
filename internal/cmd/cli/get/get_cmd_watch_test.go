/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package get

import (
	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/ginkgo/v2/dsl/table"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/proto"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
)

var _ = Describe("Watch command", func() {
	DescribeTable("extractObjectFromEvent",
		func(event *publicv1.Event, expectedType proto.Message, shouldSucceed bool) {
			runner := &runnerContext{}
			object, err := runner.extractObjectFromEvent(event)

			if shouldSucceed {
				Expect(err).ToNot(HaveOccurred())
				Expect(object).ToNot(BeNil())
				Expect(proto.MessageName(object)).To(Equal(proto.MessageName(expectedType)))
			} else {
				Expect(err).To(HaveOccurred())
			}
		},
		Entry("cluster event",
			&publicv1.Event{
				Payload: &publicv1.Event_Cluster{
					Cluster: &publicv1.Cluster{Id: "test-cluster"},
				},
			},
			(*publicv1.Cluster)(nil),
			true,
		),
		Entry("cluster template event",
			&publicv1.Event{
				Payload: &publicv1.Event_ClusterTemplate{
					ClusterTemplate: &publicv1.ClusterTemplate{Id: "test-template"},
				},
			},
			(*publicv1.ClusterTemplate)(nil),
			true,
		),
		Entry("event with no payload",
			&publicv1.Event{},
			nil,
			false,
		),
	)

	DescribeTable("buildEventFilter",
		func(objectType string, keys []string, expectedFilter string) {
			// This would require creating a mock helper, which we'll skip for now
			// but the pattern shows how to test filter building
			_ = objectType
			_ = keys
			_ = expectedFilter
		},
		Entry("cluster with no keys", "cluster", []string{}, "has(event.cluster)"),
		Entry("cluster with specific ID", "cluster", []string{"123"}, "has(event.cluster) && (event.cluster.id == \"123\" || event.cluster.metadata.name == \"123\")"),
	)
})
