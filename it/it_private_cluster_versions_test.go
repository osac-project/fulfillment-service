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
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
)

var cvVersionCounter atomic.Int64

func nextCVVersion() string {
	return fmt.Sprintf("100.0.%d", cvVersionCounter.Add(1))
}

var _ = Describe("Private cluster versions", func() {
	var (
		ctx    context.Context
		client privatev1.ClusterVersionsClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		client = privatev1.NewClusterVersionsClient(tool.InternalView().AdminConn())
	})

	createCV := func(version string, versionNames ...string) *privatev1.ClusterVersion {
		var allowedUpgrades *privatev1.ClusterVersionAllowedUpgrades
		if len(versionNames) > 0 {
			allowedUpgrades = privatev1.ClusterVersionAllowedUpgrades_builder{
				VersionNames: versionNames,
			}.Build()
		}
		response, err := client.Create(ctx, privatev1.ClusterVersionsCreateRequest_builder{
			Object: privatev1.ClusterVersion_builder{
				Spec: privatev1.ClusterVersionSpec_builder{
					Version:         version,
					Image:           fmt.Sprintf("quay.io/openshift-release-dev/ocp-release:%s", version),
					AllowedUpgrades: allowedUpgrades,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		object := response.GetObject()
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, privatev1.ClusterVersionsDeleteRequest_builder{
				Id: object.GetId(),
			}.Build())
		})
		return object
	}

	It("CRUD lifecycle", func() {
		version := nextCVVersion()
		object := createCV(version)

		// Verify create response:
		Expect(object.GetId()).ToNot(BeEmpty())
		Expect(object.GetSpec().GetVersion()).To(Equal(version))
		Expect(object.GetSpec().GetImage()).To(ContainSubstring(version))
		Expect(object.GetMetadata().GetName()).ToNot(BeEmpty())
		Expect(object.GetMetadata().HasCreationTimestamp()).To(BeTrue())
		Expect(object.GetMetadata().HasDeletionTimestamp()).To(BeFalse())

		// Get by ID:
		getResponse, err := client.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
			Id: object.GetId(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(getResponse.GetObject().GetSpec().GetVersion()).To(Equal(version))

		// List with filter:
		name := object.GetMetadata().GetName()
		listResponse, err := client.List(ctx, privatev1.ClusterVersionsListRequest_builder{
			Filter: new(fmt.Sprintf("this.metadata.name == %q", name)),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(listResponse.GetItems()).To(HaveLen(1))
		Expect(listResponse.GetItems()[0].GetId()).To(Equal(object.GetId()))

		// Update (add label):
		updateResponse, err := client.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
			Object: privatev1.ClusterVersion_builder{
				Id: object.GetId(),
				Metadata: privatev1.Metadata_builder{
					Labels: map[string]string{"env": "test"},
				}.Build(),
			}.Build(),
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"metadata.labels"}},
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(updateResponse.GetObject().GetMetadata().GetLabels()).To(
			HaveKeyWithValue("env", "test"))

		// Delete:
		_, err = client.Delete(ctx, privatev1.ClusterVersionsDeleteRequest_builder{
			Id: object.GetId(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())

		// Get returns not found:
		_, err = client.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
			Id: object.GetId(),
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.NotFound))
	})

	It("Rejects update of immutable field spec.version", func() {
		version := nextCVVersion()
		object := createCV(version)

		_, err := client.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
			Object: privatev1.ClusterVersion_builder{
				Id: object.GetId(),
				Spec: privatev1.ClusterVersionSpec_builder{
					Version: "9.9.9",
				}.Build(),
			}.Build(),
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"spec.version"}},
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
	})

	It("Creates with allowed_upgrades cross-references", func() {
		versionA := nextCVVersion()
		versionB := nextCVVersion()
		cvA := createCV(versionA)
		cvB := createCV(versionB)

		// Create CV-C referencing both A and B:
		versionC := nextCVVersion()
		nameA := cvA.GetMetadata().GetName()
		nameB := cvB.GetMetadata().GetName()
		cvC := createCV(versionC, nameA, nameB)

		// Get CV-C and verify references round-trip:
		getResponse, err := client.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
			Id: cvC.GetId(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		versionNames := getResponse.GetObject().GetSpec().GetAllowedUpgrades().GetVersionNames()
		Expect(versionNames).To(ConsistOf(nameA, nameB))
	})

	It("Delete cleans up allowed_upgrades and increments version", func() {
		versionA := nextCVVersion()
		versionB := nextCVVersion()
		versionC := nextCVVersion()
		cvA := createCV(versionA)
		cvB := createCV(versionB)
		cvC := createCV(versionC)
		nameA := cvA.GetMetadata().GetName()

		// Update CV-B and CV-C to reference CV-A:
		updateResponseB, err := client.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
			Object: privatev1.ClusterVersion_builder{
				Id: cvB.GetId(),
				Spec: privatev1.ClusterVersionSpec_builder{
					AllowedUpgrades: privatev1.ClusterVersionAllowedUpgrades_builder{
						VersionNames: []string{nameA},
					}.Build(),
				}.Build(),
			}.Build(),
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"spec.allowed_upgrades"}},
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		versionB_before := updateResponseB.GetObject().GetMetadata().GetVersion()

		updateResponseC, err := client.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
			Object: privatev1.ClusterVersion_builder{
				Id: cvC.GetId(),
				Spec: privatev1.ClusterVersionSpec_builder{
					AllowedUpgrades: privatev1.ClusterVersionAllowedUpgrades_builder{
						VersionNames: []string{nameA},
					}.Build(),
				}.Build(),
			}.Build(),
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"spec.allowed_upgrades"}},
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		versionC_before := updateResponseC.GetObject().GetMetadata().GetVersion()

		// Delete CV-A:
		_, err = client.Delete(ctx, privatev1.ClusterVersionsDeleteRequest_builder{
			Id: cvA.GetId(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())

		// Verify CV-B: version_names no longer contains nameA, metadata.version incremented:
		getB, err := client.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
			Id: cvB.GetId(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(getB.GetObject().GetSpec().GetAllowedUpgrades().GetVersionNames()).ToNot(
			ContainElement(nameA))
		Expect(getB.GetObject().GetMetadata().GetVersion()).To(
			BeNumerically(">", versionB_before))

		// Verify CV-C: version_names no longer contains nameA, metadata.version incremented:
		getC, err := client.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
			Id: cvC.GetId(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(getC.GetObject().GetSpec().GetAllowedUpgrades().GetVersionNames()).ToNot(
			ContainElement(nameA))
		Expect(getC.GetObject().GetMetadata().GetVersion()).To(
			BeNumerically(">", versionC_before))
	})

	It("Delete preserves unrelated references", func() {
		versionA := nextCVVersion()
		versionB := nextCVVersion()
		versionC := nextCVVersion()
		cvA := createCV(versionA)
		cvB := createCV(versionB)
		cvC := createCV(versionC)
		nameA := cvA.GetMetadata().GetName()
		nameB := cvB.GetMetadata().GetName()

		// Update CV-C to reference both A and B:
		_, err := client.Update(ctx, privatev1.ClusterVersionsUpdateRequest_builder{
			Object: privatev1.ClusterVersion_builder{
				Id: cvC.GetId(),
				Spec: privatev1.ClusterVersionSpec_builder{
					AllowedUpgrades: privatev1.ClusterVersionAllowedUpgrades_builder{
						VersionNames: []string{nameA, nameB},
					}.Build(),
				}.Build(),
			}.Build(),
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"spec.allowed_upgrades"}},
		}.Build())
		Expect(err).ToNot(HaveOccurred())

		// Delete CV-A:
		_, err = client.Delete(ctx, privatev1.ClusterVersionsDeleteRequest_builder{
			Id: cvA.GetId(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())

		// Get CV-C: should still reference B but not A:
		getC, err := client.Get(ctx, privatev1.ClusterVersionsGetRequest_builder{
			Id: cvC.GetId(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		versionNames := getC.GetObject().GetSpec().GetAllowedUpgrades().GetVersionNames()
		Expect(versionNames).To(ConsistOf(nameB))
		Expect(versionNames).ToNot(ContainElement(nameA))
	})
})
