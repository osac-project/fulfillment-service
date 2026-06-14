/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package keycloak

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("searchGroupRecursively", func() {
	It("should find a top-level group", func() {
		group := groupNode{
			ID:   "group-123",
			Name: "web-app",
			Path: "/web-app",
		}

		id := searchGroupRecursively(group, "/web-app")
		Expect(id).To(Equal("group-123"))
	})

	It("should find a nested group one level deep", func() {
		group := groupNode{
			ID:   "group-123",
			Name: "web-app",
			Path: "/web-app",
			SubGroups: []groupNode{
				{ID: "group-456", Name: "viewers", Path: "/web-app/viewers"},
				{ID: "group-789", Name: "managers", Path: "/web-app/managers"},
			},
		}

		id := searchGroupRecursively(group, "/web-app/viewers")
		Expect(id).To(Equal("group-456"))
	})

	It("should find a deeply nested group three levels deep", func() {
		group := groupNode{
			ID:   "group-parent",
			Name: "parent-project",
			Path: "/parent-project",
			SubGroups: []groupNode{
				{
					ID:   "group-child",
					Name: "child-project",
					Path: "/parent-project/child-project",
					SubGroups: []groupNode{
						{ID: "group-viewers", Name: "viewers", Path: "/parent-project/child-project/viewers"},
						{ID: "group-managers", Name: "managers", Path: "/parent-project/child-project/managers"},
					},
				},
			},
		}

		id := searchGroupRecursively(group, "/parent-project/child-project/viewers")
		Expect(id).To(Equal("group-viewers"))
	})

	It("should return empty string when group is not found", func() {
		group := groupNode{
			ID:   "group-123",
			Name: "web-app",
			Path: "/web-app",
			SubGroups: []groupNode{
				{ID: "group-456", Name: "viewers", Path: "/web-app/viewers"},
			},
		}

		id := searchGroupRecursively(group, "/nonexistent")
		Expect(id).To(Equal(""))
	})

	It("should search across multiple branches", func() {
		group := groupNode{
			ID:   "group-root",
			Name: "root",
			Path: "/root",
			SubGroups: []groupNode{
				{
					ID:   "group-branch1",
					Name: "branch1",
					Path: "/root/branch1",
					SubGroups: []groupNode{
						{ID: "group-leaf1", Name: "leaf1", Path: "/root/branch1/leaf1"},
					},
				},
				{
					ID:   "group-branch2",
					Name: "branch2",
					Path: "/root/branch2",
					SubGroups: []groupNode{
						{ID: "group-leaf2", Name: "leaf2", Path: "/root/branch2/leaf2"},
					},
				},
			},
		}

		// Should find in first branch
		id := searchGroupRecursively(group, "/root/branch1/leaf1")
		Expect(id).To(Equal("group-leaf1"))

		// Should find in second branch
		id = searchGroupRecursively(group, "/root/branch2/leaf2")
		Expect(id).To(Equal("group-leaf2"))
	})

	It("should handle empty subgroups", func() {
		group := groupNode{
			ID:        "group-123",
			Name:      "web-app",
			Path:      "/web-app",
			SubGroups: []groupNode{},
		}

		id := searchGroupRecursively(group, "/web-app")
		Expect(id).To(Equal("group-123"))

		id = searchGroupRecursively(group, "/web-app/viewers")
		Expect(id).To(Equal(""))
	})
})
