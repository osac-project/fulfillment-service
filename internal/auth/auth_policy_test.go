/*
Copyright (c) 2025 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/ginkgo/v2/dsl/decorators"
	. "github.com/onsi/ginkgo/v2/dsl/table"
	. "github.com/onsi/gomega"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/rego"

	"github.com/osac-project/fulfillment-service/internal/collections"
)

var _ = Describe("Authorization rules", Ordered, func() {
	var (
		ctx   context.Context
		rules string
		query rego.PreparedEvalQuery
	)

	BeforeAll(func() {
		var err error

		// Create a context:
		ctx = context.Background()

		// Create the policy server with template values:
		policyServer, err := NewPolicyServer().
			SetLogger(logger).
			SetValues(map[string]any{
				"namespace": "my-ns",
			}).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Fetch the authorization policy from the policy server:
		request := httptest.NewRequest(http.MethodGet, "/authz.rego", nil)
		recorder := httptest.NewRecorder()
		policyServer.ServeHTTP(recorder, request)
		Expect(recorder.Code).To(Equal(http.StatusOK))
		rules = recorder.Body.String()
		Expect(rules).ToNot(BeEmpty())

		// Authorino adds the package name and sets the default value for the allow variable, so
		// we need to do the same.
		buffer := &strings.Builder{}
		fmt.Fprintf(buffer, "package authz\n")
		fmt.Fprintf(buffer, "default allow := false\n")
		buffer.WriteString(rules)
		rules = buffer.String()

		// Prepare the query for evaluation. This compiles the rules once so that the compiled
		// query can be reused across multiple evaluations with different inputs.
		query, err = rego.New(
			rego.Query(`
				allow := data.authz.allow;
				subject_json := data.authz.subject_json
			`),
			rego.Module("authz.rego", rules),
		).PrepareForEval(ctx)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Can compile the rules", func() {
		module, err := ast.ParseModule("authz.rego", rules)
		Expect(err).ToNot(HaveOccurred())
		compiler := ast.NewCompiler()
		compiler.Compile(map[string]*ast.Module{
			"authz.rego": module,
		})
		Expect(compiler.Errors).To(BeEmpty())
		Expect(module.Rules).ToNot(BeEmpty())
	})

	// makeRequestContext creates a context that simulates a request for the given gRPC method.
	makeRequestContext := func(method string) any {
		return map[string]any{
			"request": map[string]any{
				"http": map[string]any{
					"path": method,
				},
			},
		}
	}

	// makeKubernetesSaAuth creates an authentication object that simulates a request from the given Kubernetes
	// service account.
	makeKubernetesSaAuth := func(user string) any {
		return map[string]any{
			"identity": map[string]any{
				"user": map[string]any{
					"username": fmt.Sprintf("system:serviceaccount:my-ns:%s", user),
				},
			},
		}
	}

	// makeKeycloakUserAuth creates an authentication object that simulates a request from the given Keycloak user.
	// If the identity map is not nil, it is merged with the default identity map.
	makeKeycloakUserAuth := func(name string, identity map[string]any) any {
		merged := map[string]any{
			"username": name,
		}
		if identity != nil {
			maps.Copy(merged, identity)
		}
		return map[string]any{
			"identity": merged,
		}
	}

	// makeKeycloakSaAuth creates an authentication object that simulates a request from the given Keycloak service
	// account.
	makeKeycloakSaAuth := func(name string, identity map[string]any) any {
		merged := map[string]any{
			"username": fmt.Sprintf("service-account-%s", name),
		}
		if identity != nil {
			maps.Copy(merged, identity)
		}
		return map[string]any{
			"identity": merged,
		}
	}

	// evaluateRules evaluates the authorization rules for the given input and returns the values of the 'allow' and
	// 'subject_json' variables. The 'allow' variable is returned as a boolean and the 'subject_json' variable is
	// deserialized into a 'Subject' object.
	evaluateRules := func(input map[string]any) (allow bool, subject *Subject) {
		results, err := query.Eval(ctx, rego.EvalInput(input))
		Expect(err).ToNot(HaveOccurred())
		Expect(results).To(HaveLen(1))

		// Get the value of the 'allow' variable:
		allowValue, ok := results[0].Bindings["allow"].(bool)
		Expect(ok).To(BeTrue())

		// Get the value of the 'subject_json' variable:
		subjectJson, ok := results[0].Bindings["subject_json"].(string)
		Expect(ok).To(BeTrue())
		var subjectValue Subject
		err = json.Unmarshal([]byte(subjectJson), &subjectValue)
		Expect(err).ToNot(HaveOccurred())

		// Return the values:
		allow = allowValue
		subject = &subjectValue
		return
	}

	It("Allows Kubernetes service accounts to use the public API", func() {
		input := map[string]any{
			"context": makeRequestContext("/osac.public.v1.Clusters/Create"),
			"auth":    makeKubernetesSaAuth("client"),
		}
		allow, _ := evaluateRules(input)
		Expect(allow).To(BeTrue())
	})

	It("Allows Keycloak users to use the public API", func() {
		input := map[string]any{
			"context": makeRequestContext("/osac.public.v1.Clusters/Create"),
			"auth":    makeKeycloakUserAuth("my-user", nil),
		}
		allow, _ := evaluateRules(input)
		Expect(allow).To(BeTrue())
	})

	It("Doesn't allow the Kubernetes 'client' service account to use the private API", func() {
		input := map[string]any{
			"context": makeRequestContext("/osac.private.v1.Clusters/Create"),
			"auth":    makeKubernetesSaAuth("client"),
		}
		allow, _ := evaluateRules(input)
		Expect(allow).To(BeFalse())
	})

	It("Doesn't allow Keycloak regular users to use the private API", func() {
		input := map[string]any{
			"context": makeRequestContext("/osac.private.v1.Clusters/Create"),
			"auth":    makeKeycloakUserAuth("my-user", nil),
		}
		allow, _ := evaluateRules(input)
		Expect(allow).To(BeFalse())
	})

	Describe("User calculation", func() {
		It("Returns the the user name for Keycloak users", func() {
			input := map[string]any{
				"context": makeRequestContext("/osac.public.v1.Clusters/Create"),
				"auth":    makeKeycloakUserAuth("my-user", nil),
			}
			_, subject := evaluateRules(input)
			Expect(subject.User).To(Equal("my-user"))
		})

		It("Returns the the service account name for Keycloak service accounts", func() {
			input := map[string]any{
				"context": makeRequestContext("/osac.public.v1.Clusters/Create"),
				"auth":    makeKeycloakSaAuth("my-sa", nil),
			}
			_, subject := evaluateRules(input)
			Expect(subject.User).To(Equal("service-account-my-sa"))
		})

		DescribeTable(
			"Returns the service account name for Kubernetes service accounts",
			func(name string) {
				input := map[string]any{
					"context": makeRequestContext("/osac.public.v1.Clusters/Create"),
					"auth":    makeKubernetesSaAuth(name),
				}
				_, subject := evaluateRules(input)
				Expect(subject.User).To(Equal(name))
			},
			Entry(
				"Administrator",
				"admin",
			),
			Entry(
				"Controller",
				"controller",
			),
			Entry(
				"Client",
				"client",
			),
		)
	})

	Describe("Tenant calculation", func() {
		DescribeTable(
			"Returns the universal set of tenants for the 'osac-admin' and 'osac-controller' service accounts",
			func(name string) {
				input := map[string]any{
					"context": makeRequestContext("/osac.public.v1.Clusters/Create"),
					"auth":    makeKeycloakSaAuth(name, nil),
				}
				_, subject := evaluateRules(input)
				Expect(subject.Tenants.Equal(AllTenants)).To(BeTrue())
			},
			Entry(
				"Administrator",
				"osac-admin",
			),
			Entry(
				"Controller",
				"osac-controller",
			),
		)

		DescribeTable(
			"Returns the values of the 'organization' claim for regular Keycloak users",
			func(organizations []string, expected collections.Set[string]) {
				input := map[string]any{
					"context": makeRequestContext("/osac.public.v1.Clusters/Create"),
					"auth": makeKeycloakUserAuth("my-user", map[string]any{
						"organization": organizations,
					}),
				}
				_, subject := evaluateRules(input)
				Expect(subject.Tenants.Equal(expected)).To(BeTrue())
			},
			Entry(
				"No tenants",
				[]string{},
				collections.NewSet[string](),
			),
			Entry(
				"One tenant",
				[]string{"tenant-a"},
				collections.NewSet("tenant-a"),
			),
			Entry(
				"Two tenants",
				[]string{
					"tenant-a",
					"tenant-b",
				},
				collections.NewSet(
					"tenant-a",
					"tenant-b",
				),
			),
		)

		DescribeTable(
			"Returns correct tenants for Kubernetes service accounts",
			func(name string, expected collections.Set[string]) {
				input := map[string]any{
					"context": makeRequestContext("/osac.public.v1.Clusters/Create"),
					"auth":    makeKubernetesSaAuth(name),
				}
				_, subject := evaluateRules(input)
				Expect(subject.Tenants.Equal(expected)).To(BeTrue())
			},
			Entry(
				"Administrator",
				"admin",
				AllTenants,
			),
			Entry(
				"Controller",
				"controller",
				AllTenants,
			),
			Entry(
				"Client",
				"client",
				collections.NewSet("my-ns"),
			),
		)
	})
})
