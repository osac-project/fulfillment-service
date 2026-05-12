/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package organization

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/types/known/timestamppb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/controllers/finalizers"
	"github.com/osac-project/fulfillment-service/internal/idp"
)

var _ = Describe("Tenant Validation", func() {
	It("should succeed with exactly one tenant", func() {
		organization := privatev1.Organization_builder{
			Metadata: privatev1.Metadata_builder{
				Tenants: []string{"tenant-1"},
			}.Build(),
		}.Build()

		task := &task{
			organization: organization,
		}

		err := task.validateTenant()
		Expect(err).ToNot(HaveOccurred())
	})

	It("should fail with zero tenants", func() {
		organization := privatev1.Organization_builder{
			Metadata: privatev1.Metadata_builder{
				Tenants: []string{},
			}.Build(),
		}.Build()

		task := &task{
			organization: organization,
		}

		err := task.validateTenant()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exactly one tenant"))
	})

	It("should fail with multiple tenants", func() {
		organization := privatev1.Organization_builder{
			Metadata: privatev1.Metadata_builder{
				Tenants: []string{"tenant-1", "tenant-2"},
			}.Build(),
		}.Build()

		task := &task{
			organization: organization,
		}

		err := task.validateTenant()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exactly one tenant"))
	})

	It("should fail with missing metadata", func() {
		organization := privatev1.Organization_builder{}.Build()

		task := &task{
			organization: organization,
		}

		err := task.validateTenant()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("exactly one tenant"))
	})
})

var _ = Describe("Finalizer Management", func() {
	It("should add finalizer on first call", func() {
		organization := privatev1.Organization_builder{
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{},
			}.Build(),
		}.Build()

		task := &task{
			organization: organization,
		}

		added := task.addFinalizer()
		Expect(added).To(BeTrue())
		Expect(organization.GetMetadata().GetFinalizers()).To(ContainElement(finalizers.Controller))
	})

	It("should not add finalizer if already present", func() {
		organization := privatev1.Organization_builder{
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller},
			}.Build(),
		}.Build()

		task := &task{
			organization: organization,
		}

		added := task.addFinalizer()
		Expect(added).To(BeFalse())
		Expect(organization.GetMetadata().GetFinalizers()).To(HaveLen(1))
	})

	It("should return immediately after adding finalizer", func() {
		organization := privatev1.Organization_builder{
			Metadata: privatev1.Metadata_builder{
				Tenants: []string{"tenant-1"},
			}.Build(),
		}.Build()

		task := &task{
			organization: organization,
		}

		err := task.update(context.Background())
		Expect(err).ToNot(HaveOccurred())

		Expect(organization.GetMetadata().GetFinalizers()).To(ContainElement(finalizers.Controller))
		Expect(organization.HasStatus()).To(BeFalse())
	})
})

var _ = Describe("Default Values", func() {
	It("should set default status if missing", func() {
		organization := privatev1.Organization_builder{}.Build()

		task := &task{
			organization: organization,
		}

		task.setDefaults()
		Expect(organization.HasStatus()).To(BeTrue())
		Expect(organization.GetStatus().GetState()).To(Equal(privatev1.OrganizationState_ORGANIZATION_STATE_PENDING))
	})

	It("should set default state if unspecified", func() {
		organization := privatev1.Organization_builder{
			Status: privatev1.OrganizationStatus_builder{}.Build(),
		}.Build()

		task := &task{
			organization: organization,
		}

		task.setDefaults()
		Expect(organization.GetStatus().GetState()).To(Equal(privatev1.OrganizationState_ORGANIZATION_STATE_PENDING))
	})

	It("should not override existing state", func() {
		organization := privatev1.Organization_builder{
			Status: privatev1.OrganizationStatus_builder{
				State: privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED,
			}.Build(),
		}.Build()

		task := &task{
			organization: organization,
		}

		task.setDefaults()
		Expect(organization.GetStatus().GetState()).To(Equal(privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED))
	})
})

var _ = Describe("IDP Sync", func() {
	var (
		ctx        context.Context
		ctrl       *gomock.Controller
		mockClient *idp.MockClient
		idpManager *idp.OrganizationManager
		reconciler *function
	)

	BeforeEach(func() {
		var err error
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		mockClient = idp.NewMockClient(ctrl)

		idpManager, err = idp.NewOrganizationManager().
			SetLogger(logger).
			SetClient(mockClient).
			Build()
		Expect(err).ToNot(HaveOccurred())

		reconciler = &function{
			logger:     logger,
			idpManager: idpManager,
		}
	})

	It("should sync organization to IDP successfully", func() {
		organization := privatev1.Organization_builder{
			Id: "org-123",
			Metadata: privatev1.Metadata_builder{
				Name:       "test-org",
				Finalizers: []string{finalizers.Controller},
				Tenants:    []string{"tenant-1"},
			}.Build(),
		}.Build()

		mockClient.EXPECT().
			CreateOrganization(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, org *idp.Organization) (*idp.Organization, error) {
				Expect(org.Name).To(Equal("test-org"))
				return &idp.Organization{
					Name:    "test-org",
					Enabled: true,
				}, nil
			}).
			Times(1)

		mockClient.EXPECT().
			CreateUser(gomock.Any(), "test-org", gomock.Any()).
			DoAndReturn(func(ctx context.Context, orgName string, user *idp.User) (*idp.User, error) {
				Expect(user.Username).To(Equal("test-org-osac-break-glass"))
				Expect(user.Email).To(Equal("break-glass@test-org.osac.local"))
				user.ID = "user-123"
				return user, nil
			}).
			Times(1)

		mockClient.EXPECT().
			AssignIdpManagerPermissions(gomock.Any(), "user-123").
			Return(nil).
			Times(1)

		task := &task{
			r:            reconciler,
			organization: organization,
		}

		err := task.update(ctx)
		Expect(err).ToNot(HaveOccurred())

		Expect(organization.GetStatus().GetState()).To(Equal(privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED))
		Expect(organization.GetStatus().GetIdpOrganizationName()).To(Equal("test-org"))
		Expect(organization.GetStatus().GetBreakGlassUserId()).To(Equal("user-123"))
		Expect(organization.GetStatus().HasBreakGlassCredentials()).To(BeTrue())
		Expect(organization.GetStatus().GetBreakGlassCredentials().GetUsername()).To(Equal("test-org-osac-break-glass"))
	})

	It("should set state to PENDING before sync", func() {
		organization := privatev1.Organization_builder{
			Metadata: privatev1.Metadata_builder{
				Name:       "test-org",
				Finalizers: []string{finalizers.Controller},
				Tenants:    []string{"tenant-1"},
			}.Build(),
		}.Build()

		mockClient.EXPECT().
			CreateOrganization(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, org *idp.Organization) (*idp.Organization, error) {
				Expect(organization.GetStatus().GetState()).To(Equal(privatev1.OrganizationState_ORGANIZATION_STATE_PENDING))
				return org, nil
			}).
			Times(1)

		mockClient.EXPECT().
			CreateUser(gomock.Any(), "test-org", gomock.Any()).
			Return(&idp.User{ID: "user-123"}, nil).
			Times(1)

		mockClient.EXPECT().
			AssignIdpManagerPermissions(gomock.Any(), "user-123").
			Return(nil).
			Times(1)

		task := &task{
			r:            reconciler,
			organization: organization,
		}

		err := task.update(ctx)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should set FAILED state on IDP error", func() {
		organization := privatev1.Organization_builder{
			Metadata: privatev1.Metadata_builder{
				Name:       "test-org",
				Finalizers: []string{finalizers.Controller},
				Tenants:    []string{"tenant-1"},
			}.Build(),
		}.Build()

		mockClient.EXPECT().
			CreateOrganization(gomock.Any(), gomock.Any()).
			Return(nil, fmt.Errorf("IDP connection timeout")).
			Times(1)

		task := &task{
			r:            reconciler,
			organization: organization,
		}

		err := task.update(ctx)
		Expect(err).ToNot(HaveOccurred())

		Expect(organization.GetStatus().GetState()).To(Equal(privatev1.OrganizationState_ORGANIZATION_STATE_FAILED))
		Expect(organization.GetStatus().GetMessage()).To(ContainSubstring("Organization creation in IDP failed"))
		Expect(organization.GetStatus().GetMessage()).To(ContainSubstring("IDP connection timeout"))
		Expect(organization.GetStatus().GetIdpOrganizationName()).To(BeEmpty())
		Expect(organization.GetStatus().GetBreakGlassUserId()).To(BeEmpty())
	})

	It("should not return error on IDP failure", func() {
		organization := privatev1.Organization_builder{
			Metadata: privatev1.Metadata_builder{
				Name:       "test-org",
				Finalizers: []string{finalizers.Controller},
				Tenants:    []string{"tenant-1"},
			}.Build(),
		}.Build()

		mockClient.EXPECT().
			CreateOrganization(gomock.Any(), gomock.Any()).
			Return(nil, fmt.Errorf("organization already exists")).
			Times(1)

		task := &task{
			r:            reconciler,
			organization: organization,
		}

		err := task.update(ctx)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("Deletion", func() {
	var (
		ctx        context.Context
		ctrl       *gomock.Controller
		mockClient *idp.MockClient
		idpManager *idp.OrganizationManager
		reconciler *function
	)

	BeforeEach(func() {
		var err error
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		mockClient = idp.NewMockClient(ctrl)

		idpManager, err = idp.NewOrganizationManager().
			SetLogger(logger).
			SetClient(mockClient).
			Build()
		Expect(err).ToNot(HaveOccurred())

		reconciler = &function{
			logger:     logger,
			idpManager: idpManager,
		}
	})

	It("should delete organization from IDP and remove finalizer", func() {
		deletionTimestamp := timestamppb.New(time.Now())
		organization := privatev1.Organization_builder{
			Id: "org-123",
			Metadata: privatev1.Metadata_builder{
				Name:              "test-org",
				Finalizers:        []string{finalizers.Controller},
				DeletionTimestamp: deletionTimestamp,
			}.Build(),
			Status: privatev1.OrganizationStatus_builder{
				State:               privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED,
				IdpOrganizationName: "test-org",
				BreakGlassUserId:    "user-123",
			}.Build(),
		}.Build()

		mockClient.EXPECT().
			DeleteOrganization(gomock.Any(), "test-org").
			Return(nil).
			Times(1)

		task := &task{
			r:            reconciler,
			organization: organization,
		}

		err := task.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(organization.GetMetadata().GetFinalizers()).ToNot(ContainElement(finalizers.Controller))
	})

	It("should skip IDP deletion and remove finalizer when organization not synced", func() {
		deletionTimestamp := timestamppb.New(time.Now())
		organization := privatev1.Organization_builder{
			Id: "org-123",
			Metadata: privatev1.Metadata_builder{
				Name:              "test-org",
				Finalizers:        []string{finalizers.Controller},
				DeletionTimestamp: deletionTimestamp,
			}.Build(),
			Status: privatev1.OrganizationStatus_builder{
				State: privatev1.OrganizationState_ORGANIZATION_STATE_PENDING,
			}.Build(),
		}.Build()

		task := &task{
			r:            reconciler,
			organization: organization,
		}

		err := task.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(organization.GetMetadata().GetFinalizers()).ToNot(ContainElement(finalizers.Controller))
	})

	It("should skip IDP deletion and remove finalizer when idp_organization_name is empty", func() {
		deletionTimestamp := timestamppb.New(time.Now())
		organization := privatev1.Organization_builder{
			Id: "org-123",
			Metadata: privatev1.Metadata_builder{
				Name:              "test-org",
				Finalizers:        []string{finalizers.Controller},
				DeletionTimestamp: deletionTimestamp,
			}.Build(),
			Status: privatev1.OrganizationStatus_builder{
				State:               privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED,
				IdpOrganizationName: "",
			}.Build(),
		}.Build()

		task := &task{
			r:            reconciler,
			organization: organization,
		}

		err := task.delete(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(organization.GetMetadata().GetFinalizers()).ToNot(ContainElement(finalizers.Controller))
	})

	It("should return error on IDP deletion failure and keep finalizer", func() {
		deletionTimestamp := timestamppb.New(time.Now())
		organization := privatev1.Organization_builder{
			Id: "org-123",
			Metadata: privatev1.Metadata_builder{
				Name:              "test-org",
				Finalizers:        []string{finalizers.Controller},
				DeletionTimestamp: deletionTimestamp,
			}.Build(),
			Status: privatev1.OrganizationStatus_builder{
				State:               privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED,
				IdpOrganizationName: "test-org",
				BreakGlassUserId:    "user-123",
			}.Build(),
		}.Build()

		mockClient.EXPECT().
			DeleteOrganization(gomock.Any(), "test-org").
			Return(fmt.Errorf("IDP connection timeout")).
			Times(1)

		task := &task{
			r:            reconciler,
			organization: organization,
		}

		err := task.delete(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to delete IDP organization"))
		Expect(err.Error()).To(ContainSubstring("IDP connection timeout"))
		Expect(organization.GetMetadata().GetFinalizers()).To(ContainElement(finalizers.Controller))
	})

	It("should remove finalizer when called", func() {
		organization := privatev1.Organization_builder{
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller, "other-finalizer"},
			}.Build(),
		}.Build()

		task := &task{
			organization: organization,
		}

		task.removeFinalizer()
		Expect(organization.GetMetadata().GetFinalizers()).ToNot(ContainElement(finalizers.Controller))
		Expect(organization.GetMetadata().GetFinalizers()).To(ContainElement("other-finalizer"))
	})

	It("should handle removal when finalizer not present", func() {
		organization := privatev1.Organization_builder{
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{"other-finalizer"},
			}.Build(),
		}.Build()

		task := &task{
			organization: organization,
		}

		task.removeFinalizer()
		Expect(organization.GetMetadata().GetFinalizers()).To(HaveLen(1))
		Expect(organization.GetMetadata().GetFinalizers()).To(ContainElement("other-finalizer"))
	})
})

var _ = Describe("Skip Reconciliation", func() {
	It("should skip reconciliation for synced organizations", func() {
		organization := privatev1.Organization_builder{
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller},
				Tenants:    []string{"tenant-1"},
			}.Build(),
			Status: privatev1.OrganizationStatus_builder{
				State:               privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED,
				IdpOrganizationName: "test-org",
				BreakGlassUserId:    "user-123",
			}.Build(),
		}.Build()

		task := &task{
			organization: organization,
		}

		err := task.update(context.Background())
		Expect(err).ToNot(HaveOccurred())
	})

	It("should skip reconciliation for failed organizations", func() {
		msg := "Previous sync failed"
		organization := privatev1.Organization_builder{
			Metadata: privatev1.Metadata_builder{
				Finalizers: []string{finalizers.Controller},
				Tenants:    []string{"tenant-1"},
			}.Build(),
			Status: privatev1.OrganizationStatus_builder{
				State:   privatev1.OrganizationState_ORGANIZATION_STATE_FAILED,
				Message: &msg,
			}.Build(),
		}.Build()

		task := &task{
			organization: organization,
		}

		err := task.update(context.Background())
		Expect(err).ToNot(HaveOccurred())
	})

})
