/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package servers

import (
	"math"
	"net/netip"
	"sort"

	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
)

func validatePoolCIDRFormat(cidrStr string, ipFamily privatev1.IPFamily, idx int) (string, error) {
	prefix, err := netip.ParsePrefix(cidrStr)
	if err != nil {
		return "", grpcstatus.Errorf(grpccodes.InvalidArgument,
			"invalid CIDR format in field 'spec.cidrs[%d]': '%s': %v", idx, cidrStr, err)
	}

	isIPv4 := prefix.Addr().Is4()
	switch ipFamily {
	case privatev1.IPFamily_IP_FAMILY_IPV4:
		if !isIPv4 {
			return "", grpcstatus.Errorf(grpccodes.InvalidArgument,
				"field 'spec.cidrs[%d]' contains IPv6 address but pool ip_family is IPv4: %s", idx, cidrStr)
		}
	case privatev1.IPFamily_IP_FAMILY_IPV6:
		if isIPv4 {
			return "", grpcstatus.Errorf(grpccodes.InvalidArgument,
				"field 'spec.cidrs[%d]' contains IPv4 address but pool ip_family is IPv6: %s", idx, cidrStr)
		}
	}

	return prefix.Masked().String(), nil
}

func validateNoCIDRSelfOverlap(cidrs []string) error {
	for i := 0; i < len(cidrs); i++ {
		for j := i + 1; j < len(cidrs); j++ {
			overlap, err := cidrsOverlap(cidrs[i], cidrs[j])
			if err != nil {
				return grpcstatus.Errorf(grpccodes.Internal, "failed to check intra-pool CIDR overlap")
			}
			if overlap {
				return grpcstatus.Errorf(grpccodes.InvalidArgument,
					"field 'spec.cidrs[%d]' (%s) overlaps with 'spec.cidrs[%d]' (%s) within the same pool",
					i, cidrs[i], j, cidrs[j])
			}
		}
	}
	return nil
}

func calculatePoolCapacity(cidrs []string, ipFamily privatev1.IPFamily) int64 {
	var total int64
	for _, cidr := range cidrs {
		cap := calculateCIDRCapacity(cidr, ipFamily)
		if total > math.MaxInt64-cap {
			return math.MaxInt64
		}
		total += cap
	}
	return total
}

func calculateCIDRCapacity(cidrStr string, ipFamily privatev1.IPFamily) int64 {
	prefix, err := netip.ParsePrefix(cidrStr)
	if err != nil {
		return 0
	}
	bits := prefix.Addr().BitLen()
	ones := prefix.Bits()
	if ones < 0 {
		return 0
	}
	hostBits := bits - ones

	if ipFamily == privatev1.IPFamily_IP_FAMILY_IPV4 {
		total := int64(1) << hostBits
		if hostBits >= 2 {
			return total - 2 // subtract network and broadcast
		}
		return total // /31 = 2 (point-to-point), /32 = 1 (host route)
	}

	// IPv6: all addresses usable; cap at math.MaxInt64 for large prefix lengths.
	if hostBits >= 63 {
		return math.MaxInt64
	}
	return int64(1) << hostBits
}

func cidrSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sortedA := make([]string, len(a))
	sortedB := make([]string, len(b))
	copy(sortedA, a)
	copy(sortedB, b)
	sort.Strings(sortedA)
	sort.Strings(sortedB)
	for i := range sortedA {
		if sortedA[i] != sortedB[i] {
			return false
		}
	}
	return true
}
