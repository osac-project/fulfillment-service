/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package servers

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
)

// applyFieldDefinitions processes field definitions from a catalog item against a resource spec.
// For non-editable fields: overrides user-provided values with the catalog item default.
// For editable fields with user values: validates against the JSON Schema.
// For editable fields without user values: applies the catalog item default.
func applyFieldDefinitions(
	spec proto.Message,
	fieldDefinitions []*privatev1.FieldDefinition,
) error {
	if len(fieldDefinitions) == 0 {
		return nil
	}

	specJSON, err := protojson.Marshal(spec)
	if err != nil {
		return grpcstatus.Errorf(grpccodes.Internal, "failed to marshal spec: %v", err)
	}

	var specMap map[string]any
	if err := json.Unmarshal(specJSON, &specMap); err != nil {
		return grpcstatus.Errorf(grpccodes.Internal, "failed to parse spec: %v", err)
	}

	for _, fd := range fieldDefinitions {
		path := fd.GetPath()
		if path == "" {
			continue
		}

		defaultVal := fd.GetDefault()
		userVal, userHasValue := getNestedValue(specMap, path)

		if !fd.GetEditable() {
			if defaultVal != nil {
				defaultAny, err := defaultVal.MarshalJSON()
				if err != nil {
					return grpcstatus.Errorf(grpccodes.Internal,
						"failed to marshal default for field '%s': %v", path, err)
				}
				var parsed any
				if err := json.Unmarshal(defaultAny, &parsed); err != nil {
					return grpcstatus.Errorf(grpccodes.Internal,
						"failed to parse default for field '%s': %v", path, err)
				}
				setNestedValue(specMap, path, parsed)
			}
		} else {
			if userHasValue && userVal != nil {
				schema := fd.GetValidationSchema()
				if schema != "" {
					if err := validateAgainstSchema(path, userVal, schema); err != nil {
						return err
					}
				}
			} else if defaultVal != nil {
				defaultAny, err := defaultVal.MarshalJSON()
				if err != nil {
					return grpcstatus.Errorf(grpccodes.Internal,
						"failed to marshal default for field '%s': %v", path, err)
				}
				var parsed any
				if err := json.Unmarshal(defaultAny, &parsed); err != nil {
					return grpcstatus.Errorf(grpccodes.Internal,
						"failed to parse default for field '%s': %v", path, err)
				}
				setNestedValue(specMap, path, parsed)
			}
		}
	}

	updatedJSON, err := json.Marshal(specMap)
	if err != nil {
		return grpcstatus.Errorf(grpccodes.Internal, "failed to serialize updated spec: %v", err)
	}

	proto.Reset(spec)
	if err := protojson.Unmarshal(updatedJSON, spec); err != nil {
		return grpcstatus.Errorf(grpccodes.Internal, "failed to apply updated spec: %v", err)
	}

	return nil
}

// validateCatalogItemAccess checks that a catalog item is published and visible to the caller's tenant.
func validateCatalogItemAccess(
	catalogItem proto.Message,
	catalogItemRef string,
) error {
	refl := catalogItem.ProtoReflect()

	deletionTimestamp := refl.Descriptor().Fields().ByName("metadata")
	if deletionTimestamp != nil {
		metadata := refl.Get(deletionTimestamp).Message()
		dtField := metadata.Descriptor().Fields().ByName("deletion_timestamp")
		if dtField != nil && metadata.Has(dtField) {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"catalog item '%s' has been deleted", catalogItemRef)
		}
	}

	publishedField := refl.Descriptor().Fields().ByName("published")
	if publishedField != nil {
		if !refl.Get(publishedField).Bool() {
			return grpcstatus.Errorf(grpccodes.NotFound,
				"catalog item '%s' is not published", catalogItemRef)
		}
	}

	return nil
}

// lookupCatalogItemTemplate extracts the template reference from a catalog item.
func lookupCatalogItemTemplate(catalogItem proto.Message) string {
	refl := catalogItem.ProtoReflect()
	templateField := refl.Descriptor().Fields().ByName("template")
	if templateField == nil {
		return ""
	}
	return refl.Get(templateField).String()
}

// getFieldDefinitions extracts field definitions from a catalog item.
func getFieldDefinitions(catalogItem proto.Message) []*privatev1.FieldDefinition {
	refl := catalogItem.ProtoReflect()
	fdField := refl.Descriptor().Fields().ByName("field_definitions")
	if fdField == nil {
		return nil
	}
	list := refl.Get(fdField).List()
	result := make([]*privatev1.FieldDefinition, list.Len())
	for i := range list.Len() {
		result[i] = list.Get(i).Message().Interface().(*privatev1.FieldDefinition)
	}
	return result
}

func validateAgainstSchema(path string, value any, schemaStr string) error {
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("schema.json", strings.NewReader(schemaStr)); err != nil {
		return grpcstatus.Errorf(grpccodes.Internal,
			"invalid validation schema for field '%s': %v", path, err)
	}
	schema, err := compiler.Compile("schema.json")
	if err != nil {
		return grpcstatus.Errorf(grpccodes.Internal,
			"failed to compile validation schema for field '%s': %v", path, err)
	}
	if err := schema.Validate(value); err != nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"validation failed for field '%s': %v", path, err)
	}
	return nil
}

// getNestedValue retrieves a value from a nested map using dot-notation path.
func getNestedValue(m map[string]any, path string) (any, bool) {
	parts := strings.Split(path, ".")
	current := any(m)
	for _, part := range parts {
		currentMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = currentMap[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

// setNestedValue sets a value in a nested map using dot-notation path, creating intermediate maps as needed.
func setNestedValue(m map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	current := m
	for i, part := range parts {
		if i == len(parts)-1 {
			current[part] = value
			return
		}
		next, ok := current[part]
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		currentMap, ok := next.(map[string]any)
		if !ok {
			currentMap = map[string]any{}
			current[part] = currentMap
		}
		current = currentMap
	}
}

// convertProtoPathToJSON converts a proto field path (snake_case) to the JSON equivalent used by protojson.
// protojson uses camelCase by default unless UseProtoNames is set.
func convertProtoPathToJSON(path string) string {
	parts := strings.Split(path, ".")
	result := make([]string, len(parts))
	for i, part := range parts {
		result[i] = toSnakeCase(part)
	}
	return strings.Join(result, ".")
}

func toSnakeCase(s string) string {
	// Proto field names are already snake_case, so this is a pass-through.
	// Kept as a utility in case the path format needs conversion later.
	return s
}

// resolveFieldByPath walks a protobuf message descriptor to validate that a dot-notation path references
// a valid field. Returns an error if any segment of the path doesn't exist.
func resolveFieldByPath(descriptor protoreflect.MessageDescriptor, path string) error {
	parts := strings.Split(path, ".")
	current := descriptor
	for i, part := range parts {
		field := current.Fields().ByName(protoreflect.Name(part))
		if field == nil {
			return fmt.Errorf("field '%s' does not exist at segment '%s' (position %d)",
				path, part, i)
		}
		if i < len(parts)-1 {
			if field.Kind() != protoreflect.MessageKind {
				return fmt.Errorf("field '%s' at segment '%s' is not a message type",
					path, part)
			}
			current = field.Message()
		}
	}
	return nil
}
