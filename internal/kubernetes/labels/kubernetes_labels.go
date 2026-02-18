/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package labels

import (
	"fmt"

	"github.com/osac-project/fulfillment-service/internal/kubernetes/gvks"
)

// ClusterOrderUuid is the label where the fulfillment API will write the identifier of the order.
var ClusterOrderUuid = fmt.Sprintf("%s/%s", gvks.ClusterOrder.Group, "clusterorder-uuid")

// ComputeInstanceUuid is the label where the fulfillment API will write the identifier of the compute instance.
var ComputeInstanceUuid = fmt.Sprintf("%s/%s", gvks.ComputeInstance.Group, "computeinstance-uuid")

// HostUuid is the label where the fulfillment API will write the identifier of the host.
var HostUuid = fmt.Sprintf("%s/%s", gvks.Host.Group, "host-uuid")

// HostPoolUuid is the label where the fulfillment API will write the identifier of the host pool.
var HostPoolUuid = fmt.Sprintf("%s/%s", gvks.HostPool.Group, "hostpool-uuid")
