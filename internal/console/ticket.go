/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package console

import (
	"context"
	"errors"
	"time"

	"github.com/osac-project/fulfillment-service/internal/token"
)

const (
	// TicketAudience is the JWT audience for console proxy tickets.
	// Used by the wiring layer when constructing token.Sealer/token.Opener.
	TicketAudience = "fulfillment-console-proxy"

	claimClientID    = "client_id"
	claimTarget      = "target"
	claimConsoleType = "console_type"
)

// Ticket is the parsed, validated console ticket.
type Ticket struct {
	Subject     string
	ClientID    string
	ConsoleType string
	JTI         string
	ExpiresAt   time.Time
	TargetURI   string
	TargetToken string
}

// targetClaims is the nested "target" claim in the JWT.
type targetClaims struct {
	URI   string `json:"uri"`
	Token string `json:"token"`
}

// TicketSealer seals console tickets as nested JWTs (JWE wrapping JWS).
// It wraps a generic token.Sealer with console-specific claim mapping.
type TicketSealer struct {
	sealer *token.Sealer
}

// NewTicketSealer creates a new ticket sealer wrapping a pre-constructed
// token.Sealer. The caller is responsible for constructing the Sealer with
// the correct audience (TicketAudience) and key material.
func NewTicketSealer(sealer *token.Sealer) *TicketSealer {
	return &TicketSealer{sealer: sealer}
}

// Seal signs and encrypts a console ticket into a nested JWT.
func (s *TicketSealer) Seal(ticket *Ticket, subject string, clientID string, consoleType string, ttl time.Duration) (string, time.Time, error) {
	if ticket == nil {
		return "", time.Time{}, errors.New("ticket must not be nil")
	}
	claims := map[string]any{
		claimClientID:    clientID,
		claimConsoleType: consoleType,
		claimTarget: targetClaims{
			URI:   ticket.TargetURI,
			Token: ticket.TargetToken,
		},
	}
	return s.sealer.Seal(subject, claims, ttl)
}

// TicketOpener decrypts and verifies console tickets from nested JWTs.
// It wraps a generic token.Opener with console-specific claim extraction.
type TicketOpener struct {
	opener *token.Opener
}

// NewTicketOpener creates a new ticket opener wrapping a pre-constructed
// token.Opener. The caller is responsible for constructing the Opener with
// the correct audience (TicketAudience), JWKS URL, and key material.
func NewTicketOpener(opener *token.Opener) *TicketOpener {
	return &TicketOpener{opener: opener}
}

// Open decrypts and verifies a console ticket, extracting the target fields.
// Each ticket can only be opened once (JTI single-use enforcement).
func (o *TicketOpener) Open(ctx context.Context, tokenString string) (*Ticket, error) {
	claims, err := o.opener.Open(ctx, tokenString)
	if err != nil {
		return nil, err
	}

	var clientID string
	if cid, ok := claims.Custom[claimClientID].(string); ok {
		clientID = cid
	}

	var consoleType string
	if ct, ok := claims.Custom[claimConsoleType].(string); ok {
		consoleType = ct
	}

	targetVal, ok := claims.Custom[claimTarget].(map[string]any)
	if !ok {
		return nil, errors.New("ticket missing target claim")
	}

	targetURI, _ := targetVal["uri"].(string)
	targetToken, _ := targetVal["token"].(string)

	if targetURI == "" {
		return nil, errors.New("ticket target.uri is empty")
	}

	return &Ticket{
		Subject:     claims.Subject,
		ClientID:    clientID,
		ConsoleType: consoleType,
		JTI:         claims.JTI,
		ExpiresAt:   claims.ExpiresAt,
		TargetURI:   targetURI,
		TargetToken: targetToken,
	}, nil
}
