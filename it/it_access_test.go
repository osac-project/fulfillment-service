/*
Copyright (c) 2025 Red Hat Inc.

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

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	ffv1 "github.com/osac-project/fulfillment-service/internal/api/fulfillment/v1"
	privatev1 "github.com/osac-project/fulfillment-service/internal/api/private/v1"
)

var _ = Describe("Access control", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("Public API", func() {
		It("Allows regular users to list cluster templates", func() {
			client := ffv1.NewClusterTemplatesClient(tool.UserConn())
			_, err := client.List(ctx, ffv1.ClusterTemplatesListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Allows regular users to list clusters", func() {
			client := ffv1.NewClustersClient(tool.UserConn())
			_, err := client.List(ctx, ffv1.ClustersListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Allows regular users to list host classes", func() {
			client := ffv1.NewHostClassesClient(tool.UserConn())
			_, err := client.List(ctx, ffv1.HostClassesListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Allows regular users to list hosts", func() {
			client := ffv1.NewHostsClient(tool.UserConn())
			_, err := client.List(ctx, ffv1.HostsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Allows regular users to list host pools", func() {
			client := ffv1.NewHostPoolsClient(tool.UserConn())
			_, err := client.List(ctx, ffv1.HostPoolsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Allows regular users to list compute instance templates", func() {
			client := ffv1.NewComputeInstanceTemplatesClient(tool.UserConn())
			_, err := client.List(ctx, ffv1.ComputeInstanceTemplatesListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Allows regular users to list compute instances", func() {
			client := ffv1.NewComputeInstancesClient(tool.UserConn())
			_, err := client.List(ctx, ffv1.ComputeInstancesListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Allows admin users to list cluster templates", func() {
			client := ffv1.NewClusterTemplatesClient(tool.AdminConn())
			_, err := client.List(ctx, ffv1.ClusterTemplatesListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Allows admin users to list clusters", func() {
			client := ffv1.NewClustersClient(tool.AdminConn())
			_, err := client.List(ctx, ffv1.ClustersListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Allows admin users to list host classes", func() {
			client := ffv1.NewHostClassesClient(tool.AdminConn())
			_, err := client.List(ctx, ffv1.HostClassesListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Allows admin users to list hosts", func() {
			client := ffv1.NewHostsClient(tool.AdminConn())
			_, err := client.List(ctx, ffv1.HostsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Allows admin users to list host pools", func() {
			client := ffv1.NewHostPoolsClient(tool.AdminConn())
			_, err := client.List(ctx, ffv1.HostPoolsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Allows admin users to list compute instance templates", func() {
			client := ffv1.NewComputeInstanceTemplatesClient(tool.AdminConn())
			_, err := client.List(ctx, ffv1.ComputeInstanceTemplatesListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Allows admin users to list compute instances", func() {
			client := ffv1.NewComputeInstancesClient(tool.AdminConn())
			_, err := client.List(ctx, ffv1.ComputeInstancesListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("Private API", func() {
		It("Allows admin users to list host classes", func() {
			client := privatev1.NewHostClassesClient(tool.AdminConn())
			_, err := client.List(ctx, privatev1.HostClassesListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Allows admin users to list cluster templates", func() {
			client := privatev1.NewClusterTemplatesClient(tool.AdminConn())
			_, err := client.List(ctx, privatev1.ClusterTemplatesListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Allows admin users to list hubs", func() {
			client := privatev1.NewHubsClient(tool.AdminConn())
			_, err := client.List(ctx, privatev1.HubsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Allows admin users to list hosts", func() {
			client := privatev1.NewHostsClient(tool.AdminConn())
			_, err := client.List(ctx, privatev1.HostsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Allows admin users to list host pools", func() {
			client := privatev1.NewHostPoolsClient(tool.AdminConn())
			_, err := client.List(ctx, privatev1.HostPoolsListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Allows admin users to list private clusters", func() {
			client := privatev1.NewClustersClient(tool.AdminConn())
			_, err := client.List(ctx, privatev1.ClustersListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Allows admin users to list compute instance templates", func() {
			client := privatev1.NewComputeInstanceTemplatesClient(tool.AdminConn())
			_, err := client.List(ctx, privatev1.ComputeInstanceTemplatesListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Allows admin users to list private compute instances", func() {
			client := privatev1.NewComputeInstancesClient(tool.AdminConn())
			_, err := client.List(ctx, privatev1.ComputeInstancesListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
		})

		It("Denies regular users access to host classes", func() {
			client := privatev1.NewHostClassesClient(tool.UserConn())
			_, err := client.List(ctx, privatev1.HostClassesListRequest_builder{}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
		})

		It("Denies regular users access to cluster templates", func() {
			client := privatev1.NewClusterTemplatesClient(tool.UserConn())
			_, err := client.List(ctx, privatev1.ClusterTemplatesListRequest_builder{}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
		})

		It("Denies regular users access to hubs", func() {
			client := privatev1.NewHubsClient(tool.UserConn())
			_, err := client.List(ctx, privatev1.HubsListRequest_builder{}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
		})

		It("Denies regular users access to hosts", func() {
			client := privatev1.NewHostsClient(tool.UserConn())
			_, err := client.List(ctx, privatev1.HostsListRequest_builder{}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
		})

		It("Denies regular users access to host pools", func() {
			client := privatev1.NewHostPoolsClient(tool.UserConn())
			_, err := client.List(ctx, privatev1.HostPoolsListRequest_builder{}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
		})

		It("Denies regular users access to private clusters", func() {
			client := privatev1.NewClustersClient(tool.UserConn())
			_, err := client.List(ctx, privatev1.ClustersListRequest_builder{}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
		})

		It("Denies regular users access to compute instance templates", func() {
			client := privatev1.NewComputeInstanceTemplatesClient(tool.UserConn())
			_, err := client.List(ctx, privatev1.ComputeInstanceTemplatesListRequest_builder{}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
		})

		It("Denies regular users access to private compute instances", func() {
			client := privatev1.NewComputeInstancesClient(tool.UserConn())
			_, err := client.List(ctx, privatev1.ComputeInstancesListRequest_builder{}.Build())
			Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
		})
	})
})
