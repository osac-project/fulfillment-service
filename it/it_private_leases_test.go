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

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/uuid"
)

var _ = Describe("Private leases", func() {
	var (
		ctx    context.Context
		client privatev1.LeasesClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		client = privatev1.NewLeasesClient(tool.AdminConn())
	})

	It("Can delete a lease", func() {
		// Create a lease without finalizers so that the delete will fully archive it:
		id := fmt.Sprintf("my_lease_%s", uuid.New())
		_, err := client.Create(ctx, privatev1.LeasesCreateRequest_builder{
			Object: privatev1.Lease_builder{
				Id: id,
				Spec: privatev1.LeaseSpec_builder{
					Holder: "controller-0",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())

		// Delete it:
		deleteResponse, err := client.Delete(ctx, privatev1.LeasesDeleteRequest_builder{
			Id: id,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(deleteResponse).ToNot(BeNil())

		// Trying to get the deleted object should either fail if the object has been completely
		// deleted and archived, or return an object that has the deletion timestamp set.
		getResponse, err := client.Get(ctx, privatev1.LeasesGetRequest_builder{
			Id: id,
		}.Build())
		if err != nil {
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.NotFound))
		} else {
			object := getResponse.GetObject()
			metadata := object.GetMetadata()
			Expect(metadata.HasDeletionTimestamp()).To(BeTrue())
		}
	})
})
