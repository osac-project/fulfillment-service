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
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"

	"github.com/prometheus/client_golang/prometheus"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
	"github.com/osac-project/fulfillment-service/internal/events"
)

type PrivateOrganizationsServerBuilder struct {
	logger            *slog.Logger
	notifier          events.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ privatev1.OrganizationsServer = (*PrivateOrganizationsServer)(nil)

type PrivateOrganizationsServer struct {
	privatev1.UnimplementedOrganizationsServer
	logger  *slog.Logger
	generic *GenericServer[*privatev1.Organization]
	dao     *dao.GenericDAO[*privatev1.Organization]
}

func NewPrivateOrganizationsServer() *PrivateOrganizationsServerBuilder {
	return &PrivateOrganizationsServerBuilder{}
}

func (b *PrivateOrganizationsServerBuilder) SetLogger(value *slog.Logger) *PrivateOrganizationsServerBuilder {
	b.logger = value
	return b
}

func (b *PrivateOrganizationsServerBuilder) SetNotifier(value events.Notifier) *PrivateOrganizationsServerBuilder {
	b.notifier = value
	return b
}

func (b *PrivateOrganizationsServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivateOrganizationsServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivateOrganizationsServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivateOrganizationsServerBuilder {
	b.tenancyLogic = value
	return b
}

// SetMetricsRegisterer sets the Prometheus registerer used to register the metrics for the underlying database
// access objects. This is optional. If not set, no metrics will be recorded.
func (b *PrivateOrganizationsServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PrivateOrganizationsServerBuilder {
	b.metricsRegisterer = value
	return b
}

func (b *PrivateOrganizationsServerBuilder) Build() (result *PrivateOrganizationsServer, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}

	// Create the generic server:
	generic, err := NewGenericServer[*privatev1.Organization]().
		SetLogger(b.logger).
		SetService(privatev1.Organizations_ServiceDesc.ServiceName).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	// Create the DAO:
	dao, err := dao.NewGenericDAO[*privatev1.Organization]().
		SetLogger(b.logger).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &PrivateOrganizationsServer{
		logger:  b.logger,
		generic: generic,
		dao:     dao,
	}
	return
}

func (s *PrivateOrganizationsServer) List(ctx context.Context,
	request *privatev1.OrganizationsListRequest) (response *privatev1.OrganizationsListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivateOrganizationsServer) Get(ctx context.Context,
	request *privatev1.OrganizationsGetRequest) (response *privatev1.OrganizationsGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivateOrganizationsServer) Create(ctx context.Context,
	request *privatev1.OrganizationsCreateRequest) (response *privatev1.OrganizationsCreateResponse, err error) {
	// For tenants the name is mandatory:
	object := request.GetObject()
	metadata := object.GetMetadata()
	name := metadata.GetName()
	if name == "" {
		err = grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"field 'metadata.name' is mandatory",
		)
		return
	}

	// For tenants the identifier must be empty or equal to the name. If it is empty it will be set to the name.
	id := object.GetId()
	if id != "" && id != name {
		err = grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"field 'id' must be empty or equal to field 'metadata.name'",
		)
		return
	}
	if id == "" {
		object.SetId(name)
	}

	// The tenant of a tenant must be itself, so either empty or equal to the name. If it is empty it will be set to
	// the name.
	tenant := metadata.GetTenant()
	if tenant != "" && tenant != name {
		err = grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"field 'metadata.tenant' must be empty or equal to field 'metadata.name'",
		)
		return
	}
	if tenant == "" {
		metadata.SetTenant(name)
	}

	// Verify that the e-mail domains are syntactically valid:
	err = s.validateDomains(object.GetSpec().GetDomains())
	if err != nil {
		return
	}

	err = s.generic.Create(ctx, request, &response)
	return
}

func (s *PrivateOrganizationsServer) Update(ctx context.Context,
	request *privatev1.OrganizationsUpdateRequest) (response *privatev1.OrganizationsUpdateResponse, err error) {
	if err = s.validateDomains(request.GetObject().GetSpec().GetDomains()); err != nil {
		return
	}
	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivateOrganizationsServer) Delete(ctx context.Context,
	request *privatev1.OrganizationsDeleteRequest) (response *privatev1.OrganizationsDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivateOrganizationsServer) Signal(ctx context.Context,
	request *privatev1.OrganizationsSignalRequest) (response *privatev1.OrganizationsSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}

// validateDomains checks that all domains in the list are valid DNS hostnames and that there are no duplicates.
// It is safe to call with a nil or empty slice.
func (s *PrivateOrganizationsServer) validateDomains(domains []string) error {
	if len(domains) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(domains))
	for i, domain := range domains {
		if err := s.validateDomain(domain, i); err != nil {
			return err
		}
		if seen[domain] {
			return grpcstatus.Errorf(
				grpccodes.InvalidArgument,
				"field 'spec.domains' contains duplicate domain '%s'",
				domain,
			)
		}
		seen[domain] = true
	}
	return nil
}

// validateDomain checks that a single domain is a syntactically valid DNS hostname suitable for use as an
// e-mail domain.
func (s *PrivateOrganizationsServer) validateDomain(domain string, index int) error {
	if domain == "" {
		return grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"field 'spec.domains[%d]' must not be empty",
			index,
		)
	}
	if len(domain) > 253 {
		return grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"field 'spec.domains[%d]' must be at most 253 characters long, but '%s' has %d characters",
			index, domain, len(domain),
		)
	}
	if net.ParseIP(domain) != nil {
		return grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"field 'spec.domains[%d]' must be a DNS hostname, not an IP address: '%s'",
			index, domain,
		)
	}

	labels := s.splitDomainLabels(domain)
	if len(labels) < 2 {
		return grpcstatus.Errorf(
			grpccodes.InvalidArgument,
			"field 'spec.domains[%d]' must have at least two labels (e.g. 'example.com'), but got '%s'",
			index, domain,
		)
	}
	for _, label := range labels {
		if err := s.validateDomainLabel(label); err != nil {
			return grpcstatus.Errorf(
				grpccodes.InvalidArgument,
				"field 'spec.domains[%d]' contains invalid label in '%s': %s",
				index, domain, err,
			)
		}
	}
	return nil
}

// splitDomainLabels splits a domain name into its dot-separated labels.
func (s *PrivateOrganizationsServer) splitDomainLabels(domain string) []string {
	var labels []string
	start := 0
	for i := 0; i <= len(domain); i++ {
		if i == len(domain) || domain[i] == '.' {
			labels = append(labels, domain[start:i])
			start = i + 1
		}
	}
	return labels
}

// validateDomainLabel checks that a single DNS label is valid per RFC 1035.
func (s *PrivateOrganizationsServer) validateDomainLabel(label string) error {
	if len(label) == 0 {
		return fmt.Errorf("label must not be empty")
	}
	if len(label) > 63 {
		return fmt.Errorf("label must be at most 63 characters long, but has %d", len(label))
	}
	for i, c := range label {
		isLower := c >= 'a' && c <= 'z'
		isDigit := c >= '0' && c <= '9'
		isHyphen := c == '-'
		if !isLower && !isDigit && !isHyphen {
			return fmt.Errorf(
				"label must only contain lowercase letters (a-z), digits (0-9) and hyphens (-), "+
					"but contains '%c' at position %d",
				c, i,
			)
		}
	}
	if label[0] == '-' {
		return fmt.Errorf("label must not start with a hyphen")
	}
	if label[len(label)-1] == '-' {
		return fmt.Errorf("label must not end with a hyphen")
	}
	return nil
}
