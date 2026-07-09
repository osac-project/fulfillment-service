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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Visibility", func() {
	Describe("Nil receiver", func() {
		It("Total returns false", func() {
			var v *Visibility
			Expect(v.Total()).To(BeFalse())
		})

		It("HasProject returns false regardless of parameters", func() {
			var v *Visibility
			Expect(v.HasProject("tenant-a", "project-x")).To(BeFalse())
			Expect(v.HasProject("", "")).To(BeFalse())
		})
	})

	Describe("Zero", func() {
		It("Is zero when newly created with no rules", func() {
			v, err := NewVisibility().Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.Zero()).To(BeTrue())
		})

		It("Is not zero after adding a project", func() {
			v, err := NewVisibility().
				AddProject("tenant-a", "project-x").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.Zero()).To(BeFalse())
		})

		It("Is not zero when total", func() {
			v, err := NewVisibility().
				SetTotal(true).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.Zero()).To(BeFalse())
		})
	})

	Describe("Total", func() {
		It("Is not total when newly created", func() {
			v, err := NewVisibility().Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.Total()).To(BeFalse())
		})

		It("Is total after calling SetTotal", func() {
			v, err := NewVisibility().
				SetTotal(true).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.Total()).To(BeTrue())
		})

		It("Previously added projects are no longer relevant after setting total", func() {
			v, err := NewVisibility().
				AddProject("tenant-a", "project-x").
				SetTotal(true).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.Total()).To(BeTrue())
			Expect(v.HasProject("tenant-a", "project-x")).To(BeTrue())
			Expect(v.HasProject("tenant-b", "project-y")).To(BeTrue())
		})
	})

	Describe("HasTenant", func() {
		It("Returns false for a nil visibility", func() {
			var v *Visibility
			Expect(v.HasTenant("tenant-a")).To(BeFalse())
		})

		It("Returns false for a visibility with no access", func() {
			v, err := NewVisibility().Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasTenant("tenant-a")).To(BeFalse())
		})

		It("Returns true for any tenant when total", func() {
			v, err := NewVisibility().
				SetTotal(true).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasTenant("tenant-a")).To(BeTrue())
			Expect(v.HasTenant("tenant-b")).To(BeTrue())
		})

		It("Returns true for a tenant that has been added", func() {
			v, err := NewVisibility().
				AddProject("tenant-a", "project-x").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasTenant("tenant-a")).To(BeTrue())
		})

		It("Returns false for a tenant that has not been added", func() {
			v, err := NewVisibility().
				AddProject("tenant-a", "project-x").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasTenant("tenant-b")).To(BeFalse())
		})
	})

	Describe("HasProject", func() {
		It("Returns false for a visibility with no access", func() {
			v, err := NewVisibility().Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasProject("tenant-a", "project-x")).To(BeFalse())
		})

		It("Returns true for any project when total", func() {
			v, err := NewVisibility().
				SetTotal(true).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasProject("tenant-a", "project-x")).To(BeTrue())
			Expect(v.HasProject("tenant-b", "project-y")).To(BeTrue())
		})

		It("Returns true for a visible project", func() {
			v, err := NewVisibility().
				AddProject("tenant-a", "project-x").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasProject("tenant-a", "project-x")).To(BeTrue())
		})

		It("Returns false for an unknown tenant", func() {
			v, err := NewVisibility().
				AddProject("tenant-a", "project-x").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasProject("tenant-b", "project-x")).To(BeFalse())
		})

		It("Returns false for a non-visible project in a known tenant", func() {
			v, err := NewVisibility().
				AddProject("tenant-a", "project-x").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasProject("tenant-a", "project-y")).To(BeFalse())
		})

		It("Returns true for a descendant of a visible project", func() {
			v, err := NewVisibility().
				AddProject("tenant-a", "project-x").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasProject("tenant-a", "project-x.child")).To(BeTrue())
			Expect(v.HasProject("tenant-a", "project-x.child.grandchild")).To(BeTrue())
		})

		It("Returns false for a project that is a prefix but not an ancestor", func() {
			v, err := NewVisibility().
				AddProject("tenant-a", "project-x").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasProject("tenant-a", "project-xyz")).To(BeFalse())
		})

		It("Returns false for the parent of a visible project", func() {
			v, err := NewVisibility().
				AddProject("tenant-a", "project-x.child").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasProject("tenant-a", "project-x")).To(BeFalse())
		})

		It("Returns true for any project when the root project is visible", func() {
			v, err := NewVisibility().
				AddProject("tenant-a", "").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasProject("tenant-a", "project-x")).To(BeTrue())
			Expect(v.HasProject("tenant-a", "project-y")).To(BeTrue())
			Expect(v.HasProject("tenant-a", "project-x.child")).To(BeTrue())
			Expect(v.HasProject("tenant-a", "project-x.child.grandchild")).To(BeTrue())
		})

		It("Returns true for the root project itself when it is visible", func() {
			v, err := NewVisibility().
				AddProject("tenant-a", "").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasProject("tenant-a", "")).To(BeTrue())
		})

		It("Does not grant visibility across tenants when root is added to one tenant", func() {
			v, err := NewVisibility().
				AddProject("tenant-a", "").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasProject("tenant-b", "project-x")).To(BeFalse())
		})
	})

	Describe("Tenants", func() {
		It("Returns nil for a nil visibility", func() {
			var v *Visibility
			Expect(v.Tenants()).To(BeNil())
		})

		It("Returns nil for a total visibility", func() {
			v, err := NewVisibility().
				SetTotal(true).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.Tenants()).To(BeNil())
		})

		It("Returns empty slice for a visibility with no access", func() {
			v, err := NewVisibility().Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.Tenants()).To(BeEmpty())
		})

		It("Returns the tenants that have been added", func() {
			v, err := NewVisibility().
				AddProject("tenant-a", "project-x").
				AddProject("tenant-b", "project-y").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.Tenants()).To(ConsistOf("tenant-a", "tenant-b"))
		})

		It("Returns a single tenant when only one has been added", func() {
			v, err := NewVisibility().
				AddProject("tenant-a", "project-x").
				AddProject("tenant-a", "project-y").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.Tenants()).To(ConsistOf("tenant-a"))
		})

		It("Returns tenants sorted alphabetically", func() {
			v, err := NewVisibility().
				AddProject("tenant-c", "project-x").
				AddProject("tenant-a", "project-x").
				AddProject("tenant-b", "project-x").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.Tenants()).To(Equal([]string{"tenant-a", "tenant-b", "tenant-c"}))
		})
	})

	Describe("Projects", func() {
		It("Returns nil for a nil visibility", func() {
			var v *Visibility
			Expect(v.Projects("tenant-a")).To(BeNil())
		})

		It("Returns nil for a total visibility", func() {
			v, err := NewVisibility().
				SetTotal(true).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.Projects("tenant-a")).To(BeNil())
		})

		It("Returns nil for a visibility with no access", func() {
			v, err := NewVisibility().Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.Projects("tenant-a")).To(BeNil())
		})

		It("Returns nil for an unknown tenant", func() {
			v, err := NewVisibility().
				AddProject("tenant-a", "project-x").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.Projects("tenant-b")).To(BeNil())
		})

		It("Returns the projects for a known tenant", func() {
			v, err := NewVisibility().
				AddProject("tenant-a", "project-x").
				AddProject("tenant-a", "project-y").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.Projects("tenant-a")).To(ConsistOf("project-x", "project-y"))
		})

		It("Returns projects sorted alphabetically", func() {
			v, err := NewVisibility().
				AddProject("tenant-a", "project-z").
				AddProject("tenant-a", "project-a").
				AddProject("tenant-a", "project-m").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.Projects("tenant-a")).To(Equal([]string{"project-a", "project-m", "project-z"}))
		})
	})

	Describe("AddTenant", func() {
		It("Makes the tenant visible", func() {
			v, err := NewVisibility().
				AddTenant("tenant-a").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasTenant("tenant-a")).To(BeTrue())
		})

		It("Does not grant project visibility", func() {
			v, err := NewVisibility().
				AddTenant("tenant-a").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasProject("tenant-a", "project-x")).To(BeFalse())
		})

		It("Does not duplicate a tenant already present", func() {
			v, err := NewVisibility().
				AddTenant("tenant-a").
				AddTenant("tenant-a").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.Tenants()).To(Equal([]string{"tenant-a"}))
		})

		It("Adds multiple tenants at once", func() {
			v, err := NewVisibility().
				AddTenants("tenant-a", "tenant-b", "tenant-c").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.Tenants()).To(Equal([]string{"tenant-a", "tenant-b", "tenant-c"}))
		})
	})

	Describe("AddProject", func() {
		It("Adds multiple projects for the same tenant", func() {
			v, err := NewVisibility().
				AddProject("tenant-a", "project-x").
				AddProject("tenant-a", "project-y").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasProject("tenant-a", "project-x")).To(BeTrue())
			Expect(v.HasProject("tenant-a", "project-y")).To(BeTrue())
		})

		It("Adds projects for multiple tenants", func() {
			v, err := NewVisibility().
				AddProject("tenant-a", "project-x").
				AddProject("tenant-b", "project-y").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasProject("tenant-a", "project-x")).To(BeTrue())
			Expect(v.HasProject("tenant-b", "project-y")).To(BeTrue())
			Expect(v.HasProject("tenant-a", "project-y")).To(BeFalse())
			Expect(v.HasProject("tenant-b", "project-x")).To(BeFalse())
		})

		It("Removes duplicate projects during build", func() {
			v, err := NewVisibility().
				AddProject("tenant-a", "project-x").
				AddProject("tenant-a", "project-x").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasProject("tenant-a", "project-x")).To(BeTrue())
			Expect(v.Projects("tenant-a")).To(Equal([]string{"project-x"}))
		})

		It("Removes a descendant if ancestor is also present", func() {
			v, err := NewVisibility().
				AddProject("tenant-a", "project-x").
				AddProject("tenant-a", "project-x.child").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasProject("tenant-a", "project-x")).To(BeTrue())
			Expect(v.HasProject("tenant-a", "project-x.child")).To(BeTrue())
			Expect(v.HasProject("tenant-a", "project-x.other")).To(BeTrue())
			Expect(v.Projects("tenant-a")).To(Equal([]string{"project-x"}))
		})
	})

	Describe("AddProjects", func() {
		It("Adds multiple projects for a tenant in one call", func() {
			v, err := NewVisibility().
				AddProjects("tenant-a", "project-x", "project-y", "project-z").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasProject("tenant-a", "project-x")).To(BeTrue())
			Expect(v.HasProject("tenant-a", "project-y")).To(BeTrue())
			Expect(v.HasProject("tenant-a", "project-z")).To(BeTrue())
		})

		It("Adds a single project", func() {
			v, err := NewVisibility().
				AddProjects("tenant-a", "project-x").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasProject("tenant-a", "project-x")).To(BeTrue())
		})

		It("Creates the tenant if it does not exist", func() {
			v, err := NewVisibility().
				AddProjects("tenant-a", "project-x").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasTenant("tenant-a")).To(BeTrue())
		})

		It("Does not duplicate projects when called multiple times", func() {
			v, err := NewVisibility().
				AddProjects("tenant-a", "project-x").
				AddProjects("tenant-a", "project-x").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.Projects("tenant-a")).To(Equal([]string{"project-x"}))
		})

		It("Can be combined with AddProject", func() {
			v, err := NewVisibility().
				AddProject("tenant-a", "project-x").
				AddProjects("tenant-a", "project-y", "project-z").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.Projects("tenant-a")).To(Equal([]string{"project-x", "project-y", "project-z"}))
		})

		It("Adds projects for different tenants", func() {
			v, err := NewVisibility().
				AddProjects("tenant-a", "project-x", "project-y").
				AddProjects("tenant-b", "project-z").
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(v.HasProject("tenant-a", "project-x")).To(BeTrue())
			Expect(v.HasProject("tenant-a", "project-y")).To(BeTrue())
			Expect(v.HasProject("tenant-b", "project-z")).To(BeTrue())
			Expect(v.HasProject("tenant-a", "project-z")).To(BeFalse())
		})
	})
})
