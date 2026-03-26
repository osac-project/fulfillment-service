/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package database

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	neturl "net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/pflag"
)

//go:embed migrations
var migrationsFS embed.FS

// Tool tries to simplify and centralize database operations that are needed frequently during the startup of a process
// that uses the database, like creating the database connection string from the command line flags, waiting till the
// database is up and running, applying the migrations and creationg the database connection pool.
type Tool interface {
	// Wait waits till the database is available.
	Wait(ctx context.Context) error

	// Migrate runs the database migrations.
	Migrate(ctx context.Context) error

	// Pool returns the pool of database connections.
	Pool(ctx context.Context) (result *pgxpool.Pool, err error)

	// URL returns the database connection URL.
	URL() string
}

type ToolBuilder struct {
	logger  *slog.Logger
	url     string
	urlFile string
}

type tool struct {
	logger *slog.Logger
	url    string
}

func NewTool() *ToolBuilder {
	return &ToolBuilder{}
}

func (b *ToolBuilder) SetLogger(value *slog.Logger) *ToolBuilder {
	b.logger = value
	return b
}

// SetURL sets the database connection URL directly.
func (b *ToolBuilder) SetURL(value string) *ToolBuilder {
	b.url = value
	return b
}

// SetURLFile sets the path to a file or directory containing database connection settings.
//
// When pointing to a file the URL is read from the file contents.
//
// When pointing to a directory, the tool scans for files named after PostgreSQL connection parameters and builds the
// URL from them. A file named 'url' provides the base URL. Files named after URL components ('host', 'port', 'user',
// 'password', 'dbname') modify the corresponding parts of the URL. Files named after file-path parameters ('sslcert',
// 'sslkey', 'sslrootcert', 'sslcrl' and 'sslcrldir', for example) are referenced by their path in the URL rather
// than their contents. All other files are treated as query parameters whose values are read from the file contents.
// For example, given a directory '/etc/db' with the following files:
//
//	/etc/db/url         - Contains 'postgres://service@db.example.com:5432/mydb'.
//	/etc/db/sslmode     - Contains 'verify-full'.
//	/etc/db/sslcert     - Contains the client certificate in PEM format.
//	/etc/db/sslkey      - Contains the client private key in PEM format.
//	/etc/db/sslrootcert - Contains the CA certificate in PEM format.
//
// The resulting URL will be:
//
//	postgres://service@db.example.com:5432/mydb?sslcert=/etc/db/sslcert&sslkey=/etc/db/sslkey&sslmode=verify-full&sslrootcert=/etc/db/sslrootcert
//
// Note that 'sslmode' is set to the file contents ('verify-full') while 'sslcert', 'sslkey', and 'sslrootcert' are
// set to the file paths, because PostgreSQL expects those parameters to point to certificate files on disk.
//
// Note that this supports the parameters supported by the `pgx` library, not all PostgreSQL connection parameters.
// For example, 'sslcrl' or 'passfile` aren't supported. Check the documentation of the `pgx` library for more details.
//
// When set, this is incompatible with the URL set with SetURL.
func (b *ToolBuilder) SetURLFile(value string) *ToolBuilder {
	b.urlFile = value
	return b
}

// SetFlags sets the command line flags that should be used to configure the tool. This is optional. Note that no
// files are read at this point; file reading is deferred to the Build method.
func (b *ToolBuilder) SetFlags(flags *pflag.FlagSet) *ToolBuilder {
	if flags == nil {
		return b
	}

	var (
		flag  string
		value string
		err   error
	)
	failure := func() {
		b.logger.Error(
			"Failed to get flag value",
			slog.String("flag", flag),
			slog.Any("error", err),
		)
	}

	// URL:
	flag = urlFlagName
	value, err = flags.GetString(flag)
	if err != nil {
		failure()
	} else if value != "" {
		b.SetURL(value)
	}

	// URL file:
	flag = urlFileFlagName
	value, err = flags.GetString(flag)
	if err != nil {
		failure()
	} else if value != "" {
		b.SetURLFile(value)
	}

	return b
}

func (b *ToolBuilder) Build() (result Tool, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}

	// Check that the URL and URL file are not both specified:
	if b.url != "" && b.urlFile != "" {
		err = errors.New(
			"database connection URL and URL file are incompatible, use one or the other but not both",
		)
		return
	}

	// Resolve the URL and collect parameters. If the value points to a directory, the tool scans the directory
	// for files that represent connection settings. Each file name is treated as a parameter name. For file-path
	// parameters ('sslcert', 'sslkey', 'sslrootcert', 'sslcrl' and 'sslcrldir', for example) the value is the path
	// to the file itself. For all other parameters the value is read from the file contents. If the directory
	// contains a file named 'url', it is used as the base URL.
	url := b.url
	parameters := map[string]string{}
	if b.urlFile != "" {
		var info os.FileInfo
		info, err = os.Stat(b.urlFile)
		if err != nil {
			err = fmt.Errorf("failed to stat database URL path '%s': %w", b.urlFile, err)
			return
		}
		if info.IsDir() {
			url, parameters, err = b.readURLDirectory(b.urlFile)
			if err != nil {
				return
			}
		} else {
			var data []byte
			data, err = os.ReadFile(b.urlFile)
			if err != nil {
				err = fmt.Errorf("failed to read database URL file '%s': %w", b.urlFile, err)
				return
			}
			url = strings.TrimSpace(string(data))
		}
	}

	if url == "" {
		err = errors.New("connection URL is mandatory")
		return
	}

	// Apply URL parameters. Some parameter names are special and modify the URL structure instead of
	// being added as query parameters: 'host', 'port', 'user', 'password', and 'dbname'.
	if len(parameters) > 0 {
		var parsed *neturl.URL
		parsed, err = neturl.Parse(url)
		if err != nil {
			err = fmt.Errorf("failed to parse database URL: %w", err)
			return
		}
		query := parsed.Query()
		for parameter, value := range parameters {
			switch parameter {
			case "host":
				port := parsed.Port()
				if port != "" {
					parsed.Host = fmt.Sprintf("%s:%s", value, port)
				} else {
					parsed.Host = value
				}
			case "port":
				parsed.Host = fmt.Sprintf("%s:%s", parsed.Hostname(), value)
			case "user":
				if parsed.User != nil {
					password, hasPassword := parsed.User.Password()
					if hasPassword {
						parsed.User = neturl.UserPassword(value, password)
					} else {
						parsed.User = neturl.User(value)
					}
				} else {
					parsed.User = neturl.User(value)
				}
			case "password":
				username := ""
				if parsed.User != nil {
					username = parsed.User.Username()
				}
				parsed.User = neturl.UserPassword(username, value)
			case "dbname":
				parsed.Path = "/" + value
			default:
				query.Set(parameter, value)
			}
		}
		parsed.RawQuery = query.Encode()
		url = parsed.String()
	}

	// Create and populate the object:
	result = &tool{
		logger: b.logger,
		url:    url,
	}
	return
}

// readURLDirectory reads a directory containing database connection settings. Each file in the directory is treated
// as a connection parameter. The file named 'url' (if present) provides the base URL. File-path parameters like
// 'sslcert' use the file path itself as their value. All other parameters use the file contents as their value.
func (b *ToolBuilder) readURLDirectory(dir string) (url string, parameters map[string]string, err error) {
	parameters = map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		err = fmt.Errorf("failed to read database URL directory '%s': %w", dir, err)
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(dir, name)
		if name == "url" {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				err = fmt.Errorf("failed to read 'url' from file '%s': %w", path, readErr)
				return
			}
			url = strings.TrimSpace(string(data))
		} else if slices.Contains(toolFilePathParameters, name) {
			parameters[name] = path
		} else {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				err = fmt.Errorf(
					"failed to read parameter '%s' from file '%s': %w", name, path, readErr,
				)
				return
			}
			parameters[name] = strings.TrimSpace(string(data))
		}
	}

	// If no url file was found, build a default base URL so that the parameter application logic
	// (which handles 'host', 'port', 'user', 'password', and 'dbname') can construct the full URL.
	if url == "" {
		url = "postgres://localhost:5432"
	}
	return
}

// Wait waits for the database to be available.
func (t *tool) Wait(ctx context.Context) error {
	// If the database IP address or host name have not been created yet then the connection will take a long time
	// to fail, approximately five minutes. To avoid that we need to explicitly set a shorter timeout.
	parsed, err := neturl.Parse(t.url)
	if err != nil {
		return err
	}
	query := parsed.Query()
	query.Set("connect_timeout", "1")
	parsed.RawQuery = query.Encode()
	waitURL := parsed.String()

	// Try to connect to the database until we succeed, without limit of attempts:
	for {
		conn, err := pgx.Connect(ctx, waitURL)
		if err != nil {
			t.logger.InfoContext(
				ctx,
				"Database isn't responding yet",
				slog.Any("error", err),
			)
			time.Sleep(1 * time.Second)
			continue
		}
		err = conn.Close(ctx)
		if err != nil {
			t.logger.ErrorContext(
				ctx,
				"Failed to close database connection",
				slog.Any("error", err),
			)
		}
		return nil
	}
}

// Migrate runs the database migrations.
func (t *tool) Migrate(ctx context.Context) error {
	// The database connection URL given by the user will probably start with 'postgres', and that works fine for
	// regular connections, but for the migration library it needs to be 'pgx5'.
	parsed, err := neturl.Parse(t.url)
	if err != nil {
		return err
	}
	parsed.Scheme = "pgx5"
	migrateURL := parsed.String()

	// Load the migration files:
	driver, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	migrations, err := migrate.NewWithSourceInstance("iofs", driver, migrateURL)
	if err != nil {
		return err
	}
	migrations.Log = &migrationsLogger{
		ctx:    ctx,
		logger: t.logger.WithGroup("migrations"),
	}
	defer func() {
		sourceErr, databaseErr := migrations.Close()
		if sourceErr != nil || databaseErr != nil {
			t.logger.ErrorContext(
				ctx,
				"Failed to close migrations",
				slog.Any("source", sourceErr),
				slog.Any("database", databaseErr),
			)
		}
	}()

	// Show the schema version before running the migrations:
	version, dirty, err := migrations.Version()
	switch {
	case err == nil:
		t.logger.InfoContext(
			ctx,
			"Version before running migrations",
			slog.Uint64("version", uint64(version)),
			slog.Bool("dirty", dirty),
		)
	case err == migrate.ErrNilVersion:
		t.logger.InfoContext(
			ctx,
			"Schema hasn't been created yet, will create it now",
		)
	default:
		return err
	}

	// Run the migrations:
	err = migrations.Up()
	switch {
	case err == nil:
		t.logger.InfoContext(
			ctx,
			"Migrations executed successfully",
		)
	case err == migrate.ErrNoChange:
		t.logger.InfoContext(
			ctx,
			"Migrationd don't need to be executed",
		)
	default:
		return err
	}

	// Show the schema version after running the migrations:
	version, dirty, err = migrations.Version()
	if err != nil {
		return err
	}
	t.logger.InfoContext(
		ctx,
		"Schema version after running migrations",
		slog.Uint64("version", uint64(version)),
		slog.Bool("dirty", dirty),
	)

	return nil
}

// URL returns the database connection URL.
func (t *tool) URL() string {
	return t.url
}

// Pool returns the pool of database connections.
func (t *tool) Pool(ctx context.Context) (result *pgxpool.Pool, err error) {
	result, err = pgxpool.New(ctx, t.url)
	return
}

// migrationsLogger is an adapter to implement the logging interface of the underlying migrations library using our
// logging library.
type migrationsLogger struct {
	ctx    context.Context
	logger *slog.Logger
}

// Verbose is part of the implementation of the migrate.Logger interface.
func (l *migrationsLogger) Verbose() bool {
	return true
}

// Printf is part of the implementation of the migrate.Logger interface.
func (l *migrationsLogger) Printf(format string, v ...any) {
	message := strings.TrimSpace(fmt.Sprintf(format, v...))
	l.logger.InfoContext(l.ctx, message)
}

// toolFilePathParameters is the set of PostgreSQL connection parameters whose values are file paths. When these are
// found in a connection settings directory, the value used in the URL is the path to the file itself, not the
// file contents.
var toolFilePathParameters = []string{
	"sslcert",
	"sslkey",
	"sslrootcert",
	"sslcrl",
	"sslcrldir",
}
