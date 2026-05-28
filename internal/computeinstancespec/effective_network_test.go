/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package computeinstancespec

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
)

func TestEffectiveNetworkAttachments(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		spec    *privatev1.ComputeInstanceSpec
		want    []*privatev1.NetworkAttachment
		wantErr bool
	}{
		{
			name:    "nil spec",
			spec:    nil,
			want:    nil,
			wantErr: false,
		},
		{
			name:    "empty",
			spec:    privatev1.ComputeInstanceSpec_builder{}.Build(),
			want:    nil,
			wantErr: false,
		},
		{
			name: "legacy subnet empty string",
			spec: privatev1.ComputeInstanceSpec_builder{
				Subnet: new(""),
			}.Build(),
			want:    nil,
			wantErr: false, // treat as no networking specified
		},
		{
			name: "legacy subnet only",
			spec: privatev1.ComputeInstanceSpec_builder{
				Subnet: new("sn-1"),
			}.Build(),
			want: []*privatev1.NetworkAttachment{
				privatev1.NetworkAttachment_builder{Subnet: "sn-1"}.Build(),
			},
			wantErr: false,
		},
		{
			name: "legacy subnet and security groups",
			spec: privatev1.ComputeInstanceSpec_builder{
				Subnet:         new("sn-1"),
				SecurityGroups: []string{"sg-1", "sg-2"},
			}.Build(),
			want: []*privatev1.NetworkAttachment{
				privatev1.NetworkAttachment_builder{
					Subnet:         "sn-1",
					SecurityGroups: []string{"sg-1", "sg-2"},
				}.Build(),
			},
			wantErr: false,
		},
		{
			name: "legacy security groups only",
			spec: privatev1.ComputeInstanceSpec_builder{
				SecurityGroups: []string{"sg-1"},
			}.Build(),
			want:    nil,
			wantErr: true, // subnet is required when security_groups are set
		},
		{
			name: "network_attachments only",
			spec: privatev1.ComputeInstanceSpec_builder{
				NetworkAttachments: []*privatev1.NetworkAttachment{
					privatev1.NetworkAttachment_builder{Subnet: "a"}.Build(),
					privatev1.NetworkAttachment_builder{Subnet: "b"}.Build(),
				},
			}.Build(),
			want: []*privatev1.NetworkAttachment{
				privatev1.NetworkAttachment_builder{Subnet: "a"}.Build(),
				privatev1.NetworkAttachment_builder{Subnet: "b"}.Build(),
			},
			wantErr: false,
		},
		{
			name: "conflict legacy subnet with network_attachments",
			spec: privatev1.ComputeInstanceSpec_builder{
				Subnet: new("sn-1"),
				NetworkAttachments: []*privatev1.NetworkAttachment{
					privatev1.NetworkAttachment_builder{Subnet: "a"}.Build(),
				},
			}.Build(),
			want:    nil,
			wantErr: true,
		},
		{
			name: "conflict legacy security_groups with network_attachments",
			spec: privatev1.ComputeInstanceSpec_builder{
				SecurityGroups: []string{"sg-1"},
				NetworkAttachments: []*privatev1.NetworkAttachment{
					privatev1.NetworkAttachment_builder{Subnet: "a"}.Build(),
				},
			}.Build(),
			want:    nil,
			wantErr: true,
		},
		{
			name: "conflict deprecated subnet present but empty string with network_attachments",
			spec: privatev1.ComputeInstanceSpec_builder{
				Subnet: new(""),
				NetworkAttachments: []*privatev1.NetworkAttachment{
					privatev1.NetworkAttachment_builder{Subnet: "a"}.Build(),
				},
			}.Build(),
			want:    nil,
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := EffectiveNetworkAttachments(tc.spec)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if diff := cmp.Diff(tc.want, got, protocmp.Transform()); diff != "" {
				t.Fatalf("(-want +got):\n%s", diff)
			}
		})
	}
}
