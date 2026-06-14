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
	"net"

	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const (
	cidrIPv4 = "IPv4"
	cidrIPv6 = "IPv6"
)

// parseAndValidateCIDR parses cidrStr, validates it matches the expected IP version,
// and returns the canonical CIDR notation.
func parseAndValidateCIDR(cidrStr string, ipVersion string) (string, error) {
	_, network, err := net.ParseCIDR(cidrStr)
	if err != nil {
		return "", grpcstatus.Errorf(grpccodes.InvalidArgument,
			"invalid %s CIDR format '%s': %v", ipVersion, cidrStr, err)
	}

	isIPv4 := network.IP.To4() != nil
	if ipVersion == cidrIPv4 && !isIPv4 {
		return "", grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'ipv4_cidr' contains IPv6 address: %s", cidrStr)
	}
	if ipVersion == cidrIPv6 && isIPv4 {
		return "", grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'ipv6_cidr' contains IPv4 address: %s", cidrStr)
	}

	return network.String(), nil
}

// validateCIDR validates a CIDR string and checks if it matches the expected IP version.
func validateCIDR(cidrStr string, ipVersion string) error {
	_, err := parseAndValidateCIDR(cidrStr, ipVersion)
	return err
}
