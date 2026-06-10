/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package publicv1

import (
	"log/slog"
)

// LogValue implements slog.LogValuer for IdentityProviderTestResult to redact error details.
// Redacts: detailed error messages that may contain credentials or connection strings.
func (r *IdentityProviderTestResult) LogValue() slog.Value {
	if r == nil {
		return slog.Value{}
	}

	attrs := []slog.Attr{
		slog.Bool("success", r.Success),
	}

	// Redact message content - may contain connection details, credentials, or error details
	// Only log whether a message exists
	if r.Message != nil {
		attrs = append(attrs, slog.Bool("has_message", true))
	}

	return slog.GroupValue(attrs...)
}

// LogValue implements slog.LogValuer for IdentityProviderHealth to redact detailed error messages.
func (h *IdentityProviderHealth) LogValue() slog.Value {
	if h == nil {
		return slog.Value{}
	}

	attrs := []slog.Attr{
		slog.String("status", h.Status.String()),
	}

	if h.LastChecked != nil {
		attrs = append(attrs, slog.Time("last_checked", h.LastChecked.AsTime()))
	}

	// Redact message - may contain internal network details or error information
	if h.Message != nil {
		attrs = append(attrs, slog.Bool("has_message", true))
	}

	return slog.GroupValue(attrs...)
}
