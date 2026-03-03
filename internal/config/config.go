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
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/spf13/pflag"
	"google.golang.org/grpc"

	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/logging"
	"github.com/osac-project/fulfillment-service/internal/network"
	"github.com/osac-project/fulfillment-service/internal/oauth"
	"github.com/osac-project/fulfillment-service/internal/packages"
	"github.com/osac-project/fulfillment-service/internal/version"
)

// Config is the type used to store the configuration of the client.
type Config struct {
	TokenScript       string     `json:"token_script,omitempty"`
	Plaintext         bool       `json:"plaintext,omitempty"`
	Insecure          bool       `json:"insecure,omitempty"`
	CaFiles           []CaFile   `json:"ca_files,omitempty"`
	Address           string     `json:"address,omitempty"`
	Private           bool       `json:"packages,omitempty"`
	AccessToken       string     `json:"access_token,omitempty"`
	RefreshToken      string     `json:"refresh_token,omitempty"`
	TokenExpiry       time.Time  `json:"token_expiry,omitempty"`
	OAuthFlow         oauth.Flow `json:"oauth_flow,omitempty"`
	OauthIssuer       string     `json:"oauth_issuer,omitempty"`
	OAuthClientId     string     `json:"oauth_client_id,omitempty"`
	OAuthClientSecret string     `json:"oauth_client_secret,omitempty"`
	OAuthScopes       []string   `json:"oauth_scopes,omitempty"`
	OAuthRedirectUri  string     `json:"oauth_redirect_uri,omitempty"`
	OAuthUser         string     `json:"oauth_user,omitempty"`
	OAuthPassword     string     `json:"oauth_password,omitempty"`

	caPool *x509.CertPool
}

// CaFile represents a CA certificate file with its name and optionally its content. The content is stored for relative
// paths to allow the configuration to work when the tool is used from a different directory.
type CaFile struct {
	Name    string `json:"name,omitempty"`
	Content string `json:"content,omitempty"`
}

// Load loads the configuration from the configuration file.
func Load(ctx context.Context) (cfg *Config, err error) {
	// Load the file:
	file, err := Location()
	if err != nil {
		return
	}
	_, err = os.Stat(file)
	if os.IsNotExist(err) {
		cfg = &Config{}
		err = nil
		return
	}
	if err != nil {
		err = fmt.Errorf("failed to check if config file '%s' exists: %v", file, err)
		return
	}
	data, err := os.ReadFile(file)
	if err != nil {
		err = fmt.Errorf("failed to read config file '%s': %v", file, err)
		return
	}
	cfg = &Config{}
	if len(data) == 0 {
		return
	}
	err = json.Unmarshal(data, cfg)
	if err != nil {
		err = fmt.Errorf("failed to parse config file '%s': %v", file, err)
		return
	}

	// Create the CA pool:
	err = cfg.createCaPool(ctx)
	if err != nil {
		err = fmt.Errorf("failed to create CA pool: %w", err)
		return
	}

	return
}

// Save saves the given configuration to the configuration file.
func Save(cfg *Config) error {
	file, err := Location()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %v", err)
	}
	dir := filepath.Dir(file)
	err = os.MkdirAll(dir, os.FileMode(0755))
	if err != nil {
		return fmt.Errorf("failed to create directory %s: %v", dir, err)
	}
	err = os.WriteFile(file, data, 0600)
	if err != nil {
		return fmt.Errorf("failed to write file '%s': %v", file, err)
	}
	return nil
}

// Location returns the location of the configuration file.
func Location() (result string, err error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return
	}
	result = filepath.Join(configDir, "fulfillment-cli", "config.json")
	return
}

// TokenSource creates a token source from the configuration.
func (c *Config) TokenSource(ctx context.Context) (result auth.TokenSource, err error) {
	// Get the logger:
	logger := logging.LoggerFromContext(ctx)

	// Get the token store:
	tokenStore := c.TokenStore()

	// If an OAuth flow has been configured, then use it to create a non interactive OAuth token source:
	if c.OAuthFlow != "" {
		result, err = oauth.NewTokenSource().
			SetLogger(logger).
			SetFlow(c.OAuthFlow).
			SetInteractive(false).
			SetIssuer(c.OauthIssuer).
			SetClientId(c.OAuthClientId).
			SetClientSecret(c.OAuthClientSecret).
			SetScopes(c.OAuthScopes...).
			SetRedirectUri(c.OAuthRedirectUri).
			SetUsername(c.OAuthUser).
			SetPassword(c.OAuthPassword).
			SetInsecure(c.Insecure).
			SetCaPool(c.caPool).
			SetStore(tokenStore).
			Build()
		if err != nil {
			err = fmt.Errorf("failed to create OAuth token source: %w", err)
		}
		return
	}

	// If a token script has been configured, then use it to create a script token source:
	if c.TokenScript != "" {
		result, err = auth.NewScriptTokenSource().
			SetLogger(logger).
			SetScript(c.TokenScript).
			SetStore(tokenStore).
			Build()
		if err != nil {
			err = fmt.Errorf("failed to create script token source: %w", err)
		}
		return
	}

	// Finally, if there is an access token try to use it:
	if c.AccessToken != "" {
		result, err = auth.NewStaticTokenSource().
			SetLogger(logger).
			SetToken(&auth.Token{
				Access: c.AccessToken,
			}).
			Build()
		if err != nil {
			err = fmt.Errorf("failed to create static token source: %w", err)
		}
		return
	}

	// If we are here then there is no way to get tokens, so it will all be anonymous.
	result = nil
	return
}

// Conect creates a gRPC connection from the configuration.
func (c *Config) Connect(ctx context.Context, flags *pflag.FlagSet) (result *grpc.ClientConn, err error) {
	// Get the logger:
	logger := logging.LoggerFromContext(ctx)

	// Try to create a token source. This will be nil if no token source can be created, which means that the
	// connection will be anonymous.
	tokenSource, err := c.TokenSource(ctx)
	if err != nil {
		err = fmt.Errorf("failed to create token source: %w", err)
		return
	}

	// Create the version interceptor:
	versionInterceptor, err := version.NewInterceptor().
		SetLogger(logger).
		Build()
	if err != nil {
		err = fmt.Errorf("failed to create version interceptor: %w", err)
		return
	}

	// Create the gRPC client:
	result, err = network.NewGrpcClient().
		SetLogger(logger).
		SetPlaintext(c.Plaintext).
		SetInsecure(c.Insecure).
		SetCaPool(c.caPool).
		SetTokenSource(tokenSource).
		SetAddress(c.Address).
		AddUnaryInterceptor(versionInterceptor.UnaryClient).
		AddStreamInterceptor(versionInterceptor.StreamClient).
		Build()
	if err != nil {
		err = fmt.Errorf("failed to create gRPC client: %w", err)
		return
	}

	return
}

// Packages returns the list of packages that should be enabled according to the configuration. The public packages
// will always be enabled, but the private packages will be enabled only if the `private` flag is true.
//
// The packages are returned as a map, where the key is the name of the package and the value is an integer indicating
// the relative order of the types of the package order of the package when presented to the user. For example, if the
// package 'private.v1' has order 1 and package 'fulfillment.v1' has order 2, then the types of the 'private.v1' should
// be presented first, even if the alphabetical order would put the 'fulfillment.v1' types first.
func (c *Config) Packages() map[string]int {
	result := map[string]int{}
	for _, name := range packages.Public {
		result[name] = 1
	}
	if c.Private {
		for _, name := range packages.Private {
			result[name] = 0
		}
	}
	return result
}

// TokenStore returns an implementation of the auth.TokenStore interface that loads and saves tokens from/to
// the configuration.
func (c *Config) TokenStore() auth.TokenStore {
	return &configTokenStore{
		config: c,
		lock:   &sync.RWMutex{},
	}
}

// CaPool returns the CA pool from the configuration. If the CA pool is not set, it will be created and cached.
func (c *Config) CaPool(ctx context.Context) (result *x509.CertPool, err error) {
	if c.caPool != nil {
		err = c.createCaPool(ctx)
		if err != nil {
			return
		}
	}
	result = c.caPool
	return
}

func (c *Config) createCaPool(ctx context.Context) error {
	// Get the logger:
	logger := logging.LoggerFromContext(ctx)

	// Create a temporary directory for the CA files that we have content for. Those will usually be the CA files
	// that were specified with relative paths when the configuration was saved. The rest of the CA files, the ones with
	// absolute paths, will be loaded from the filesystem.
	var (
		contentFiles []CaFile
		otherFiles   []string
	)
	for _, caFile := range c.CaFiles {
		if caFile.Content != "" {
			contentFiles = append(contentFiles, caFile)
		} else {
			otherFiles = append(otherFiles, caFile.Name)
		}
	}
	contentDir, err := os.MkdirTemp("", "ca-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary directory for CA files: %w", err)
	}
	defer func() {
		err := os.RemoveAll(contentDir)
		if err != nil {
			logger.ErrorContext(
				ctx,
				"Failed to remove temporary directory for CA files",
				slog.Any("error", err),
			)
		}
	}()
	for i, contentFile := range contentFiles {
		contentName := fmt.Sprintf("%d-%s", i, filepath.Base(contentFile.Name))
		contentPath := filepath.Join(contentDir, contentName)
		err = os.WriteFile(contentPath, []byte(contentFile.Content), 0600)
		if err != nil {
			return fmt.Errorf("failed to write CA file to temporary directory: %w", err)
		}
	}

	// Create the CA pool:
	c.caPool, err = network.NewCertPool().
		SetLogger(logger).
		AddSystemFiles(true).
		AddKubernetesFiles(true).
		AddFile(contentDir).
		AddFiles(otherFiles...).
		Build()
	return err
}

type configTokenStore struct {
	config *Config
	lock   *sync.RWMutex
}

func (s *configTokenStore) Load(ctx context.Context) (result *auth.Token, err error) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	if s.config.AccessToken == "" {
		return
	}
	result = &auth.Token{
		Access:  s.config.AccessToken,
		Refresh: s.config.RefreshToken,
		Expiry:  s.config.TokenExpiry,
	}
	return
}

func (s *configTokenStore) Save(ctx context.Context, token *auth.Token) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	if token == nil {
		return errors.New("token cannot be nil")
	}
	accessChanged := s.config.AccessToken != token.Access
	refreshChanged := s.config.RefreshToken != token.Refresh
	expiryChanged := s.config.TokenExpiry != token.Expiry
	if !accessChanged && !refreshChanged && !expiryChanged {
		return nil
	}
	s.config.AccessToken = token.Access
	s.config.RefreshToken = token.Refresh
	s.config.TokenExpiry = token.Expiry
	return Save(s.config)
}
