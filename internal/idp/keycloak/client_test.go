/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package keycloak

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/osac-project/fulfillment-service/internal/apiclient"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/idp"
)

var _ = Describe("Keycloak Client", func() {
	var (
		ctx    context.Context
		client *Client
		server *httptest.Server
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	AfterEach(func() {
		if server != nil {
			server.Close()
		}
	})

	Describe("CreateOrganization", func() {
		It("creates an organization in Keycloak", func() {
			var receivedOrg *keycloakOrganization
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost && r.URL.Path == "/admin/realms/osac/organizations" {
					// Create request
					receivedOrg = &keycloakOrganization{}
					json.NewDecoder(r.Body).Decode(receivedOrg)
					w.WriteHeader(http.StatusCreated)
					return
				}

				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" && r.URL.RawQuery == "exact=true&search=test-org" {
					// Get request to verify creation
					enabled := true
					response := []keycloakOrganization{{
						ID:      "org-uuid-123",
						Name:    "test-org",
						Alias:   "Test Organization",
						Enabled: &enabled,
					}}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					if err := json.NewEncoder(w).Encode(response); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)

			org := &idp.Organization{
				Name:        "test-org",
				DisplayName: "Test Organization",
				Enabled:     true,
			}
			createdOrg, err := client.CreateOrganization(ctx, org)
			Expect(err).ToNot(HaveOccurred())
			Expect(receivedOrg.Name).To(Equal("test-org"))
			Expect(createdOrg).ToNot(BeNil())
			Expect(createdOrg.ID).To(Equal("org-uuid-123"))
			Expect(createdOrg.Name).To(Equal("test-org"))
			Expect(createdOrg.DisplayName).To(Equal("Test Organization"))
			Expect(createdOrg.Enabled).To(BeTrue())
		})
		It("returns an error if the organization already exists", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusConflict)
			}))

			client = createTestClient(server.URL)

			org := &idp.Organization{
				Name:        "test-org",
				DisplayName: "Test Organization",
				Enabled:     true,
			}
			_, err := client.CreateOrganization(ctx, org)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("already exists"))
			Expect(err.Error()).To(ContainSubstring("test-org"))

			var apiErr *apiclient.APIError
			Expect(errors.As(err, &apiErr)).To(BeTrue())
			Expect(apiErr.StatusCode).To(Equal(http.StatusConflict))
		})

		It("returns an error on server error", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}))

			client = createTestClient(server.URL)

			org := &idp.Organization{Name: "test-org"}
			_, err := client.CreateOrganization(ctx, org)
			Expect(err).To(HaveOccurred())
		})

		It("sends domains to Keycloak when specified", func() {
			var receivedOrg *keycloakOrganization
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost && r.URL.Path == "/admin/realms/osac/organizations" {
					receivedOrg = &keycloakOrganization{}
					json.NewDecoder(r.Body).Decode(receivedOrg)
					w.WriteHeader(http.StatusCreated)
					return
				}
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" &&
					r.URL.RawQuery == "exact=true&search=test-org" {
					enabled := true
					response := []keycloakOrganization{{
						ID:      "org-uuid-123",
						Name:    "test-org",
						Enabled: &enabled,
						Domains: []*keycloakOrganizationDomain{
							{Name: "example.com"},
							{Name: "corp.example.org"},
						},
					}}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(response)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)
			org := &idp.Organization{
				Name:    "test-org",
				Enabled: true,
				Domains: []string{"example.com", "corp.example.org"},
			}
			createdOrg, err := client.CreateOrganization(ctx, org)
			Expect(err).ToNot(HaveOccurred())
			Expect(receivedOrg.Domains).To(HaveLen(2))
			Expect(receivedOrg.Domains[0].Name).To(Equal("example.com"))
			Expect(receivedOrg.Domains[1].Name).To(Equal("corp.example.org"))
			Expect(createdOrg.Domains).To(ConsistOf("example.com", "corp.example.org"))
		})

		It("sends empty domains list when no domains specified", func() {
			var receivedOrg *keycloakOrganization
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost && r.URL.Path == "/admin/realms/osac/organizations" {
					receivedOrg = &keycloakOrganization{}
					json.NewDecoder(r.Body).Decode(receivedOrg)
					w.WriteHeader(http.StatusCreated)
					return
				}
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" &&
					r.URL.RawQuery == "exact=true&search=test-org" {
					enabled := true
					response := []keycloakOrganization{{
						ID:      "org-uuid-123",
						Name:    "test-org",
						Enabled: &enabled,
					}}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(response)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)
			org := &idp.Organization{
				Name:    "test-org",
				Enabled: true,
			}
			_, err := client.CreateOrganization(ctx, org)
			Expect(err).ToNot(HaveOccurred())
			Expect(receivedOrg.Domains).To(BeEmpty())
		})
	})

	Describe("UpdateOrganization", func() {
		It("updates an organization in Keycloak", func() {
			var receivedOrg *keycloakOrganization
			var receivedMethod string
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPut && r.URL.Path == "/admin/realms/osac/organizations/org-uuid-123" {
					receivedMethod = r.Method
					receivedOrg = &keycloakOrganization{}
					json.NewDecoder(r.Body).Decode(receivedOrg)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" &&
					r.URL.RawQuery == "exact=true&search=test-org" {
					enabled := true
					response := []keycloakOrganization{{
						ID:      "org-uuid-123",
						Name:    "test-org",
						Enabled: &enabled,
						Domains: []*keycloakOrganizationDomain{
							{Name: "updated.example.com"},
						},
					}}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(response)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)
			org := &idp.Organization{
				ID:      "org-uuid-123",
				Name:    "test-org",
				Enabled: true,
				Domains: []string{"updated.example.com"},
			}
			updatedOrg, err := client.UpdateOrganization(ctx, org)
			Expect(err).ToNot(HaveOccurred())
			Expect(receivedMethod).To(Equal(http.MethodPut))
			Expect(receivedOrg.Domains).To(HaveLen(1))
			Expect(receivedOrg.Domains[0].Name).To(Equal("updated.example.com"))
			Expect(updatedOrg).ToNot(BeNil())
			Expect(updatedOrg.ID).To(Equal("org-uuid-123"))
			Expect(updatedOrg.Domains).To(ConsistOf("updated.example.com"))
		})

		It("updates an organization with empty domains", func() {
			var receivedOrg *keycloakOrganization
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPut && r.URL.Path == "/admin/realms/osac/organizations/org-uuid-123" {
					receivedOrg = &keycloakOrganization{}
					json.NewDecoder(r.Body).Decode(receivedOrg)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" &&
					r.URL.RawQuery == "exact=true&search=test-org" {
					enabled := true
					response := []keycloakOrganization{{
						ID:      "org-uuid-123",
						Name:    "test-org",
						Enabled: &enabled,
					}}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(response)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)
			org := &idp.Organization{
				ID:      "org-uuid-123",
				Name:    "test-org",
				Enabled: true,
			}
			updatedOrg, err := client.UpdateOrganization(ctx, org)
			Expect(err).ToNot(HaveOccurred())
			Expect(receivedOrg.Domains).To(BeEmpty())
			Expect(updatedOrg.Domains).To(BeEmpty())
		})

		It("returns an error when organization ID is empty", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)
			org := &idp.Organization{
				Name:    "test-org",
				Enabled: true,
			}
			_, err := client.UpdateOrganization(ctx, org)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("organization ID is required"))
		})

		It("returns an error when organization is nil", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)
			_, err := client.UpdateOrganization(ctx, nil)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("organization is required"))
		})

		It("returns an error on server error", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}))

			client = createTestClient(server.URL)
			org := &idp.Organization{
				ID:   "org-uuid-123",
				Name: "test-org",
			}
			_, err := client.UpdateOrganization(ctx, org)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("GetOrganization", func() {
		It("retrieves an organization from Keycloak", func() {
			enabled := true
			testOrgs := []keycloakOrganization{{
				ID:      "org-id",
				Name:    "test-org",
				Alias:   "Test Organization",
				Enabled: &enabled,
			}}

			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Method).To(Equal(http.MethodGet))
				Expect(r.URL.Path).To(Equal("/admin/realms/osac/organizations"))
				Expect(r.URL.RawQuery).To(Equal("exact=true&search=test-org"))

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if err := json.NewEncoder(w).Encode(testOrgs); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}))

			client = createTestClient(server.URL)

			org, err := client.GetOrganization(ctx, "test-org")
			Expect(err).ToNot(HaveOccurred())
			Expect(org.Name).To(Equal("test-org"))
			Expect(org.DisplayName).To(Equal("Test Organization"))
		})
		It("returns an error if the organization is not found", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)
			_, err := client.GetOrganization(ctx, "test-org")
			Expect(err).To(HaveOccurred())
		})

		It("returns an error on malformed JSON response", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("invalid json"))
			}))

			client = createTestClient(server.URL)
			_, err := client.GetOrganization(ctx, "test-org")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to decode organization response"))
		})
	})

	Describe("CreateUser", func() {
		It("creates a user and extracts ID from Location header", func() {
			var receivedUser *keycloakUser
			var addedUserID string
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// First request: create user in realm
				if r.Method == http.MethodPost && r.URL.Path == "/admin/realms/osac/users" {
					receivedUser = &keycloakUser{}
					json.NewDecoder(r.Body).Decode(receivedUser)
					w.Header().Set("Location", "/admin/realms/osac/users/user-123-abc")
					w.WriteHeader(http.StatusCreated)
					return
				}

				// Second request: get organization by name
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" && r.URL.RawQuery == "exact=true&search=test-org" {
					enabled := true
					response := []keycloakOrganization{{
						ID:      "org-id",
						Name:    "test-org",
						Alias:   "Test Organization",
						Enabled: &enabled,
					}}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					if err := json.NewEncoder(w).Encode(response); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				// Third request: add user to organization
				if r.Method == http.MethodPost && r.URL.Path == "/admin/realms/osac/organizations/org-id/members" {
					json.NewDecoder(r.Body).Decode(&addedUserID)
					w.WriteHeader(http.StatusNoContent)
					return
				}

				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)

			user := &idp.User{
				Username:      "testuser",
				Email:         "test@example.com",
				EmailVerified: true,
				Enabled:       true,
				FirstName:     "Test",
				LastName:      "User",
			}
			createdUser, err := client.CreateUser(ctx, "test-org", user)
			Expect(err).ToNot(HaveOccurred())
			Expect(receivedUser.Username).To(Equal("testuser"))
			Expect(addedUserID).To(Equal("user-123-abc"))
			Expect(createdUser).ToNot(BeNil())
			Expect(createdUser.ID).To(Equal("user-123-abc"))
			Expect(createdUser.Username).To(Equal("testuser"))
			Expect(createdUser.Email).To(Equal("test@example.com"))
		})
		It("returns an error if the Location header is not present", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusCreated)
			}))

			client = createTestClient(server.URL)
			_, err := client.CreateUser(ctx, "test-org", &idp.User{
				Username: "testuser",
				Email:    "test@example.com",
				Enabled:  true,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Location header not present in create user response"))
		})

		It("returns an error if the user already exists", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusConflict)
			}))

			client = createTestClient(server.URL)
			_, err := client.CreateUser(ctx, "test-org", &idp.User{
				Username: "testuser",
				Email:    "test@example.com",
				Enabled:  true,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create user"))

			var apiErr *apiclient.APIError
			Expect(errors.As(err, &apiErr)).To(BeTrue())
			Expect(apiErr.StatusCode).To(Equal(http.StatusConflict))
		})

		It("returns an error on server error", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}))

			client = createTestClient(server.URL)
			_, err := client.CreateUser(ctx, "test-org", &idp.User{
				Username: "testuser",
				Email:    "test@example.com",
				Enabled:  true,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create user"))
		})

		It("returns created user even if adding to organization fails", func() {
			userCreated := false
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// First request: create user in realm (succeeds)
				if r.Method == http.MethodPost && r.URL.Path == "/admin/realms/osac/users" {
					userCreated = true
					w.Header().Set("Location", "/admin/realms/osac/users/user-123-abc")
					w.WriteHeader(http.StatusCreated)
					return
				}

				// Second request: add user to organization (fails)
				if r.Method == http.MethodPost && r.URL.Path == "/admin/realms/osac/organizations/test-org/members" {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}

				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)
			createdUser, err := client.CreateUser(ctx, "test-org", &idp.User{
				Username: "testuser",
				Email:    "test@example.com",
				Enabled:  true,
			})

			// Should return an error indicating org add failed
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to add user to organization"))
			Expect(err.Error()).To(ContainSubstring("user-123-abc created in realm"))

			// But should still return the created user so caller can retry org add
			Expect(createdUser).ToNot(BeNil())
			Expect(createdUser.ID).To(Equal("user-123-abc"))
			Expect(createdUser.Username).To(Equal("testuser"))
			Expect(userCreated).To(BeTrue())
		})
	})

	Describe("ListUsers", func() {
		It("fetches all users across multiple pages", func() {
			requestCount := 0
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Method).To(Equal(http.MethodGet))
				Expect(r.URL.Path).To(Equal("/admin/realms/osac/organizations/test-org/members"))

				// Parse query parameters
				query := r.URL.Query()
				first := query.Get("first")
				max := query.Get("max")

				requestCount++

				// Simulate pagination: first page returns 100 users, second page returns 50
				var users []keycloakUser
				if first == "0" && max == "100" {
					// First page: return 100 users
					for i := 0; i < 100; i++ {
						enabled := true
						users = append(users, keycloakUser{
							ID:       fmt.Sprintf("user-%d", i),
							Username: fmt.Sprintf("user%d", i),
							Enabled:  &enabled,
						})
					}
				} else if first == "100" && max == "100" {
					// Second page: return 50 users (less than max, indicates last page)
					for i := 100; i < 150; i++ {
						enabled := true
						users = append(users, keycloakUser{
							ID:       fmt.Sprintf("user-%d", i),
							Username: fmt.Sprintf("user%d", i),
							Enabled:  &enabled,
						})
					}
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if err := json.NewEncoder(w).Encode(users); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}))

			client = createTestClient(server.URL)

			users, err := client.ListUsers(ctx, "test-org")
			Expect(err).ToNot(HaveOccurred())

			// Should have fetched all 150 users across 2 pages
			Expect(users).To(HaveLen(150))

			// Should have made 2 requests (one per page)
			Expect(requestCount).To(Equal(2))

			// Verify first and last user
			Expect(users[0].ID).To(Equal("user-0"))
			Expect(users[149].ID).To(Equal("user-149"))
		})

		It("returns an error on server error", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}))

			client = createTestClient(server.URL)
			_, err := client.ListUsers(ctx, "test-org")
			Expect(err).To(HaveOccurred())
		})

		It("returns an error on malformed JSON response", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("invalid json"))
			}))

			client = createTestClient(server.URL)
			_, err := client.ListUsers(ctx, "test-org")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to decode organization members"))
		})

		It("respects context cancellation during pagination", func() {
			requestCount := 0
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount++

				// Return a full page so pagination continues
				var users []keycloakUser
				for i := 0; i < 100; i++ {
					enabled := true
					users = append(users, keycloakUser{
						ID:       fmt.Sprintf("user-%d", i),
						Username: fmt.Sprintf("user%d", i),
						Enabled:  &enabled,
					})
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if err := json.NewEncoder(w).Encode(users); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}))

			client = createTestClient(server.URL)

			// Create a context that is already cancelled
			cancelledCtx, cancel := context.WithCancel(context.Background())
			cancel()

			_, err := client.ListUsers(cancelledCtx, "test-org")
			Expect(err).To(HaveOccurred())
			Expect(err).To(Equal(context.Canceled))

			// Should not have made any requests since context was already cancelled
			Expect(requestCount).To(Equal(0))
		})
	})
	Describe("GetUser", func() {
		It("gets a user from Keycloak", func() {
			enabled := true
			testUser := &keycloakUser{
				ID:            "user-123-abc",
				Username:      "testuser",
				Email:         "test@example.com",
				EmailVerified: &enabled,
				Enabled:       &enabled,
				FirstName:     "Test",
				LastName:      "User",
			}

			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Method).To(Equal(http.MethodGet))
				Expect(r.URL.Path).To(Equal("/admin/realms/osac/users/user-123-abc"))

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if err := json.NewEncoder(w).Encode(testUser); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}))
			client = createTestClient(server.URL)
			user, err := client.GetUser(ctx, "test-org", "user-123-abc")
			Expect(err).ToNot(HaveOccurred())
			Expect(user.ID).To(Equal("user-123-abc"))
			Expect(user.Username).To(Equal("testuser"))
			Expect(user.Email).To(Equal("test@example.com"))
			Expect(user.Enabled).To(BeTrue())
			Expect(user.FirstName).To(Equal("Test"))
			Expect(user.LastName).To(Equal("User"))
			Expect(user.Attributes).To(BeNil())
			Expect(user.Groups).To(BeNil())
			Expect(user.Credentials).To(BeNil())
			Expect(user.RequiredActions).To(BeNil())
		})
		It("returns an error if the user is not found", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)
			_, err := client.GetUser(ctx, "test-org", "user-123-abc")
			Expect(err).To(HaveOccurred())
		})

		It("returns an error on malformed JSON response", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("invalid json"))
			}))

			client = createTestClient(server.URL)
			_, err := client.GetUser(ctx, "test-org", "user-123-abc")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to decode user response"))
		})

		It("returns an error on server error", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}))

			client = createTestClient(server.URL)
			_, err := client.GetUser(ctx, "test-org", "user-123-abc")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("DeleteUser", func() {
		It("deletes a user from Keycloak realm", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Method).To(Equal(http.MethodDelete))
				// DeleteUser only deletes from realm - Keycloak auto-removes from all organizations
				Expect(r.URL.Path).To(Equal("/admin/realms/osac/users/user-123-abc"))
				w.WriteHeader(http.StatusNoContent)
			}))
			client = createTestClient(server.URL)
			err := client.DeleteUser(ctx, "test-org", "user-123-abc")
			Expect(err).ToNot(HaveOccurred())
		})
		It("returns an error if the user is not found", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Path).To(Equal("/admin/realms/osac/users/user-123-abc"))
				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)
			err := client.DeleteUser(ctx, "test-org", "user-123-abc")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to delete user"))

			var apiErr *apiclient.APIError
			Expect(errors.As(err, &apiErr)).To(BeTrue())
			Expect(apiErr.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("returns an error on server error", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Path).To(Equal("/admin/realms/osac/users/user-123-abc"))
				w.WriteHeader(http.StatusInternalServerError)
			}))

			client = createTestClient(server.URL)
			err := client.DeleteUser(ctx, "test-org", "user-123-abc")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("DeleteOrganization", func() {
		It("deletes an organization from Keycloak", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// First request: query user by username (for break-glass deletion)
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/users" && r.URL.Query().Get("username") == "test-org-osac-break-glass" {
					// Return break-glass user
					response := []keycloakUser{{
						ID:       "break-glass-id",
						Username: "test-org-osac-break-glass",
						Email:    "break-glass@test-org.osac.local",
					}}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					if err := json.NewEncoder(w).Encode(response); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				// Second request: delete break-glass user
				if r.Method == http.MethodDelete && r.URL.Path == "/admin/realms/osac/users/break-glass-id" {
					w.WriteHeader(http.StatusNoContent)
					return
				}

				// Third request: get organization by name
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" && r.URL.RawQuery == "exact=true&search=test-org" {
					enabled := true
					response := []keycloakOrganization{{
						ID:      "org-id",
						Name:    "test-org",
						Alias:   "Test Organization",
						Enabled: &enabled,
					}}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					if err := json.NewEncoder(w).Encode(response); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				// Fourth request: delete organization
				if r.Method == http.MethodDelete && r.URL.Path == "/admin/realms/osac/organizations/org-id" {
					w.WriteHeader(http.StatusNoContent)
					return
				}

				w.WriteHeader(http.StatusBadRequest)
			}))

			client = createTestClient(server.URL)
			err := client.DeleteOrganization(ctx, "test-org")
			Expect(err).ToNot(HaveOccurred())
		})

		It("returns an error if the organization is not found", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)
			err := client.DeleteOrganization(ctx, "test-org")
			Expect(err).To(HaveOccurred())
		})

		It("returns an error on server error", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}))

			client = createTestClient(server.URL)
			err := client.DeleteOrganization(ctx, "test-org")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("ListOrganizationRoles", func() {
		It("is not yet implemented (returns nil, nil)", func() {
			client = createTestClient(server.URL)
			result, err := client.ListOrganizationRoles(ctx, "test-org")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeNil()) // TODO returns nil until implemented
		})
	})

	Describe("ListClientRoles", func() {
		It("fetches client-level roles", func() {
			clientRole := true
			roles := []keycloakRole{
				{ID: "role1", Name: "manage-users", ClientRole: &clientRole},
				{ID: "role2", Name: "view-users", ClientRole: &clientRole},
			}
			clients := []keycloakClient{
				{ID: "internal-uuid-123", ClientID: "realm-management"},
			}

			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Method).To(Equal(http.MethodGet))

				// First call is to resolve client ID
				if r.URL.Path == "/admin/realms/osac/clients" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					if err := json.NewEncoder(w).Encode(clients); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				// Second call is to fetch roles
				Expect(r.URL.Path).To(Equal("/admin/realms/osac/clients/internal-uuid-123/roles"))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if err := json.NewEncoder(w).Encode(roles); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}))

			client = createTestClient(server.URL)
			result, err := client.ListClientRoles(ctx, "test-org", "realm-management")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(2))
			Expect(result[0].Name).To(Equal("manage-users"))
			Expect(result[1].Name).To(Equal("view-users"))
		})

		It("returns an error on server error", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}))

			client = createTestClient(server.URL)
			_, err := client.ListClientRoles(ctx, "test-org", "realm-management")
			Expect(err).To(HaveOccurred())
		})

		It("returns an error on malformed JSON response when fetching roles", func() {
			clients := []keycloakClient{
				{ID: "internal-uuid-123", ClientID: "realm-management"},
			}

			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)

				// First call returns valid clients, second call returns invalid JSON
				if r.URL.Path == "/admin/realms/osac/clients" {
					if err := json.NewEncoder(w).Encode(clients); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
				} else {
					w.Write([]byte("invalid json"))
				}
			}))

			client = createTestClient(server.URL)
			_, err := client.ListClientRoles(ctx, "test-org", "realm-management")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to decode client roles"))
		})
	})

	Describe("AssignOrganizationRolesToUser", func() {
		It("assigns realm roles to a user", func() {
			clientRole := false
			role1 := keycloakRole{
				ID:         "role-id-1",
				Name:       "admin",
				ClientRole: &clientRole,
			}
			role2 := keycloakRole{
				ID:         "role-id-2",
				Name:       "editor",
				ClientRole: &clientRole,
			}

			var receivedRoles []keycloakRole
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")

				// First call: get realm role "admin"
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/roles/admin" {
					w.WriteHeader(http.StatusOK)
					if err := json.NewEncoder(w).Encode(role1); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				// Second call: get realm role "editor"
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/roles/editor" {
					w.WriteHeader(http.StatusOK)
					if err := json.NewEncoder(w).Encode(role2); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				// Third call: assign roles to user
				if r.Method == http.MethodPost && r.URL.Path == "/admin/realms/osac/users/user-123/role-mappings/realm" {
					if err := json.NewDecoder(r.Body).Decode(&receivedRoles); err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					w.WriteHeader(http.StatusNoContent)
					return
				}

				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)
			roles := []*idp.Role{
				{Name: "admin"},
				{Name: "editor"},
			}
			err := client.AssignOrganizationRolesToUser(ctx, "test-org", "user-123", roles)
			Expect(err).ToNot(HaveOccurred())
			Expect(receivedRoles).To(HaveLen(2))
			Expect(receivedRoles[0].ID).To(Equal("role-id-1"))
			Expect(receivedRoles[0].Name).To(Equal("admin"))
			Expect(receivedRoles[1].ID).To(Equal("role-id-2"))
			Expect(receivedRoles[1].Name).To(Equal("editor"))
		})

		It("returns an error when GetRealmRole fails", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// GetRealmRole fails
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/roles/admin" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.WriteHeader(http.StatusBadRequest)
			}))

			client = createTestClient(server.URL)
			roles := []*idp.Role{{Name: "admin"}}
			err := client.AssignOrganizationRolesToUser(ctx, "test-org", "user-123", roles)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get realm role"))
			Expect(err.Error()).To(ContainSubstring("admin"))
		})

		It("returns an error when role assignment fails", func() {
			clientRole := false
			role := keycloakRole{
				ID:         "role-id-1",
				Name:       "admin",
				ClientRole: &clientRole,
			}

			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")

				// GetRealmRole succeeds
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/roles/admin" {
					w.WriteHeader(http.StatusOK)
					if err := json.NewEncoder(w).Encode(role); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				// Role assignment fails
				if r.Method == http.MethodPost && r.URL.Path == "/admin/realms/osac/users/user-123/role-mappings/realm" {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}

				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)
			roles := []*idp.Role{{Name: "admin"}}
			err := client.AssignOrganizationRolesToUser(ctx, "test-org", "user-123", roles)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to assign realm roles to user"))
		})

		It("handles empty roles list", func() {
			var receivedRoles []keycloakRole
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Should only receive the POST request with empty array
				if r.Method == http.MethodPost && r.URL.Path == "/admin/realms/osac/users/user-123/role-mappings/realm" {
					if err := json.NewDecoder(r.Body).Decode(&receivedRoles); err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					w.WriteHeader(http.StatusNoContent)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)
			roles := []*idp.Role{}
			err := client.AssignOrganizationRolesToUser(ctx, "test-org", "user-123", roles)
			Expect(err).ToNot(HaveOccurred())
			Expect(receivedRoles).To(BeEmpty())
		})

		It("properly escapes user ID in URL", func() {
			clientRole := false
			role := keycloakRole{
				ID:         "role-id-1",
				Name:       "admin",
				ClientRole: &clientRole,
			}

			var requestedUserID string
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")

				// GetRealmRole
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/roles/admin" {
					w.WriteHeader(http.StatusOK)
					if err := json.NewEncoder(w).Encode(role); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				// Assignment - verify user ID with special characters is properly escaped
				if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/role-mappings/realm") {
					// Store the RequestURI (which preserves escaping) for verification
					requestedUserID = r.RequestURI
					w.WriteHeader(http.StatusNoContent)
					return
				}

				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)
			roles := []*idp.Role{{Name: "admin"}}
			// Use a user ID with special characters that need URL escaping
			err := client.AssignOrganizationRolesToUser(ctx, "test-org", "user/with@special#chars", roles)
			Expect(err).ToNot(HaveOccurred())
			// Verify the path contains the URL-escaped user ID (note: @ is not escaped by url.PathEscape)
			Expect(requestedUserID).To(ContainSubstring("user%2Fwith@special%23chars"))
		})
	})

	Describe("AssignClientRolesToUser", func() {
		It("assigns client roles to a user", func() {
			clients := []keycloakClient{
				{ID: "internal-uuid-123", ClientID: "realm-management"},
			}

			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// First call is to resolve client ID
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/clients" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					if err := json.NewEncoder(w).Encode(clients); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				// Second call is to assign roles
				Expect(r.Method).To(Equal(http.MethodPost))
				Expect(r.URL.Path).To(Equal("/admin/realms/osac/users/user-123/role-mappings/clients/internal-uuid-123"))
				w.WriteHeader(http.StatusNoContent)
			}))

			client = createTestClient(server.URL)
			roles := []*idp.Role{
				{ID: "role1", Name: "manage-users"},
			}
			err := client.AssignClientRolesToUser(ctx, "test-org", "user-123", "realm-management", roles)
			Expect(err).ToNot(HaveOccurred())
		})

		It("returns an error on server error", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}))

			client = createTestClient(server.URL)
			roles := []*idp.Role{{ID: "role1", Name: "manage-users"}}
			err := client.AssignClientRolesToUser(ctx, "test-org", "user-123", "realm-management", roles)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("RemoveOrganizationRolesFromUser", func() {
		It("removes realm roles from a user", func() {
			clientRole := false
			role1 := keycloakRole{
				ID:         "role-id-1",
				Name:       "admin",
				ClientRole: &clientRole,
			}
			role2 := keycloakRole{
				ID:         "role-id-2",
				Name:       "editor",
				ClientRole: &clientRole,
			}

			var receivedRoles []keycloakRole
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")

				// First call: get realm role "admin"
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/roles/admin" {
					w.WriteHeader(http.StatusOK)
					if err := json.NewEncoder(w).Encode(role1); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				// Second call: get realm role "editor"
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/roles/editor" {
					w.WriteHeader(http.StatusOK)
					if err := json.NewEncoder(w).Encode(role2); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				// Third call: remove roles from user
				if r.Method == http.MethodDelete && r.URL.Path == "/admin/realms/osac/users/user-123/role-mappings/realm" {
					if err := json.NewDecoder(r.Body).Decode(&receivedRoles); err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					w.WriteHeader(http.StatusNoContent)
					return
				}

				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)
			roles := []*idp.Role{
				{Name: "admin"},
				{Name: "editor"},
			}
			err := client.RemoveOrganizationRolesFromUser(ctx, "test-org", "user-123", roles)
			Expect(err).ToNot(HaveOccurred())
			Expect(receivedRoles).To(HaveLen(2))
			Expect(receivedRoles[0].ID).To(Equal("role-id-1"))
			Expect(receivedRoles[0].Name).To(Equal("admin"))
			Expect(receivedRoles[1].ID).To(Equal("role-id-2"))
			Expect(receivedRoles[1].Name).To(Equal("editor"))
		})

		It("returns an error when GetRealmRole fails", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// GetRealmRole fails
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/roles/admin" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.WriteHeader(http.StatusBadRequest)
			}))

			client = createTestClient(server.URL)
			roles := []*idp.Role{{Name: "admin"}}
			err := client.RemoveOrganizationRolesFromUser(ctx, "test-org", "user-123", roles)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get realm role"))
			Expect(err.Error()).To(ContainSubstring("admin"))
		})

		It("returns an error when role removal fails", func() {
			clientRole := false
			role := keycloakRole{
				ID:         "role-id-1",
				Name:       "admin",
				ClientRole: &clientRole,
			}

			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")

				// GetRealmRole succeeds
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/roles/admin" {
					w.WriteHeader(http.StatusOK)
					if err := json.NewEncoder(w).Encode(role); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				// Role removal fails
				if r.Method == http.MethodDelete && r.URL.Path == "/admin/realms/osac/users/user-123/role-mappings/realm" {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}

				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)
			roles := []*idp.Role{{Name: "admin"}}
			err := client.RemoveOrganizationRolesFromUser(ctx, "test-org", "user-123", roles)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to remove realm roles from user"))
		})

		It("handles empty roles list", func() {
			var receivedRoles []keycloakRole
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Should only receive the DELETE request with empty array
				if r.Method == http.MethodDelete && r.URL.Path == "/admin/realms/osac/users/user-123/role-mappings/realm" {
					if err := json.NewDecoder(r.Body).Decode(&receivedRoles); err != nil {
						http.Error(w, err.Error(), http.StatusBadRequest)
						return
					}
					w.WriteHeader(http.StatusNoContent)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)
			roles := []*idp.Role{}
			err := client.RemoveOrganizationRolesFromUser(ctx, "test-org", "user-123", roles)
			Expect(err).ToNot(HaveOccurred())
			Expect(receivedRoles).To(BeEmpty())
		})

		It("properly escapes user ID in URL", func() {
			clientRole := false
			role := keycloakRole{
				ID:         "role-id-1",
				Name:       "admin",
				ClientRole: &clientRole,
			}

			var requestedUserID string
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")

				// GetRealmRole
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/roles/admin" {
					w.WriteHeader(http.StatusOK)
					if err := json.NewEncoder(w).Encode(role); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				// Removal - verify user ID with special characters is properly escaped
				if r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/role-mappings/realm") {
					// Store the RequestURI (which preserves escaping) for verification
					requestedUserID = r.RequestURI
					w.WriteHeader(http.StatusNoContent)
					return
				}

				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)
			roles := []*idp.Role{{Name: "admin"}}
			// Use a user ID with special characters that need URL escaping
			err := client.RemoveOrganizationRolesFromUser(ctx, "test-org", "user/with@special#chars", roles)
			Expect(err).ToNot(HaveOccurred())
			// Verify the path contains the URL-escaped user ID (note: @ is not escaped by url.PathEscape)
			Expect(requestedUserID).To(ContainSubstring("user%2Fwith@special%23chars"))
		})

		It("handles role not found scenario", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// GetRealmRole returns 404 for non-existent role
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/roles/nonexistent-role" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.WriteHeader(http.StatusBadRequest)
			}))

			client = createTestClient(server.URL)
			roles := []*idp.Role{{Name: "nonexistent-role"}}
			err := client.RemoveOrganizationRolesFromUser(ctx, "test-org", "user-123", roles)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get realm role"))
			Expect(err.Error()).To(ContainSubstring("nonexistent-role"))
		})
	})

	Describe("RemoveClientRolesFromUser", func() {
		It("removes client roles from a user", func() {
			clients := []keycloakClient{
				{ID: "internal-uuid-123", ClientID: "realm-management"},
			}

			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// First call is to resolve client ID
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/clients" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					if err := json.NewEncoder(w).Encode(clients); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				// Second call is to remove roles
				Expect(r.Method).To(Equal(http.MethodDelete))
				Expect(r.URL.Path).To(Equal("/admin/realms/osac/users/user-123/role-mappings/clients/internal-uuid-123"))
				w.WriteHeader(http.StatusNoContent)
			}))

			client = createTestClient(server.URL)
			roles := []*idp.Role{
				{ID: "role1", Name: "manage-users"},
			}
			err := client.RemoveClientRolesFromUser(ctx, "test-org", "user-123", "realm-management", roles)
			Expect(err).ToNot(HaveOccurred())
		})

		It("returns an error on server error", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}))

			client = createTestClient(server.URL)
			roles := []*idp.Role{{ID: "role1", Name: "manage-users"}}
			err := client.RemoveClientRolesFromUser(ctx, "test-org", "user-123", "realm-management", roles)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("GetUserOrganizationRoles", func() {
		It("is not yet implemented (returns nil, nil)", func() {
			client = createTestClient(server.URL)
			result, err := client.GetUserOrganizationRoles(ctx, "test-org", "user-123")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(BeNil()) // TODO returns nil until implemented
		})
	})

	Describe("GetUserClientRoles", func() {
		It("gets client roles assigned to a user", func() {
			clientRole := true
			roles := []keycloakRole{
				{ID: "role1", Name: "manage-users", ClientRole: &clientRole},
			}
			clients := []keycloakClient{
				{ID: "internal-uuid-123", ClientID: "realm-management"},
			}

			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Method).To(Equal(http.MethodGet))

				// First call is to resolve client ID
				if r.URL.Path == "/admin/realms/osac/clients" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					if err := json.NewEncoder(w).Encode(clients); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				// Second call is to fetch user's client roles
				Expect(r.URL.Path).To(Equal("/admin/realms/osac/users/user-123/role-mappings/clients/internal-uuid-123"))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if err := json.NewEncoder(w).Encode(roles); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}))

			client = createTestClient(server.URL)
			result, err := client.GetUserClientRoles(ctx, "test-org", "user-123", "realm-management")
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(HaveLen(1))
			Expect(result[0].Name).To(Equal("manage-users"))
		})

		It("returns an error on server error", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}))

			client = createTestClient(server.URL)
			_, err := client.GetUserClientRoles(ctx, "test-org", "user-123", "realm-management")
			Expect(err).To(HaveOccurred())
		})

		It("returns an error on malformed JSON response when fetching roles", func() {
			clients := []keycloakClient{
				{ID: "internal-uuid-123", ClientID: "realm-management"},
			}

			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)

				// First call returns valid clients, second call returns invalid JSON
				if r.URL.Path == "/admin/realms/osac/clients" {
					if err := json.NewEncoder(w).Encode(clients); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
				} else {
					w.Write([]byte("invalid json"))
				}
			}))

			client = createTestClient(server.URL)
			_, err := client.GetUserClientRoles(ctx, "test-org", "user-123", "realm-management")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to decode user client roles"))
		})
	})

	Describe("AssignOrganizationAdminPermissions", func() {
		It("is not yet implemented (returns nil)", func() {
			client = createTestClient(server.URL)
			err := client.AssignOrganizationAdminPermissions(ctx, "test-org", "user-123")
			Expect(err).ToNot(HaveOccurred()) // TODO returns nil until implemented
		})
	})

	Describe("AssignIdpManagerPermissions", func() {
		It("assigns tenant-idp-manager role to user", func() {
			clientRole := false
			role := keycloakRole{
				ID:         "role-idp-manager",
				Name:       "tenant-idp-manager",
				ClientRole: &clientRole,
			}

			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")

				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/roles/tenant-idp-manager" {
					// Return the tenant-idp-manager role
					w.WriteHeader(http.StatusOK)
					if err := json.NewEncoder(w).Encode(role); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
				} else if r.Method == http.MethodPost && r.URL.Path == "/admin/realms/osac/users/user-123/role-mappings/realm" {
					// Assignment endpoint
					w.WriteHeader(http.StatusNoContent)
				} else {
					w.WriteHeader(http.StatusNotFound)
				}
			}))

			client = createTestClient(server.URL)
			err := client.AssignIdpManagerPermissions(ctx, "user-123")
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Describe("GetRealmClientByClientID", func() {
		It("resolves client ID from clientId attribute", func() {
			clients := []keycloakClient{
				{ID: "internal-uuid-123", ClientID: "realm-management"},
			}

			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.Method).To(Equal(http.MethodGet))
				Expect(r.URL.Path).To(Equal("/admin/realms/osac/clients"))
				Expect(r.URL.Query().Get("clientId")).To(Equal("realm-management"))

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if err := json.NewEncoder(w).Encode(clients); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}))

			client = createTestClient(server.URL)
			internalID, err := client.GetRealmClientByClientID(ctx, "realm-management", "osac")
			Expect(err).ToNot(HaveOccurred())
			Expect(internalID).To(Equal("internal-uuid-123"))
		})

		It("returns valid UUID immediately without making API call", func() {
			// Create a server that will fail if called
			serverCalled := false
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				serverCalled = true
				w.WriteHeader(http.StatusInternalServerError)
			}))

			client = createTestClient(server.URL)
			validUUID := "550e8400-e29b-41d4-a716-446655440000"
			internalID, err := client.GetRealmClientByClientID(ctx, validUUID, "osac")
			Expect(err).ToNot(HaveOccurred())
			Expect(internalID).To(Equal(validUUID))

			// Verify no HTTP call was made
			Expect(serverCalled).To(BeFalse())
		})

		It("returns an error when clientId is not found", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if err := json.NewEncoder(w).Encode([]keycloakClient{}); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}))

			client = createTestClient(server.URL)
			_, err := client.GetRealmClientByClientID(ctx, "not-a-valid-uuid", "osac")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})

		It("returns an error on server error", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}))

			client = createTestClient(server.URL)
			_, err := client.GetRealmClientByClientID(ctx, "realm-management", "osac")
			Expect(err).To(HaveOccurred())
		})

		It("returns an error on malformed JSON response", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("invalid json"))
			}))

			client = createTestClient(server.URL)
			_, err := client.GetRealmClientByClientID(ctx, "realm-management", "osac")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to decode clients response"))
		})

		It("caches realm-management UUID to avoid repeated API calls", func() {
			clients := []keycloakClient{
				{ID: "internal-uuid-123", ClientID: "realm-management"},
			}

			requestCount := 0
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount++
				Expect(r.Method).To(Equal(http.MethodGet))
				Expect(r.URL.Path).To(Equal("/admin/realms/osac/clients"))

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if err := json.NewEncoder(w).Encode(clients); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}))

			client = createTestClient(server.URL)

			// First call - makes HTTP request
			internalID1, err := client.GetRealmClientByClientID(ctx, "realm-management", "osac")
			Expect(err).ToNot(HaveOccurred())
			Expect(internalID1).To(Equal("internal-uuid-123"))
			Expect(requestCount).To(Equal(1))

			// Second call - uses cached UUID, no HTTP request
			internalID2, err := client.GetRealmClientByClientID(ctx, "realm-management", "osac")
			Expect(err).ToNot(HaveOccurred())
			Expect(internalID2).To(Equal("internal-uuid-123"))
			Expect(requestCount).To(Equal(1)) // Still 1, not incremented

			// Third call - uses cached UUID
			internalID3, err := client.GetRealmClientByClientID(ctx, "realm-management", "osac")
			Expect(err).ToNot(HaveOccurred())
			Expect(internalID3).To(Equal("internal-uuid-123"))
			Expect(requestCount).To(Equal(1))
		})

		It("does not cache UUIDs passed directly", func() {
			serverCalled := false
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				serverCalled = true
				w.WriteHeader(http.StatusInternalServerError)
			}))

			client = createTestClient(server.URL)
			validUUID := "550e8400-e29b-41d4-a716-446655440000"

			// First call with UUID
			id1, err := client.GetRealmClientByClientID(ctx, validUUID, "osac")
			Expect(err).ToNot(HaveOccurred())
			Expect(id1).To(Equal(validUUID))
			Expect(serverCalled).To(BeFalse())

			// Second call with same UUID
			id2, err := client.GetRealmClientByClientID(ctx, validUUID, "osac")
			Expect(err).ToNot(HaveOccurred())
			Expect(id2).To(Equal(validUUID))
			Expect(serverCalled).To(BeFalse()) // Still no HTTP call
		})
	})

	Describe("CreateAuthorizationResource", func() {
		It("creates an authorization resource in Keycloak", func() {
			var receivedResource *keycloakAuthorizationResource
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// First call: lookup authorization client UUID
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/clients" && r.URL.Query().Get("clientId") == "osac-authorization" {
					response := []keycloakClient{{
						ID:       "auth-client-uuid-123",
						ClientID: "osac-authorization",
					}}
					w.Header().Set("Content-Type", "application/json")
					if err := json.NewEncoder(w).Encode(response); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				// Second call: create authorization resource
				if r.Method == http.MethodPost && r.URL.Path == "/admin/realms/osac/clients/auth-client-uuid-123/authz/resource-server/resource" {
					receivedResource = &keycloakAuthorizationResource{}
					json.NewDecoder(r.Body).Decode(receivedResource)

					response := keycloakAuthorizationResource{
						ID:         "resource-uuid-456",
						Name:       receivedResource.Name,
						Type:       receivedResource.Type,
						Scopes:     receivedResource.Scopes,
						URIs:       receivedResource.URIs,
						Attributes: receivedResource.Attributes,
					}
					w.Header().Set("Content-Type", "application/json")
					if err := json.NewEncoder(w).Encode(response); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)

			resource := &idp.AuthorizationResource{
				Name: "PROJECT-acme-web-app",
				Scopes: []string{
					idp.ScopeViewProject,
					idp.ScopeManageProject,
				},
				Attributes: map[string][]string{
					"project_id": {"project-123"},
					"tenant":     {"acme"},
				},
			}

			createdResource, err := client.CreateAuthorizationResource(ctx, resource)

			Expect(err).ToNot(HaveOccurred())
			Expect(createdResource).ToNot(BeNil())
			Expect(createdResource.ID).To(Equal("resource-uuid-456"))
			Expect(createdResource.Name).To(Equal("PROJECT-acme-web-app"))
			Expect(createdResource.Scopes).To(ConsistOf(idp.ScopeViewProject, idp.ScopeManageProject))
			Expect(receivedResource.Name).To(Equal("PROJECT-acme-web-app"))
		})

		It("caches the authorization client UUID on subsequent calls", func() {
			clientLookupCount := 0
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Client UUID lookup
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/clients" {
					clientLookupCount++
					response := []keycloakClient{{
						ID:       "auth-client-uuid-123",
						ClientID: "osac-authorization",
					}}
					w.Header().Set("Content-Type", "application/json")
					if err := json.NewEncoder(w).Encode(response); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				// Create resource
				if r.Method == http.MethodPost {
					response := keycloakAuthorizationResource{
						ID:   "resource-uuid",
						Name: "PROJECT-test",
					}
					w.Header().Set("Content-Type", "application/json")
					if err := json.NewEncoder(w).Encode(response); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)

			resource := &idp.AuthorizationResource{Name: "PROJECT-test"}

			// First call - should lookup client UUID
			_, err := client.CreateAuthorizationResource(ctx, resource)
			Expect(err).ToNot(HaveOccurred())
			Expect(clientLookupCount).To(Equal(1))

			// Second call - should use cached UUID
			_, err = client.CreateAuthorizationResource(ctx, resource)
			Expect(err).ToNot(HaveOccurred())
			Expect(clientLookupCount).To(Equal(1)) // Should still be 1 (cached)
		})

		It("returns error when authorization client not found", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/clients" {
					// Return empty list
					w.Header().Set("Content-Type", "application/json")
					if err := json.NewEncoder(w).Encode([]keycloakClient{}); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)

			resource := &idp.AuthorizationResource{Name: "PROJECT-test"}
			_, err := client.CreateAuthorizationResource(ctx, resource)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get authorization client ID"))
		})
	})

	Describe("GetAuthorizationResource", func() {
		It("retrieves an authorization resource by ID", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Client UUID lookup
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/clients" {
					response := []keycloakClient{{
						ID:       "auth-client-uuid-123",
						ClientID: "osac-authorization",
					}}
					w.Header().Set("Content-Type", "application/json")
					if err := json.NewEncoder(w).Encode(response); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				// Get resource
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/clients/auth-client-uuid-123/authz/resource-server/resource/resource-456" {
					response := keycloakAuthorizationResource{
						ID:   "resource-456",
						Name: "PROJECT-acme-web-app",
						Scopes: []keycloakAuthorizationScope{
							{Name: idp.ScopeViewProject},
							{Name: idp.ScopeManageProject},
						},
					}
					w.Header().Set("Content-Type", "application/json")
					if err := json.NewEncoder(w).Encode(response); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)

			resource, err := client.GetAuthorizationResource(ctx, "resource-456")

			Expect(err).ToNot(HaveOccurred())
			Expect(resource).ToNot(BeNil())
			Expect(resource.ID).To(Equal("resource-456"))
			Expect(resource.Name).To(Equal("PROJECT-acme-web-app"))
			Expect(resource.Scopes).To(ConsistOf(idp.ScopeViewProject, idp.ScopeManageProject))
		})

		It("returns error when resource not found", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Client UUID lookup
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/clients" {
					response := []keycloakClient{{
						ID:       "auth-client-uuid-123",
						ClientID: "osac-authorization",
					}}
					w.Header().Set("Content-Type", "application/json")
					if err := json.NewEncoder(w).Encode(response); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				// Resource not found
				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)

			_, err := client.GetAuthorizationResource(ctx, "nonexistent-resource")

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get authorization resource"))
		})
	})

	Describe("DeleteAuthorizationResource", func() {
		It("deletes an authorization resource by ID", func() {
			deletedResourceID := ""
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Client UUID lookup
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/clients" {
					response := []keycloakClient{{
						ID:       "auth-client-uuid-123",
						ClientID: "osac-authorization",
					}}
					w.Header().Set("Content-Type", "application/json")
					if err := json.NewEncoder(w).Encode(response); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				// Delete resource
				if r.Method == http.MethodDelete && r.URL.Path == "/admin/realms/osac/clients/auth-client-uuid-123/authz/resource-server/resource/resource-456" {
					deletedResourceID = "resource-456"
					w.WriteHeader(http.StatusNoContent)
					return
				}

				w.WriteHeader(http.StatusNotFound)
			}))

			client = createTestClient(server.URL)

			err := client.DeleteAuthorizationResource(ctx, "resource-456")

			Expect(err).ToNot(HaveOccurred())
			Expect(deletedResourceID).To(Equal("resource-456"))
		})

		It("returns error when deletion fails", func() {
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Client UUID lookup
				if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/clients" {
					response := []keycloakClient{{
						ID:       "auth-client-uuid-123",
						ClientID: "osac-authorization",
					}}
					w.Header().Set("Content-Type", "application/json")
					if err := json.NewEncoder(w).Encode(response); err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					return
				}

				// Deletion fails
				w.WriteHeader(http.StatusInternalServerError)
			}))

			client = createTestClient(server.URL)

			err := client.DeleteAuthorizationResource(ctx, "resource-456")

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to delete authorization resource"))
		})
	})

	Describe("Identity Provider Operations", func() {
		Describe("CreateIdentityProvider", func() {
			It("creates an identity provider in Keycloak", func() {
				var receivedIdp *keycloakIdentityProvider
				var createdResponse keycloakIdentityProvider
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" {
						// Organization lookup
						orgs := []keycloakOrganization{{
							ID:   "org-123",
							Name: "test-org",
						}}
						w.Header().Set("Content-Type", "application/json")
						if err := json.NewEncoder(w).Encode(orgs); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}
					if r.Method == http.MethodPost && r.URL.Path == "/admin/realms/osac/organizations/org-123/identity-providers" {
						// Link IdP to organization
						w.WriteHeader(http.StatusNoContent)
						return
					}
					if r.Method == http.MethodPost && r.URL.Path == "/admin/realms/osac/identity-provider/instances" {
						// Create request
						receivedIdp = &keycloakIdentityProvider{}
						err := json.NewDecoder(r.Body).Decode(receivedIdp)
						if err != nil {
							http.Error(w, err.Error(), http.StatusBadRequest)
							return
						}

						// Store the created IdP for subsequent GET
						createdResponse = keycloakIdentityProvider{
							Alias:       receivedIdp.Alias,
							DisplayName: receivedIdp.DisplayName,
							InternalID:  "idp-uuid-123",
							ProviderID:  receivedIdp.ProviderID,
							Enabled:     receivedIdp.Enabled,
							Config:      receivedIdp.Config,
						}
						w.WriteHeader(http.StatusCreated)
						return
					}
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations/org-123/identity-providers/corporate-ldap" {
						// Get request after creation (via organization)
						w.Header().Set("Content-Type", "application/json")
						if err := json.NewEncoder(w).Encode(createdResponse); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))

				client = createTestClient(server.URL)

				idpProvider := &idp.IdentityProvider{
					Alias:       "corporate-ldap",
					DisplayName: "Corporate LDAP",
					Type:        "ldap",
					Enabled:     true,
					Config: map[string]string{
						"connectionUrl": "ldap://ldap.example.com:389",
						"bindDn":        "cn=admin,dc=example,dc=com",
					},
				}
				createdIdp, err := client.CreateIdentityProvider(ctx, "test-org", idpProvider)
				Expect(err).ToNot(HaveOccurred())
				Expect(receivedIdp).ToNot(BeNil(), "server handler should have set receivedIdp")
				Expect(receivedIdp.Alias).To(Equal("corporate-ldap"))
				Expect(receivedIdp.ProviderID).To(Equal("ldap"))
				Expect(createdIdp).ToNot(BeNil())
				Expect(createdIdp.Alias).To(Equal("corporate-ldap"))
				Expect(createdIdp.DisplayName).To(Equal("Corporate LDAP"))
				Expect(createdIdp.Type).To(Equal("ldap"))
				Expect(createdIdp.Enabled).To(BeTrue())
				Expect(createdIdp.Config).To(HaveKeyWithValue("connectionUrl", "ldap://ldap.example.com:389"))
			})

			It("creates an OIDC identity provider", func() {
				var createdResponse keycloakIdentityProvider
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" {
						// Organization lookup
						orgs := []keycloakOrganization{{
							ID:   "org-123",
							Name: "test-org",
						}}
						w.Header().Set("Content-Type", "application/json")
						if err := json.NewEncoder(w).Encode(orgs); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}
					if r.Method == http.MethodPost && r.URL.Path == "/admin/realms/osac/organizations/org-123/identity-providers" {
						// Link IdP to organization
						w.WriteHeader(http.StatusNoContent)
						return
					}
					if r.Method == http.MethodPost && r.URL.Path == "/admin/realms/osac/identity-provider/instances" {
						var receivedIdp keycloakIdentityProvider
						json.NewDecoder(r.Body).Decode(&receivedIdp)

						createdResponse = keycloakIdentityProvider{
							Alias:       receivedIdp.Alias,
							DisplayName: receivedIdp.DisplayName,
							InternalID:  "idp-uuid-456",
							ProviderID:  receivedIdp.ProviderID,
							Enabled:     receivedIdp.Enabled,
							Config:      receivedIdp.Config,
						}
						w.WriteHeader(http.StatusCreated)
						return
					}
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations/org-123/identity-providers/google-sso" {
						// Get request after creation (via organization)
						w.Header().Set("Content-Type", "application/json")
						if err := json.NewEncoder(w).Encode(createdResponse); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))

				client = createTestClient(server.URL)

				idpProvider := &idp.IdentityProvider{
					Alias:       "google-sso",
					DisplayName: "Google SSO",
					Type:        "oidc",
					Enabled:     true,
					Config: map[string]string{
						"authorizationUrl": "https://accounts.google.com/o/oauth2/v2/auth",
						"tokenUrl":         "https://oauth2.googleapis.com/token",
						"clientId":         "my-client-id",
						"clientSecret":     "my-client-secret",
					},
				}
				createdIdp, err := client.CreateIdentityProvider(ctx, "test-org", idpProvider)
				Expect(err).ToNot(HaveOccurred())
				Expect(createdIdp).ToNot(BeNil())
				Expect(createdIdp.Alias).To(Equal("google-sso"))
				Expect(createdIdp.Type).To(Equal("oidc"))
				Expect(createdIdp.Config).To(HaveKeyWithValue("authorizationUrl", "https://accounts.google.com/o/oauth2/v2/auth"))
			})

			It("returns an error when creation fails", func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusConflict)
					w.Write([]byte(`{"errorMessage":"Identity Provider with alias already exists"}`))
				}))

				client = createTestClient(server.URL)

				idpProvider := &idp.IdentityProvider{
					Alias:   "existing-idp",
					Type:    "ldap",
					Enabled: true,
				}
				_, err := client.CreateIdentityProvider(ctx, "test-org", idpProvider)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to create identity provider"))
			})
		})

		Describe("GetIdentityProvider", func() {
			It("retrieves an identity provider by alias", func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" {
						// Organization lookup
						orgs := []keycloakOrganization{{
							ID:   "org-123",
							Name: "org-1",
						}}
						w.Header().Set("Content-Type", "application/json")
						if err := json.NewEncoder(w).Encode(orgs); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations/org-123/identity-providers/corporate-ldap" {
						response := keycloakIdentityProvider{
							Alias:       "corporate-ldap",
							DisplayName: "Corporate LDAP",
							InternalID:  "idp-123",
							ProviderID:  "ldap",
							Enabled:     true,
							Config: map[string]string{
								"connectionUrl": "ldap://ldap.example.com:389",
								"bindDn":        "cn=admin,dc=example,dc=com",
							},
						}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						if err := json.NewEncoder(w).Encode(response); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))

				client = createTestClient(server.URL)

				idpProvider, err := client.GetIdentityProvider(ctx, "org-1", "corporate-ldap")
				Expect(err).ToNot(HaveOccurred())
				Expect(idpProvider).ToNot(BeNil())
				Expect(idpProvider.Alias).To(Equal("corporate-ldap"))
				Expect(idpProvider.DisplayName).To(Equal("Corporate LDAP"))
				Expect(idpProvider.Type).To(Equal("ldap"))
				Expect(idpProvider.Enabled).To(BeTrue())
				Expect(idpProvider.Config).To(HaveKeyWithValue("connectionUrl", "ldap://ldap.example.com:389"))
			})

			It("returns an error when IdP not found", func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" {
						// Organization lookup succeeds
						orgs := []keycloakOrganization{{
							ID:   "org-123",
							Name: "org-1",
						}}
						w.Header().Set("Content-Type", "application/json")
						if err := json.NewEncoder(w).Encode(orgs); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}
					// IdP lookup fails (all other paths)
					w.WriteHeader(http.StatusNotFound)
				}))

				client = createTestClient(server.URL)

				_, err := client.GetIdentityProvider(ctx, "org-1", "nonexistent")
				Expect(err).To(HaveOccurred())
			})
		})

		Describe("GetOrganizationIdentityProvider", func() {
			It("retrieves an IdP assigned to an organization", func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// Handle GetOrganization request
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" && r.URL.RawQuery == "exact=true&search=test-org" {
						response := []keycloakOrganization{{
							ID:   "org-123",
							Name: "test-org",
						}}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						if err := json.NewEncoder(w).Encode(response); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}

					// Handle GetOrganizationIdentityProvider request
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations/org-123/identity-providers/corporate-ldap" {
						response := keycloakIdentityProvider{
							Alias:       "corporate-ldap",
							DisplayName: "Corporate LDAP",
							ProviderID:  "ldap",
							Enabled:     true,
							Config: map[string]string{
								"connectionUrl": "ldap://ldap.example.com:389",
							},
						}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						if err := json.NewEncoder(w).Encode(response); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}

					w.WriteHeader(http.StatusNotFound)
				}))

				client = createTestClient(server.URL)

				idpProvider, err := client.GetIdentityProvider(ctx, "test-org", "corporate-ldap")
				Expect(err).ToNot(HaveOccurred())
				Expect(idpProvider).ToNot(BeNil())
				Expect(idpProvider.Alias).To(Equal("corporate-ldap"))
				Expect(idpProvider.DisplayName).To(Equal("Corporate LDAP"))
				Expect(idpProvider.Type).To(Equal("ldap"))
			})

			It("returns error when IdP not assigned to organization", func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// Handle GetOrganization request
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" && r.URL.RawQuery == "exact=true&search=test-org" {
						response := []keycloakOrganization{{
							ID:   "org-123",
							Name: "test-org",
						}}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						if err := json.NewEncoder(w).Encode(response); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}

					// IdP not assigned to organization
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations/org-123/identity-providers/nonexistent" {
						w.WriteHeader(http.StatusNotFound)
						return
					}

					w.WriteHeader(http.StatusNotFound)
				}))

				client = createTestClient(server.URL)

				_, err := client.GetIdentityProvider(ctx, "test-org", "nonexistent")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to get identity provider"))
			})

			It("returns error when organization not found", func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// Organization doesn't exist
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" {
						response := []keycloakOrganization{}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						if err := json.NewEncoder(w).Encode(response); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))

				client = createTestClient(server.URL)

				_, err := client.GetIdentityProvider(ctx, "nonexistent-org", "corporate-ldap")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("organization"))
			})
		})

		Describe("ListIdentityProviders", func() {
			It("lists IdPs assigned to an organization", func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// Handle GetOrganization request
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" && r.URL.RawQuery == "exact=true&search=test-org" {
						response := []keycloakOrganization{{
							ID:   "org-123",
							Name: "test-org",
						}}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						if err := json.NewEncoder(w).Encode(response); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}

					// Handle ListIdentityProviders request
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations/org-123/identity-providers" {
						response := []keycloakIdentityProvider{
							{
								Alias:       "corporate-ldap",
								DisplayName: "Corporate LDAP",
								ProviderID:  "ldap",
								Enabled:     true,
							},
							{
								Alias:       "okta-sso",
								DisplayName: "Okta SSO",
								ProviderID:  "oidc",
								Enabled:     true,
							},
						}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						if err := json.NewEncoder(w).Encode(response); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}

					w.WriteHeader(http.StatusNotFound)
				}))

				client = createTestClient(server.URL)

				idps, err := client.ListIdentityProviders(ctx, "test-org")
				Expect(err).ToNot(HaveOccurred())
				Expect(idps).To(HaveLen(2))
				Expect(idps[0].Alias).To(Equal("corporate-ldap"))
				Expect(idps[0].Type).To(Equal("ldap"))
				Expect(idps[1].Alias).To(Equal("okta-sso"))
				Expect(idps[1].Type).To(Equal("oidc"))
			})

			It("returns empty list when no IdPs assigned to organization", func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// Handle GetOrganization request
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" && r.URL.RawQuery == "exact=true&search=test-org" {
						response := []keycloakOrganization{{
							ID:   "org-123",
							Name: "test-org",
						}}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						if err := json.NewEncoder(w).Encode(response); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}

					// No IdPs assigned
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations/org-123/identity-providers" {
						response := []keycloakIdentityProvider{}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						if err := json.NewEncoder(w).Encode(response); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}

					w.WriteHeader(http.StatusNotFound)
				}))

				client = createTestClient(server.URL)

				idps, err := client.ListIdentityProviders(ctx, "test-org")
				Expect(err).ToNot(HaveOccurred())
				Expect(idps).To(BeEmpty())
			})

			It("returns error when organization not found", func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// Organization doesn't exist
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" {
						response := []keycloakOrganization{}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						if err := json.NewEncoder(w).Encode(response); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))

				client = createTestClient(server.URL)

				_, err := client.ListIdentityProviders(ctx, "nonexistent-org")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("organization"))
			})
		})
	})

	Describe("Authorization Groups", func() {
		Describe("CreateAuthorizationGroup", func() {
			It("should create hierarchical organization groups", func() {
				createdGroups := make(map[string]string) // name -> id
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// First request: get organization by name
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" && r.URL.RawQuery == "exact=true&search=acme-corp" {
						orgs := []keycloakOrganization{
							{ID: "org-123", Name: "acme-corp"},
						}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						if err := json.NewEncoder(w).Encode(orgs); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}
					// Create parent group: /web-app
					if r.Method == http.MethodPost && r.URL.Path == "/admin/realms/osac/organizations/org-123/groups" {
						var payload map[string]interface{}
						if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
							http.Error(w, err.Error(), http.StatusBadRequest)
							return
						}
						groupName := payload["name"].(string)
						groupID := "group-" + groupName
						createdGroups[groupName] = groupID
						w.Header().Set("Location", "/admin/realms/osac/organizations/org-123/groups/"+groupID)
						w.WriteHeader(http.StatusCreated)
						return
					}
					// Create child group: system:viewers under /web-app
					if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/children") {
						var payload map[string]interface{}
						if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
							http.Error(w, err.Error(), http.StatusBadRequest)
							return
						}
						groupName := payload["name"].(string)
						groupID := "group-" + groupName
						createdGroups[groupName] = groupID
						w.Header().Set("Location", "/admin/realms/osac/organizations/org-123/groups/"+groupID)
						w.WriteHeader(http.StatusCreated)
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))

				client = createTestClient(server.URL)

				groupID, err := client.CreateAuthorizationGroup(ctx, "acme-corp", "system:viewers", "/web-app/system:viewers")
				Expect(err).ToNot(HaveOccurred())
				Expect(groupID).To(Equal(createdGroups["system:viewers"]))
				Expect(createdGroups).To(HaveKey("web-app"))
				Expect(createdGroups).To(HaveKey("system:viewers"))
			})

			It("should return error when organization is not found", func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						if err := json.NewEncoder(w).Encode([]keycloakOrganization{}); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))

				client = createTestClient(server.URL)

				groupID, err := client.CreateAuthorizationGroup(ctx, "nonexistent-org", "system:viewers", "/web-app/system:viewers")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to get organization"))
				Expect(groupID).To(BeEmpty())
			})

			It("should handle 409 conflict by looking up existing group", func() {
				createdGroups := make(map[string]string)
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// Get organization
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" && strings.Contains(r.URL.RawQuery, "search=acme-corp") {
						orgs := []keycloakOrganization{
							{ID: "org-123", Name: "acme-corp"},
						}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						if err := json.NewEncoder(w).Encode(orgs); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}
					// List or search for existing group
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations/org-123/groups" {
						// Return the existing group for both search and list operations
						groups := []struct {
							ID   string `json:"id"`
							Path string `json:"path"`
						}{
							{ID: "existing-group-id", Path: "/web-app"},
						}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						if err := json.NewEncoder(w).Encode(groups); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}
					// Create parent group fails with 409
					if r.Method == http.MethodPost && r.URL.Path == "/admin/realms/osac/organizations/org-123/groups" {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusConflict)
						json.NewEncoder(w).Encode(map[string]string{"errorMessage": "Group with the given name already exists."})
						return
					}
					// Create child group under existing parent
					if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/children") {
						var payload map[string]interface{}
						if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
							http.Error(w, err.Error(), http.StatusBadRequest)
							return
						}
						groupName := payload["name"].(string)
						groupID := "group-" + groupName
						createdGroups[groupName] = groupID
						w.Header().Set("Location", "/admin/realms/osac/organizations/org-123/groups/"+groupID)
						w.WriteHeader(http.StatusCreated)
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))

				client = createTestClient(server.URL)

				groupID, err := client.CreateAuthorizationGroup(ctx, "acme-corp", "system:viewers", "/web-app/system:viewers")
				Expect(err).ToNot(HaveOccurred())
				Expect(groupID).To(Equal(createdGroups["system:viewers"]))
				Expect(createdGroups).To(HaveKey("system:viewers"))
			})
		})

		Describe("DeleteAuthorizationGroup", func() {
			It("should delete an organization group by ID", func() {
				var deletedGroupID string
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// First request: get organization
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" && r.URL.RawQuery == "exact=true&search=acme-corp" {
						orgs := []keycloakOrganization{
							{ID: "org-123", Name: "acme-corp"},
						}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						if err := json.NewEncoder(w).Encode(orgs); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}
					// Second request: delete organization group
					if r.Method == http.MethodDelete && r.URL.Path == "/admin/realms/osac/organizations/org-123/groups/group-id-123" {
						deletedGroupID = "group-id-123"
						w.WriteHeader(http.StatusNoContent)
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))

				client = createTestClient(server.URL)

				err := client.DeleteAuthorizationGroup(ctx, "acme-corp", "group-id-123")
				Expect(err).ToNot(HaveOccurred())
				Expect(deletedGroupID).To(Equal("group-id-123"))
			})

			It("should return error when organization is not found", func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						if err := json.NewEncoder(w).Encode([]keycloakOrganization{}); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))

				client = createTestClient(server.URL)

				err := client.DeleteAuthorizationGroup(ctx, "nonexistent-org", "group-id-123")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to get organization"))
			})

			It("should return error when group deletion fails", func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// First request: get organization
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" {
						orgs := []keycloakOrganization{
							{ID: "org-123", Name: "acme-corp"},
						}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						if err := json.NewEncoder(w).Encode(orgs); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}
					// Second request: delete fails
					if r.Method == http.MethodDelete && r.URL.Path == "/admin/realms/osac/organizations/org-123/groups/nonexistent-group" {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))

				client = createTestClient(server.URL)

				err := client.DeleteAuthorizationGroup(ctx, "acme-corp", "nonexistent-group")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to delete organization group"))
			})
		})

		Describe("GetGroupIDByPath", func() {
			It("should find an organization group by its path", func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// First request: get organization
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" && r.URL.RawQuery == "exact=true&search=acme-corp" {
						orgs := []keycloakOrganization{
							{ID: "org-123", Name: "acme-corp"},
						}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						if err := json.NewEncoder(w).Encode(orgs); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}
					// Second request: list organization groups (no search parameter - returns all groups)
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations/org-123/groups" {
						// Return all groups - implementation searches recursively through the list
						groups := []struct {
							ID   string `json:"id"`
							Path string `json:"path"`
						}{
							{ID: "group-123", Path: "/web-app/system:viewers"},
							{ID: "group-456", Path: "/web-app/system:managers"},
						}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						if err := json.NewEncoder(w).Encode(groups); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))

				client = createTestClient(server.URL)

				groupID, err := client.GetGroupIDByPath(ctx, "acme-corp", "/web-app/system:viewers")
				Expect(err).ToNot(HaveOccurred())
				Expect(groupID).To(Equal("group-123"))
			})

			It("should return error when organization is not found", func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						if err := json.NewEncoder(w).Encode([]keycloakOrganization{}); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))

				client = createTestClient(server.URL)

				_, err := client.GetGroupIDByPath(ctx, "nonexistent-org", "/web-app/system:viewers")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to get organization"))
			})

			It("should return error when group is not found", func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// First request: get organization
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" {
						orgs := []keycloakOrganization{
							{ID: "org-123", Name: "acme-corp"},
						}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						if err := json.NewEncoder(w).Encode(orgs); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}
					// Second request: search returns empty
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations/org-123/groups" {
						groups := []struct {
							ID   string `json:"id"`
							Path string `json:"path"`
						}{}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						if err := json.NewEncoder(w).Encode(groups); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))

				client = createTestClient(server.URL)

				_, err := client.GetGroupIDByPath(ctx, "acme-corp", "/nonexistent-group")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("organization group not found"))
			})

			It("should return error when list fails", func() {
				server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// First request: get organization
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations" {
						orgs := []keycloakOrganization{
							{ID: "org-123", Name: "acme-corp"},
						}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						if err := json.NewEncoder(w).Encode(orgs); err != nil {
							http.Error(w, err.Error(), http.StatusInternalServerError)
							return
						}
						return
					}
					// Second request: list fails
					if r.Method == http.MethodGet && r.URL.Path == "/admin/realms/osac/organizations/org-123/groups" {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					w.WriteHeader(http.StatusNotFound)
				}))

				client = createTestClient(server.URL)

				_, err := client.GetGroupIDByPath(ctx, "acme-corp", "/web-app/system:viewers")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to list organization groups"))
			})
		})
	})
})

func createTestClient(serverURL string) *Client {
	tokenSource, err := auth.NewStaticTokenSource().
		SetLogger(logger).
		SetToken(&auth.Token{Access: "test-token"}).
		Build()
	Expect(err).ToNot(HaveOccurred())

	client, err := NewClient().
		SetLogger(logger).
		SetBaseURL(serverURL).
		SetTokenSource(tokenSource).
		Build()
	Expect(err).ToNot(HaveOccurred())

	return client
}
