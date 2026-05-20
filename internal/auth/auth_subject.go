/*
Copyright (c) 2025 Red Hat Inc.

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
	"fmt"

	"github.com/osac-project/fulfillment-service/internal/collections"
)

// Subject represents an entity, such as person or a service account.
type Subject struct {
	// User is the name of the user.
	User string

	// Tenants is the set of tenants that the subject belongs to. It may be the universal set if the subject has
	// access to all tenants.
	Tenants collections.Set[string]

	// Projects is the set of projects that the subject has access to. It may be the universal set if the subject
	// has access to all projects.
	Projects collections.Set[string]
}

// subjectJson is the JSON representation of a subject used internally for serialization and deserialization.
type subjectJson struct {
	User     string   `json:"user"`
	Tenants  []string `json:"tenants"`
	Projects []string `json:"projects"`
}

// UnmarshalJSON implements custom JSON unmarshalling for Subject. It interprets the special value '*' in the tenants
// and projects arrays as meaning all tenants or all projects respectively. This special value must not be combined
// with specific names, that will be considered as an error.
func (s *Subject) UnmarshalJSON(data []byte) error {
	var subject subjectJson
	if err := json.Unmarshal(data, &subject); err != nil {
		return err
	}
	s.User = subject.User
	var err error
	s.Tenants, err = unmarshalSet(subject.Tenants, "tenant")
	if err != nil {
		return err
	}
	s.Projects, err = unmarshalSet(subject.Projects, "project")
	if err != nil {
		return err
	}
	return nil
}

// unmarshalSet converts a string slice into a Set, interpreting the special value '*' as the universal set. The
// kind parameter is used for error messages.
func unmarshalSet(items []string, kind string) (collections.Set[string], error) {
	universal := false
	for _, item := range items {
		if item == universalMarker {
			universal = true
			break
		}
	}
	if universal {
		if len(items) > 1 {
			return collections.Set[string]{}, fmt.Errorf(
				"the universal marker '%s' cannot be combined with specific %s names",
				universalMarker, kind,
			)
		}
		return collections.NewUniversalSet[string](), nil
	}
	return collections.NewSet(items...), nil
}

// MarshalJSON implements custom JSON marshalling for Subject.
func (s *Subject) MarshalJSON() (data []byte, err error) {
	tenants, err := marshalSet(s.Tenants, "tenant")
	if err != nil {
		return
	}
	projects, err := marshalSet(s.Projects, "project")
	if err != nil {
		return
	}
	data, err = json.Marshal(subjectJson{
		User:     s.User,
		Tenants:  tenants,
		Projects: projects,
	})
	return
}

// marshalSet converts a Set into a string slice for JSON serialization. Universal sets are represented as a
// single-element slice containing the special value '*'. The kind parameter is used for error messages.
func marshalSet(set collections.Set[string], kind string) ([]string, error) {
	if set.Universal() {
		return []string{universalMarker}, nil
	}
	if set.Finite() {
		return set.Inclusions(), nil
	}
	return nil, fmt.Errorf("the %s set is infinite", kind)
}

// universalMarker is the special value used in the subject's tenant and project lists to indicate that the subject has
// access to all of them.
const universalMarker = "*"
