/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package servers

import (
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("extractTicket", func() {
	It("should extract from Authorization Bearer header", func() {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer my-jwt-token")

		ticket, err := extractTicket(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(ticket).To(Equal("my-jwt-token"))
	})

	It("should reject non-Bearer Authorization header", func() {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")

		_, err := extractTicket(req)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("Bearer"))
	})

	It("should extract from console-ticket cookie", func() {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "console-ticket", Value: "cookie-jwt"})

		ticket, err := extractTicket(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(ticket).To(Equal("cookie-jwt"))
	})

	It("should prefer Authorization header over cookie", func() {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer header-jwt")
		req.AddCookie(&http.Cookie{Name: "console-ticket", Value: "cookie-jwt"})

		ticket, err := extractTicket(req)
		Expect(err).NotTo(HaveOccurred())
		Expect(ticket).To(Equal("header-jwt"))
	})

	It("should return error when neither header nor cookie present", func() {
		req := httptest.NewRequest(http.MethodGet, "/", nil)

		_, err := extractTicket(req)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("missing ticket"))
	})

	It("should ignore empty cookie value", func() {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "console-ticket", Value: ""})

		_, err := extractTicket(req)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("missing ticket"))
	})
})

var _ = Describe("checkOrigin", func() {
	var handler *ConsoleProxyWSHandler

	It("should accept any non-empty origin with wildcard", func() {
		handler = &ConsoleProxyWSHandler{allowedOrigins: []string{"*"}}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Origin", "https://evil.com")
		Expect(handler.checkOrigin(req)).To(BeTrue())
	})

	It("should accept exact match", func() {
		handler = &ConsoleProxyWSHandler{allowedOrigins: []string{"https://console.example.com"}}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Origin", "https://console.example.com")
		Expect(handler.checkOrigin(req)).To(BeTrue())
	})

	It("should reject non-matching origin", func() {
		handler = &ConsoleProxyWSHandler{allowedOrigins: []string{"https://console.example.com"}}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Origin", "https://evil.com")
		Expect(handler.checkOrigin(req)).To(BeFalse())
	})

	It("should reject empty origin even with wildcard", func() {
		handler = &ConsoleProxyWSHandler{allowedOrigins: []string{"*"}}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		Expect(handler.checkOrigin(req)).To(BeFalse())
	})

	It("should match case-insensitively", func() {
		handler = &ConsoleProxyWSHandler{allowedOrigins: []string{"https://Console.Example.COM"}}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Origin", "https://console.example.com")
		Expect(handler.checkOrigin(req)).To(BeTrue())
	})

	It("should check multiple allowed origins", func() {
		handler = &ConsoleProxyWSHandler{allowedOrigins: []string{
			"https://a.example.com",
			"https://b.example.com",
		}}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Origin", "https://b.example.com")
		Expect(handler.checkOrigin(req)).To(BeTrue())
	})

	It("should reject all origins when allowed list is empty", func() {
		handler = &ConsoleProxyWSHandler{allowedOrigins: []string{}}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Origin", "https://any.com")
		Expect(handler.checkOrigin(req)).To(BeFalse())
	})
})

var _ = Describe("ServeHTTP Origin enforcement", func() {
	It("should return 403 for cookie auth without Origin header", func() {
		handler := &ConsoleProxyWSHandler{
			core:           &ConsoleProxyCore{logger: logger},
			allowedOrigins: []string{"https://good.com"},
		}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "console-ticket", Value: "some-jwt"})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		Expect(w.Code).To(Equal(http.StatusForbidden))
		Expect(w.Body.String()).To(ContainSubstring("origin not allowed"))
	})

	It("should return 403 for cookie auth with disallowed Origin", func() {
		handler := &ConsoleProxyWSHandler{
			core:           &ConsoleProxyCore{logger: logger},
			allowedOrigins: []string{"https://good.com"},
		}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "console-ticket", Value: "some-jwt"})
		req.Header.Set("Origin", "https://evil.com")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		Expect(w.Code).To(Equal(http.StatusForbidden))
	})

	It("should skip Origin check for Bearer auth", func() {
		handler := &ConsoleProxyWSHandler{
			core:           &ConsoleProxyCore{logger: logger},
			allowedOrigins: []string{"https://good.com"},
		}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer some-token")
		req.Header.Set("Origin", "https://evil.com")
		w := httptest.NewRecorder()
		// Handler will panic at Opener.Open (nil) after passing Origin.
		// Recover so we can inspect the recorder -- a panic proves we got
		// past Origin validation without writing 403.
		func() {
			defer func() { recover() }()
			handler.ServeHTTP(w, req)
		}()
		Expect(w.Code).NotTo(Equal(http.StatusForbidden))
	})

	It("should pass cookie auth with allowed Origin to ticket verification", func() {
		handler := &ConsoleProxyWSHandler{
			core:           &ConsoleProxyCore{logger: logger},
			allowedOrigins: []string{"https://good.com"},
		}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "console-ticket", Value: "some-jwt"})
		req.Header.Set("Origin", "https://good.com")
		w := httptest.NewRecorder()
		// Origin passes; handler panics at nil Opener -- not a 403.
		func() {
			defer func() { recover() }()
			handler.ServeHTTP(w, req)
		}()
		Expect(w.Code).NotTo(Equal(http.StatusForbidden))
	})
})

var _ = Describe("handoffConn", func() {
	It("should close connection if not taken", func() {
		conn := newMockConn("")
		h := newHandoffConn(conn)

		h.CloseIfNotTaken()
		Expect(conn.closed).To(BeTrue())
	})

	It("should not close connection after Take", func() {
		conn := newMockConn("")
		h := newHandoffConn(conn)

		taken := h.Take()
		Expect(taken).NotTo(BeNil())

		h.CloseIfNotTaken()
		Expect(conn.closed).To(BeFalse())
	})

	It("should be safe to call CloseIfNotTaken with nil conn", func() {
		h := &handoffConn{}
		Expect(func() { h.CloseIfNotTaken() }).NotTo(Panic())
	})
})
