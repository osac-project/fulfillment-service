/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package dao

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Errors", func() {
	Describe("ErrNotFound", func() {
		It("Implements the error interface", func() {
			var err error = &ErrNotFound{IDs: []string{"123"}}
			Expect(err).ToNot(BeNil())
		})

		It("Returns generic message when there are no identifiers", func() {
			err := &ErrNotFound{}
			Expect(err.Error()).To(Equal("object not found"))
		})

		It("Returns expected message for a single identifier", func() {
			err := &ErrNotFound{IDs: []string{"my-id"}}
			Expect(err.Error()).To(Equal("object with identifier 'my-id' not found"))
		})

		It("Returns expected message for two identifiers", func() {
			err := &ErrNotFound{IDs: []string{"a", "b"}}
			Expect(err.Error()).To(Equal("objects with identifiers 'a' and 'b' not found"))
		})

		It("Returns expected message for three identifiers", func() {
			err := &ErrNotFound{IDs: []string{"a", "b", "c"}}
			Expect(err.Error()).To(Equal("objects with identifiers 'a', 'b' and 'c' not found"))
		})
	})

	Describe("ErrAlreadyExists", func() {
		It("Implements the error interface", func() {
			var err error = &ErrAlreadyExists{ID: "123"}
			Expect(err).ToNot(BeNil())
		})

		It("Returns expected error message", func() {
			err := &ErrAlreadyExists{ID: "my-id"}
			Expect(err.Error()).To(Equal("object with identifier 'my-id' already exists"))
		})
	})

	Describe("ErrDenied", func() {
		It("Implements the error interface", func() {
			var err error = &ErrDenied{Reason: "not allowed"}
			Expect(err).ToNot(BeNil())
		})

		It("Returns the Reason field as the error message", func() {
			err := &ErrDenied{Reason: "operation not permitted"}
			Expect(err.Error()).To(Equal("operation not permitted"))
		})

		It("Returns empty string when Reason is empty", func() {
			err := &ErrDenied{}
			Expect(err.Error()).To(BeEmpty())
		})
	})
})
