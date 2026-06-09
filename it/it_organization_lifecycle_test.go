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

func createOrg(ctx context.Context, client privatev1.OrganizationsClient, name string) string {
	createResponse, err := client.Create(ctx, privatev1.OrganizationsCreateRequest_builder{
		Object: privatev1.Organization_builder{
			Metadata: privatev1.Metadata_builder{
				Name: name,
			}.Build(),
		}.Build(),
	}.Build())
	Expect(err).ToNot(HaveOccurred())
	id := createResponse.GetObject().GetId()
	DeferCleanup(func() {
		_, _ = client.Delete(ctx, privatev1.OrganizationsDeleteRequest_builder{
			Id: id,
		}.Build())
	})
	return id
}

func waitForOrgSynced(ctx context.Context, client privatev1.OrganizationsClient, id string) {
	Eventually(
		func(g Gomega) {
			getResponse, err := client.Get(ctx, privatev1.OrganizationsGetRequest_builder{
				Id: id,
			}.Build())
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(getResponse.GetObject().GetStatus().GetState()).To(
				Equal(privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED),
			)
			g.Expect(getResponse.GetObject().GetStatus().GetBreakGlassUserId()).ToNot(BeEmpty())
		},
		time.Minute,
		time.Second,
	).Should(Succeed())
}

func verifyOrgInKeycloak(ctx context.Context, name string) {
	code, body, err := tool.KeycloakAdminRequest(ctx, http.MethodGet,
		fmt.Sprintf("/organizations?exact=true&search=%s", name), nil)
	Expect(err).ToNot(HaveOccurred())
	Expect(code).To(Equal(http.StatusOK))
	var kcOrgs []map[string]any
	Expect(json.Unmarshal(body, &kcOrgs)).To(Succeed())
	Expect(kcOrgs).ToNot(BeEmpty())
}

func verifyOrgRemovedFromKeycloak(ctx context.Context, name string) {
	Eventually(
		func(g Gomega) {
			code, body, err := tool.KeycloakAdminRequest(ctx, http.MethodGet,
				fmt.Sprintf("/organizations?exact=true&search=%s", name), nil)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(code).To(Equal(http.StatusOK))
			var kcOrgs []map[string]any
			g.Expect(json.Unmarshal(body, &kcOrgs)).To(Succeed())
			g.Expect(kcOrgs).To(BeEmpty())
		},
		time.Minute,
		time.Second,
	).Should(Succeed())
}

// loginAsBreakGlass creates a gRPC connection authenticated as the break-glass user
// for the given organization. It waits for the org to reach SYNCED, retrieves or sets
// the break-glass password, clears the UPDATE_PASSWORD required action, and returns
// a gRPC connection and the token source. The connection is registered for cleanup.
func loginAsBreakGlass(
	ctx context.Context,
	client privatev1.OrganizationsClient,
	name, id string,
) (*grpc.ClientConn, auth.TokenSource) {
	var breakGlassUserId string
	var breakGlassUsername string
	var breakGlassPassword string
	Eventually(
		func(g Gomega) {
			getResponse, err := client.Get(ctx, privatev1.OrganizationsGetRequest_builder{
				Id: id,
			}.Build())
			g.Expect(err).ToNot(HaveOccurred())
			status := getResponse.GetObject().GetStatus()
			g.Expect(status.GetState()).To(
				Equal(privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED),
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

var _ = Describe("Organization lifecycle", func() {
	var (
		ctx    context.Context
		client privatev1.OrganizationsClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		client = privatev1.NewOrganizationsClient(tool.InternalView().AdminConn())
	})

	It("CRUD happy flow", func() {
		name := fmt.Sprintf("test-%s", uuid.New())

		By(fmt.Sprintf("Creating organization %q", name))
		id := createOrg(ctx, client, name)

		By("Waiting for organization to reach SYNCED state")
		waitForOrgSynced(ctx, client, id)

		By("Verifying organization fields via Get")
		getResponse, err := client.Get(ctx, privatev1.OrganizationsGetRequest_builder{
			Id: id,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		object := getResponse.GetObject()
		Expect(object.GetMetadata().GetName()).To(Equal(name))
		Expect(object.GetStatus().GetBreakGlassUserId()).ToNot(BeEmpty())
		Expect(object.GetStatus().GetIdpOrganizationName()).To(Equal(name))

		By("Verifying organization appears in List")
		listResponse, err := client.List(ctx, privatev1.OrganizationsListRequest_builder{}.Build())
		Expect(err).ToNot(HaveOccurred())
		ids := make([]string, len(listResponse.GetItems()))
		for i, item := range listResponse.GetItems() {
			ids[i] = item.GetId()
		}
		Expect(ids).To(ContainElement(id))

		By("Verifying organization exists in Keycloak")
		verifyOrgInKeycloak(ctx, name)

		By("Deleting organization")
		_, err = client.Delete(ctx, privatev1.OrganizationsDeleteRequest_builder{
			Id: id,
		}.Build())
		Expect(err).ToNot(HaveOccurred())

		By("Waiting for organization to return NotFound")
		Eventually(
			func(g Gomega) {
				_, err := client.Get(ctx, privatev1.OrganizationsGetRequest_builder{
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

		By("Verifying organization removed from Keycloak")
		verifyOrgRemovedFromKeycloak(ctx, name)
	})

	It("Break-glass auth and RBAC", func() {
		name := fmt.Sprintf("test-%s", uuid.New())

		By(fmt.Sprintf("Creating organization %q", name))
		id := createOrg(ctx, client, name)

		By("Logging in as break-glass user")
		bgConn, _ := loginAsBreakGlass(ctx, client, name, id)

		By("Verifying break-glass user cannot list orgs on private API")
		bgPrivateClient := privatev1.NewOrganizationsClient(bgConn)
		_, err := bgPrivateClient.List(ctx, privatev1.OrganizationsListRequest_builder{}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
	})

	It("Duplicate org name fails", func() {
		name := fmt.Sprintf("test-%s", uuid.New())

		By(fmt.Sprintf("Creating organization %q", name))
		id := createOrg(ctx, client, name)
		waitForOrgSynced(ctx, client, id)

		By("Attempting to create another org with the same name")
		_, err := client.Create(ctx, privatev1.OrganizationsCreateRequest_builder{
			Object: privatev1.Organization_builder{
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

	It("Rename org is rejected", func() {
		name := fmt.Sprintf("test-%s", uuid.New())

		By(fmt.Sprintf("Creating organization %q", name))
		id := createOrg(ctx, client, name)

		By("Waiting for organization to reach SYNCED with finalizer")
		Eventually(
			func(g Gomega) {
				getResponse, err := client.Get(ctx, privatev1.OrganizationsGetRequest_builder{
					Id: id,
				}.Build())
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(getResponse.GetObject().GetMetadata().GetFinalizers()).To(
					ContainElement(finalizers.Controller),
				)
				g.Expect(getResponse.GetObject().GetStatus().GetState()).To(
					Equal(privatev1.OrganizationState_ORGANIZATION_STATE_SYNCED),
				)
			},
			time.Minute,
			time.Second,
		).Should(Succeed())

		By("Attempting to rename the organization")
		newName := fmt.Sprintf("renamed-%s", uuid.New())
		_, err := client.Update(ctx, privatev1.OrganizationsUpdateRequest_builder{
			Object: privatev1.Organization_builder{
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

var _ = Describe("Organization authorization boundaries", func() {
	var (
		ctx    context.Context
		client privatev1.OrganizationsClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		client = privatev1.NewOrganizationsClient(tool.InternalView().AdminConn())
	})

	It("Regular user cannot create org", func() {
		By("Connecting as regular user to private API")
		userClient := privatev1.NewOrganizationsClient(tool.InternalView().UserConn())
		name := fmt.Sprintf("test-%s", uuid.New())

		By(fmt.Sprintf("Attempting to create organization %q as regular user", name))
		_, err := userClient.Create(ctx, privatev1.OrganizationsCreateRequest_builder{
			Object: privatev1.Organization_builder{
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

	It("Anonymous request cannot create org", func() {
		By("Connecting anonymously (no token) to private API")
		anonClient := privatev1.NewOrganizationsClient(tool.InternalView().AnonymousConn())
		name := fmt.Sprintf("test-%s", uuid.New())

		By(fmt.Sprintf("Attempting to create organization %q without authentication", name))
		_, err := anonClient.Create(ctx, privatev1.OrganizationsCreateRequest_builder{
			Object: privatev1.Organization_builder{
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

	It("Break-glass from org-A cannot access org-B", func() {
		By("Creating org-A")
		nameA := fmt.Sprintf("test-%s", uuid.New())
		idA := createOrg(ctx, client, nameA)

		By("Creating org-B")
		nameB := fmt.Sprintf("test-%s", uuid.New())
		idB := createOrg(ctx, client, nameB)
		waitForOrgSynced(ctx, client, idB)

		By("Logging in as break-glass user for org-A")
		bgConnA, _ := loginAsBreakGlass(ctx, client, nameA, idA)

		By("Attempting to access org-B using org-A's break-glass credentials")
		bgPublicClientA := publicv1.NewOrganizationsClient(bgConnA)
		_, err := bgPublicClientA.Get(ctx, publicv1.OrganizationsGetRequest_builder{
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

		By(fmt.Sprintf("Creating organization %q", name))
		id := createOrg(ctx, client, name)

		By("Logging in as break-glass user")
		bgConn, _ := loginAsBreakGlass(ctx, client, name, id)

		bgPrivateClient := privatev1.NewOrganizationsClient(bgConn)

		By("Attempting to create a new org as break-glass user")
		_, err := bgPrivateClient.Create(ctx, privatev1.OrganizationsCreateRequest_builder{
			Object: privatev1.Organization_builder{
				Metadata: privatev1.Metadata_builder{
					Name: fmt.Sprintf("test-%s", uuid.New()),
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok := grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))

		By("Attempting to delete an org as break-glass user")
		_, err = bgPrivateClient.Delete(ctx, privatev1.OrganizationsDeleteRequest_builder{
			Id: id,
		}.Build())
		Expect(err).To(HaveOccurred())
		status, ok = grpcstatus.FromError(err)
		Expect(ok).To(BeTrue())
		Expect(status.Code()).To(Equal(grpccodes.PermissionDenied))
	})

	It("Break-glass JWT contains correct org membership", func() {
		name := fmt.Sprintf("test-%s", uuid.New())

		By(fmt.Sprintf("Creating organization %q", name))
		id := createOrg(ctx, client, name)

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

		// The organization claim can be either an array or an object depending on whether
		// the user has group memberships within the organization
		var orgNames []string
		if orgList, ok := orgClaim.([]any); ok {
			// Array format: ["org-name"]
			orgNames = make([]string, len(orgList))
			for i, o := range orgList {
				orgNames[i], _ = o.(string)
			}
		} else if orgMap, ok := orgClaim.(map[string]any); ok {
			// Object format: {"org-name": {"groups": [...]}}
			orgNames = make([]string, 0, len(orgMap))
			for orgName := range orgMap {
				orgNames = append(orgNames, orgName)
			}
		} else {
			Fail("'organization' claim should be either an array or an object")
		}
		Expect(orgNames).To(ContainElement(name))

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

var _ = Describe("Organization edge cases and resilience", func() {
	var (
		ctx    context.Context
		client privatev1.OrganizationsClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		client = privatev1.NewOrganizationsClient(tool.InternalView().AdminConn())
	})

	It("Empty name is rejected", func() {
		By("Attempting to create organization with empty name")
		_, err := client.Create(ctx, privatev1.OrganizationsCreateRequest_builder{
			Object: privatev1.Organization_builder{
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

	It("Name with special characters is rejected", func() {
		By("Attempting to create organization with invalid characters")
		invalidNames := []string{
			"test/slash",
			"test org space",
			"test@at-sign",
			"test:colon",
		}
		for _, name := range invalidNames {
			By(fmt.Sprintf("Trying name %q", name))
			_, err := client.Create(ctx, privatev1.OrganizationsCreateRequest_builder{
				Object: privatev1.Organization_builder{
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

	It("Very long name is rejected", func() {
		By("Attempting to create organization with a 300-character name")
		longName := strings.Repeat("a", 300)
		_, err := client.Create(ctx, privatev1.OrganizationsCreateRequest_builder{
			Object: privatev1.Organization_builder{
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

	It("Re-create after delete succeeds with same name", func() {
		name := fmt.Sprintf("test-%s", uuid.New())

		By(fmt.Sprintf("Creating organization %q", name))
		id := createOrg(ctx, client, name)
		waitForOrgSynced(ctx, client, id)

		By("Deleting the organization")
		_, err := client.Delete(ctx, privatev1.OrganizationsDeleteRequest_builder{
			Id: id,
		}.Build())
		Expect(err).ToNot(HaveOccurred())

		By("Waiting for organization to be fully removed")
		Eventually(
			func(g Gomega) {
				_, err := client.Get(ctx, privatev1.OrganizationsGetRequest_builder{
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

		By("Re-creating organization with the same name")
		createResponse, err := client.Create(ctx, privatev1.OrganizationsCreateRequest_builder{
			Object: privatev1.Organization_builder{
				Metadata: privatev1.Metadata_builder{
					Name: name,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		newId := createResponse.GetObject().GetId()
		DeferCleanup(func() {
			_, _ = client.Delete(ctx, privatev1.OrganizationsDeleteRequest_builder{
				Id: newId,
			}.Build())
		})

		By("Verifying the re-created organization reaches SYNCED")
		waitForOrgSynced(ctx, client, newId)
	})

	It("Delete during sync is handled gracefully", func() {
		name := fmt.Sprintf("test-%s", uuid.New())

		By(fmt.Sprintf("Creating organization %q", name))
		createResponse, err := client.Create(ctx, privatev1.OrganizationsCreateRequest_builder{
			Object: privatev1.Organization_builder{
				Metadata: privatev1.Metadata_builder{
					Name: name,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		id := createResponse.GetObject().GetId()

		By("Immediately deleting the organization before it reaches SYNCED")
		_, err = client.Delete(ctx, privatev1.OrganizationsDeleteRequest_builder{
			Id: id,
		}.Build())
		Expect(err).ToNot(HaveOccurred())

		By("Verifying the organization eventually returns NotFound")
		Eventually(
			func(g Gomega) {
				_, err := client.Get(ctx, privatev1.OrganizationsGetRequest_builder{
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

		By("Verifying the organization is also removed from Keycloak")
		verifyOrgRemovedFromKeycloak(ctx, name)
	})
})
