/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package computeinstance

import (
	"context"
	"io"
	"log/slog"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"

	"github.com/osac-project/fulfillment-service/internal/config"
	"github.com/osac-project/fulfillment-service/internal/errormessages"
	"github.com/osac-project/fulfillment-service/internal/logging"
	"github.com/osac-project/fulfillment-service/internal/terminal"
	"github.com/osac-project/fulfillment-service/internal/testing"
)

// newTestConsole returns a logger and console that do not write to the process
// stdout/stderr (avoids Ginkgo progress noise when exercising the CLI).
func newTestConsole() (*slog.Logger, *terminal.Console) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	console, err := terminal.NewConsole().
		SetLogger(logger).
		SetStdout(io.Discard).
		SetStderr(io.Discard).
		Build()
	Expect(err).NotTo(HaveOccurred())
	return logger, console
}

var _ = Describe("create_compute_instance_cmd", func() {
	Context("subnet flag validation", func() {
		BeforeEach(func() {
			GinkgoT().Setenv("HOME", GinkgoT().TempDir())
			Expect(config.Save(&config.Config{
				Address:   "127.0.0.1:1",
				Plaintext: true,
			})).To(Succeed())
		})

		It("returns error when subnet flag is omitted", func() {
			logger, console := newTestConsole()

			ctx := terminal.ConsoleIntoContext(logging.LoggerIntoContext(context.Background(), logger), console)

			cmd := Cmd()
			cmd.SetContext(ctx)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"--template", "some-template"})
			err := cmd.Execute()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(errormessages.ComputeInstanceSpecSubnetRequired))
		})

		It("returns error when subnet flag is whitespace only", func() {
			logger, console := newTestConsole()

			ctx := terminal.ConsoleIntoContext(logging.LoggerIntoContext(context.Background(), logger), console)

			cmd := Cmd()
			cmd.SetContext(ctx)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{"--template", "some-template", "--subnet", " \t "})
			err := cmd.Execute()
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(errormessages.ComputeInstanceSpecSubnetRequired))
		})
	})

	Context("with mock API server", func() {
		BeforeEach(func() {
			GinkgoT().Setenv("HOME", GinkgoT().TempDir())

			scenario := &testing.ComputeInstanceScenario{
				Name:        "cli-test-scenario",
				Description: "CLI unit test scenario for compute instances",
				Templates: []*testing.TemplateData{
					{
						ID:          "tpl-test-001",
						Name:        "test-template",
						Title:       "Test Template",
						Description: "A test compute instance template",
					},
				},
				Instances: []*testing.InstanceData{
					{
						ID:                "ci-existing-001",
						Name:              "existing-instance",
						Template:          "tpl-test-001",
						Subnet:            "subnet-e2e-existing",
						State:             publicv1.ComputeInstanceState_COMPUTE_INSTANCE_STATE_RUNNING,
						InternalIPAddress: "192.168.1.100",
					},
				},
			}

			server := testing.NewServer()
			DeferCleanup(server.Stop)

			publicv1.RegisterComputeInstancesServer(server.Registrar(), testing.NewMockComputeInstancesServer(scenario))
			publicv1.RegisterComputeInstanceTemplatesServer(server.Registrar(), testing.NewMockComputeInstanceTemplatesServer(scenario))

			server.Start()

			Expect(config.Save(&config.Config{
				Address:   server.Address(),
				Plaintext: true,
			})).To(Succeed())
		})

		It("creates a compute instance when template and subnet are set", func() {
			logger, console := newTestConsole()

			ctx := terminal.ConsoleIntoContext(logging.LoggerIntoContext(context.Background(), logger), console)

			cmd := Cmd()
			cmd.SetContext(ctx)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs([]string{
				"--template", "tpl-test-001",
				"--subnet", "subnet-e2e-new",
				"-n", "cli-create-success",
			})
			err := cmd.Execute()
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
