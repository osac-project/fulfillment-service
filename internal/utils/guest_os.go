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
	"strings"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
)

// InferGuestOSFamily infers the guest OS family from the image source reference.
// If the image source reference contains "windows" (case-insensitive), the function
// sets guest_os_family to "windows". Otherwise, it leaves the field empty.
//
// This function does not override user-provided values. If guest_os_family is already
// set (non-empty), inference is skipped.
//
// Per design decisions D-01, D-02, D-03, D-04, D-05:
// - Inference fires only when guest_os_family is empty (not set by caller)
// - Case-insensitive substring match on image.source_ref for 'windows'
// - On match, set guest_os_family to 'windows'; on no match, leave empty
// - Consumers (AAP, operator) handle empty as 'linux' via their own defaults
// - When guest_os_family is not set and inference does not trigger, the field stays empty
func InferGuestOSFamily(spec *privatev1.ComputeInstanceSpec) {
	// Guard: return if spec is nil
	if spec == nil {
		return
	}

	// Guard: return if guest_os_family is already set (user value takes precedence)
	if spec.HasGuestOsFamily() {
		return
	}

	// Guard: return if no image to infer from
	if !spec.HasImage() {
		return
	}

	// Extract sourceRef
	sourceRef := spec.GetImage().GetSourceRef()
	if sourceRef == "" {
		return
	}

	// If sourceRef contains "windows" (case-insensitive), set guest_os_family to "windows"
	if strings.Contains(strings.ToLower(sourceRef), "windows") {
		spec.SetGuestOsFamily("windows")
	}
	// Otherwise leave empty (do NOT set "linux" per D-03, D-07)
}

// ValidateGuestOSFamily performs soft validation on the guest_os_family value.
// It accepts empty, "linux", and "windows" without logging. For unknown values,
// it logs a warning but does not return an error.
//
// Per design decisions D-13, D-14:
// - Soft validation logs a warning via slog when an unknown value is received
// - No error returned to client
// - Warning goes to server log only (no response annotation or client-visible warning)
func ValidateGuestOSFamily(ctx context.Context, logger *slog.Logger, value string) {
	// Accept empty, "linux", and "windows" without logging
	if value == "" || value == "linux" || value == "windows" {
		return
	}

	// Log warning for unknown values
	if logger != nil {
		logger.WarnContext(ctx, "unknown guest_os_family value received",
			"value", value,
			"expected", []string{"linux", "windows"})
	}
}
