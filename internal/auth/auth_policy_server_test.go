/*
Copyright (c) 2026 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package auth

import (
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
)

var _ = Describe("Policy server", func() {
	Describe("Build", func() {
		It("Fails if logger is not set", func() {
			_, err := NewPolicyServer().
				Build()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("logger"))
		})

		It("Succeeds if logger is set", func() {
			server, err := NewPolicyServer().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})
	})

	Describe("Serve", func() {
		var server *PolicyServer

		BeforeEach(func() {
			var err error
			server, err = NewPolicyServer().
				SetLogger(logger).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		It("Returns the policy for a valid file name", func() {
			request := httptest.NewRequest(http.MethodGet, "/authz.rego", nil)
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusOK))
			Expect(recorder.Header().Get("Content-Type")).To(Equal("text/plain"))
			Expect(recorder.Body.String()).To(ContainSubstring("import rego.v1"))
			Expect(recorder.Body.String()).To(ContainSubstring("allow if"))
		})

		It("Returns 404 for a non-existent file", func() {
			request := httptest.NewRequest(http.MethodGet, "/nonexistent.rego", nil)
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusNotFound))
		})

		It("Returns 404 for the root path", func() {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusNotFound))
		})

		It("Renders policy values", func() {
			server, err := NewPolicyServer().
				SetLogger(logger).
				SetValues(map[string]any{
					"namespace": "my-ns",
				}).
				Build()
			Expect(err).ToNot(HaveOccurred())

			request := httptest.NewRequest(http.MethodGet, "/authz.rego", nil)
			recorder := httptest.NewRecorder()
			server.ServeHTTP(recorder, request)
			Expect(recorder.Code).To(Equal(http.StatusOK))
			Expect(recorder.Body.String()).To(ContainSubstring("system:serviceaccount:my-ns:admin"))
			Expect(recorder.Body.String()).To(ContainSubstring("system:serviceaccount:my-ns:controller"))
		})
	})
})
