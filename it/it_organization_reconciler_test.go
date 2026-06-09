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
	"time"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/controllers/finalizers"
	"github.com/osac-project/fulfillment-service/internal/uuid"
)

var _ = Describe("Organization reconciler", func() {
	var (
		ctx    context.Context
		client privatev1.OrganizationsClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		client = privatev1.NewOrganizationsClient(tool.InternalView().AdminConn())
	})

	It("Creates the Keycloak organization", func() {
		// Create the organization and remember to delete it after the test:
		name := fmt.Sprintf("my-%s", uuid.New())
		createResponse, err := client.Create(ctx, privatev1.OrganizationsCreateRequest_builder{
			Object: privatev1.Organization_builder{
				Metadata: privatev1.Metadata_builder{
					Name: name,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		id := createResponse.GetObject().GetId()
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, privatev1.OrganizationsDeleteRequest_builder{
				Id: id,
			}.Build())
		})

		// Verify that the reconciler eventually adds the finalizer, sets the state to 'SYNCED', and populates
		// the Keycloak organization name:
		Eventually(
			func(g Gomega) {
				getResponse, err := client.Get(ctx, privatev1.OrganizationsGetRequest_builder{
					Id: id,
				}.Build())
				g.Expect(err).ToNot(HaveOccurred())
				object := getResponse.GetObject()
				metadata := object.GetMetadata()
				g.Expect(metadata.GetFinalizers()).To(ContainElement(finalizers.Controller))
				status := object.GetStatus()
				g.Expect(status.GetState()).To(
					Equal(privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED),
				)
				g.Expect(status.GetIdpOrganizationName()).To(Equal(name))
				g.Expect(status.GetBreakGlassUserId()).ToNot(BeEmpty())
			},
			time.Minute,
			time.Second,
		).Should(Succeed())
	})

	It("Syncs domains to Keycloak", func() {
		name := fmt.Sprintf("my-%s", uuid.New())
		domains := []string{
			fmt.Sprintf("%s.example.com", uuid.New()),
			fmt.Sprintf("%s.example.org", uuid.New()),
		}
		createResponse, err := client.Create(ctx, privatev1.OrganizationsCreateRequest_builder{
			Object: privatev1.Organization_builder{
				Metadata: privatev1.Metadata_builder{
					Name: name,
				}.Build(),
				Spec: privatev1.OrganizationSpec_builder{
					Domains: domains,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		id := createResponse.GetObject().GetId()
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, privatev1.OrganizationsDeleteRequest_builder{
				Id: id,
			}.Build())
		})

		Eventually(
			func(g Gomega) {
				getResponse, err := client.Get(ctx, privatev1.OrganizationsGetRequest_builder{
					Id: id,
				}.Build())
				g.Expect(err).ToNot(HaveOccurred())
				object := getResponse.GetObject()
				g.Expect(object.GetStatus().GetState()).To(
					Equal(privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED),
				)
			},
			time.Minute,
			time.Second,
		).Should(Succeed())
	})

	It("Deletes the Keycloak organization", func() {
		// Create the organization:
		name := fmt.Sprintf("my-%s", uuid.New())
		createResponse, err := client.Create(ctx, privatev1.OrganizationsCreateRequest_builder{
			Object: privatev1.Organization_builder{
				Metadata: privatev1.Metadata_builder{
					Name: name,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		id := createResponse.GetObject().GetId()

		// Wait for the reconciler to sync the organization to the IDP before deleting, as otherwise it would be
		// deleted immediately by the server without going through the reconciler.
		Eventually(
			func(g Gomega) {
				getResponse, err := client.Get(ctx, privatev1.OrganizationsGetRequest_builder{
					Id: id,
				}.Build())
				g.Expect(err).ToNot(HaveOccurred())
				object := getResponse.GetObject()
				metadata := object.GetMetadata()
				g.Expect(metadata.GetFinalizers()).To(ContainElement(finalizers.Controller))
				status := object.GetStatus()
				g.Expect(status.GetState()).To(
					Equal(privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED),
				)
			},
			time.Minute,
			time.Second,
		).Should(Succeed())

		// Delete the organization:
		_, err = client.Delete(ctx, privatev1.OrganizationsDeleteRequest_builder{
			Id: id,
		}.Build())
		Expect(err).ToNot(HaveOccurred())

		// Verify the organization eventually disappears:
		Eventually(
			func(g Gomega) {
				_, err := client.Get(ctx, privatev1.OrganizationsGetRequest_builder{
					Id: id,
				}.Build())
				g.Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				g.Expect(ok).To(BeTrue())
				g.Expect(status.Code()).To(Equal(grpccodes.NotFound))
			},
			time.Minute,
			time.Second,
		).Should(Succeed())
	})
})
