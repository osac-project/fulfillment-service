/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package login

import (
	"context"
	"crypto/x509"
	"embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/config"
	"github.com/osac-project/fulfillment-service/internal/exit"
	"github.com/osac-project/fulfillment-service/internal/logging"
	"github.com/osac-project/fulfillment-service/internal/network"
	"github.com/osac-project/fulfillment-service/internal/oauth"
	"github.com/osac-project/fulfillment-service/internal/terminal"
)

//go:embed templates
var templatesFS embed.FS

func Cmd() *cobra.Command {
	runner := &runnerContext{}
	result := &cobra.Command{
		Use:                   "login [FLAGS] ADDRESS",
		DisableFlagsInUseLine: true,
		Short:                 "Save connection and authentication details.",
		RunE:                  runner.run,
	}
	flags := result.Flags()
	flags.BoolVar(
		&runner.args.plaintext,
		"plaintext",
		false,
		"Disables use of TLS for communications with the API server.",
	)
	flags.BoolVar(
		&runner.args.insecure,
		"insecure",
		false,
		"Disables verification of TLS certificates and host names of the OAuth and API servers.",
	)
	flags.StringArrayVar(
		&runner.args.caFiles,
		"ca-file",
		[]string{},
		"File or directory containing trusted CA certificates.",
	)
	flags.StringVar(
		&runner.args.address,
		"address",
		os.Getenv("FULFILLMENT_SERVICE_ADDRESS"),
		"Server address.",
	)
	flags.BoolVar(
		&runner.args.private,
		"private",
		false,
		"Enables use of the private API.",
	)
	flags.StringVar(
		&runner.args.token,
		"token",
		os.Getenv("FULFILLMENT_SERVICE_TOKEN"),
		"Authentication token",
	)
	flags.StringVar(
		&runner.args.tokenScript,
		"token-script",
		os.Getenv("FULFILLMENT_SERVICE_TOKEN_SCRIPT"),
		"Shell command that will be executed to obtain the token. For example, to automatically get the "+
			"token of the Kubernetes 'client' service account of the 'example' namespace the value "+
			"could be 'kubectl create token -n example client --duration 1h'. Note that is important "+
			"to quote this shell command correctly, as it will be passed to your shell for "+
			"execution.",
	)
	flags.StringVar(
		&runner.args.oauthIssuer,
		"oauth-issuer",
		"",
		"OAuth issuer URL. This is optional. By default the first issuer advertised by the server is used.",
	)
	flags.StringVar(
		&runner.args.oauthFlow,
		"oauth-flow",
		string(oauth.DeviceFlow),
		fmt.Sprintf(
			"OAuth flow to use. Must be '%s', '%s', '%s' or '%s'.",
			oauth.CodeFlow, oauth.DeviceFlow, oauth.CredentialsFlow, oauth.PasswordFlow,
		),
	)
	flags.StringVar(
		&runner.args.oauthClientId,
		"oauth-client-id",
		"fulfillment-cli",
		"OAuth client identifier.",
	)
	flags.StringVar(
		&runner.args.oauthClientSecret,
		"oauth-client-secret",
		"",
		fmt.Sprintf(
			"OAuth client secret. This is required for the '%s' flow.",
			oauth.CredentialsFlow,
		),
	)
	flags.StringSliceVar(
		&runner.args.oauthScopes,
		"oauth-scopes",
		[]string{},
		"Comma separated list of OAuth scopes to request.",
	)
	flags.StringVar(
		&runner.args.oauthRedirectUri,
		"oauth-redirect-uri",
		defaultRedirectUri,
		fmt.Sprintf(
			"Redirect URI to use for the OAuth code flow. The default value '%s' means "+
				"binding to localhost on a randomly selected port.",
			defaultRedirectUri,
		),
	)
	flags.StringVar(
		&runner.args.oauthUser,
		"oauth-user",
		"",
		fmt.Sprintf(
			"OAuth user name. This is required for the '%s' flow.",
			oauth.PasswordFlow,
		),
	)
	flags.StringVar(
		&runner.args.oauthPassword,
		"oauth-password",
		"",
		fmt.Sprintf(
			"OAuth password. This is required for the '%s' flow.",
			oauth.PasswordFlow,
		),
	)
	flags.MarkHidden("address")
	flags.MarkHidden("private")
	flags.MarkHidden("token")
	flags.MarkHidden("token-script")
	return result
}

type runnerContext struct {
	logger     *slog.Logger
	console    *terminal.Console
	flags      *pflag.FlagSet
	address    string
	plaintext  bool
	caPool     *x509.CertPool
	tokenStore auth.TokenStore
	args       struct {
		plaintext         bool
		insecure          bool
		caFiles           []string
		address           string
		private           bool
		token             string
		tokenScript       string
		oauthIssuer       string
		oauthFlow         string
		oauthClientId     string
		oauthClientSecret string
		oauthScopes       []string
		oauthRedirectUri  string
		oauthUser         string
		oauthPassword     string
	}
}

func (c *runnerContext) run(cmd *cobra.Command, args []string) error {
	var err error

	// Get the context:
	ctx := cmd.Context()

	// Get the logger, console and flags:
	c.logger = logging.LoggerFromContext(ctx)
	c.console = terminal.ConsoleFromContext(ctx)
	c.flags = cmd.Flags()

	// Load the templates for the console messages:
	err = c.console.AddTemplates(templatesFS, "templates")
	if err != nil {
		return fmt.Errorf("failed to load templates: %w", err)
	}

	// The address used to be specified with a command line flag, but now we also take it from the arguments:
	c.address = c.args.address
	if c.address == "" {
		if len(args) == 1 {
			c.address = args[0]
		} else {
			return fmt.Errorf("address is mandatory")
		}
	}

	// Parse the address:
	c.address, c.plaintext, err = c.parseAddress(c.address)
	if err != nil {
		return fmt.Errorf("failed to parse address: %w", err)
	}

	// Check if the plaintext flag has been explcitly set, and if it conflicts with the result of parsing the
	// address. If it does conflict, then explain the issue to the user.
	if c.flags.Changed("plaintext") && c.plaintext != c.args.plaintext {
		c.console.Render(ctx, "plaintext_conflict.txt", map[string]any{
			"Address":   c.address,
			"Plaintext": c.plaintext,
		})
		return exit.Error(1)
	}

	// Create the CA pool:
	c.caPool, err = network.NewCertPool().
		SetLogger(c.logger).
		AddSystemFiles(true).
		AddKubernetesFiles(true).
		AddFiles(c.args.caFiles...).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create CA pool: %w", err)
	}

	// Create an anonymous gRPC client that we will use to fetch the metadata:
	grpcConn, err := network.NewGrpcClient().
		SetLogger(c.logger).
		SetFlags(c.flags, network.GrpcClientName).
		SetPlaintext(c.plaintext).
		SetInsecure(c.args.insecure).
		SetCaPool(c.caPool).
		SetAddress(c.address).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create anonymous gRPC connection: %w", err)
	}
	defer func() {
		if grpcConn != nil {
			err := grpcConn.Close()
			if err != nil {
				c.logger.ErrorContext(
					ctx,
					"Failed to close gRPC connection",
					slog.Any("error", err),
				)
			}
		}
	}()

	// Fetch the capabilities:
	capabilities, err := c.fetchCapabilities(ctx, grpcConn)
	if err != nil {
		return fmt.Errorf("failed to fetch capabilities: %w", err)
	}
	c.logger.DebugContext(
		ctx,
		"Fetched capabilities",
		slog.Any("capabilities", capabilities),
	)

	// Select the token issuer. The result may be no issuer, which means that no authentication will be used, it
	// will all be anonoymous.
	tokenIssuer, err := c.selectTokenIssuer(ctx, capabilities)
	if err != nil {
		return fmt.Errorf("failed to select token issuer: %w", err)
	}

	// Create an empty configuration and a token store that will load/save tokens from/to that configuration:
	cfg := &config.Config{}
	c.tokenStore = cfg.TokenStore()

	// Create the token source only if a token issuer has been selected.
	tokenSource, err := c.createTokenSource(ctx, tokenIssuer)
	if err != nil {
		return fmt.Errorf("failed to create token source: %w", err)
	}

	// If we got a token source, then try to obtain a token using it, as this will trigger the authentication flow
	// and verify that it works correctly.
	if tokenSource != nil {
		_, err = tokenSource.Token(ctx)
		if err != nil {
			return fmt.Errorf("failed to obtain token using token source: %w", err)
		}
	}

	// Save the basic details of the configuration:
	cfg.Plaintext = c.plaintext
	cfg.Insecure = c.args.insecure
	cfg.Address = c.address
	cfg.Private = c.args.private

	// For CA files that are absolute we need to store only the path, but for those that are relative we need to
	// save the content because otherwise we will not be able to use them when the command is executed from a
	// different directory.
	for _, caFile := range c.args.caFiles {
		if filepath.IsAbs(caFile) {
			cfg.CaFiles = append(cfg.CaFiles, config.CaFile{
				Name: caFile,
			})
		} else {
			caContent, err := os.ReadFile(caFile)
			if err != nil {
				return fmt.Errorf("failed to read CA file '%s': %w", caFile, err)
			}
			cfg.CaFiles = append(cfg.CaFiles, config.CaFile{
				Name:    caFile,
				Content: string(caContent),
			})
		}
	}

	// Save the authenticatoin configuration. Note that the OAuth settings are only saved when they are actually
	// used, and they won't be actually used if the user selected to use a static token or a token script.
	if c.args.token != "" {
		cfg.AccessToken = c.args.token
	} else if c.args.tokenScript != "" {
		cfg.TokenScript = c.args.tokenScript
	} else if tokenIssuer != "" {
		cfg.OauthIssuer = tokenIssuer
		cfg.OAuthFlow = oauth.Flow(c.args.oauthFlow)
		cfg.OAuthClientId = c.args.oauthClientId
		cfg.OAuthClientSecret = c.args.oauthClientSecret
		cfg.OAuthScopes = c.args.oauthScopes
		cfg.OAuthRedirectUri = c.args.oauthRedirectUri
		cfg.OAuthUser = c.args.oauthUser
		cfg.OAuthPassword = c.args.oauthPassword
	}

	// Replace the gRPC anonymous connection with the authenticated one:
	err = grpcConn.Close()
	if err != nil {
		return fmt.Errorf("failed to close anonymous gRPC connection: %w", err)
	}
	grpcConn, err = network.NewGrpcClient().
		SetLogger(c.logger).
		SetFlags(c.flags, network.GrpcClientName).
		SetPlaintext(c.plaintext).
		SetInsecure(c.args.insecure).
		SetCaPool(c.caPool).
		SetAddress(c.address).
		SetTokenSource(tokenSource).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create authenticated gRPC connection: %w", err)
	}

	// Check if the configuration is working by invoking the health check method:
	healthClient := healthv1.NewHealthClient(grpcConn)
	healthResponse, err := healthClient.Check(ctx, &healthv1.HealthCheckRequest{})
	if err != nil {
		return err
	}
	if healthResponse.Status != healthv1.HealthCheckResponse_SERVING {
		return fmt.Errorf("server is not serving, status is '%s'", healthResponse.Status)
	}

	// Everything is working, so we can save the configuration:
	err = config.Save(cfg)
	if err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	return nil
}

// parseAddress parses the address and returns the address and whether accoding to that address the connection should
// use plaintext, without TLS.
func (c *runnerContext) parseAddress(text string) (address string, plaintext bool, err error) {
	parser, err := network.NewAddressParser().
		SetLogger(c.logger).
		Build()
	if err != nil {
		err = fmt.Errorf("failed to create address parser: %w", err)
		return
	}
	address, plaintext, err = parser.Parse(text)
	return
}

func (c *runnerContext) fetchCapabilities(ctx context.Context,
	grpcConn *grpc.ClientConn) (result *publicv1.CapabilitiesGetResponse, err error) {
	capabilitiesClient := publicv1.NewCapabilitiesClient(grpcConn)
	result, err = capabilitiesClient.Get(ctx, publicv1.CapabilitiesGetRequest_builder{}.Build())
	return
}

func (c *runnerContext) selectTokenIssuer(ctx context.Context, capabilities *publicv1.CapabilitiesGetResponse) (result string, err error) {
	advertisedIssuers := capabilities.GetAuthn().GetTrustedTokenIssuers()
	if len(advertisedIssuers) > 0 {
		result = advertisedIssuers[0]
		if len(advertisedIssuers) > 1 {
			c.logger.WarnContext(
				ctx,
				"Server advertises multiple issuers, selecting the first one",
				slog.Any("advertised", advertisedIssuers),
				slog.Any("selected", result),
			)
		}
	} else {
		c.logger.WarnContext(
			ctx,
			"Server advertises no issuers",
			slog.Any("selected", result),
		)
	}
	return
}

// createTokenSource creates a token source from the configuration. The token source will be nil if no token, token
// script or token issuer has been specified.
func (c *runnerContext) createTokenSource(ctx context.Context, tokenIssuer string) (result auth.TokenSource, err error) {
	// Use a token if specified:
	if c.args.token != "" {
		result, err = auth.NewStaticTokenSource().
			SetLogger(c.logger).
			SetToken(&auth.Token{
				Access: c.args.token,
			}).
			Build()
		if err != nil {
			err = fmt.Errorf("failed to create static token source: %w", err)
		}
		return
	}

	// Use a token script if specified::
	if c.args.tokenScript != "" {
		result, err = auth.NewScriptTokenSource().
			SetLogger(c.logger).
			SetScript(c.args.tokenScript).
			SetStore(c.tokenStore).
			Build()
		if err != nil {
			err = fmt.Errorf("failed to create script token source: %w", err)
		}
		return
	}

	// If a token issuer has been selected, then use OAuth to create a token source:
	if tokenIssuer != "" {
		result, err = oauth.NewTokenSource().
			SetLogger(c.logger).
			SetStore(c.tokenStore).
			SetListener(&oauthFlowListener{
				runner: c,
			}).
			SetInsecure(c.args.insecure).
			SetCaPool(c.caPool).
			SetInteractive(true).
			SetIssuer(tokenIssuer).
			SetFlow(oauth.Flow(c.args.oauthFlow)).
			SetClientId(c.args.oauthClientId).
			SetClientSecret(c.args.oauthClientSecret).
			SetScopes(c.args.oauthScopes...).
			SetRedirectUri(c.args.oauthRedirectUri).
			SetUsername(c.args.oauthUser).
			SetPassword(c.args.oauthPassword).
			Build()
		if err != nil {
			err = fmt.Errorf("failed to create OAuth token source: %w", err)
		}
		return
	}

	// Finally, if there is no token, toke script or token issuer, return nil:
	result = nil
	return
}

type oauthFlowListener struct {
	runner *runnerContext
}

func (l *oauthFlowListener) Start(ctx context.Context, event oauth.FlowStartEvent) error {
	switch event.Flow {
	case oauth.CodeFlow:
		return l.startCodeFlow(ctx, event)
	case oauth.DeviceFlow:
		return l.startDeviceFlow(ctx, event)
	case oauth.CredentialsFlow, oauth.PasswordFlow:
		// These flows don't require user interaction, so there is nothing to do here.
		return nil
	default:
		return fmt.Errorf(
			"unsupported flow '%s', must be '%s', '%s', '%s' or '%s'",
			event.Flow, oauth.CodeFlow, oauth.DeviceFlow, oauth.CredentialsFlow, oauth.PasswordFlow,
		)
	}
}

func (l *oauthFlowListener) startCodeFlow(ctx context.Context, event oauth.FlowStartEvent) error {
	l.runner.console.Render(ctx, "start_code_flow.txt", map[string]any{
		"AuthorizationUri": event.AuthorizationUri,
	})
	return nil
}

func (l *oauthFlowListener) startDeviceFlow(ctx context.Context, event oauth.FlowStartEvent) error {
	// If the authorizatoin server has provided a complete URL, with the code included, then use it, otherwise use
	// the URL without the code:
	verficationUri := event.VerificationUriComplete
	if verficationUri == "" {
		verficationUri = event.VerificationUri
	}

	// Calculate the expiration time to show to the user::
	now := time.Now()
	expiresIn := humanize.RelTime(now, now.Add(event.ExpiresIn), "from now", "")
	l.runner.console.Render(ctx, "start_device_flow.txt", map[string]any{
		"VerificationUri": verficationUri,
		"UserCode":        event.UserCode,
		"ExpiresIn":       expiresIn,
	})
	return nil
}

func (l *oauthFlowListener) End(ctx context.Context, event oauth.FlowEndEvent) error {
	if event.Outcome {
		l.runner.console.Render(ctx, "auth_success.txt", nil)
	} else {
		l.runner.console.Render(ctx, "auth_failure.txt", nil)
	}
	return nil
}

// defaultRedirectUri is the default redirect URI used for the OAuth code flow. The value 'http://localhost:0' means
// binding to localhost on a randomly selected port.
const defaultRedirectUri = "http://localhost:0"
