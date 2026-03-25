# This is the base image, both for the builder and runtime containers.
ARG BASE=registry.access.redhat.com/ubi10/ubi:10.1-1773895909

# First stage is to build the image that contains the image with all the development tools needed to build the binary.
FROM ${BASE} AS builder

# Set this to 'true' to build the binary with debugging symbols and disabling optimizations.
ARG DEBUG=false

# Install packages:
RUN \
  set -e; \
  pkgs=(git golang); \
  dnf install -y "${pkgs[@]}"; \
  dnf clean all -y

# Copy only the 'go.mod' and 'go.sum' files and try to download the required modules, so that hopefully this will be
# in a layer that can be cached reused for builds that don't change the dependencies.
WORKDIR /source
COPY go.mod go.sum /source/
RUN \
  set -e; \
  go mod download

# Copy the rest of the source and build the binary:
COPY . /source/
RUN \
  set -e; \
  version=$(git describe --tags --always); \
  gcflags=""; \
  ldflags="-X github.com/osac-project/fulfillment-service/internal/version.id=${version}"; \
  if [[ "${DEBUG}" == "true" ]]; then \
    gcflags="all=-N -l"; \
  fi; \
  go build -gcflags="${gcflags}" -ldflags="${ldflags}" ./cmd/fulfillment-service

# Second stage is to build the image that contains the binary, without the development tools, except the debugger
# if enabled.
FROM ${BASE} AS runtime

# Set this to 'true' to include the 'dlv' debugger in the image.
ARG DEBUG=false

# Install packages:
RUN \
  set -e; \
  pkgs=(openssl); \
  if [[ "${DEBUG}" == "true" ]]; then \
    pkgs+=(delve); \
  fi; \
  dnf install -y "${pkgs[@]}"; \
  dnf clean all -y

# Install the binary:
COPY --from=builder /source/fulfillment-service /usr/local/bin
