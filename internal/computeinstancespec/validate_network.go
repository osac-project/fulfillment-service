/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

// Package computeinstancespec holds validation logic for compute instance spec fields.
package computeinstancespec

import (
	"fmt"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
)

// ValidateNetworkAttachments validates the network_attachments field.
// It checks that:
// - The field must be provided (not nil)
// - The array must not be empty (at least one attachment required)
// - All attachments have non-empty subnet
func ValidateNetworkAttachments(networkAttachments []*privatev1.NetworkAttachment) error {
	if networkAttachments == nil || len(networkAttachments) == 0 {
		return fmt.Errorf("network_attachments: at least one attachment is required")
	}

	for i, att := range networkAttachments {
		if att == nil {
			return fmt.Errorf("network_attachments[%d]: attachment cannot be null", i)
		}
		if att.GetSubnet() == "" {
			return fmt.Errorf("network_attachments[%d]: subnet is required", i)
		}
	}
	return nil
}
