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
	"context"
	"fmt"
	"log/slog"

	"github.com/osac-project/fulfillment-service/internal/collections"
)

// DefaultAttributionLogicBuilder contains the data and logic needed to create default attribution logic.
type DefaultAttributionLogicBuilder struct {
	logger *slog.Logger
}

// DefaultAttributionLogic is the default implementation of AttributionLogic that extracts the subject from the context
// and returns the subject name as the creator.
type DefaultAttributionLogic struct {
	logger *slog.Logger
}

// NewDefaultAttributionLogic creates a new builder for default attribution logic.
func NewDefaultAttributionLogic() *DefaultAttributionLogicBuilder {
	return &DefaultAttributionLogicBuilder{}
}

// SetLogger sets the logger that will be used by the attribution logic. This is mandatory.
func (b *DefaultAttributionLogicBuilder) SetLogger(value *slog.Logger) *DefaultAttributionLogicBuilder {
	b.logger = value
	return b
}

// Build creates the default attribution logic that extracts the subject from the auth context and returns the name as
// the creator.
func (b *DefaultAttributionLogicBuilder) Build() (result *DefaultAttributionLogic, err error) {
	// Check that the logger has been set:
	if b.logger == nil {
		err = fmt.Errorf("logger is mandatory")
		return
	}

	// Create the attribution logic:
	result = &DefaultAttributionLogic{
		logger: b.logger,
	}
	return
}

// DetermineAssignedCreators extracts the subject from the auth context and returns the subject name as the creator.
func (l *DefaultAttributionLogic) DetermineAssignedCreators(ctx context.Context) (result collections.Set[string], err error) {
	subject := SubjectFromContext(ctx)
	result = collections.NewSet(subject.User)
	return
}
