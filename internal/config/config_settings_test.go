/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"github.com/zalando/go-keyring"

	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/oauth"
)

var _ = Describe("Settings", func() {
	var (
		ctx context.Context
		tmp string
	)

	BeforeEach(func() {
		var err error

		// Create a context:
		ctx = context.Background()

		// Create a temporary directory:
		tmp, err = os.MkdirTemp("", "*.test")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(os.RemoveAll, tmp)
	})

	Describe("Builder", func() {
		It("Fails if logger is nil", func() {
			settings, err := NewSettings().
				Build()
			Expect(err).To(HaveOccurred())
			Expect(settings).To(BeNil())
		})
	})

	Describe("Common behavior", func() {
		BeforeEach(func() {
			keyring.MockInit()
		})

		It("Loads general settings from the config file", func() {
			// Create the config file:
			file := filepath.Join(tmp, "config.json")
			content := []byte(`{
				"address": "api.example.com:443",
				"insecure": true,
				"oauth_flow": "code",
				"oauth_issuer": "https://example.com",
				"oauth_redirect_uri": "https://example.com/callback",
				"oauth_scopes": ["openid"],
				"private": true
			}`)
			err := os.WriteFile(file, content, 0600)
			Expect(err).ToNot(HaveOccurred())

			// Load the settings:
			settings, err := NewSettings().
				SetLogger(logger).
				SetDir(tmp).
				Build()
			Expect(err).ToNot(HaveOccurred())
			err = settings.Load(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Verify the settings:
			Expect(settings.Address()).To(Equal("api.example.com:443"))
			Expect(settings.Insecure()).To(BeTrue())
			Expect(settings.OAuthFlow()).To(Equal(oauth.CodeFlow))
			Expect(settings.OAuthIssuer()).To(Equal("https://example.com"))
			Expect(settings.OAuthRedirectUri()).To(Equal("https://example.com/callback"))
			Expect(settings.OAuthScopes()).To(Equal([]string{"openid"}))
			Expect(settings.Private()).To(BeTrue())
		})

		It("Saves general settings in the config file", func() {
			// Create the settings and save them:
			settings, err := NewSettings().
				SetLogger(logger).
				SetDir(tmp).
				Build()
			Expect(err).ToNot(HaveOccurred())
			settings.SetAddress("api.example.com:443")
			settings.SetInsecure(true)
			settings.SetPrivate(true)
			settings.SetOAuthFlow(oauth.CodeFlow)
			settings.SetOAuthRedirectUri("https://example.com/callback")
			settings.SetOAuthScopes([]string{"openid"})
			settings.SetOauthIssuer("https://example.com")
			err = settings.Save(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Verify that the config file contains the expected data:
			file := filepath.Join(tmp, "config.json")
			content, err := os.ReadFile(file)
			Expect(err).ToNot(HaveOccurred())
			Expect(content).To(MatchJSON(`{
				"address": "api.example.com:443",
				"insecure": true,
				"oauth_flow": "code",
				"oauth_issuer": "https://example.com",
				"oauth_redirect_uri": "https://example.com/callback",
				"oauth_scopes": ["openid"],
				"private": true
			}`))
		})

		It("Returns empty settings when no file exists", func() {
			settings, err := NewSettings().
				SetLogger(logger).
				SetDir(tmp).
				Build()
			Expect(err).ToNot(HaveOccurred())
			err = settings.Load(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(settings.Address()).To(BeEmpty())
			Expect(settings.Insecure()).To(BeFalse())
			Expect(settings.OAuthFlow()).To(Equal(oauth.Flow("")))
			Expect(settings.OAuthIssuer()).To(BeEmpty())
			Expect(settings.OAuthRedirectUri()).To(BeEmpty())
			Expect(settings.OAuthScopes()).To(BeEmpty())
			Expect(settings.Private()).To(BeFalse())
		})

		It("Returns nil token when no access token is present", func() {
			settings, err := NewSettings().
				SetLogger(logger).
				SetDir(tmp).
				Build()
			Expect(err).ToNot(HaveOccurred())
			store := settings.TokenStore()
			token, err := store.Load(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(token).To(BeNil())
		})

		It("Skips save when tokens have not changed", func() {
			cfg, err := NewSettings().
				SetLogger(logger).
				SetDir(tmp).
				Build()
			Expect(err).ToNot(HaveOccurred())
			cfg.SetAccessToken("access-abc")
			cfg.SetRefreshToken("refresh-xyz")
			cfg.SetTokenExpiry(time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC))

			store := cfg.TokenStore()
			err = store.Save(ctx, &auth.Token{
				Access:  "access-abc",
				Refresh: "refresh-xyz",
				Expiry:  time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
			})
			Expect(err).ToNot(HaveOccurred())
		})
	})

	When("Keyring available", func() {
		BeforeEach(func() {
			keyring.MockInit()
		})

		It("Saves secrets in the keyring, not in the config file", func() {
			// Create the settings and save them:
			settings, err := NewSettings().
				SetLogger(logger).
				SetDir(tmp).
				Build()
			Expect(err).ToNot(HaveOccurred())
			settings.SetTokenExpiry(time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC))
			settings.SetOAuthClientId("my-client")
			settings.SetOAuthClientSecret("my-secret")
			settings.SetOAuthUser("my-user")
			settings.SetOAuthPassword("my-password")
			settings.SetAccessToken("my-access-token")
			settings.SetRefreshToken("my-refresh-token")
			err = settings.Save(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Verify that the keyring contains the expected data:
			data, err := keyring.Get("osac", "secrets")
			Expect(err).ToNot(HaveOccurred())
			Expect(data).To(MatchJSON(`{
				"access_token": "my-access-token",
				"refresh_token": "my-refresh-token",
				"token_expiry": "2026-06-01T12:00:00Z",
				"oauth_client_id": "my-client",
				"oauth_client_secret": "my-secret",
				"oauth_user": "my-user",
				"oauth_password": "my-password"
			}`))

			// Verify that the settings file is empty:
			file := filepath.Join(tmp, "config.json")
			content, err := os.ReadFile(file)
			Expect(err).ToNot(HaveOccurred())
			Expect(content).To(MatchJSON(`{}`))
		})

		It("Persists tokens when saving through the token store", func() {
			// Create the settings and save them:
			settings, err := NewSettings().
				SetLogger(logger).
				SetDir(tmp).
				Build()
			Expect(err).ToNot(HaveOccurred())
			settings.SetAddress("api.example.com:443")
			Expect(settings.Save(ctx)).To(Succeed())

			// Save a token:
			store := settings.TokenStore()
			err = store.Save(ctx, &auth.Token{
				Access:  "new-access",
				Refresh: "new-refresh",
				Expiry:  time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
			})
			Expect(err).ToNot(HaveOccurred())

			// Verify that the keyring contains the expected data:
			data, err := keyring.Get("osac", "secrets")
			Expect(err).ToNot(HaveOccurred())
			Expect(data).To(MatchJSON(`{
				"access_token": "new-access",
				"refresh_token": "new-refresh",
				"token_expiry": "2026-07-01T12:00:00Z"
			}`))
		})
	})

	When("Keyring is not available", func() {
		BeforeEach(func() {
			keyring.MockInitWithError(fmt.Errorf("keyring backend not available"))
		})

		It("Saves secrets in the secrets file, not in the keyring", func() {
			// Create the settings and save them:
			settings, err := NewSettings().
				SetLogger(logger).
				SetDir(tmp).
				Build()
			Expect(err).ToNot(HaveOccurred())
			settings.SetTokenExpiry(time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC))
			settings.SetOAuthClientId("my-client")
			settings.SetOAuthClientSecret("my-secret")
			settings.SetOAuthUser("my-user")
			settings.SetOAuthPassword("my-password")
			settings.SetAccessToken("my-access-token")
			settings.SetRefreshToken("my-refresh-token")
			err = settings.Save(ctx)
			Expect(err).ToNot(HaveOccurred())

			// Verify that the secrets file contains the expected data:
			file := filepath.Join(tmp, "secrets.json")
			content, err := os.ReadFile(file)
			Expect(err).ToNot(HaveOccurred())
			Expect(content).To(MatchJSON(`{
				"access_token": "my-access-token",
				"refresh_token": "my-refresh-token",
				"token_expiry": "2026-06-01T12:00:00Z",
				"oauth_client_id": "my-client",
				"oauth_client_secret": "my-secret",
				"oauth_user": "my-user",
				"oauth_password": "my-password"
			}`))
		})

		It("Persists tokens when saving through the token store", func() {
			// Create the settings and save them:
			settings, err := NewSettings().
				SetLogger(logger).
				SetDir(tmp).
				Build()
			Expect(err).ToNot(HaveOccurred())
			settings.SetAddress("api.example.com:443")
			Expect(settings.Save(ctx)).To(Succeed())

			// Save a token:
			store := settings.TokenStore()
			err = store.Save(ctx, &auth.Token{
				Access:  "new-access",
				Refresh: "new-refresh",
				Expiry:  time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
			})
			Expect(err).ToNot(HaveOccurred())

			// Verify that the secrets file contains the expected data:
			file := filepath.Join(tmp, "secrets.json")
			content, err := os.ReadFile(file)
			Expect(err).ToNot(HaveOccurred())
			Expect(content).To(MatchJSON(`{
				"access_token": "new-access",
				"refresh_token": "new-refresh",
				"token_expiry": "2026-07-01T12:00:00Z"
			}`))
		})
	})

	Describe("Armed check", func() {
		It("Returns false when settings are nil", func() {
			var settings *Settings
			Expect(settings.Armed()).To(BeFalse())
		})

		It("Returns true when the settings are armed", func() {
			settings, err := NewSettings().
				SetLogger(logger).
				SetDir(tmp).
				Build()
			Expect(err).ToNot(HaveOccurred())
			settings.SetAddress("api.example.com:443")
			Expect(settings.Armed()).To(BeTrue())
		})

		It("Returns false when the settings are not armed", func() {
			settings, err := NewSettings().
				SetLogger(logger).
				SetDir(tmp).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(settings.Armed()).To(BeFalse())
		})
	})
})
