/*
Copyright (c) 2025 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package database

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/osac-project/fulfillment-service/internal/logging"
)

// ContainerBuilder contains the data and logic needed to create a database server container. Don't create instances of
// this type directly, use the NewServer function instead.
type ContainerBuilder struct {
	logger *slog.Logger
	tool   string
}

// Container knows how to start a PostgreSQL database server inside a container, and how to create databases. It is
// intended for use in unit tests where an ephemeral PostgreSQL instance is needed. It lives in the database package
// because it is closely related to the database infrastructure, and to avoid dependency cycles between test utilities
// and the database package. It should not be used to create databases for production environments.
//
// When started, the container creates a shared user and a template database with all migrations applied. New database
// instances created without a specific version are cloned from this template, which is significantly faster than
// running all migrations from scratch each time.
type Container struct {
	logger         *slog.Logger
	tool           string
	id             string
	host           string
	port           string
	adminPassword  string
	sharedPassword string
	configFile     string
	adminConn      *pgx.Conn
	outWriter      *logging.Writer
	errWriter      *logging.Writer
	count          int
	instances      []*Instance
}

// NewContainer creates a builder that can then be used to configure and create a database server. The resulting server
// starts a PostgreSQL instance inside a container using podman or docker and is intended exclusively for unit tests. Do
// not use it to create databases for production environments.
func NewContainer() *ContainerBuilder {
	return &ContainerBuilder{}
}

// SetLogger sets the logger that the server will use to write messages to the log. This is mandatory.
func (b *ContainerBuilder) SetLogger(value *slog.Logger) *ContainerBuilder {
	b.logger = value
	return b
}

// SetTool sets the tool used to start the database server container. This is optional, by default it will try to use
// 'podman', and if that is not available it will try to use 'docker'.
func (b *ContainerBuilder) SetTool(value string) *ContainerBuilder {
	b.tool = value
	return b
}

// Build uses the information stored in the builder to create the database server container object. The container is not
// started at this point; call the Start method to start it.
func (b *ContainerBuilder) Build() (result *Container, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}

	// Select the container tool to use:
	tool := b.tool
	if tool == "" {
		tool, err = b.selectTool()
		if err != nil {
			err = fmt.Errorf("failed to select container tool: %w", err)
			return
		}
	}
	b.logger.Info(
		"Selected container tool",
		slog.String("tool", tool),
	)

	// Generate random passwords for the database administrator and the shared user:
	adminPassword := uuid.NewString()
	sharedPassword := uuid.NewString()

	// Prepare writers to write the output of the commands to the log, redacting the passwords:
	outLogger := b.logger.With(
		slog.String("stream", "stdout"),
	)
	outWriter, err := logging.NewWriter().
		SetLogger(outLogger).
		SetLevel(slog.LevelDebug).
		AddSecrets(adminPassword, sharedPassword).
		Build()
	if err != nil {
		err = fmt.Errorf("failed to create writer for command output: %w", err)
		return
	}
	errLogger := b.logger.With(
		slog.String("stream", "stderr"),
	)
	errWriter, err := logging.NewWriter().
		SetLogger(errLogger).
		SetLevel(slog.LevelDebug).
		AddSecrets(adminPassword, sharedPassword).
		Build()
	if err != nil {
		err = fmt.Errorf("failed to create writer for command errors: %w", err)
		return
	}

	// Create the server object:
	result = &Container{
		logger:         b.logger,
		tool:           tool,
		adminPassword:  adminPassword,
		sharedPassword: sharedPassword,
		outWriter:      outWriter,
		errWriter:      errWriter,
	}
	return
}

// selectTool selects the tool to use to start the database server container.
func (b *ContainerBuilder) selectTool() (result string, err error) {
	for _, tool := range containerTools {
		var toolPath string
		toolPath, err = exec.LookPath(tool)
		if err != nil {
			b.logger.Info(
				"Container tool not available",
				slog.String("tool", tool),
				slog.Any("error", err),
			)
			continue
		}
		result = toolPath
		return
	}
	err = errors.New("can't find any available container tool")
	return
}

// Start starts the database server inside a container and waits until it is ready to accept connections. Cleaning when
// something fails, or when the container is no longer needed, is the responsibility of the caller.
func (s *Container) Start(ctx context.Context) error {
	// Create the configuration file:
	err := s.createConfigFile(ctx)
	if err != nil {
		return err
	}

	// Start the database server:
	runCmd := exec.Command(
		s.tool,
		"run",
		"--env", fmt.Sprintf("POSTGRESQL_ADMIN_PASSWORD=%s", s.adminPassword),
		"--publish", "5432",
		"--detach",
		"--rm",
		"--volume", fmt.Sprintf("%s:%s:Z", s.configFile, containerConfigPath),
		containerImage,
	)
	runOut := &bytes.Buffer{}
	runCmd.Stdout = io.MultiWriter(runOut, s.outWriter)
	runCmd.Stderr = s.errWriter
	err = runCmd.Run()
	if err != nil {
		return fmt.Errorf("failed to start database container: %w", err)
	}
	s.id = strings.TrimSpace(runOut.String())

	// Find out the port number assigned to the database server:
	portCmd := exec.Command(
		s.tool,
		"port",
		s.id,
		"5432/tcp",
	)
	portOut := &bytes.Buffer{}
	portCmd.Stdout = io.MultiWriter(portOut, s.outWriter)
	portCmd.Stderr = s.errWriter
	err = portCmd.Run()
	if err != nil {
		return fmt.Errorf("failed to get port for database container: %w", err)
	}
	portLines := strings.Split(portOut.String(), "\n")
	if len(portLines) < 1 || strings.TrimSpace(portLines[0]) == "" {
		return fmt.Errorf("failed to parse port output: no lines returned")
	}
	hostPort := strings.TrimSpace(portLines[0])
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		return fmt.Errorf("failed to parse host:port '%s': %w", hostPort, err)
	}
	if host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	s.host = host
	s.port = port

	// Wait till the database server is responding:
	adminUrl := fmt.Sprintf(
		"postgres://postgres:%s@%s:%s/postgres?sslmode=disable&connect_timeout=1",
		s.adminPassword, host, port,
	)
	var adminConn *pgx.Conn
	for {
		adminConn, err = pgx.Connect(ctx, adminUrl)
		if err == nil {
			break
		}
		s.logger.DebugContext(
			ctx,
			"Database server isn't responding yet",
			slog.Any("error", err),
		)
		select {
		case <-time.After(1 * time.Second):
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for database server to respond: %w", ctx.Err())
		}
	}
	s.adminConn = adminConn

	// Create the shared user that will own all databases:
	_, err = s.adminConn.Exec(
		ctx,
		fmt.Sprintf(
			"create user %s with password '%s'",
			containerTemplateUser, s.sharedPassword,
		),
	)
	if err != nil {
		return fmt.Errorf("failed to create shared user: %w", err)
	}

	// Create the template database with all migrations applied. New instances that don't request a specific
	// migration version will be cloned from this template instead of running migrations from scratch.
	_, err = s.adminConn.Exec(
		ctx,
		fmt.Sprintf(
			"create database %s owner %s",
			containerTemplateDatabase, containerTemplateUser,
		),
	)
	if err != nil {
		return fmt.Errorf("failed to create template database: %w", err)
	}
	templateUrl := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		containerTemplateUser, s.sharedPassword, s.host, s.port, containerTemplateDatabase,
	)
	templateTool, err := NewTool().
		SetLogger(s.logger).
		SetURL(templateUrl).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create database tool for template: %w", err)
	}
	err = templateTool.Migrate(ctx, math.MaxUint)
	if err != nil {
		return fmt.Errorf("failed to run migrations on template database: %w", err)
	}
	_, err = s.adminConn.Exec(
		ctx,
		fmt.Sprintf(
			"alter database %s is_template true",
			containerTemplateDatabase,
		),
	)
	if err != nil {
		return fmt.Errorf("failed to mark template database: %w", err)
	}

	return nil
}

// createConfigFile creates a temporary configuration file with performance and logging settings. The file name is
// stored in the object so it can be removed when the container is stopped.
func (c *Container) createConfigFile(ctx context.Context) (err error) {
	// Remember to remove the temporary file if somethng fails:
	var tmpFile *os.File
	defer func() {
		if err == nil || tmpFile == nil {
			return
		}
		removeErr := os.Remove(tmpFile.Name())
		if removeErr == nil {
			return
		}
		c.logger.ErrorContext(
			ctx,
			"Failed to remove configuration file",
			slog.String("file", tmpFile.Name()),
			slog.Any("error", removeErr),
		)
	}()

	// Write a temporary configuration file with performance and logging settings. The image picks up '*.conf'
	// files from '/opt/app-root/src/postgresql-cfg' and appends them to the generated 'postgresql.conf', so these
	// settings override the defaults.
	tmpFile, err = os.CreateTemp("", "*.conf")
	if err != nil {
		err = fmt.Errorf("failed to create configuration file: %w", err)
		return
	}
	_, err = tmpFile.WriteString(containerConfigText)
	if err != nil {
		err = fmt.Errorf("failed to write configuration file: %w", err)
		return
	}
	err = tmpFile.Close()
	if err != nil {
		err = fmt.Errorf("failed to close configuration file: %w", err)
		return
	}

	// The sclorg image runs PostgreSQL as UID 26, which in rootless podman maps to a different host UID. Since
	// os.CreateTemp creates files with mode 0600, the container process can't read them. Widen to 0644 so the
	// mapped UID can read the configuration.
	err = os.Chmod(tmpFile.Name(), 0644)
	if err != nil {
		err = fmt.Errorf("failed to set permissions on configuration file: %w", err)
		return
	}

	// Store the file name so that it can be removed when the container is stopped.
	c.configFile = tmpFile.Name()

	return nil
}

// Stop stops the database server and removes all databases that were created.
func (s *Container) Stop(ctx context.Context) error {
	// Remember to remove the configuration file:
	defer func() {
		if s.configFile == "" {
			return
		}
		removeErr := os.Remove(s.configFile)
		if removeErr != nil {
			s.logger.ErrorContext(
				ctx,
				"Failed to remove configuration file",
				slog.String("file", s.configFile),
				slog.Any("error", removeErr),
			)
			return
		}
	}()

	// Delete all instance databases:
	for _, instance := range s.instances {
		err := instance.Close(ctx)
		if err != nil {
			s.logger.ErrorContext(
				ctx,
				"Failed to close database instance",
				slog.Any("error", err),
			)
		}
	}

	// Drop the template database and the shared user:
	_, err := s.adminConn.Exec(
		ctx,
		fmt.Sprintf(
			"alter database %s is_template false",
			containerTemplateDatabase,
		),
	)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to unmark template database",
			slog.Any("error", err),
		)
	}
	_, err = s.adminConn.Exec(
		ctx,
		fmt.Sprintf(
			"drop database if exists %s with (force)",
			containerTemplateDatabase,
		),
	)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to drop template database",
			slog.Any("error", err),
		)
	}
	_, err = s.adminConn.Exec(
		ctx,
		fmt.Sprintf(
			"drop user if exists %s",
			containerTemplateUser,
		),
	)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to drop shared user",
			slog.Any("error", err),
		)
	}

	// Close the database handle:
	err = s.adminConn.Close(ctx)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to close database handle",
			slog.Any("error", err),
		)
	}

	// Get the logs of the database server:
	logsCmd := exec.Command(s.tool, "logs", s.id)
	logsOut := &bytes.Buffer{}
	logsCmd.Stdout = io.MultiWriter(logsOut, s.outWriter)
	logsCmd.Stderr = s.errWriter
	err = logsCmd.Run()
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to get container logs",
			slog.Any("error", err),
		)
	}

	// Stop the database server:
	killCmd := exec.Command(s.tool, "kill", s.id)
	killOut := &bytes.Buffer{}
	killCmd.Stdout = io.MultiWriter(killOut, s.outWriter)
	killCmd.Stderr = s.errWriter
	err = killCmd.Run()
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to kill database container",
			slog.Any("error", err),
		)
	}

	return nil
}

// InstanceBuilder contains the data and logic needed to create a database instance. Don't create instances of this
// type directly, use the NewInstance method of the Server type instead.
type InstanceBuilder struct {
	container *Container
	version   *uint
}

// Instance is a PostgreSQL database created inside a Container. It delegates user credentials to the container's
// shared user.
type Instance struct {
	container *Container
	name      string
	version   *uint
	url       string
	lock      *sync.Mutex
}

// NewInstance creates a builder that can then be used to configure and create a new database instance inside this
// server.
func (s *Container) NewInstance() *InstanceBuilder {
	return &InstanceBuilder{
		container: s,
	}
}

// SetVersion sets the migration version to apply after creating the database. By default all available migrations
// are applied. Pass a specific version to migrate only up to that version.
func (b *InstanceBuilder) SetVersion(value uint) *InstanceBuilder {
	b.version = &value
	return b
}

// Build uses the information stored in the builder to create the database instance. When no version has been set
// the database is cloned from the pre-migrated template, which is significantly faster than running all migrations.
// When a specific version has been set the database is created from scratch and only the requested migrations are
// applied.
func (b *InstanceBuilder) Build() (result *Instance, err error) {
	result = &Instance{
		container: b.container,
		version:   b.version,
		lock:      &sync.Mutex{},
	}
	b.container.instances = append(b.container.instances, result)
	return
}

func (i *Instance) initIfNeeded(ctx context.Context) error {
	i.lock.Lock()
	defer i.lock.Unlock()
	if i.name != "" {
		return nil
	}
	return i.init(ctx)
}

func (i *Instance) init(ctx context.Context) error {
	// Calculate the name:
	i.container.count++
	i.name = fmt.Sprintf("%s%d", containerTemplateDatabase, i.container.count)

	// Calculate the URL:
	i.url = fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		containerTemplateUser, i.container.sharedPassword, i.container.host, i.container.port, i.name,
	)

	// If a version has been set, we need to create a blank database and run migrations up to the requested version,
	// otherwise we can clone the template database, which already has all migrations applied.
	if i.version != nil {
		return i.initFromScratch(ctx)
	}
	return i.initFromTemplate(ctx)
}

func (i *Instance) initFromTemplate(ctx context.Context) error {
	_, err := i.container.adminConn.Exec(
		ctx,
		fmt.Sprintf(
			"create database %s template %s owner %s",
			i.name, containerTemplateDatabase, containerTemplateUser,
		),
	)
	if err != nil {
		return fmt.Errorf("failed to create database '%s' from template: %w", i.name, err)
	}
	return nil
}

func (i *Instance) initFromScratch(ctx context.Context) error {
	_, err := i.container.adminConn.Exec(
		ctx,
		fmt.Sprintf(
			"create database %s owner %s",
			i.name, containerTemplateUser,
		),
	)
	if err != nil {
		return fmt.Errorf("failed to create database '%s' from scratch: %w", i.name, err)
	}
	tool, err := NewTool().
		SetLogger(i.container.logger).
		SetURL(i.url).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create database tool: %w", err)
	}
	err = tool.Migrate(ctx, *i.version)
	if err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	return nil
}

// Url returns the connection URL for this database instance.
func (i *Instance) Url(ctx context.Context) (result string, err error) {
	err = i.initIfNeeded(ctx)
	if err != nil {
		return
	}
	result = i.url
	return
}

// Pool creates and returns a new connection pool for this instance. The caller is responsible for closing the pool
// when it is no longer needed.
func (i *Instance) Pool(ctx context.Context) (result *pgxpool.Pool, err error) {
	err = i.initIfNeeded(ctx)
	if err != nil {
		return
	}
	result, err = pgxpool.New(ctx, i.url)
	if err != nil {
		err = fmt.Errorf("failed to create connection pool: %w", err)
	}
	return
}

// Connnection returns a new database connection for this instance. The caller is responsible for closing the connection
// when it is no longer needed.
func (i *Instance) Connection(ctx context.Context) (result *pgx.Conn, err error) {
	err = i.initIfNeeded(ctx)
	if err != nil {
		return
	}
	conn, err := pgx.Connect(ctx, i.url)
	if err != nil {
		err = fmt.Errorf("failed to create database connection: %w", err)
		return
	}
	result = conn
	return
}

// Close deletes the database that was created for this instance.
func (i *Instance) Close(ctx context.Context) error {
	i.lock.Lock()
	defer i.lock.Unlock()
	if i.name == "" {
		return nil
	}
	_, err := i.container.adminConn.Exec(
		ctx,
		fmt.Sprintf(
			`drop database if exists %s with (force)`,
			i.name,
		))
	if err != nil {
		return fmt.Errorf("failed to drop database '%s': %w", i.name, err)
	}
	return nil
}

// containerTools is the list of container tools that we will try to use to start the database server container, in
// order of preference.
var containerTools = []string{
	"podman",
	"docker",
}

// containerImage is the PostgreSQL container image. This is the same sclorg image used by the integration test Helm
// chart.
const containerImage = "quay.io/sclorg/postgresql-15-c9s" +
	"@sha256:c51a29654b63e2683f83efde5c751833fc79d360918c30269801608dff3c533a"

// containerConfigPath is the path inside the container where the custom configuration file is mounted. The sclorg image
// includes all *.conf files from this directory at the end of the generated postgresql.conf.
const containerConfigPath = "/opt/app-root/src/postgresql-cfg/custom.conf"

// containerConfigText is the PostgreSQL configuration written to a temporary file on the host and bind-mounted into the
// container. It disables durability features to speed up tests and configures logging so that all statements are
// visible in the container output.
const containerConfigText = `
fsync = off
log_destination = 'stderr'
log_statement = 'all'
logging_collector = off
`

// containerTemplateDatabase is the name of the template database that is created when the container is started.
const containerTemplateDatabase = "d"

// containerTemplateUser is the name of the user that is created when the container is started.
const containerTemplateUser = "u"
