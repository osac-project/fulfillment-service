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
	"context"
	"log/slog"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/neilotoole/jsoncolor"
	"github.com/spf13/cobra"

	"github.com/osac-project/fulfillment-service/internal/config"
	"github.com/osac-project/fulfillment-service/internal/exit"
	"github.com/osac-project/fulfillment-service/internal/logging"
	"github.com/osac-project/fulfillment-service/internal/terminal"
)

func Cmd() *cobra.Command {
	runner := &runnerContext{}
	result := &cobra.Command{
		Use:   "token [OPTION]...",
		Short: "Shows the authentication token, requesting a new one if necessary",
		RunE:  runner.run,
	}
	flags := result.Flags()
	flags.BoolVarP(
		&runner.refresh,
		"refresh",
		"r",
		false,
		"Show the refresh token instead of the access token.",
	)
	flags.BoolVarP(
		&runner.header,
		"header",
		"H",
		false,
		"Print the token header. This will work only if the token is a JSON web token.",
	)
	flags.BoolVarP(
		&runner.payload,
		"payload",
		"p",
		false,
		"Shows the token payload in JSON format. This will work only if the token is a JSON web token.",
	)
	flags.BoolVarP(
		&runner.rfc3339,
		"rfc-3339",
		"R",
		false,
		"Displays the time claims as RFC 3339 timestamps. By default the time claims are displayed as "+
			"seconds since the Unix epoch, as that is the format used by JSON web tokens",
	)
	flags.BoolVarP(
		&runner.utc,
		"utc",
		"U",
		false,
		"Displays the time claims using the UTC time zone.",
	)

	return result
}

type runnerContext struct {
	logger  *slog.Logger
	console *terminal.Console
	refresh bool
	header  bool
	payload bool
	rfc3339 bool
	utc     bool
}

func (c *runnerContext) run(cmd *cobra.Command, args []string) error {
	var err error

	// Get the context:
	ctx := cmd.Context()

	// Get the logger, console and flags:
	c.logger = logging.LoggerFromContext(ctx)
	c.console = terminal.ConsoleFromContext(ctx)

	// Get the configuration:
	cfg, err := config.Load(ctx)
	if err != nil {
		return err
	}
	if cfg == nil {
		c.console.Printf(ctx, "There is no configuration, run the 'login' command.\n")
		return exit.Error(1)
	}

	// Get the token:
	source, err := cfg.TokenSource(ctx)
	if err != nil {
		return err
	}
	token, err := source.Token(ctx)
	if err != nil {
		return err
	}

	// Select the token to print:
	var selected string
	if !c.refresh {
		if token.Access == "" {
			c.console.Printf(ctx, "No access token available.\n")
			return exit.Error(1)
		}
		selected = token.Access
	} else {
		if token.Refresh == "" {
			c.console.Printf(ctx, "No refresh token available.\n")
			return exit.Error(1)
		}
		selected = token.Refresh
	}

	// If the header or the payload have been requested, then try to parse the selected token as a JWT:
	var parsed *jwt.Token
	if c.header || c.payload {
		parser := jwt.NewParser(jwt.WithJSONNumber())
		parsed, _, err = parser.ParseUnverified(selected, &jwt.MapClaims{})
		if err != nil {
			c.console.Printf(ctx, "Failed to parse token as a JSON web token: %s\n", err)
			return exit.Error(1)
		}
	}

	// Print the token:
	switch {
	case c.header:
		c.console.RenderJson(ctx, parsed.Header)
	case c.payload:
		claims := *parsed.Claims.(*jwt.MapClaims)
		claims = c.replaceTimeClaims(ctx, claims)
		c.console.RenderJson(ctx, claims)
	default:
		c.console.Printf(ctx, "%s\n", selected)
	}
	return nil
}

func (c *runnerContext) replaceTimeClaims(ctx context.Context, claims jwt.MapClaims) jwt.MapClaims {
	result := jwt.MapClaims{}
	for name, value := range claims {
		switch name {
		case "iat", "exp", "nbf", "auth_time":
			result[name] = c.replaceTimeClaim(ctx, name, value.(jsoncolor.Number))
		default:
			result[name] = value
		}
	}
	return result
}

func (c *runnerContext) replaceTimeClaim(ctx context.Context, name string, value jsoncolor.Number) any {
	if !c.rfc3339 {
		return value
	}
	s, err := value.Int64()
	if err != nil {
		c.logger.ErrorContext(
			ctx,
			"Failed to parse claim as seconds",
			slog.String("name", name),
			slog.Any("value", value),
			slog.Any("error", err),
		)
		return value
	}
	t := time.Unix(s, 0)
	if c.utc {
		t = t.UTC()
	}
	return t.Format(time.RFC3339)
}
