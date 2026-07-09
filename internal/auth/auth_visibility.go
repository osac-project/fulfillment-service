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
	"slices"
	"sort"
	"strings"
)

// VisibilityBuilder is a builder for creating new visibilities.
type VisibilityBuilder struct {
	total bool
	rules []visibilityRule
}

// Visibility represents the visibility of the current user.
type Visibility struct {
	// total indicates, when true, that the user has permission to see all tenants and projects.
	total bool

	// rules describes the visibility for each tenant.
	rules []visibilityRule
}

// visibilityRule represents the visibility of the current user for a given tenant.
type visibilityRule struct {
	// tenant is the name of the tenant.
	tenant string

	// projects is the list of projects visible for the tenant.
	projects []string
}

// TotalVisibility is a pre-built visibility that grants access to all tenants and projects.
var TotalVisibility = &Visibility{
	total: true,
}

// ZeroVisibility is a pre-built visibility that grants no access to any tenants or projects.
var ZeroVisibility = &Visibility{
	total: false,
	rules: nil,
}

// NewVisibility creates a new empty visibility.
func NewVisibility() *VisibilityBuilder {
	return &VisibilityBuilder{}
}

// SetTotal sets the total flag of the visibility. When this isn't explicitly specified the default is false.
func (b *VisibilityBuilder) SetTotal(value bool) *VisibilityBuilder {
	b.total = value
	return b
}

// AddTenant adds the given tenant to the visibility.
func (b *VisibilityBuilder) AddTenant(value string) *VisibilityBuilder {
	for i := range b.rules {
		if b.rules[i].tenant == value {
			return b
		}
	}
	b.rules = append(b.rules, visibilityRule{
		tenant:   value,
		projects: nil,
	})
	return b
}

// AddTenants adds the given tenants to the visibility.
func (b *VisibilityBuilder) AddTenants(values ...string) *VisibilityBuilder {
	for _, value := range values {
		b.AddTenant(value)
	}
	return b
}

// AddProject adds the given project to the visibility for the given tenant.
func (b *VisibilityBuilder) AddProject(tenant, project string) *VisibilityBuilder {
	for i := range b.rules {
		if b.rules[i].tenant == tenant {
			b.rules[i].projects = append(b.rules[i].projects, project)
			return b
		}
	}
	b.rules = append(b.rules, visibilityRule{
		tenant: tenant,
		projects: []string{
			project,
		},
	})
	return b
}

// AddProjects adds the given projects to the visibility for the given tenant.
func (b *VisibilityBuilder) AddProjects(tenant string, projects ...string) *VisibilityBuilder {
	for _, project := range projects {
		b.AddProject(tenant, project)
	}
	return b
}

// Build creates a new visibility from the builder.
func (b *VisibilityBuilder) Build() (result *Visibility, err error) {
	// Make a copy of the rules, and sort them by tenant:
	rules := slices.Clone(b.rules)
	sort.Slice(rules, func(i, j int) bool {
		tenantI := rules[i].tenant
		tenantJ := rules[j].tenant
		return tenantI < tenantJ
	})

	// For each rule make sure that it doesn't contain duplicate projects, that projects are not ancestors of other
	// projects, and that projects are sorted:
	for i := range rules {
		rule := &rules[i]
		projects := slices.Clone(rule.projects)
		sort.Strings(projects)
		projects = slices.CompactFunc(projects, func(a, b string) bool {
			return a == b || visibilityIsAncestor(b, a)
		})
		rule.projects = projects
	}

	// Create the result:
	result = &Visibility{
		total: b.total,
		rules: rules,
	}
	return
}

// Zero returns true if the visibility grants no access to any tenants or projects.
func (v *Visibility) Zero() bool {
	return v == nil || (!v.total && len(v.rules) == 0)
}

// Total returns true if the visibility grants access to all tenants and projects.
func (v *Visibility) Total() bool {
	return v != nil && v.total
}

// HasTenant returns true if the given tenant is visible, regardless of which projects are visible within it.
func (v *Visibility) HasTenant(tenant string) bool {
	if v == nil {
		return false
	}
	if v.total {
		return true
	}
	for i := range v.rules {
		if v.rules[i].tenant == tenant {
			return true
		}
	}
	return false
}

// HasProject returns true if the given project within the given tenant is visible.
func (v *Visibility) HasProject(tenant, project string) bool {
	if v == nil {
		return false
	}
	if v.total {
		return true
	}
	var rule *visibilityRule
	for i := range v.rules {
		if v.rules[i].tenant == tenant {
			rule = &v.rules[i]
			break
		}
	}
	if rule == nil {
		return false
	}
	for _, visible := range rule.projects {
		if project == visible || visibilityIsAncestor(visible, project) {
			return true
		}
	}
	return false
}

// Tenants returns the list of tenants where the user has visibility, sorted alphabetically. Returns nil when the
// visibility is nil or total, as a total visibility is not limited to specific tenants.
func (v *Visibility) Tenants() []string {
	if v == nil || v.total {
		return nil
	}
	result := make([]string, len(v.rules))
	for i := range v.rules {
		result[i] = v.rules[i].tenant
	}
	sort.Strings(result)
	return result
}

// Projects returns the list of visible projects for the given tenant, sorted alphabetically. Returns nil when the
// visibility is nil, total, or doesn't contain the given tenant.
func (v *Visibility) Projects(tenant string) []string {
	if v == nil || v.total {
		return nil
	}
	for i := range v.rules {
		if v.rules[i].tenant == tenant {
			result := make([]string, len(v.rules[i].projects))
			copy(result, v.rules[i].projects)
			sort.Strings(result)
			return result
		}
	}
	return nil
}

// visibilityIsAncestor checks if the given project is an ancestor of the given descendant. Project names are
// dot-separated paths, so an ancestor is a prefix of the descendant. The empty string is considered an ancestor of
// every non-empty project because it represents the root. Note that this returns false if ancestor and descendant are
// the same.
func visibilityIsAncestor(ancestor, descendant string) bool {
	ancestorLen := len(ancestor)
	descendantLen := len(descendant)
	if descendantLen <= ancestorLen {
		return false
	}
	if ancestorLen == 0 {
		return true
	}
	if !strings.HasPrefix(descendant, ancestor) {
		return false
	}
	if descendant[ancestorLen] != '.' {
		return false
	}
	return true
}
