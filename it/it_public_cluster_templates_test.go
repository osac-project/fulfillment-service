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

	ffv1 "github.com/osac-project/fulfillment-service/internal/api/fulfillment/v1"
)

var _ = Describe("Cluster templates", func() {
	var (
		ctx    context.Context
		client ffv1.ClusterTemplatesClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		client = ffv1.NewClusterTemplatesClient(tool.UserConn())
	})

	It("Can get the list of templates", func() {
		listResponse, err := client.List(ctx, ffv1.ClusterTemplatesListRequest_builder{}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(listResponse).ToNot(BeNil())
		items := listResponse.GetItems()
		Expect(items).ToNot(BeEmpty())
	})

	It("Can get a specific template", func() {
		// First get the list to find an existing template:
		listResponse, err := client.List(ctx, ffv1.ClusterTemplatesListRequest_builder{}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(listResponse).ToNot(BeNil())
		items := listResponse.GetItems()
		Expect(items).ToNot(BeEmpty())

		// Get the first template from the list:
		firstTemplate := items[0]
		id := firstTemplate.GetId()

		// Get the template and verify that the returned object is correct:
		response, err := client.Get(ctx, ffv1.ClusterTemplatesGetRequest_builder{
			Id: id,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())
		object := response.GetObject()
		Expect(object).ToNot(BeNil())
		Expect(object.GetId()).To(Equal(id))
		metadata := object.GetMetadata()
		Expect(metadata).ToNot(BeNil())
		Expect(metadata.HasCreationTimestamp()).To(BeTrue())
		Expect(metadata.HasDeletionTimestamp()).To(BeFalse())
	})
})
