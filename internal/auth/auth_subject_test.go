/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package auth

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/osac-project/fulfillment-service/internal/collections"
)

var _ = Describe("Subject", func() {
	Describe("JSON unmarshalling", func() {
		It("Parses user and specific tenants", func() {
			var subject Subject
			err := json.Unmarshal(
				[]byte(`{
					"user": "alice",
					"tenants": ["t1", "t2"]
				}`),
				&subject,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(subject.User).To(Equal("alice"))
			Expect(subject.Tenants.Equal(collections.NewSet("t1", "t2"))).To(BeTrue())
		})

		It("Parses asterisk as universal set", func() {
			var subject Subject
			err := json.Unmarshal(
				[]byte(`{
					"user": "admin",
					"tenants": ["*"]
				}`),
				&subject,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(subject.User).To(Equal("admin"))
			Expect(subject.Tenants.Universal()).To(BeTrue())
		})

		It("Rejects asterisk mixed with specific tenants", func() {
			var subject Subject
			err := json.Unmarshal(
				[]byte(`{
					"user": "admin",
					"tenants": ["*", "t1"]
				}`),
				&subject,
			)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot be combined"))
		})

		It("Parses empty tenants as empty set", func() {
			var subject Subject
			err := json.Unmarshal(
				[]byte(`{
					"user": "alice",
					"tenants": []
				}`),
				&subject,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(subject.Tenants.Empty()).To(BeTrue())
		})

		It("Parses missing tenants as empty set", func() {
			var subject Subject
			err := json.Unmarshal(
				[]byte(`{
					"user": "alice"
				}`),
				&subject,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(subject.Tenants.Empty()).To(BeTrue())
		})

		It("Parses user and specific projects", func() {
			var subject Subject
			err := json.Unmarshal(
				[]byte(`{
					"user": "alice",
					"projects": ["p1", "p2"]
				}`),
				&subject,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(subject.User).To(Equal("alice"))
			Expect(subject.Projects.Equal(collections.NewSet("p1", "p2"))).To(BeTrue())
		})

		It("Parses asterisk as universal project set", func() {
			var subject Subject
			err := json.Unmarshal(
				[]byte(`{
					"user": "admin",
					"projects": ["*"]
				}`),
				&subject,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(subject.User).To(Equal("admin"))
			Expect(subject.Projects.Universal()).To(BeTrue())
		})

		It("Rejects asterisk mixed with specific projects", func() {
			var subject Subject
			err := json.Unmarshal(
				[]byte(`{
					"user": "admin",
					"projects": ["*", "p1"]
				}`),
				&subject,
			)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot be combined"))
		})

		It("Parses empty projects as empty set", func() {
			var subject Subject
			err := json.Unmarshal(
				[]byte(`{
					"user": "alice",
					"projects": []
				}`),
				&subject,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(subject.Projects.Empty()).To(BeTrue())
		})

		It("Parses missing projects as empty set", func() {
			var subject Subject
			err := json.Unmarshal(
				[]byte(`{
					"user": "alice"
				}`),
				&subject,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(subject.Projects.Empty()).To(BeTrue())
		})

		It("Parses tenants and projects together", func() {
			var subject Subject
			err := json.Unmarshal(
				[]byte(`{
					"user": "alice",
					"tenants": ["t1"],
					"projects": ["p1", "p2"]
				}`),
				&subject,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(subject.User).To(Equal("alice"))
			Expect(subject.Tenants.Equal(collections.NewSet("t1"))).To(BeTrue())
			Expect(subject.Projects.Equal(collections.NewSet("p1", "p2"))).To(BeTrue())
		})
	})

	Describe("JSON marshalling", func() {
		It("Marshals specific tenants", func() {
			subject := &Subject{
				User:    "alice",
				Tenants: collections.NewSet("t1"),
			}
			data, err := json.Marshal(subject)
			Expect(err).ToNot(HaveOccurred())
			var raw map[string]any
			err = json.Unmarshal(data, &raw)
			Expect(err).ToNot(HaveOccurred())
			Expect(raw["user"]).To(Equal("alice"))
			Expect(raw["tenants"]).To(ConsistOf("t1"))
		})

		It("Marshals universal set as asterisk", func() {
			subject := &Subject{
				User:    "admin",
				Tenants: AllTenants,
			}
			data, err := json.Marshal(subject)
			Expect(err).ToNot(HaveOccurred())
			var raw map[string]any
			err = json.Unmarshal(data, &raw)
			Expect(err).ToNot(HaveOccurred())
			Expect(raw["user"]).To(Equal("admin"))
			Expect(raw["tenants"]).To(ConsistOf("*"))
		})

		It("Marshals specific projects", func() {
			subject := &Subject{
				User:     "alice",
				Projects: collections.NewSet("p1"),
			}
			data, err := json.Marshal(subject)
			Expect(err).ToNot(HaveOccurred())
			var raw map[string]any
			err = json.Unmarshal(data, &raw)
			Expect(err).ToNot(HaveOccurred())
			Expect(raw["user"]).To(Equal("alice"))
			Expect(raw["projects"]).To(ConsistOf("p1"))
		})

		It("Marshals universal project set as asterisk", func() {
			subject := &Subject{
				User:     "admin",
				Projects: AllProjects,
			}
			data, err := json.Marshal(subject)
			Expect(err).ToNot(HaveOccurred())
			var raw map[string]any
			err = json.Unmarshal(data, &raw)
			Expect(err).ToNot(HaveOccurred())
			Expect(raw["user"]).To(Equal("admin"))
			Expect(raw["projects"]).To(ConsistOf("*"))
		})

		It("Round-trips specific tenants", func() {
			original := &Subject{
				User:    "alice",
				Tenants: collections.NewSet("t1", "t2"),
			}
			data, err := json.Marshal(original)
			Expect(err).ToNot(HaveOccurred())
			var restored Subject
			err = json.Unmarshal(data, &restored)
			Expect(err).ToNot(HaveOccurred())
			Expect(restored.User).To(Equal(original.User))
			Expect(restored.Tenants.Equal(original.Tenants)).To(BeTrue())
		})

		It("Round-trips universal set", func() {
			original := &Subject{
				User:    "admin",
				Tenants: AllTenants,
			}
			data, err := json.Marshal(original)
			Expect(err).ToNot(HaveOccurred())
			var restored Subject
			err = json.Unmarshal(data, &restored)
			Expect(err).ToNot(HaveOccurred())
			Expect(restored.User).To(Equal(original.User))
			Expect(restored.Tenants.Universal()).To(BeTrue())
		})

		It("Round-trips specific projects", func() {
			original := &Subject{
				User:     "alice",
				Projects: collections.NewSet("p1", "p2"),
			}
			data, err := json.Marshal(original)
			Expect(err).ToNot(HaveOccurred())
			var restored Subject
			err = json.Unmarshal(data, &restored)
			Expect(err).ToNot(HaveOccurred())
			Expect(restored.User).To(Equal(original.User))
			Expect(restored.Projects.Equal(original.Projects)).To(BeTrue())
		})

		It("Round-trips universal project set", func() {
			original := &Subject{
				User:     "admin",
				Projects: AllProjects,
			}
			data, err := json.Marshal(original)
			Expect(err).ToNot(HaveOccurred())
			var restored Subject
			err = json.Unmarshal(data, &restored)
			Expect(err).ToNot(HaveOccurred())
			Expect(restored.User).To(Equal(original.User))
			Expect(restored.Projects.Universal()).To(BeTrue())
		})

		It("Round-trips tenants and projects together", func() {
			original := &Subject{
				User:     "alice",
				Tenants:  collections.NewSet("t1"),
				Projects: collections.NewSet("p1", "p2"),
			}
			data, err := json.Marshal(original)
			Expect(err).ToNot(HaveOccurred())
			var restored Subject
			err = json.Unmarshal(data, &restored)
			Expect(err).ToNot(HaveOccurred())
			Expect(restored.User).To(Equal(original.User))
			Expect(restored.Tenants.Equal(original.Tenants)).To(BeTrue())
			Expect(restored.Projects.Equal(original.Projects)).To(BeTrue())
		})
	})
})
