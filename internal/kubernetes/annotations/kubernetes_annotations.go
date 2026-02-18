/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package annotations

import (
	"fmt"

	"github.com/osac-project/fulfillment-service/internal/kubernetes/gvks"
)

// ComputeInstanceTenant is the annotation where the fulfillment API will write the tenant of the compute instance.
var ComputeInstanceTenant = fmt.Sprintf("%s/tenant", gvks.ComputeInstance.Group)
