/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package it

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/controllers/finalizers"
	"github.com/osac-project/fulfillment-service/internal/uuid"
)

func createTenant(ctx context.Context, client privatev1.TenantsClient, name string) string {
	createResponse, err := client.Create(ctx, privatev1.TenantsCreateRequest_builder{
		Object: privatev1.Tenant_builder{
			Metadata: privatev1.Metadata_builder{
				Name: name,
			}.Build(),
		}.Build(),
	}.Build())
	Expect(err).ToNot(HaveOccurred())
	id := createResponse.GetObject().GetId()
	DeferCleanup(func() {
		_, _ = client.Delete(ctx, privatev1.TenantsDeleteRequest_builder{
			Id: id,
		}.Build())
		status, ok := grpcstatus.FromError(err)
		if ok && status.Code() == grpccodes.NotFound {
			return
		}
		Expect(err).ToNot(HaveOccurred())
	})
	return id
}

func deleteProject(ctx context.Context, client privatev1.ProjectsClient, id string) {
	// Recursively find and delete all the child projects:
	listFilter := fmt.Sprintf("this.metadata.project == %q && !has(this.metadata.deletion_timestamp)", id)
	listRequest := privatev1.ProjectsListRequest_builder{
		Filter: &listFilter,
	}.Build()
	for {
		listResponse, err := client.List(ctx, listRequest)
		Expect(err).ToNot(HaveOccurred())
		if listResponse.GetTotal() == 0 {
			break
		}
		listItems := listResponse.GetItems()
		for _, listItem := range listItems {
			deleteProject(ctx, client, listItem.GetId())
		}
	}

	// Delete this project, and wait for it to be fully removed::
	_, err := client.Delete(ctx, privatev1.ProjectsDeleteRequest_builder{
		Id: id,
	}.Build())
	Expect(err).ToNot(HaveOccurred())
	Eventually(
		func(g Gomega) {
			_, err := client.Get(ctx, privatev1.ProjectsGetRequest_builder{
				Id: id,
			}.Build())
			g.Expect(err).To(HaveOccurred())
			status, ok := grpcstatus.FromError(err)
			g.Expect(ok).To(BeTrue())
			g.Expect(status.Code()).To(Equal(grpccodes.NotFound))
		},
		time.Minute,
		time.Second,
	).Should(Succeed())
}

func deleteTenant(ctx context.Context, tenantsClient privatev1.TenantsClient, projectsClient privatev1.ProjectsClient,
	id string) {
	// Before deleting the tenant we need to delete all the projects associated with it:
	listProjectsFilter := fmt.Sprintf("this.metadata.tenant == %q && !has(this.metadata.deletion_timestamp)", id)
	listProjectsRequest := privatev1.ProjectsListRequest_builder{
		Filter: &listProjectsFilter,
	}.Build()
	for {
		listProjectsResponse, err := projectsClient.List(ctx, listProjectsRequest)
		Expect(err).ToNot(HaveOccurred())
		if listProjectsResponse.GetTotal() == 0 {
			break
		}
		listProjectsItems := listProjectsResponse.GetItems()
		for _, listProjectItem := range listProjectsItems {
			deleteProject(ctx, projectsClient, listProjectItem.GetId())
		}
	}

	// Now we can delete the tenant:
	_, err := tenantsClient.Delete(ctx, privatev1.TenantsDeleteRequest_builder{
		Id: id,
	}.Build())
	Expect(err).ToNot(HaveOccurred())
	Eventually(
		func(g Gomega) {
			_, err := tenantsClient.Get(ctx, privatev1.TenantsGetRequest_builder{
				Id: id,
			}.Build())
			g.Expect(err).To(HaveOccurred())
		},
		time.Minute,
		time.Second,
	).Should(Succeed())
}

func waitForTenantSynced(ctx context.Context, client privatev1.TenantsClient, id string) {
	Eventually(
		func(g Gomega) {
			getResponse, err := client.Get(ctx, privatev1.TenantsGetRequest_builder{
				Id: id,
			}.Build())
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(getResponse.GetObject().GetStatus().GetState()).To(
				Equal(privatev1.TenantState_TENANT_STATE_SYNCED),
			)
			g.Expect(getResponse.GetObject().GetStatus().GetBreakGlassUserId()).ToNot(BeEmpty())
		},
		time.Minute,
		time.Second,
	).Should(Succeed())
}

func verifyTenantInKeycloak(ctx context.Context, name string) {
	code, body, err := tool.KeycloakAdminRequest(ctx, http.MethodGet,
		fmt.Sprintf("/organizations?exact=true&search=%s", name), nil)
	Expect(err).ToNot(HaveOccurred())
	Expect(code).To(Equal(http.StatusOK))
	var kcTenants []map[string]any
	Expect(json.Unmarshal(body, &kcTenants)).To(Succeed())
	Expect(kcTenants).ToNot(BeEmpty())
}

func verifyTenantRemovedFromKeycloak(ctx context.Context, name string) {
	Eventually(
		func(g Gomega) {
			code, body, err := tool.KeycloakAdminRequest(ctx, http.MethodGet,
				fmt.Sprintf("/organizations?exact=true&search=%s", name), nil)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(code).To(Equal(http.StatusOK))
			var kcTenants []map[string]any
			g.Expect(json.Unmarshal(body, &kcTenants)).To(Succeed())
			g.Expect(kcTenants).To(BeEmpty())
		},
		time.Minute,
		time.Second,
	).Should(Succeed())
}

// loginAsBreakGlass creates a gRPC connection authenticated as the break-glass user
// for the given tenant. It waits for the tenant to reach SYNCED, retrieves or sets
// the break-glass password, clears the UPDATE_PASSWORD required action, and returns
// a gRPC connection and the token source. The connection is registered for cleanup.
func loginAsBreakGlass(
	ctx context.Context,
	client privatev1.TenantsClient,
	name, id string,
) (*grpc.ClientConn, auth.TokenSource) {
	var breakGlassUserId string
	var breakGlassUsername string
	var breakGlassPassword string
	Eventually(
		func(g Gomega) {
			getResponse, err := client.Get(ctx, privatev1.TenantsGetRequest_builder{
				Id: id,
			}.Build())
			g.Expect(err).ToNot(HaveOccurred())
			status := getResponse.GetObject().GetStatus()
			g.Expect(status.GetState()).To(
				Equal(privatev1.TenantState_TENANT_STATE_SYNCED),
			)
			g.Expect(status.GetBreakGlassUserId()).ToNot(BeEmpty())
			breakGlassUserId = status.GetBreakGlassUserId()
			creds := status.GetBreakGlassCredentials()
			if creds != nil {
				breakGlassUsername = creds.GetUsername()
				breakGlassPassword = creds.GetPassword()
			}
		},
		time.Minute,
		time.Second,
	).Should(Succeed())

	if breakGlassPassword == "" {
		breakGlassPassword = "test-break-glass-pass-123!"
		breakGlassUsername = fmt.Sprintf("%s-osac-break-glass", name)
	}

	code, _, err := tool.KeycloakAdminRequest(ctx, http.MethodPut,
		fmt.Sprintf("/users/%s/reset-password", breakGlassUserId),
		map[string]any{
			"type":      "password",
			"value":     breakGlassPassword,
			"temporary": false,
		})
	Expect(err).ToNot(HaveOccurred())
	Expect(code).To(Equal(http.StatusNoContent))

	code, _, err = tool.KeycloakAdminRequest(ctx, http.MethodPut,
		fmt.Sprintf("/users/%s", breakGlassUserId),
		map[string]any{
			"requiredActions": []string{},
		})
	Expect(err).ToNot(HaveOccurred())
	Expect(code).To(Equal(http.StatusNoContent))

	tokenSource, err := tool.makeKeycloakTokenSource(ctx, breakGlassUsername, breakGlassPassword)
	Expect(err).ToNot(HaveOccurred())

	conn, err := tool.makeGrpcConn(internalServiceAddr, tokenSource)
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(func() {
		_ = conn.Close()
	})

	return conn, tokenSource
}

var _ = Describe("Tenant lifecycle", func() {
	var (
		tenantsClient  privatev1.TenantsClient
		projectsClient privatev1.ProjectsClient
	)

	BeforeEach(func() {
		tenantsClient = privatev1.NewTenantsClient(tool.InternalView().AdminConn())
		projectsClient = privatev1.NewProjectsClient(tool.InternalView().AdminConn())
	})

	It("CRUD happy flow", func(ctx context.Context) {
		name := fmt.Sprintf("test-%s", uuid.New())

		By(fmt.Sprintf("Creating tenant %q", name))
		id := createTenant(ctx, tenantsClient, name)

		By("Waiting for tenant to reach SYNCED state")
		waitForTenantSynced(ctx, tenantsClient, id)

		By("Verifying tenant fields via Get")
		getResponse, err := tenantsClient.Get(ctx, privatev1.TenantsGetRequest_builder{
			Id: id,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		object := getResponse.GetObject()
		Expect(object.GetMetadata().GetName()).To(Equal(name))
		Expect(object.GetStatus().GetBreakGlassUserId()).ToNot(BeEmpty())
		Expect(object.GetStatus().GetIdpTenantName()).To(Equal(name))

		By("Verifying tenant appears in List")
		listResponse, err := tenantsClient.List(ctx, privatev1.TenantsListRequest_builder{}.Build())
		Expect(err).ToNot(HaveOccurred())
		ids := make([]string, len(listResponse.GetItems()))
		for i, item := range listResponse.GetItems() {
			ids[i] = item.GetId()
		}
		Expect(ids).To(ContainElement(id))

		By("Verifying tenant exists in Keycloak")
		verifyTenantInKeycloak(ctx, name)

		By("Deleting tenant")
		deleteTenant(ctx, tenantsClient, projectsClient, id)

		By("Waiting for tenant to return NotFound")
		Eventually(
			func(g Gomega) {
				_, err := tenantsClient.Get(ctx, privatev1.TenantsGetRequest_builder{
					Id: id,
				}.Build())
				g.Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				g.Expect(ok).To(BeTrue())
				g.Expect(status.Code()).To(Equal(grpccodes.NotFound))
			},
			time.Minute,
			time.Second,
		).Should(Succeed())

		By("Verifying tenant removed from Keycloak")
		verifyTenantRemovedFromKeycloak(ctx, name)
	})

	It("Break-glass auth and RBAC", func(ctx context.Context) {
		name := fmt.Sprintf("test-%s", uuid.New())

		By(fmt.Sprintf("Creating tenant %q", name))
		id := createTenant(ctx, tenantsClient, name)

		By("Logging in as break-glass user")
		bgConn, _ := loginAsBreakGlass(ctx, tenantsClient, name, id)

		By("Verifying break-glass user cannot list orgs on private API")
		bgPrivateClient := privatev1.NewTenantsClient(bgConn)
		_, err := bgPrivateClient.List(ctx, privatev1.TenantsListRequest_builder{}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
	})

	It("Duplicate tenant name fails", func(ctx context.Context) {
		name := fmt.Sprintf("test-%s", uuid.New())

		By(fmt.Sprintf("Creating tenant %q", name))
		id := createTenant(ctx, tenantsClient, name)
		waitForTenantSynced(ctx, tenantsClient, id)

		By("Attempting to create another tenant with the same name")
		_, err := tenantsClient.Create(ctx, privatev1.TenantsCreateRequest_builder{
			Object: privatev1.Tenant_builder{
				Metadata: privatev1.Metadata_builder{
					Name: name,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.AlreadyExists))
	})

	It("Rename tenant is rejected", func(ctx context.Context) {
		name := fmt.Sprintf("test-%s", uuid.New())

		By(fmt.Sprintf("Creating tenant %q", name))
		id := createTenant(ctx, tenantsClient, name)

		By("Waiting for tenant to reach SYNCED with finalizer")
		Eventually(
			func(g Gomega) {
				getResponse, err := tenantsClient.Get(ctx, privatev1.TenantsGetRequest_builder{
					Id: id,
				}.Build())
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(getResponse.GetObject().GetMetadata().GetFinalizers()).To(
					ContainElement(finalizers.Controller),
				)
				g.Expect(getResponse.GetObject().GetStatus().GetState()).To(
					Equal(privatev1.TenantState_TENANT_STATE_SYNCED),
				)
			},
			time.Minute,
			time.Second,
		).Should(Succeed())

		By("Attempting to rename the tenant")
		newName := fmt.Sprintf("renamed-%s", uuid.New())
		_, err := tenantsClient.Update(ctx, privatev1.TenantsUpdateRequest_builder{
			Object: privatev1.Tenant_builder{
				Id: id,
				Metadata: privatev1.Metadata_builder{
					Name: newName,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
	})
})

var _ = Describe("Tenant authorization boundaries", func() {
	var (
		ctx    context.Context
		client privatev1.TenantsClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		client = privatev1.NewTenantsClient(tool.InternalView().AdminConn())
	})

	It("Regular user cannot create tenant", func() {
		By("Connecting as regular user to private API")
		userClient := privatev1.NewTenantsClient(tool.InternalView().UserConn())
		name := fmt.Sprintf("test-%s", uuid.New())

		By(fmt.Sprintf("Attempting to create tenant %q as regular user", name))
		_, err := userClient.Create(ctx, privatev1.TenantsCreateRequest_builder{
			Object: privatev1.Tenant_builder{
				Metadata: privatev1.Metadata_builder{
					Name: name,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
	})

	It("Anonymous request cannot create tenant", func() {
		By("Connecting anonymously (no token) to private API")
		anonClient := privatev1.NewTenantsClient(tool.InternalView().AnonymousConn())
		name := fmt.Sprintf("test-%s", uuid.New())

		By(fmt.Sprintf("Attempting to create tenant %q without authentication", name))
		_, err := anonClient.Create(ctx, privatev1.TenantsCreateRequest_builder{
			Object: privatev1.Tenant_builder{
				Metadata: privatev1.Metadata_builder{
					Name: name,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(SatisfyAny(
			Equal(grpccodes.Unauthenticated),
			Equal(grpccodes.PermissionDenied),
		))
	})

	It("Break-glass from tenant-A cannot access tenant-B", func() {
		By("Creating tenant-A")
		nameA := fmt.Sprintf("test-%s", uuid.New())
		idA := createTenant(ctx, client, nameA)

		By("Creating tenant-B")
		nameB := fmt.Sprintf("test-%s", uuid.New())
		idB := createTenant(ctx, client, nameB)
		waitForTenantSynced(ctx, client, idB)

		By("Logging in as break-glass user for tenant-A")
		bgConnA, _ := loginAsBreakGlass(ctx, client, nameA, idA)

		By("Attempting to access tenant-B using tenant-A's break-glass credentials")
		bgPublicClientA := publicv1.NewTenantsClient(bgConnA)
		_, err := bgPublicClientA.Get(ctx, publicv1.TenantsGetRequest_builder{
			Id: idB,
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(SatisfyAny(
			Equal(grpccodes.PermissionDenied),
			Equal(grpccodes.NotFound),
		))
	})

	It("Break-glass cannot call admin-only APIs", func() {
		name := fmt.Sprintf("test-%s", uuid.New())

		By(fmt.Sprintf("Creating tenant %q", name))
		id := createTenant(ctx, client, name)

		By("Logging in as break-glass user")
		bgConn, _ := loginAsBreakGlass(ctx, client, name, id)

		bgPrivateClient := privatev1.NewTenantsClient(bgConn)

		By("Attempting to create a new tenant as break-glass user")
		_, err := bgPrivateClient.Create(ctx, privatev1.TenantsCreateRequest_builder{
			Object: privatev1.Tenant_builder{
				Metadata: privatev1.Metadata_builder{
					Name: fmt.Sprintf("test-%s", uuid.New()),
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))

		By("Attempting to delete a tenant as break-glass user")
		_, err = bgPrivateClient.Delete(ctx, privatev1.TenantsDeleteRequest_builder{
			Id: id,
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok = grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
	})

	It("Break-glass JWT contains correct org membership", func() {
		name := fmt.Sprintf("test-%s", uuid.New())

		By(fmt.Sprintf("Creating tenant %q", name))
		id := createTenant(ctx, client, name)

		By("Logging in as break-glass user")
		_, tokenSource := loginAsBreakGlass(ctx, client, name, id)

		By("Obtaining JWT access token")
		token, err := tokenSource.Token(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(token.Access).ToNot(BeEmpty())

		By("Decoding JWT payload")
		parts := strings.Split(token.Access, ".")
		Expect(parts).To(HaveLen(3))

		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		Expect(err).ToNot(HaveOccurred())

		var claims map[string]any
		Expect(json.Unmarshal(payload, &claims)).To(Succeed())

		By("Verifying 'organization' claim contains the org name")
		orgClaim, ok := claims["organization"]
		Expect(ok).To(BeTrue(), "JWT should contain 'organization' claim")
		orgList, ok := orgClaim.([]any)
		Expect(ok).To(BeTrue(), "'organization' claim should be an array")
		orgStrings := make([]string, len(orgList))
		for i, o := range orgList {
			orgStrings[i], _ = o.(string)
		}
		Expect(orgStrings).To(ContainElement(name))

		By("Verifying 'tenant-idp-manager' role in realm_access")
		realmAccess, ok := claims["realm_access"]
		Expect(ok).To(BeTrue(), "JWT should contain 'realm_access' claim")
		ra, ok := realmAccess.(map[string]any)
		Expect(ok).To(BeTrue())
		roles, ok := ra["roles"]
		Expect(ok).To(BeTrue(), "realm_access should contain 'roles'")
		roleList, ok := roles.([]any)
		Expect(ok).To(BeTrue())
		roleStrings := make([]string, len(roleList))
		for i, r := range roleList {
			roleStrings[i], _ = r.(string)
		}
		Expect(roleStrings).To(ContainElement("tenant-idp-manager"))
	})
})

var _ = Describe("Tenant edge cases and resilience", func() {
	var (
		tenantsClient  privatev1.TenantsClient
		projectsClient privatev1.ProjectsClient
	)

	BeforeEach(func() {
		tenantsClient = privatev1.NewTenantsClient(tool.InternalView().AdminConn())
		projectsClient = privatev1.NewProjectsClient(tool.InternalView().AdminConn())
	})

	It("Empty name is rejected", func(ctx context.Context) {
		By("Attempting to create tenant with empty name")
		_, err := tenantsClient.Create(ctx, privatev1.TenantsCreateRequest_builder{
			Object: privatev1.Tenant_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
	})

	It("Name with special characters is rejected", func(ctx context.Context) {
		By("Attempting to create tenant with invalid characters")
		invalidNames := []string{
			"test/slash",
			"test org space",
			"test@at-sign",
			"test:colon",
		}
		for _, name := range invalidNames {
			By(fmt.Sprintf("Trying name %q", name))
			_, err := tenantsClient.Create(ctx, privatev1.TenantsCreateRequest_builder{
				Object: privatev1.Tenant_builder{
					Metadata: privatev1.Metadata_builder{
						Name: name,
					}.Build(),
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred(), "expected error for name %q", name)
			status, ok := grpcstatus.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(status.Code()).To(Equal(grpccodes.InvalidArgument),
				"expected InvalidArgument for name %q, got %v", name, status.Code())
		}
	})

	It("Very long name is rejected", func(ctx context.Context) {
		By("Attempting to create tenant with a 300-character name")
		longName := strings.Repeat("a", 300)
		_, err := tenantsClient.Create(ctx, privatev1.TenantsCreateRequest_builder{
			Object: privatev1.Tenant_builder{
				Metadata: privatev1.Metadata_builder{
					Name: longName,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.InvalidArgument))
	})

	It("Re-create after delete succeeds with same name", func(ctx context.Context) {
		name := fmt.Sprintf("test-%s", uuid.New())

		By(fmt.Sprintf("Creating tenant %q", name))
		id := createTenant(ctx, tenantsClient, name)
		waitForTenantSynced(ctx, tenantsClient, id)

		By("Deleting the tenant")
		deleteTenant(ctx, tenantsClient, projectsClient, id)

		By("Waiting for tenant to be fully removed")
		Eventually(
			func(g Gomega) {
				_, err := tenantsClient.Get(ctx, privatev1.TenantsGetRequest_builder{
					Id: id,
				}.Build())
				g.Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				g.Expect(ok).To(BeTrue())
				g.Expect(status.Code()).To(Equal(grpccodes.NotFound))
			},
			2*time.Minute,
			time.Second,
		).Should(Succeed())

		By("Re-creating tenant with the same name")
		createResponse, err := tenantsClient.Create(ctx, privatev1.TenantsCreateRequest_builder{
			Object: privatev1.Tenant_builder{
				Metadata: privatev1.Metadata_builder{
					Name: name,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		newId := createResponse.GetObject().GetId()
		DeferCleanup(func() {
			_, _ = tenantsClient.Delete(ctx, privatev1.TenantsDeleteRequest_builder{
				Id: newId,
			}.Build())
			status, ok := grpcstatus.FromError(err)
			if ok && status.Code() == grpccodes.NotFound {
				return
			}
			Expect(err).ToNot(HaveOccurred())
		})

		By("Verifying the re-created tenant reaches SYNCED")
		waitForTenantSynced(ctx, tenantsClient, newId)
	})

	It("Delete during sync is handled gracefully", func(ctx context.Context) {
		name := fmt.Sprintf("test-%s", uuid.New())

		By(fmt.Sprintf("Creating tenant %q", name))
		createResponse, err := tenantsClient.Create(ctx, privatev1.TenantsCreateRequest_builder{
			Object: privatev1.Tenant_builder{
				Metadata: privatev1.Metadata_builder{
					Name: name,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		id := createResponse.GetObject().GetId()

		By("Immediately deleting the tenant before it reaches SYNCED")
		deleteTenant(ctx, tenantsClient, projectsClient, id)

		By("Verifying the tenant eventually returns NotFound")
		Eventually(
			func(g Gomega) {
				_, err := tenantsClient.Get(ctx, privatev1.TenantsGetRequest_builder{
					Id: id,
				}.Build())
				g.Expect(err).To(HaveOccurred())
				status, ok := grpcstatus.FromError(err)
				g.Expect(ok).To(BeTrue())
				g.Expect(status.Code()).To(Equal(grpccodes.NotFound))
			},
			2*time.Minute,
			time.Second,
		).Should(Succeed())

		By("Verifying the tenant is also removed from Keycloak")
		verifyTenantRemovedFromKeycloak(ctx, name)
	})
})
