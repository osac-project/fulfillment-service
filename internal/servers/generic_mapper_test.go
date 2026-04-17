/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package servers

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var _ = Describe("Generic mapper", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	parseDate := func(text string) *timestamppb.Timestamp {
		value, err := time.Parse(time.RFC3339, text)
		Expect(err).ToNot(HaveOccurred())
		return timestamppb.New(value)
	}

	DescribeTable(
		"Copy cluster private to public",
		func(from *privatev1.Cluster, to *publicv1.Cluster, expected *publicv1.Cluster) {
			mapper, err := NewGenericMapper[*privatev1.Cluster, *publicv1.Cluster]().
				SetLogger(logger).
				SetStrict(false).
				Build()
			Expect(err).ToNot(HaveOccurred())
			err = mapper.Copy(ctx, from, to)
			Expect(err).ToNot(HaveOccurred())
			marshalOptions := protojson.MarshalOptions{
				UseProtoNames: true,
			}
			actualJson, err := marshalOptions.Marshal(to)
			Expect(err).ToNot(HaveOccurred())
			expectedJson, err := marshalOptions.Marshal(expected)
			Expect(err).ToNot(HaveOccurred())
			Expect(actualJson).To(MatchJSON(expectedJson))
		},
		Entry(
			"Nil",
			nil,
			nil,
			nil,
		),
		Entry(
			"Empty",
			&privatev1.Cluster{},
			&publicv1.Cluster{},
			&publicv1.Cluster{},
		),
		Entry(
			"Identifier",
			privatev1.Cluster_builder{
				Id: "123",
			}.Build(),
			&publicv1.Cluster{},
			publicv1.Cluster_builder{
				Id: "123",
			}.Build(),
		),
		Entry(
			"Creation timestamp",
			privatev1.Cluster_builder{
				Metadata: privatev1.Metadata_builder{
					CreationTimestamp: parseDate("2025-06-02T14:53:00Z"),
				}.Build(),
			}.Build(),
			&publicv1.Cluster{},
			publicv1.Cluster_builder{
				Metadata: publicv1.Metadata_builder{
					CreationTimestamp: parseDate("2025-06-02T14:53:00Z"),
				}.Build(),
			}.Build(),
		),
		Entry(
			"Deletion timestamp",
			privatev1.Cluster_builder{
				Metadata: privatev1.Metadata_builder{
					DeletionTimestamp: parseDate("2025-06-02T14:53:00Z"),
				}.Build(),
			}.Build(),
			&publicv1.Cluster{},
			publicv1.Cluster_builder{
				Metadata: publicv1.Metadata_builder{
					DeletionTimestamp: parseDate("2025-06-02T14:53:00Z"),
				}.Build(),
			}.Build(),
		),
		Entry(
			"Empty spec",
			privatev1.Cluster_builder{
				Spec: privatev1.ClusterSpec_builder{}.Build(),
			}.Build(),
			&publicv1.Cluster{},
			publicv1.Cluster_builder{
				Spec: publicv1.ClusterSpec_builder{}.Build(),
			}.Build(),
		),
		Entry(
			"Spec with node sets",
			privatev1.Cluster_builder{
				Spec: privatev1.ClusterSpec_builder{
					NodeSets: map[string]*privatev1.ClusterNodeSet{
						"my_node_set": privatev1.ClusterNodeSet_builder{
							HostType: "my_host_type",
							Size:     123,
						}.Build(),
						"your_node_set": privatev1.ClusterNodeSet_builder{
							HostType: "your_host_type",
							Size:     456,
						}.Build(),
					},
				}.Build(),
			}.Build(),
			&publicv1.Cluster{},
			publicv1.Cluster_builder{
				Spec: publicv1.ClusterSpec_builder{
					NodeSets: map[string]*publicv1.ClusterNodeSet{
						"my_node_set": publicv1.ClusterNodeSet_builder{
							HostType: "my_host_type",
							Size:     123,
						}.Build(),
						"your_node_set": publicv1.ClusterNodeSet_builder{
							HostType: "your_host_type",
							Size:     456,
						}.Build(),
					},
				}.Build(),
			}.Build(),
		),
		Entry(
			"Empty status",
			privatev1.Cluster_builder{
				Status: privatev1.ClusterStatus_builder{}.Build(),
			}.Build(),
			&publicv1.Cluster{},
			publicv1.Cluster_builder{
				Status: publicv1.ClusterStatus_builder{}.Build(),
			}.Build(),
		),
		Entry(
			"Status with state",
			privatev1.Cluster_builder{
				Status: privatev1.ClusterStatus_builder{
					State: privatev1.ClusterState_CLUSTER_STATE_READY,
				}.Build(),
			}.Build(),
			&publicv1.Cluster{},
			publicv1.Cluster_builder{
				Status: publicv1.ClusterStatus_builder{
					State: publicv1.ClusterState_CLUSTER_STATE_READY,
				}.Build(),
			}.Build(),
		),
		Entry(
			"Status with one conditions",
			privatev1.Cluster_builder{
				Status: privatev1.ClusterStatus_builder{
					Conditions: []*privatev1.ClusterCondition{
						privatev1.ClusterCondition_builder{
							Type:               privatev1.ClusterConditionType_CLUSTER_CONDITION_TYPE_READY,
							Status:             privatev1.ConditionStatus_CONDITION_STATUS_TRUE,
							LastTransitionTime: parseDate("2025-06-02T14:53:00Z"),
							Reason:             proto.String("MyReason"),
							Message:            proto.String("My message."),
						}.Build(),
					},
				}.Build(),
			}.Build(),
			&publicv1.Cluster{},
			publicv1.Cluster_builder{
				Status: publicv1.ClusterStatus_builder{
					Conditions: []*publicv1.ClusterCondition{
						publicv1.ClusterCondition_builder{
							Type:               publicv1.ClusterConditionType_CLUSTER_CONDITION_TYPE_READY,
							Status:             publicv1.ConditionStatus_CONDITION_STATUS_TRUE,
							LastTransitionTime: parseDate("2025-06-02T14:53:00Z"),
							Reason:             proto.String("MyReason"),
							Message:            proto.String("My message."),
						}.Build(),
					},
				}.Build(),
			}.Build(),
		),
		Entry(
			"Status with two conditions",
			privatev1.Cluster_builder{
				Status: privatev1.ClusterStatus_builder{
					Conditions: []*privatev1.ClusterCondition{
						privatev1.ClusterCondition_builder{
							Type:               privatev1.ClusterConditionType_CLUSTER_CONDITION_TYPE_READY,
							Status:             privatev1.ConditionStatus_CONDITION_STATUS_TRUE,
							LastTransitionTime: parseDate("2025-06-02T14:53:00Z"),
							Reason:             proto.String("MyReason"),
							Message:            proto.String("My message."),
						}.Build(),
						privatev1.ClusterCondition_builder{
							Type:               privatev1.ClusterConditionType_CLUSTER_CONDITION_TYPE_FAILED,
							Status:             privatev1.ConditionStatus_CONDITION_STATUS_FALSE,
							LastTransitionTime: parseDate("2025-06-03T14:53:00Z"),
							Reason:             proto.String("YourReason"),
							Message:            proto.String("Your message."),
						}.Build(),
					},
				}.Build(),
			}.Build(),
			&publicv1.Cluster{},
			publicv1.Cluster_builder{
				Status: publicv1.ClusterStatus_builder{
					Conditions: []*publicv1.ClusterCondition{
						publicv1.ClusterCondition_builder{
							Type:               publicv1.ClusterConditionType_CLUSTER_CONDITION_TYPE_READY,
							Status:             publicv1.ConditionStatus_CONDITION_STATUS_TRUE,
							LastTransitionTime: parseDate("2025-06-02T14:53:00Z"),
							Reason:             proto.String("MyReason"),
							Message:            proto.String("My message."),
						}.Build(),
						publicv1.ClusterCondition_builder{
							Type:               publicv1.ClusterConditionType_CLUSTER_CONDITION_TYPE_FAILED,
							Status:             publicv1.ConditionStatus_CONDITION_STATUS_FALSE,
							LastTransitionTime: parseDate("2025-06-03T14:53:00Z"),
							Reason:             proto.String("YourReason"),
							Message:            proto.String("Your message."),
						}.Build(),
					},
				}.Build(),
			}.Build(),
		),
	)

	DescribeTable(
		"CopyUpdate compute instance public to private",
		func(from *publicv1.ComputeInstance, to *privatev1.ComputeInstance, expected *privatev1.ComputeInstance) {
			mapper, err := NewGenericMapper[*publicv1.ComputeInstance, *privatev1.ComputeInstance]().
				SetLogger(logger).
				SetStrict(false).
				Build()
			Expect(err).ToNot(HaveOccurred())
			err = mapper.CopyUpdate(ctx, from, to)
			Expect(err).ToNot(HaveOccurred())
			marshalOptions := protojson.MarshalOptions{
				UseProtoNames: true,
			}
			actualJson, err := marshalOptions.Marshal(to)
			Expect(err).ToNot(HaveOccurred())
			expectedJson, err := marshalOptions.Marshal(expected)
			Expect(err).ToNot(HaveOccurred())
			Expect(actualJson).To(MatchJSON(expectedJson))
		},
		Entry(
			"Preserves absent optional scalar fields",
			// From: only template set, cores/memory_gib absent
			publicv1.ComputeInstance_builder{
				Spec: publicv1.ComputeInstanceSpec_builder{
					Template: "general.large",
				}.Build(),
			}.Build(),
			// To: existing object with optional fields populated
			privatev1.ComputeInstance_builder{
				Id: "existing-id",
				Spec: privatev1.ComputeInstanceSpec_builder{
					Template:    "general.small",
					Cores:       proto.Int32(4),
					MemoryGib:   proto.Int32(8),
					RunStrategy: proto.String("Always"),
				}.Build(),
			}.Build(),
			// Expected: optional fields preserved
			privatev1.ComputeInstance_builder{
				Id: "existing-id",
				Spec: privatev1.ComputeInstanceSpec_builder{
					Template:    "general.large",
					Cores:       proto.Int32(4),
					MemoryGib:   proto.Int32(8),
					RunStrategy: proto.String("Always"),
				}.Build(),
			}.Build(),
		),
		Entry(
			"Replaces present repeated fields instead of appending",
			// From: new security_groups list
			publicv1.ComputeInstance_builder{
				Spec: publicv1.ComputeInstanceSpec_builder{
					Template:       "general.small",
					SecurityGroups: []string{"sg-new"},
				}.Build(),
			}.Build(),
			// To: existing object with different security_groups
			privatev1.ComputeInstance_builder{
				Id: "existing-id",
				Spec: privatev1.ComputeInstanceSpec_builder{
					Template:       "general.small",
					SecurityGroups: []string{"sg-old-1", "sg-old-2"},
				}.Build(),
			}.Build(),
			// Expected: security_groups replaced (not appended)
			privatev1.ComputeInstance_builder{
				Id: "existing-id",
				Spec: privatev1.ComputeInstanceSpec_builder{
					Template:       "general.small",
					SecurityGroups: []string{"sg-new"},
				}.Build(),
			}.Build(),
		),
		Entry(
			"Preserves absent repeated fields",
			// From: no security_groups field
			publicv1.ComputeInstance_builder{
				Spec: publicv1.ComputeInstanceSpec_builder{
					Template: "general.large",
				}.Build(),
			}.Build(),
			// To: existing object with security_groups
			privatev1.ComputeInstance_builder{
				Id: "existing-id",
				Spec: privatev1.ComputeInstanceSpec_builder{
					Template:       "general.small",
					SecurityGroups: []string{"sg-existing"},
				}.Build(),
			}.Build(),
			// Expected: security_groups preserved
			privatev1.ComputeInstance_builder{
				Id: "existing-id",
				Spec: privatev1.ComputeInstanceSpec_builder{
					Template:       "general.large",
					SecurityGroups: []string{"sg-existing"},
				}.Build(),
			}.Build(),
		),
		Entry(
			"Overwrites present optional fields",
			// From: cores set to new value
			publicv1.ComputeInstance_builder{
				Spec: publicv1.ComputeInstanceSpec_builder{
					Template: "general.large",
					Cores:    proto.Int32(16),
				}.Build(),
			}.Build(),
			// To: existing object with different cores
			privatev1.ComputeInstance_builder{
				Id: "existing-id",
				Spec: privatev1.ComputeInstanceSpec_builder{
					Template:    "general.small",
					Cores:       proto.Int32(4),
					MemoryGib:   proto.Int32(8),
					RunStrategy: proto.String("Always"),
				}.Build(),
			}.Build(),
			// Expected: cores updated, other optionals preserved
			privatev1.ComputeInstance_builder{
				Id: "existing-id",
				Spec: privatev1.ComputeInstanceSpec_builder{
					Template:    "general.large",
					Cores:       proto.Int32(16),
					MemoryGib:   proto.Int32(8),
					RunStrategy: proto.String("Always"),
				}.Build(),
			}.Build(),
		),
		Entry(
			"Mixed: updates scalar, preserves absent optional, replaces list",
			// From: new template, new security_groups, cores absent
			publicv1.ComputeInstance_builder{
				Spec: publicv1.ComputeInstanceSpec_builder{
					Template:       "general.large",
					SecurityGroups: []string{"sg-new-1", "sg-new-2"},
				}.Build(),
			}.Build(),
			// To: existing object with cores and old security_groups
			privatev1.ComputeInstance_builder{
				Id: "existing-id",
				Spec: privatev1.ComputeInstanceSpec_builder{
					Template:       "general.small",
					Cores:          proto.Int32(4),
					SecurityGroups: []string{"sg-old"},
				}.Build(),
			}.Build(),
			// Expected: template updated, cores preserved, security_groups replaced
			privatev1.ComputeInstance_builder{
				Id: "existing-id",
				Spec: privatev1.ComputeInstanceSpec_builder{
					Template:       "general.large",
					Cores:          proto.Int32(4),
					SecurityGroups: []string{"sg-new-1", "sg-new-2"},
				}.Build(),
			}.Build(),
		),
	)

	DescribeTable(
		"Merge cluster private to public",
		func(from *privatev1.Cluster, to *publicv1.Cluster, expected *publicv1.Cluster) {
			mapper, err := NewGenericMapper[*privatev1.Cluster, *publicv1.Cluster]().
				SetLogger(logger).
				SetStrict(false).
				Build()
			Expect(err).ToNot(HaveOccurred())
			err = mapper.Merge(ctx, from, to)
			Expect(err).ToNot(HaveOccurred())
			marshalOptions := protojson.MarshalOptions{
				UseProtoNames: true,
			}
			actualJson, err := marshalOptions.Marshal(to)
			Expect(err).ToNot(HaveOccurred())
			expectedJson, err := marshalOptions.Marshal(expected)
			Expect(err).ToNot(HaveOccurred())
			Expect(actualJson).To(MatchJSON(expectedJson))
		},
		Entry(
			"Replace scalar field",
			privatev1.Cluster_builder{
				Id: "new-id",
			}.Build(),
			publicv1.Cluster_builder{
				Id: "old-id",
			}.Build(),
			publicv1.Cluster_builder{
				Id: "new-id",
			}.Build(),
		),
		Entry(
			"Merge into empty target",
			privatev1.Cluster_builder{
				Id: "123",
				Metadata: privatev1.Metadata_builder{
					CreationTimestamp: parseDate("2025-06-02T14:53:00Z"),
				}.Build(),
			}.Build(),
			&publicv1.Cluster{},
			publicv1.Cluster_builder{
				Id: "123",
				Metadata: publicv1.Metadata_builder{
					CreationTimestamp: parseDate("2025-06-02T14:53:00Z"),
				}.Build(),
			}.Build(),
		),
		Entry(
			"Combine fields of nested messages",
			privatev1.Cluster_builder{
				Metadata: privatev1.Metadata_builder{
					CreationTimestamp: parseDate("2025-06-02T14:53:00Z"),
				}.Build(),
			}.Build(),
			publicv1.Cluster_builder{
				Metadata: publicv1.Metadata_builder{
					DeletionTimestamp: parseDate("2025-06-02T15:00:00Z"),
				}.Build(),
			}.Build(),
			publicv1.Cluster_builder{
				Metadata: publicv1.Metadata_builder{
					CreationTimestamp: parseDate("2025-06-02T14:53:00Z"),
					DeletionTimestamp: parseDate("2025-06-02T15:00:00Z"),
				}.Build(),
			}.Build(),
		),
		Entry(
			"Merge entries of maps",
			privatev1.Cluster_builder{
				Spec: privatev1.ClusterSpec_builder{
					NodeSets: map[string]*privatev1.ClusterNodeSet{
						"new_node_set": privatev1.ClusterNodeSet_builder{
							HostType: "new_host_type",
							Size:     789,
						}.Build(),
					},
				}.Build(),
			}.Build(),
			publicv1.Cluster_builder{
				Spec: publicv1.ClusterSpec_builder{
					NodeSets: map[string]*publicv1.ClusterNodeSet{
						"existing_node_set": publicv1.ClusterNodeSet_builder{
							HostType: "existing_host_type",
							Size:     456,
						}.Build(),
					},
				}.Build(),
			}.Build(),
			publicv1.Cluster_builder{
				Spec: publicv1.ClusterSpec_builder{
					NodeSets: map[string]*publicv1.ClusterNodeSet{
						"existing_node_set": publicv1.ClusterNodeSet_builder{
							HostType: "existing_host_type",
							Size:     456,
						}.Build(),
						"new_node_set": publicv1.ClusterNodeSet_builder{
							HostType: "new_host_type",
							Size:     789,
						}.Build(),
					},
				}.Build(),
			}.Build(),
		),
		Entry(
			"Replace map entry",
			privatev1.Cluster_builder{
				Spec: privatev1.ClusterSpec_builder{
					NodeSets: map[string]*privatev1.ClusterNodeSet{
						"node_set": privatev1.ClusterNodeSet_builder{
							HostType: "updated_host_type",
							Size:     999,
						}.Build(),
					},
				}.Build(),
			}.Build(),
			publicv1.Cluster_builder{
				Spec: publicv1.ClusterSpec_builder{
					NodeSets: map[string]*publicv1.ClusterNodeSet{
						"node_set": publicv1.ClusterNodeSet_builder{
							HostType: "original_host_type",
							Size:     123,
						}.Build(),
					},
				}.Build(),
			}.Build(),
			publicv1.Cluster_builder{
				Spec: publicv1.ClusterSpec_builder{
					NodeSets: map[string]*publicv1.ClusterNodeSet{
						"node_set": publicv1.ClusterNodeSet_builder{
							HostType: "updated_host_type",
							Size:     999,
						}.Build(),
					},
				}.Build(),
			}.Build(),
		),
	)
})
