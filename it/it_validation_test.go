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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
)

var _ = Describe("Protovalidate validation", func() {
	var (
		ctx         context.Context
		capClient   privatev1.CapabilitiesClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		capClient = privatev1.NewCapabilitiesClient(tool.InternalView().AdminConn())
	})

	It("Rejects Capability with invalid metadata name (too long)", func() {
		// Create a Capability with name > 63 chars:
		invalidName := "this-name-is-way-too-long-and-exceeds-the-sixty-three-character-limit-for-dns-labels"

		_, err := capClient.Create(ctx, privatev1.CapabilitiesCreateRequest_builder{
			Object: privatev1.Capability_builder{
				Metadata: privatev1.Metadata_builder{
					Name: invalidName,
				}.Build(),
			}.Build(),
		}.Build())

		// Verify validation error:
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue(), "error should be a gRPC status error")
		Expect(status.Code()).To(Equal(grpccodes.InvalidArgument), "should return InvalidArgument")
		Expect(status.Message()).To(ContainSubstring("validation"))
		Expect(status.Message()).To(ContainSubstring("name"))
	})

	It("Rejects Capability with invalid metadata name pattern", func() {
		// Create a Capability with uppercase (invalid for DNS label):
		invalidName := "Invalid-Name-With-Uppercase"

		_, err := capClient.Create(ctx, privatev1.CapabilitiesCreateRequest_builder{
			Object: privatev1.Capability_builder{
				Metadata: privatev1.Metadata_builder{
					Name: invalidName,
				}.Build(),
			}.Build(),
		}.Build())

		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
		Expect(status.Message()).To(ContainSubstring("validation"))
	})

	It("Rejects Capability with label key that is too long", func() {
		// Create a label key > 316 chars:
		longKey := ""
		for i := 0; i < 320; i++ {
			longKey = longKey + "a"
		}

		_, err := capClient.Create(ctx, privatev1.CapabilitiesCreateRequest_builder{
			Object: privatev1.Capability_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "valid-name",
					Labels: map[string]string{
						longKey: "value",
					},
				}.Build(),
			}.Build(),
		}.Build())

		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
		Expect(status.Message()).To(ContainSubstring("validation"))
	})

	It("Accepts Capability with valid metadata", func() {
		validName := "valid-capability-name"

		response, err := capClient.Create(ctx, privatev1.CapabilitiesCreateRequest_builder{
			Object: privatev1.Capability_builder{
				Metadata: privatev1.Metadata_builder{
					Name: validName,
					Labels: map[string]string{
						"key1":             "value1",
						"example.com/key2": "value2",
					},
					Annotations: map[string]string{
						"annotation-key": "annotation-value",
					},
				}.Build(),
			}.Build(),
		}.Build())

		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())
		Expect(response.Object.Metadata.Name).To(Equal(validName))

		// Clean up:
		DeferCleanup(func() {
			_, _ = capClient.Delete(ctx, privatev1.CapabilitiesDeleteRequest_builder{
				Id: response.Object.Id,
			}.Build())
		})
	})

	It("Accepts Capability with empty name (optional field)", func() {
		response, err := capClient.Create(ctx, privatev1.CapabilitiesCreateRequest_builder{
			Object: privatev1.Capability_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "",
				}.Build(),
			}.Build(),
		}.Build())

		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())
		Expect(response.Object.Metadata.Name).To(Equal(""))

		// Clean up:
		DeferCleanup(func() {
			_, _ = capClient.Delete(ctx, privatev1.CapabilitiesDeleteRequest_builder{
				Id: response.Object.Id,
			}.Build())
		})
	})
})
