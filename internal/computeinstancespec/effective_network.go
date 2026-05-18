/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

// Package computeinstancespec holds derived views of compute instance spec fields (for example
// resolving deprecated networking fields into the preferred shape).
package computeinstancespec

import (
	"fmt"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
)

// ValidateNetworkAttachments validates the network_attachments field.
// It checks that all attachments have non-empty subnet.
func ValidateNetworkAttachments(networkAttachments []*privatev1.NetworkAttachment) error {
	for i, att := range networkAttachments {
		if att.GetSubnet() == "" {
			return fmt.Errorf("network_attachments[%d]: subnet is required", i)
		}
	}
	return nil
}

// validateMutualExclusivity checks that legacy fields and network_attachments are not both used.
func validateMutualExclusivity(spec *privatev1.ComputeInstanceSpec) error {
	if len(spec.GetNetworkAttachments()) > 0 && legacyNetworkingInUse(spec) {
		return fmt.Errorf("do not combine deprecated subnet or security_groups with network_attachments")
	}
	return nil
}

// validateLegacyFields validates that legacy subnet/security_groups fields are consistent.
func validateLegacyFields(spec *privatev1.ComputeInstanceSpec) error {
	// Check if we have security groups
	var hasSGs bool
	for _, id := range spec.GetSecurityGroups() {
		if id != "" {
			hasSGs = true
			break
		}
	}

	// If we have security groups, we must have a non-empty subnet
	if hasSGs {
		subnet := ""
		if spec.HasSubnet() {
			subnet = spec.GetSubnet()
		}
		if subnet == "" {
			return fmt.Errorf("subnet is required when security_groups are set")
		}
	}

	return nil
}

// EffectiveNetworkAttachments returns the list of network attachments to validate and reconcile.
//
// If spec.network_attachments is non-empty, it is returned as-is and deprecated subnet /
// security_groups must not be used on the same object.
//
// Otherwise, when deprecated subnet and/or non-empty security_groups are set, a single synthetic
// attachment is returned so existing data keeps working.
func EffectiveNetworkAttachments(spec *privatev1.ComputeInstanceSpec) ([]*privatev1.NetworkAttachment, error) {
	if spec == nil {
		return nil, nil
	}

	// Check mutual exclusivity between legacy and new fields
	err := validateMutualExclusivity(spec)
	if err != nil {
		return nil, err
	}

	// Handle new network_attachments field
	newList := spec.GetNetworkAttachments()
	if len(newList) > 0 {
		// Validate the network_attachments structure
		err := ValidateNetworkAttachments(newList)
		if err != nil {
			return nil, err
		}
		return newList, nil
	}

	// No legacy networking fields present
	if !legacyNetworkingInUse(spec) {
		return nil, nil
	}

	// Validate legacy fields
	err = validateLegacyFields(spec)
	if err != nil {
		return nil, err
	}

	// Transform legacy fields into synthetic attachment
	var sgs []string
	for _, id := range spec.GetSecurityGroups() {
		if id != "" {
			sgs = append(sgs, id)
		}
	}
	subnet := ""
	if spec.HasSubnet() {
		subnet = spec.GetSubnet()
	}

	// If both are empty, return empty (validation already passed)
	if subnet == "" && len(sgs) == 0 {
		return nil, nil
	}

	// At this point, subnet is guaranteed to be non-empty (validateLegacyFields ensures it)
	return []*privatev1.NetworkAttachment{
		privatev1.NetworkAttachment_builder{
			Subnet:         subnet,
			SecurityGroups: sgs,
		}.Build(),
	}, nil
}

func legacyNetworkingInUse(spec *privatev1.ComputeInstanceSpec) bool {
	// Any presence of the deprecated subnet field counts as "legacy in use", even if the
	// string value is empty (optional proto3 + presence edge cases).
	if spec.HasSubnet() {
		return true
	}
	for _, id := range spec.GetSecurityGroups() {
		if id != "" {
			return true
		}
	}
	return false
}
