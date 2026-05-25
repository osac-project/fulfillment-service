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
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
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
type Container struct {
	logger    *slog.Logger
	tool      string
	id        string
	host      string
	port      string
	handle    *sql.DB
	count     int
	instances []*Instance
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

	// Create the server object:
	result = &Container{
		logger: b.logger,
		tool:   tool,
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
	// Generate a random password for the database administrator:
	password := uuid.NewString()

	// Start the database server:
	runOut := &bytes.Buffer{}
	runErr := &bytes.Buffer{}
	runCmd := exec.Command(
		s.tool,
		"run",
		"--env", "POSTGRES_PASSWORD="+password,
		"--publish", "5432",
		"--detach",
		"--rm",
		"docker.io/postgres:15",
		"-c", "log_destination=stderr",
		"-c", "log_statement=all",
		"-c", "logging_collector=off",
		"-c", "fsync=off",
	)
	runCmd.Stdout = runOut
	runCmd.Stderr = runErr
	err := runCmd.Run()
	if err != nil {
		return fmt.Errorf("failed to start database container: %s: %w", runErr.String(), err)
	}
	s.id = strings.TrimSpace(runOut.String())

	// Find out the port number assigned to the database server:
	portOut := &bytes.Buffer{}
	portErr := &bytes.Buffer{}
	portCmd := exec.Command(
		s.tool,
		"port",
		s.id,
		"5432/tcp",
	)
	portCmd.Stdout = portOut
	portCmd.Stderr = portErr
	err = portCmd.Run()
	if err != nil {
		return fmt.Errorf("failed to get port for database container: %s: %w", portErr.String(), err)
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
	url := fmt.Sprintf(
		"postgres://postgres:%s@%s:%s/postgres?sslmode=disable",
		password, host, port,
	)
	handle, err := sql.Open("pgx", url)
	if err != nil {
		return fmt.Errorf("failed to open database connection: %w", err)
	}
	for {
		err = handle.PingContext(ctx)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			handle.Close()
			return fmt.Errorf("timed out waiting for database server to respond: %w", err)
		}
		select {
		case <-time.After(1 * time.Second):
		case <-ctx.Done():
			handle.Close()
			return fmt.Errorf("timed out waiting for database server to respond: %w", ctx.Err())
		}
	}
	s.handle = handle

	return nil
}

// Stop stops the database server and removes all databases that were created.
func (s *Container) Stop(ctx context.Context) error {
	// Delete all databases:
	for _, instance := range s.instances {
		err := instance.Close()
		if err != nil {
			s.logger.ErrorContext(
				ctx,
				"Failed to close database instance",
				slog.Any("error", err),
			)
		}
	}

	// Close the database handle:
	err := s.handle.Close()
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to close database handle",
			slog.Any("error", err),
		)
	}

	// Get the logs of the database server:
	logsOut := &bytes.Buffer{}
	logsErr := &bytes.Buffer{}
	logsCmd := exec.Command(s.tool, "logs", s.id)
	logsCmd.Stdout = logsOut
	logsCmd.Stderr = logsErr
	err = logsCmd.Run()
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to get container logs",
			slog.String("stdout", logsOut.String()),
			slog.String("stderr", logsErr.String()),
			slog.Any("error", err),
		)
	} else {
		s.logger.Info(
			"Database server container logs",
			slog.String("stdout", logsOut.String()),
			slog.String("stderr", logsErr.String()),
		)
	}

	// Stop the database server:
	killOut := &bytes.Buffer{}
	killErr := &bytes.Buffer{}
	killCmd := exec.Command(s.tool, "kill", s.id)
	killCmd.Stdout = killOut
	killCmd.Stderr = killErr
	err = killCmd.Run()
	if err != nil {
		return fmt.Errorf("failed to kill database container: %s: %w", killErr.String(), err)
	}

	return nil
}

// InstanceBuilder contains the data and logic needed to create a database instance. Don't create instances of this
// type directly, use the NewInstance method of the Server type instead.
type InstanceBuilder struct {
	server *Container
}

// Instance is a PostgreSQL database created inside a Server.
type Instance struct {
	server   *Container
	name     string
	user     string
	password string
}

// NewInstance creates a builder that can then be used to configure and create a new database instance inside this
// server.
func (s *Container) NewInstance() *InstanceBuilder {
	return &InstanceBuilder{
		server: s,
	}
}

// Build uses the information stored in the builder to create the database instance.
func (b *InstanceBuilder) Build() (result *Instance, err error) {
	s := b.server

	// Generate the database name and password:
	name := fmt.Sprintf("test_%d", s.count)
	user := fmt.Sprintf("test_%d", s.count)
	password := uuid.NewString()
	s.count++

	// Create the user:
	_, execErr := s.handle.Exec(fmt.Sprintf(
		"create user %s with password '%s'",
		user, password,
	))
	if execErr != nil {
		err = fmt.Errorf("failed to create user '%s': %w", user, execErr)
		return
	}

	// Create the database:
	_, execErr = s.handle.Exec(fmt.Sprintf(
		"create database %s owner %s;",
		name, user,
	))
	if execErr != nil {
		err = fmt.Errorf("failed to create database '%s': %w", name, execErr)
		return
	}

	// Create and populate the object:
	result = &Instance{
		server:   s,
		name:     name,
		user:     user,
		password: password,
	}

	// Remember to remove it:
	s.instances = append(s.instances, result)

	return
}

// Url returns the connection URL for this database instance.
func (i *Instance) Url() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		i.user, i.password, i.server.host, i.server.port, i.name,
	)
}

// Handle creates and returns a new database handle for this instance. The caller is responsible for closing the handle
// when it is no longer needed.
func (i *Instance) Handle() (result *sql.DB, err error) {
	url := i.Url()
	handle, err := sql.Open("pgx", url)
	if err != nil {
		err = fmt.Errorf("failed to open database handle: %w", err)
		return
	}
	result = handle
	return
}

// Close deletes the database and the user that were created for this instance.
func (i *Instance) Close() error {
	var errs []error

	// Drop the database:
	_, err := i.server.handle.Exec(fmt.Sprintf(
		`drop database if exists %s with (force)`,
		i.name,
	))
	if err != nil {
		errs = append(errs, fmt.Errorf("failed to drop database '%s': %w", i.name, err))
	}

	// Drop the user:
	_, err = i.server.handle.Exec(fmt.Sprintf(
		`drop user if exists %s`,
		i.user,
	))
	if err != nil {
		errs = append(errs, fmt.Errorf("failed to drop user '%s': %w", i.user, err))
	}

	return errors.Join(errs...)
}

// containerTools is the list of container tools that we will try to use to start the database server container, in
// order of preference.
var containerTools = []string{
	"podman",
	"docker",
}
