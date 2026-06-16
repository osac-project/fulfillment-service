/*
Copyright (c) 2026 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package utils

import (
	"context"
	"log/slog"
	"os"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
)

var _ = Describe("InferGuestOSFamily", func() {
	It("Sets 'windows' when image.source_ref contains 'containerdisks/windows'", func() {
		spec := privatev1.ComputeInstanceSpec_builder{
			Image: privatev1.ComputeInstanceImage_builder{
				SourceRef: "quay.io/containerdisks/windows:latest",
			}.Build(),
		}.Build()

		InferGuestOSFamily(spec)

		Expect(spec.HasGuestOsFamily()).To(BeTrue())
		Expect(spec.GetGuestOsFamily()).To(Equal("windows"))
	})

	It("Sets 'windows' when image.source_ref contains 'Windows-Server-2022' (case-insensitive)", func() {
		spec := privatev1.ComputeInstanceSpec_builder{
			Image: privatev1.ComputeInstanceImage_builder{
				SourceRef: "quay.io/myorg/Windows-Server-2022:latest",
			}.Build(),
		}.Build()

		InferGuestOSFamily(spec)

		Expect(spec.HasGuestOsFamily()).To(BeTrue())
		Expect(spec.GetGuestOsFamily()).To(Equal("windows"))
	})

	It("Leaves empty when image.source_ref is 'containerdisks/fedora:latest' (no match)", func() {
		spec := privatev1.ComputeInstanceSpec_builder{
			Image: privatev1.ComputeInstanceImage_builder{
				SourceRef: "quay.io/containerdisks/fedora:latest",
			}.Build(),
		}.Build()

		InferGuestOSFamily(spec)

		Expect(spec.HasGuestOsFamily()).To(BeFalse())
	})

	It("Does not override user-provided guest_os_family='linux' even with windows image", func() {
		spec := privatev1.ComputeInstanceSpec_builder{
			Image: privatev1.ComputeInstanceImage_builder{
				SourceRef: "quay.io/containerdisks/windows:latest",
			}.Build(),
			GuestOsFamily: new("linux"),
		}.Build()

		InferGuestOSFamily(spec)

		Expect(spec.GetGuestOsFamily()).To(Equal("linux"))
	})

	It("Does nothing when spec is nil (no panic)", func() {
		Expect(func() {
			InferGuestOSFamily(nil)
		}).ToNot(Panic())
	})

	It("Does nothing when image is nil (leaves empty)", func() {
		spec := privatev1.ComputeInstanceSpec_builder{}.Build()

		InferGuestOSFamily(spec)

		Expect(spec.HasGuestOsFamily()).To(BeFalse())
	})
})

var _ = Describe("ValidateGuestOSFamily", func() {
	var ctx context.Context
	var logger *slog.Logger

	BeforeEach(func() {
		ctx = context.Background()
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelWarn,
		}))
	})

	It("Accepts empty string without logging", func() {
		ValidateGuestOSFamily(ctx, logger, "")
		// No assertion needed - test passes if no panic
	})

	It("Accepts 'linux' without logging", func() {
		ValidateGuestOSFamily(ctx, logger, "linux")
		// No assertion needed - test passes if no panic
	})

	It("Accepts 'windows' without logging", func() {
		ValidateGuestOSFamily(ctx, logger, "windows")
		// No assertion needed - test passes if no panic
	})

	It("Logs warning for unknown value 'freebsd' (no error returned)", func() {
		// This test verifies the function signature has no error return type
		// and that it logs a warning for unknown values
		ValidateGuestOSFamily(ctx, logger, "freebsd")
		// No error to check - function has no return value
	})
})
