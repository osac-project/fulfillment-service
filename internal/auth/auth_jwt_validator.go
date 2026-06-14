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
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JwtValidator validates bearer token strings by parsing them, verifying the signature against a JSON web key set, and
// checking standard and application-level claims.
//
//go:generate mockgen -destination=auth_jwt_validator_mock.go -package=auth . JwtValidator
type JwtValidator interface {
	// Validate parses the given bearer token string, verifies its signature against a JSON web key set, and checks
	// that all required claims are present and valid. Returns the parsed token on success, and an error if the
	// token is not valid. The returned error will always be safe to send to clients, and will never contain
	// security sensitive data.
	Validate(ctx context.Context, bearer string) (*jwt.Token, error)
}

// JwtValidatorBuilder contains the data and logic needed to build a JwtValidator. Don't create instances of this type
// directly, use the NewJwtValidator function instead.
type JwtValidatorBuilder struct {
	logger          *slog.Logger
	jwksCache       JwksCache
	tokenLeeway     time.Duration
	cacheEnabled    *bool
	cleanupInterval time.Duration
}

// jwtValidator is the default implementation of JwtValidator. Don't create instances of this type directly, use the
// NewJwtValidator function instead.
type jwtValidator struct {
	logger          *slog.Logger
	jwksCache       JwksCache
	tokenParser     *jwt.Parser
	tokenLeeway     time.Duration
	cacheMap        *sync.Map
	cleanupInterval time.Duration
	cleanupNano     atomic.Int64
	cleanupLock     sync.Mutex
}

// jwtValidatorCacheEntry holds a validated token together with its pre-computed expiration deadline: the token's 'exp'
// claim plus the configured leeway. This avoids re-parsing claims on every cache hit.
type jwtValidatorCacheEntry struct {
	tokenObject   *jwt.Token
	tokenDeadline time.Time
}

// NewJwtValidator creates a builder that can then be used to configure and create a new JwtValidator.
func NewJwtValidator() *JwtValidatorBuilder {
	return &JwtValidatorBuilder{}
}

// SetLogger sets the logger that will be used to write to the log. This is mandatory.
func (b *JwtValidatorBuilder) SetLogger(value *slog.Logger) *JwtValidatorBuilder {
	b.logger = value
	return b
}

// SetJwksCache sets the JWKS cache used to look up signing keys for signature verification. This is mandatory.
func (b *JwtValidatorBuilder) SetJwksCache(value JwksCache) *JwtValidatorBuilder {
	b.jwksCache = value
	return b
}

// SetExpirationLeeway sets the maximum time that a token will be considered valid after it has expired. The default is
// zero, meaning no leeway.
func (b *JwtValidatorBuilder) SetExpirationLeeway(value time.Duration) *JwtValidatorBuilder {
	b.tokenLeeway = value
	return b
}

// SetCacheEnabled enables or disables caching of validated tokens. When enabled, successfully validated tokens are
// stored in memory and returned directly on subsequent calls with the same bearer string, bypassing signature
// verification and claims validation. The default is enabled.
func (b *JwtValidatorBuilder) SetCacheEnabled(value bool) *JwtValidatorBuilder {
	b.cacheEnabled = &value
	return b
}

// SetCleanupInterval sets the minimum time between cache cleanup runs. When the cache is enabled, each call to
// Validate checks whether this interval has elapsed since the last cleanup. If so, a background goroutine is
// launched to remove expired tokens from the cache. The default is 15 minutes.
func (b *JwtValidatorBuilder) SetCleanupInterval(value time.Duration) *JwtValidatorBuilder {
	b.cleanupInterval = value
	return b
}

// Build uses the data stored in the builder to create and configure a new JwtValidator.
func (b *JwtValidatorBuilder) Build() (result JwtValidator, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.jwksCache == nil {
		err = errors.New("JWKS cache is mandatory")
		return
	}
	if b.tokenLeeway < 0 {
		err = errors.New("leeway must be zero or positive")
		return
	}
	if b.cleanupInterval < 0 {
		err = errors.New("cleanup interval must be zero or positive")
		return
	}

	// Apply defaults:
	cleanupInterval := b.cleanupInterval
	if cleanupInterval == 0 {
		cleanupInterval = 15 * time.Minute
	}

	// Create the parser:
	parserOptions := []jwt.ParserOption{
		jwt.WithValidMethods([]string{
			"RS256", "RS384", "RS512",
		}),
		jwt.WithIssuedAt(),
		jwt.WithExpirationRequired(),
	}
	if b.tokenLeeway > 0 {
		parserOptions = append(parserOptions, jwt.WithLeeway(b.tokenLeeway))
	}
	tokenParser := jwt.NewParser(parserOptions...)

	// Create the cache map:
	var cacheMap *sync.Map
	if b.cacheEnabled == nil || *b.cacheEnabled {
		cacheMap = &sync.Map{}
	}

	// Create and populate the object:
	result = &jwtValidator{
		logger:          b.logger,
		jwksCache:       b.jwksCache,
		tokenParser:     tokenParser,
		tokenLeeway:     b.tokenLeeway,
		cacheMap:        cacheMap,
		cleanupInterval: cleanupInterval,
	}
	return
}

// Validate parses and validates the bearer token.
func (v *jwtValidator) Validate(ctx context.Context, bearer string) (result *jwt.Token, err error) {
	if v.cacheMap != nil {
		v.maybeCleanup()
		cached, ok := v.cacheMap.Load(bearer)
		if ok {
			entry := cached.(*jwtValidatorCacheEntry)
			if time.Now().After(entry.tokenDeadline) {
				v.cacheMap.Delete(bearer)
			} else {
				result = entry.tokenObject
				return
			}
		}
	}
	token, err := v.tokenParser.ParseWithClaims(
		bearer, jwt.MapClaims{},
		func(token *jwt.Token) (key any, err error) {
			return v.selectKey(ctx, token)
		},
	)
	if err != nil {
		err = v.translateError(err)
		result = nil
		return
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		err = errors.New("token is not valid")
		result = nil
		return
	}
	err = v.validateClaims(claims)
	if err != nil {
		return
	}
	if v.cacheMap != nil {
		exp, _ := claims.GetExpirationTime()
		if exp != nil {
			v.cacheMap.Store(bearer, &jwtValidatorCacheEntry{
				tokenObject:   token,
				tokenDeadline: exp.Add(v.tokenLeeway),
			})
		}
	}
	result = token
	return
}

// maybeCleanup checks whether enough time has elapsed since the last cache cleanup, and if so launches a background
// goroutine to remove expired tokens.
func (v *jwtValidator) maybeCleanup() {
	nowNano := time.Now().UnixNano()
	cleanupNano := v.cleanupNano.Load()
	if cleanupNano != 0 && time.Duration(nowNano-cleanupNano) < v.cleanupInterval {
		return
	}
	if !v.cleanupLock.TryLock() {
		return
	}
	v.cleanupNano.Store(nowNano)
	go func() {
		defer v.cleanupLock.Unlock()
		v.doCleanup()
	}()
}

// doCleanup iterates the token cache and removes entries whose expiration deadline has passed.
func (v *jwtValidator) doCleanup() {
	now := time.Now()
	v.cacheMap.Range(func(key, value any) bool {
		entry := value.(*jwtValidatorCacheEntry)
		if now.After(entry.tokenDeadline) {
			v.cacheMap.Delete(key)
		}
		return true
	})
}

// translateError translates JWT library errors into errors that are safe to send to clients.
func (v *jwtValidator) translateError(err error) error {
	// Note that currently the JWT library generates errors that also safe to send to clients, we do this just in
	// case that library changes its error messages in the future.
	switch {
	case errors.Is(err, jwt.ErrTokenExpired):
		return errors.New("token is expired")
	case errors.Is(err, jwt.ErrTokenUsedBeforeIssued):
		return errors.New("token is issued in the future")
	case errors.Is(err, jwt.ErrTokenNotValidYet):
		return errors.New("token is not valid yet")
	default:
		return errors.New("token is not valid")
	}
}

// selectKey extracts the issuer and key identifier from the token and looks up the signing key in the JWKS cache.
func (v *jwtValidator) selectKey(ctx context.Context, token *jwt.Token) (result any, err error) {
	issuerUrl, err := token.Claims.GetIssuer()
	if err != nil {
		err = errors.New("token does not contain the 'iss' claim")
		return
	}
	keyId, ok := token.Header["kid"].(string)
	if !ok || keyId == "" {
		err = errors.New("token does not contain the 'kid' claim")
		return
	}
	result, err = v.jwksCache.Get(ctx, issuerUrl, keyId)
	if err != nil {
		err = fmt.Errorf("issuer URL '%s' is not trusted", issuerUrl)
	}
	return
}

// validateClaims validates the application-level claims.
func (v *jwtValidator) validateClaims(claims jwt.MapClaims) error {
	tokenType, ok := claims["typ"].(string)
	if ok && !strings.EqualFold(tokenType, "Bearer") {
		return fmt.Errorf("token type '%s' is not supported", tokenType)
	}
	subject, ok := claims["sub"].(string)
	if !ok || subject == "" {
		return errors.New("token does not contain the 'sub' claim")
	}
	return nil
}
