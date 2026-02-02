FROM registry.access.redhat.com/ubi10/ubi:10.1-1763341459 AS builder

# Install packages:
RUN \
  dnf install -y \
  git \
  golang \
  && \
  dnf clean all -y


# Copy only the 'go.mod' and 'go.sum' files and try to download the required modules, so that hopefully this will be
# in a layer that can be cached reused for builds that don't change the dependencies.
WORKDIR /source
COPY go.mod go.sum /source
RUN go mod download

# Copy the rest of the source and build the binary:
COPY . /source
RUN \
  version=$(git describe --tags --always) && \
  go build -ldflags="-X github.com/innabox/fulfillment-service/internal/version.id=${version}"

FROM registry.access.redhat.com/ubi10/ubi:10.1-1763341459 AS runtime

RUN \
  dnf install -y \
  openssl \
  && \
  dnf clean all -y

# Install the binary:
COPY --from=builder /source/fulfillment-service /usr/local/bin
