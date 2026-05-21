# First stage is to build the image that contains the image with all the development tools needed to build the binary.
FROM registry.access.redhat.com/hi/go:1.26-builder AS builder

# Set this to 'true' to build the binary with debugging symbols and disabling optimizations.
ARG DEBUG=false

# Install packages:
RUN \
  set -e; \
  pkgs=(git); \
  dnf install -y "${pkgs[@]}"; \
  dnf clean all -y

# Copy only the 'go.mod' and 'go.sum' files and try to download the required modules, so that hopefully this will be
# in a layer that can be cached reused for builds that don't change the dependencies.
WORKDIR /source
COPY go.mod go.sum /source/
RUN \
  set -e; \
  go mod download

# Version can be passed as a build arg to avoid requiring git inside the container.
# This is necessary when building from a git worktree, where the .git reference
# cannot be resolved inside the container context.
ARG VERSION=""

# Copy the rest of the source and build the binary:
COPY . /source/
RUN \
  set -e; \
  version="${VERSION}"; \
  if [[ -z "${version}" ]]; then \
    version=$(git describe --tags --always 2>/dev/null || echo "dev"); \
  fi; \
  gcflags=""; \
  ldflags="-X github.com/osac-project/fulfillment-service/internal/version.id=${version}"; \
  if [[ "${DEBUG}" == "true" ]]; then \
    gcflags="all=-N -l"; \
  fi; \
  go build -gcflags="${gcflags}" -ldflags="${ldflags}" ./cmd/fulfillment-service

# dlv stage — only pulled into the build graph when targeting runtime-debug.
FROM builder AS dlv-builder
RUN go install github.com/go-delve/delve/cmd/dlv@latest

# Common runtime base with the service binary.
FROM registry.access.redhat.com/hi/core-runtime:latest-openssl AS runtime-base
COPY --from=builder /source/fulfillment-service /usr/local/bin

# Debug variant — adds dlv on top of the base; build with: podman build --target runtime-debug .
FROM runtime-base AS runtime-debug
COPY --from=dlv-builder /go/bin/dlv /usr/local/bin

# Default (non-debug) runtime — must stay last so plain `podman build .` targets it.
FROM runtime-base AS runtime
