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
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/santhosh-tekuri/jsonschema/v6"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
)

var (
	celSyntaxEnv     *cel.Env
	celSyntaxEnvOnce sync.Once
	celSyntaxEnvErr  error
)

// validateCELSyntax checks that a filter string is a syntactically valid, complete CEL expression.
// This prevents filter bypass attacks where a malicious filter like "true) || (true" could
// break out of parenthesized composition and change operator precedence.
func validateCELSyntax(filter string) error {
	celSyntaxEnvOnce.Do(func() {
		celSyntaxEnv, celSyntaxEnvErr = cel.NewEnv()
	})
	if celSyntaxEnvErr != nil {
		return fmt.Errorf("failed to create CEL environment: %w", celSyntaxEnvErr)
	}
	_, issues := celSyntaxEnv.Parse(filter)
	if issues != nil && issues.Err() != nil {
		return fmt.Errorf("syntax error: %w", issues.Err())
	}
	return nil
}

// catalogItem is implemented by both ClusterCatalogItem and ComputeInstanceCatalogItem.
type catalogItem interface {
	proto.Message
	GetPublished() bool
	GetTemplate() string
	GetFieldDefinitions() []*privatev1.FieldDefinition
	GetMetadata() *privatev1.Metadata
}

// validateFieldDefinitions checks that field definitions are well-formed. Non-editable fields must have a default
// value, otherwise provisioning from this catalog item will always fail.
func validateFieldDefinitions(fieldDefinitions []*privatev1.FieldDefinition) error {
	for _, fd := range fieldDefinitions {
		if !fd.GetEditable() && !fd.HasDefault() {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"non-editable field '%s' must have a default value", fd.GetPath())
		}
	}
	return nil
}

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

	marshaller := protojson.MarshalOptions{UseProtoNames: true}
	specJSON, err := marshaller.Marshal(spec)
	if err != nil {
		return grpcstatus.Errorf(grpccodes.Internal, "failed to marshal spec: %v", err)
	}

	var specMap map[string]any
	if err := json.Unmarshal(specJSON, &specMap); err != nil {
		return grpcstatus.Errorf(grpccodes.Internal, "failed to parse spec: %v", err)
	}

	compiler := jsonschema.NewCompiler()

	for _, fd := range fieldDefinitions {
		path := fd.GetPath()
		if path == "" {
			continue
		}

		defaultVal := fd.GetDefault()
		userVal, userHasValue := getNestedValue(specMap, path)

		if !fd.GetEditable() {
			if defaultVal == nil {
				return grpcstatus.Errorf(grpccodes.Internal,
					"catalog item misconfigured: non-editable field '%s' has no default value", path)
			}
			if err := applyDefault(specMap, path, defaultVal); err != nil {
				return err
			}
		} else {
			if userHasValue && userVal != nil {
				schema := fd.GetValidationSchema()
				if schema != "" {
					if err := validateAgainstSchema(compiler, path, userVal, schema); err != nil {
						return err
					}
				}
			} else {
				if err := applyDefault(specMap, path, defaultVal); err != nil {
					return err
				}
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

// validateCatalogItemAccess checks that a catalog item is published and not deleted.
// Tenant visibility is enforced by the GenericDAO's tenancy logic at the query level.
func validateCatalogItemAccess(item catalogItem, ref string) error {
	if item.GetMetadata().HasDeletionTimestamp() {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"catalog item '%s' has been deleted", ref)
	}
	if !item.GetPublished() {
		return grpcstatus.Errorf(grpccodes.NotFound,
			"catalog item '%s' is not published", ref)
	}
	return nil
}

func applyDefault(specMap map[string]any, path string, defaultVal *structpb.Value) error {
	if defaultVal == nil {
		return nil
	}
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
	return nil
}

func validateAgainstSchema(compiler *jsonschema.Compiler, path string, value any, schemaStr string) error {
	resourceName := "schema_" + strings.ReplaceAll(path, ".", "_") + ".json"
	var schemaDoc any
	if err := json.Unmarshal([]byte(schemaStr), &schemaDoc); err != nil {
		return grpcstatus.Errorf(grpccodes.Internal,
			"invalid validation schema for field '%s': %v", path, err)
	}
	if err := compiler.AddResource(resourceName, schemaDoc); err != nil {
		return grpcstatus.Errorf(grpccodes.Internal,
			"invalid validation schema for field '%s': %v", path, err)
	}
	schema, err := compiler.Compile(resourceName)
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
