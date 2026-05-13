/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package reflection

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/proto"
)

// Renderer is a minimal interface satisfied by *terminal.Console.
// Using an interface avoids a circular import (terminal imports reflection for table rendering).
type Renderer interface {
	Render(ctx context.Context, name string, data any)
}

// FindObject resolves ref to exactly one object by building a CEL filter and calling List.
// On zero or multiple matches the appropriate console template is rendered and nil is returned,
// so callers should check for a nil result rather than a non-nil error.
//
// Expected templates (looked up in the console's registered template set):
//   - "no_matches.txt"      vars: Object (string), Ref (string)
//   - "multiple_matches.txt" vars: Matches ([]proto.Message), Object (string), Ref (string), Total (int32)
func (h *ObjectHelper) FindObject(ctx context.Context, ref string, console Renderer) (result proto.Message, err error) {
	filter := fmt.Sprintf(`this.id == %[1]q || this.metadata.name == %[1]q`, ref)
	response, err := h.List(ctx, ListOptions{
		Filter: filter,
		Limit:  10,
	})
	if err != nil {
		err = fmt.Errorf(
			"failed to find object of type '%s' with identifier or name '%s': %w",
			h, ref, err,
		)
		return
	}
	items := response.Items
	total := response.Total
	switch len(items) {
	case 0:
		console.Render(ctx, "no_matches.txt", map[string]any{
			"Object": h.Singular(),
			"Ref":    ref,
		})
		return nil, nil
	case 1:
		return items[0], nil
	default:
		console.Render(ctx, "multiple_matches.txt", map[string]any{
			"Matches": items,
			"Object":  h.Singular(),
			"Ref":     ref,
			"Total":   total,
		})
		return nil, nil
	}
}
