/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package computeinstance

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ = Describe("isPermanentError", func() {
	It("should classify PermissionDenied as permanent", func() {
		err := status.Error(codes.PermissionDenied, "denied")
		Expect(isPermanentError(err)).To(BeTrue())
	})

	It("should classify NotFound as permanent", func() {
		err := status.Error(codes.NotFound, "not found")
		Expect(isPermanentError(err)).To(BeTrue())
	})

	It("should classify Unauthenticated as permanent", func() {
		err := status.Error(codes.Unauthenticated, "no auth")
		Expect(isPermanentError(err)).To(BeTrue())
	})

	It("should classify FailedPrecondition as permanent", func() {
		err := status.Error(codes.FailedPrecondition, "not running")
		Expect(isPermanentError(err)).To(BeTrue())
	})

	It("should classify InvalidArgument as permanent", func() {
		err := status.Error(codes.InvalidArgument, "bad request")
		Expect(isPermanentError(err)).To(BeTrue())
	})

	It("should classify Unimplemented as permanent", func() {
		err := status.Error(codes.Unimplemented, "not implemented")
		Expect(isPermanentError(err)).To(BeTrue())
	})

	It("should classify Unavailable as transient", func() {
		err := status.Error(codes.Unavailable, "temporarily unavailable")
		Expect(isPermanentError(err)).To(BeFalse())
	})

	It("should classify Internal as transient", func() {
		err := status.Error(codes.Internal, "internal error")
		Expect(isPermanentError(err)).To(BeFalse())
	})

	It("should classify non-gRPC errors as transient", func() {
		err := &time.ParseError{}
		Expect(isPermanentError(err)).To(BeFalse())
	})
})
