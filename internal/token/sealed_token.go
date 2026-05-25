/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package token

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwe"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

// Sealer signs JWT claims (JWS, RS256) and encrypts the result (JWE,
// RSA-OAEP-256, A256GCM, DEFLATE) to produce nested JWTs per RFC 7519
// Section 11.2. Keys are hot-reloaded from PEM files on mtime change.
type Sealer struct {
	signingKey    *certKeyReloader
	encryptionKey *certKeyReloader
	issuer        string
	audience      []string
}

// NewSealer creates a new Sealer.
// certFile/keyFile: signing key pair (for JWS).
// encryptionCertFile: recipient's encryption certificate (public key for JWE).
// issuer: URL used as the JWT iss claim.
// audience: values for the JWT aud claim.
func NewSealer(
	logger *slog.Logger,
	certFile, keyFile string,
	encryptionCertFile string,
	issuer string,
	audience []string,
) (*Sealer, error) {
	signingKey := &certKeyReloader{
		logger:   logger.With(slog.String("component", "token_sealer_signing")),
		certFile: certFile,
		keyFile:  keyFile,
	}
	if err := signingKey.ensureLoaded(); err != nil {
		return nil, fmt.Errorf("initial load of signing keypair: %w", err)
	}

	encryptionKey := &certKeyReloader{
		logger:   logger.With(slog.String("component", "token_sealer_encryption")),
		certFile: encryptionCertFile,
	}
	if err := encryptionKey.ensureLoaded(); err != nil {
		return nil, fmt.Errorf("initial load of encryption certificate: %w", err)
	}

	return &Sealer{
		signingKey:    signingKey,
		encryptionKey: encryptionKey,
		issuer:        issuer,
		audience:      audience,
	}, nil
}

// Seal creates a nested JWT. Builds JWT claims (iss, aud, sub, jti, iat,
// exp + custom), signs as JWS (RS256), then encrypts as JWE (RSA-OAEP-256,
// A256GCM, DEFLATE). Returns the compact JWE serialization and expiry time.
func (s *Sealer) Seal(subject string, claims map[string]any, ttl time.Duration) (string, time.Time, error) {
	if err := s.signingKey.ensureLoaded(); err != nil {
		return "", time.Time{}, fmt.Errorf("reload signing key: %w", err)
	}
	if err := s.encryptionKey.ensureLoaded(); err != nil {
		return "", time.Time{}, fmt.Errorf("reload encryption key: %w", err)
	}

	now := time.Now()
	expiresAt := now.Add(ttl)
	jti := uuid.New().String()

	builder := jwt.NewBuilder().
		Issuer(s.issuer).
		Audience(s.audience).
		Subject(subject).
		JwtID(jti).
		IssuedAt(now).
		Expiration(expiresAt)

	for k, v := range claims {
		switch k {
		case "iss", "aud", "sub", "jti", "iat", "exp", "nbf":
			continue
		}
		builder = builder.Claim(k, v)
	}

	tok, err := builder.Build()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("build token: %w", err)
	}

	// Sign (JWS) then encrypt (JWE) -> nested JWT.
	s.signingKey.mu.Lock()
	privKey := s.signingKey.privateKey
	sigJWK := s.signingKey.jwkKey
	s.signingKey.mu.Unlock()

	s.encryptionKey.mu.Lock()
	encPubKey := s.encryptionKey.publicKey
	s.encryptionKey.mu.Unlock()

	// Build the encryption JWK with kid for the JWE header.
	encJWK, err := jwk.Import(encPubKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("import encryption public key: %w", err)
	}
	if err := jwk.AssignKeyID(encJWK); err != nil {
		return "", time.Time{}, fmt.Errorf("assign encryption kid: %w", err)
	}

	// Use the signing JWK (with kid) for the JWS header.
	sigPrivJWK, err := jwk.Import(privKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("import signing private key: %w", err)
	}
	kid, _ := sigJWK.KeyID()
	if err := sigPrivJWK.Set(jwk.KeyIDKey, kid); err != nil {
		return "", time.Time{}, fmt.Errorf("set kid on signing key: %w", err)
	}

	serialized, err := jwt.NewSerializer().
		Sign(jwt.WithKey(jwa.RS256(), sigPrivJWK)).
		Encrypt(
			jwt.WithKey(jwa.RSA_OAEP_256(), encJWK),
			jwt.WithEncryptOption(jwe.WithContentEncryption(jwa.A256GCM())),
			jwt.WithEncryptOption(jwe.WithCompress(jwa.Deflate())),
		).
		Serialize(tok)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign and encrypt token: %w", err)
	}

	return string(serialized), expiresAt, nil
}

// JWKSet returns the JWS signing public key as a JWK Set for JWKS discovery.
func (s *Sealer) JWKSet() (jwk.Set, error) {
	if err := s.signingKey.ensureLoaded(); err != nil {
		return nil, fmt.Errorf("reload signing key: %w", err)
	}

	s.signingKey.mu.Lock()
	k := s.signingKey.jwkKey
	s.signingKey.mu.Unlock()

	set := jwk.NewSet()
	if err := set.AddKey(k); err != nil {
		return nil, fmt.Errorf("add key to set: %w", err)
	}
	return set, nil
}
