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
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/golang-jwt/jwt/v5"
	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/ginkgo/v2/dsl/reporting"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gmeasure"
	"go.uber.org/mock/gomock"

	. "github.com/osac-project/fulfillment-service/internal/testing"
	"github.com/osac-project/fulfillment-service/internal/uuid"
)

var _ = Describe("JWT token validator creation", func() {
	var ctrl *gomock.Controller

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		DeferCleanup(ctrl.Finish)
	})

	It("Can't be built without a logger", func() {
		jwksCache := NewMockJwksCache(ctrl)
		_, err := NewJwtValidator().
			SetJwksCache(jwksCache).
			Build()
		Expect(err).To(MatchError("logger is mandatory"))
	})

	It("Can't be built without a JWKS cache", func() {
		_, err := NewJwtValidator().
			SetLogger(logger).
			Build()
		Expect(err).To(MatchError("JWKS cache is mandatory"))
	})

	It("Can't be built with a negative leeway", func() {
		jwksCache := NewMockJwksCache(ctrl)
		_, err := NewJwtValidator().
			SetLogger(logger).
			SetJwksCache(jwksCache).
			SetExpirationLeeway(-time.Second).
			Build()
		Expect(err).To(MatchError("leeway must be zero or positive"))
	})

	It("Can be built with valid parameters", func() {
		jwksCache := NewMockJwksCache(ctrl)
		validator, err := NewJwtValidator().
			SetLogger(logger).
			SetJwksCache(jwksCache).
			Build()
		Expect(err).ToNot(HaveOccurred())
		Expect(validator).ToNot(BeNil())
	})
})

var _ = Describe("JWT token validation", func() {
	var (
		ctrl          *gomock.Controller
		goodIssuerUrl string
		badIssuerUrl  string
		jwksCache     *MockJwksCache
	)

	BeforeEach(func() {
		// Create the mock controller:
		ctrl = gomock.NewController(GinkgoT())
		DeferCleanup(ctrl.Finish)

		// Create the issuer URLs:
		goodIssuerUrl = "https://good-issuer.example.com"
		badIssuerUrl = "https://bad-issuer.example.com"

		// Create the JWKS cache:
		jwksCache = NewMockJwksCache(ctrl)
		jwksCache.EXPECT().
			Get(gomock.Any(), goodIssuerUrl, "123").
			Return(JwtPublicKey(), nil).
			AnyTimes()
		jwksCache.EXPECT().
			Get(gomock.Any(), badIssuerUrl, "123").
			Return(nil, fmt.Errorf("issuer URL '%s' is not trusted", badIssuerUrl)).
			AnyTimes()
	})

	It("Accepts a valid token", func(ctx context.Context) {
		validator, err := NewJwtValidator().
			SetLogger(logger).
			SetJwksCache(jwksCache).
			Build()
		Expect(err).ToNot(HaveOccurred())
		bearer := MakeTokenString(goodIssuerUrl, "Bearer", time.Minute)
		token, err := validator.Validate(ctx, bearer)
		Expect(err).ToNot(HaveOccurred())
		Expect(token).ToNot(BeNil())
		Expect(token.Valid).To(BeTrue())
	})

	It("Returns error for garbage input", func(ctx context.Context) {
		validator, err := NewJwtValidator().
			SetLogger(logger).
			SetJwksCache(jwksCache).
			Build()
		Expect(err).ToNot(HaveOccurred())
		token, err := validator.Validate(ctx, "junk")
		Expect(err).To(MatchError("token is not valid"))
		Expect(token).To(BeNil())
	})

	It("Returns error for an expired token", func(ctx context.Context) {
		validator, err := NewJwtValidator().
			SetLogger(logger).
			SetJwksCache(jwksCache).
			Build()
		Expect(err).ToNot(HaveOccurred())
		bearer := MakeTokenString(goodIssuerUrl, "Bearer", -time.Minute)
		token, err := validator.Validate(ctx, bearer)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError("token is expired"))
		Expect(token).To(BeNil())
	})

	It("Returns error for a future-issued token", func(ctx context.Context) {
		validator, err := NewJwtValidator().
			SetLogger(logger).
			SetJwksCache(jwksCache).
			Build()
		Expect(err).ToNot(HaveOccurred())
		iat := time.Now().Add(time.Minute)
		exp := iat.Add(time.Minute)
		bearer := MakeTokenObject(jwt.MapClaims{
			"iss": goodIssuerUrl,
			"iat": iat.Unix(),
			"exp": exp.Unix(),
		}).Raw
		token, err := validator.Validate(ctx, bearer)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError("token is issued in the future"))
		Expect(token).To(BeNil())
	})

	It("Returns error for a token with nbf in the future", func(ctx context.Context) {
		validator, err := NewJwtValidator().
			SetLogger(logger).
			SetJwksCache(jwksCache).
			Build()
		Expect(err).ToNot(HaveOccurred())
		iat := time.Now()
		nbf := iat.Add(time.Minute)
		exp := nbf.Add(time.Minute)
		bearer := MakeTokenObject(jwt.MapClaims{
			"iss": goodIssuerUrl,
			"iat": iat.Unix(),
			"nbf": nbf.Unix(),
			"exp": exp.Unix(),
		}).Raw
		token, err := validator.Validate(ctx, bearer)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError("token is not valid yet"))
		Expect(token).To(BeNil())
	})

	It("Returns error for an untrusted issuer", func(ctx context.Context) {
		validator, err := NewJwtValidator().
			SetLogger(logger).
			SetJwksCache(jwksCache).
			Build()
		Expect(err).ToNot(HaveOccurred())
		bearer := MakeTokenString(badIssuerUrl, "Bearer", time.Minute)
		token, err := validator.Validate(ctx, bearer)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError("token is not valid"))
		Expect(token).To(BeNil())
	})

	It("Returns error for a 'typ' other than 'Bearer'", func(ctx context.Context) {
		validator, err := NewJwtValidator().
			SetLogger(logger).
			SetJwksCache(jwksCache).
			Build()
		Expect(err).ToNot(HaveOccurred())
		bearer := MakeTokenString(goodIssuerUrl, "Refresh", time.Minute)
		token, err := validator.Validate(ctx, bearer)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(fmt.Sprintf("token type '%s' is not supported", "Refresh")))
		Expect(token).To(BeNil())
	})

	It("Accepts a token without 'typ' claim", func(ctx context.Context) {
		validator, err := NewJwtValidator().
			SetLogger(logger).
			SetJwksCache(jwksCache).
			Build()
		Expect(err).ToNot(HaveOccurred())
		bearer := MakeTokenObject(jwt.MapClaims{
			"iss": goodIssuerUrl,
			"typ": nil,
		}).Raw
		token, err := validator.Validate(ctx, bearer)
		Expect(err).ToNot(HaveOccurred())
		Expect(token).ToNot(BeNil())
		Expect(token.Valid).To(BeTrue())
	})

	It("Returns error for a token without 'sub' claim", func(ctx context.Context) {
		validator, err := NewJwtValidator().
			SetLogger(logger).
			SetJwksCache(jwksCache).
			Build()
		Expect(err).ToNot(HaveOccurred())
		bearer := MakeTokenObject(jwt.MapClaims{
			"iss": goodIssuerUrl,
			"sub": nil,
		}).Raw
		token, err := validator.Validate(ctx, bearer)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError("token does not contain the 'sub' claim"))
		Expect(token).To(BeNil())
	})

	It("Respects leeway for slightly expired tokens", func(ctx context.Context) {
		validator, err := NewJwtValidator().
			SetLogger(logger).
			SetJwksCache(jwksCache).
			SetExpirationLeeway(5 * time.Second).
			Build()
		Expect(err).ToNot(HaveOccurred())
		bearer := MakeTokenObject(jwt.MapClaims{
			"iss": goodIssuerUrl,
			"exp": time.Now().Add(-time.Second).Unix(),
		}).Raw
		token, err := validator.Validate(ctx, bearer)
		Expect(err).ToNot(HaveOccurred())
		Expect(token).ToNot(BeNil())
		Expect(token.Valid).To(BeTrue())
	})
})

var _ = Describe("JWT token validator cache cleanup", func() {
	var (
		ctrl      *gomock.Controller
		issuerUrl string
		jwksCache *MockJwksCache
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		DeferCleanup(ctrl.Finish)

		issuerUrl = "https://good-issuer.example.com"
		jwksCache = NewMockJwksCache(ctrl)
		jwksCache.EXPECT().
			Get(gomock.Any(), issuerUrl, "123").
			Return(JwtPublicKey(), nil).
			AnyTimes()
	})

	It("Removes expired tokens from the cache", func(ctx context.Context) {
		validator, err := NewJwtValidator().
			SetLogger(logger).
			SetJwksCache(jwksCache).
			SetCacheEnabled(true).
			SetCleanupInterval(1 * time.Millisecond).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Validate a token that expires in 1 second:
		shortLived := MakeTokenObject(jwt.MapClaims{
			"iss": issuerUrl,
			"exp": time.Now().Add(1 * time.Second).Unix(),
		}).Raw
		_, err = validator.Validate(ctx, shortLived)
		Expect(err).ToNot(HaveOccurred())

		// Validate a long-lived token:
		longLived := MakeTokenObject(jwt.MapClaims{
			"iss": issuerUrl,
			"exp": time.Now().Add(10 * time.Minute).Unix(),
		}).Raw
		_, err = validator.Validate(ctx, longLived)
		Expect(err).ToNot(HaveOccurred())

		// Wait for the short-lived token to expire and for the cleanup interval to elapse:
		time.Sleep(1100 * time.Millisecond)

		// Trigger cleanup by validating again:
		_, err = validator.Validate(ctx, longLived)
		Expect(err).ToNot(HaveOccurred())

		// Give the background goroutine time to complete:
		time.Sleep(50 * time.Millisecond)

		// The short-lived token should have been evicted, so the next call will re-parse and fail
		// because it is now expired:
		token, err := validator.Validate(ctx, shortLived)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError("token is expired"))
		Expect(token).To(BeNil())

		// The long-lived token should still be cached and valid:
		token, err = validator.Validate(ctx, longLived)
		Expect(err).ToNot(HaveOccurred())
		Expect(token).ToNot(BeNil())
	})
})

var _ = Describe("JWT token validator performance", func() {
	var (
		ctrl      *gomock.Controller
		jwksCache *MockJwksCache
		issuerUrl string
		bearers   []string
	)

	BeforeEach(func() {
		// Create the mock controller:
		ctrl = gomock.NewController(GinkgoT())
		DeferCleanup(ctrl.Finish)

		// Create the issuer URL:
		issuerUrl = "https://bench.example.com"

		// Create the JWKS cache:
		jwksCache = NewMockJwksCache(ctrl)
		jwksCache.EXPECT().
			Get(gomock.Any(), issuerUrl, "123").
			Return(JwtPublicKey(), nil).
			AnyTimes()

	})

	It("Is at least an order of magnitude faster with caching", func(ctx context.Context) {
		// Create a collection of bearer tokens to run the benchmark with:
		bearers = make([]string, 100)
		for i := range bearers {
			bearers[i] = MakeTokenObject(jwt.MapClaims{
				"iss": issuerUrl,
				"sub": uuid.New(),
			}).Raw
		}

		// Prepare the same sampling configuration for both experiments:
		samplingConfig := SamplingConfig{
			N:        1000,
			Duration: 10 * time.Second,
		}

		// Run the experiment with the cache disabled:
		withoutCache, err := NewJwtValidator().
			SetLogger(logger).
			SetJwksCache(jwksCache).
			SetCacheEnabled(false).
			Build()
		Expect(err).ToNot(HaveOccurred())
		experimentWithout := NewExperiment("Without cache")
		AddReportEntry(experimentWithout.Name, experimentWithout, ReportEntryVisibilityFailureOrVerbose)
		experimentWithout.SampleDuration(
			"validate",
			func(idx int) {
				bearer := bearers[rand.IntN(len(bearers))]
				_, err := withoutCache.Validate(ctx, bearer)
				Expect(err).ToNot(HaveOccurred())
			},
			samplingConfig,
		)

		// Run the experiement with the cache enabled:
		withCache, err := NewJwtValidator().
			SetLogger(logger).
			SetJwksCache(jwksCache).
			SetCacheEnabled(true).
			Build()
		Expect(err).ToNot(HaveOccurred())
		experimentWith := NewExperiment("With cache")
		AddReportEntry(experimentWith.Name, experimentWith, ReportEntryVisibilityFailureOrVerbose)
		experimentWith.SampleDuration(
			"validate",
			func(idx int) {
				bearer := bearers[rand.IntN(len(bearers))]
				_, err := withCache.Validate(ctx, bearer)
				Expect(err).ToNot(HaveOccurred())
			},
			samplingConfig,
		)

		// Get the stats for the experiments:
		statsWithout := experimentWithout.GetStats("validate")
		statsWith := experimentWith.GetStats("validate")

		// Calculate the raking using the median duration as the metric:
		ranking := RankStats(LowerMedianIsBetter, statsWith, statsWithout)
		AddReportEntry("Ranking", ranking, ReportEntryVisibilityFailureOrVerbose)

		// Verifh that the validation is at least an order of magnitude faster with the cache enabled:
		Expect(statsWith.DurationFor(StatMedian)).To(
			BeNumerically("<", statsWithout.DurationFor(StatMedian)/10),
		)
	})
})
