/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package connect

import (
	"context"
	"fmt"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// ResolveInstance resolves a name or ID to a compute instance ID.
func ResolveInstance(ctx context.Context, conn *grpc.ClientConn, key string) (string, error) {
	client := publicv1.NewComputeInstancesClient(conn)
	listFilter := fmt.Sprintf(
		"this.id == %[1]q || this.metadata.name == %[1]q",
		key,
	)
	resp, err := client.List(ctx, publicv1.ComputeInstancesListRequest_builder{
		Filter: new(listFilter),
		Limit:  proto.Int32(2),
	}.Build())
	if err != nil {
		return "", fmt.Errorf("failed to look up compute instance: %w", err)
	}

	items := resp.GetItems()
	switch len(items) {
	case 0:
		return "", fmt.Errorf("compute instance %q not found", key)
	case 1:
		return items[0].GetId(), nil
	default:
		return "", fmt.Errorf("multiple compute instances match %q; use the ID instead", key)
	}
}
