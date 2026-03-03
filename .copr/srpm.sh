#!/bin/bash
#
# Copyright (c) 2025 Red Hat, Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
# License. You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
# language governing permissions and limitations under the License.
#

set -e

# Name of the project:
name="fulfillment-service"

# Directory containing this script:
here="$(dirname "$(readlink -f "$0")")"

# Output directory:
outdir="${outdir:-$here}"

# Install git if not available:
if ! command -v git >/dev/null 2>&1; then
  dnf install -y git
fi

# Calculate the version from git. This uses git describe to get a version string based on the most recent tag. If there
# are commits after the tag, the version will include the commit count and short hash. Any leading 'v' is stripped and
# the format is converted to use RPM's caret notation for post-release versions (e.g., 0.0.20^29.gfa27fa8).
version=$(
  git describe --tags --always 2>/dev/null |
  sed \
    -e 's/^v//' \
    -e 's/-\([0-9]*\)-g/^\1.g/'
)

# Calculate the date for the changelog entry in the format required by RPM:
date=$(date +'%a %b %d %Y')

# Create the tarball:
git -C "${here}/.." archive \
  --prefix="${name}-${version}/" \
  --output="${outdir}/${name}-${version}.tar.gz" \
  HEAD

# Create the spec file:
sed \
  -e "s/@version@/${version}/g" \
  -e "s/@date@/${date}/g" \
  "${here}/${name}.spec.in" > "${here}/${name}.spec"

# Build the SRPM:
rpmbuild \
  --define "_srcrpmdir ${outdir}" \
  --define "_sourcedir ${outdir}" \
  -bs "${here}/${name}.spec"
