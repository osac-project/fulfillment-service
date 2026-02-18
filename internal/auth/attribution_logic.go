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

	"github.com/osac-project/fulfillment-service/internal/collections"
)

// AttributionLogic defines the logic for determining what users and group should be assisged as the creators of
// objects.
//
//go:generate mockgen -destination=attribution_logic_mock.go -package=auth . AttributionLogic
type AttributionLogic interface {
	// DetermineAssignedCreators calculates and returns the list of creators that should be assigned to an object
	// being created. The context will contain authentication and authorization information that can be used to
	// determine the appropriate creators.
	DetermineAssignedCreators(ctx context.Context) (collections.Set[string], error)
}
