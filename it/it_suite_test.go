/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package it

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"
	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/ginkgo/v2/dsl/decorators"
	. "github.com/onsi/gomega"
	"github.com/osac-project/fulfillment-common/logging"
	"k8s.io/klog/v2"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"
)

// Config contains configuration options for the integration tests.
type Config struct {
	// KeepKind indicates whether to preserve the kind cluster after tests complete.
	// By default, the kind cluster is deleted after running the tests.
	KeepKind bool `json:"keep_kind" envconfig:"keep_kind" default:"false"`

	// KeepService indicates whether to preserve the application chart after tests complete.
	// By default, the application chart is uninstalled after running the tests.
	KeepService bool `json:"keep_service" envconfig:"keep_service" default:"false"`

	// DeployMode indicates how to deploy the service. Valid values are 'helm' and 'kustomize'.
	// By default, the service is deployed using Helm.
	DeployMode string `json:"deploy_mode" envconfig:"deploy_mode" default:"helm"`
}

var (
	logger *slog.Logger
	config *Config
	tool   *Tool
)

func TestIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Integration")
}

var _ = BeforeSuite(func() {
	var err error

	// Create a context:
	ctx := context.Background()

	// Create the logger:
	logger, err = logging.NewLogger().
		SetWriter(GinkgoWriter).
		SetLevel(slog.LevelDebug.String()).
		Build()
	Expect(err).ToNot(HaveOccurred())

	// Configure the Kubernetes libraries to use our logger:
	logrLogger := logr.FromSlogHandler(logger.Handler())
	crlog.SetLogger(logrLogger)
	klog.SetLogger(logrLogger)

	// Load configuration from environment variables:
	config = &Config{}
	err = envconfig.Process("it", config)
	Expect(err).ToNot(HaveOccurred())
	logger.Info(
		"Configuration",
		slog.Any("values", config),
	)

	// Create and setup the tool:
	tool, err = NewTool().
		SetLogger(logger).
		SetKeepCluster(config.KeepKind).
		SetKeepService(config.KeepService).
		SetDeployMode(config.DeployMode).
		AddCrdFile(filepath.Join("crds", "clusterorders.osac.openshift.io.yaml")).
		AddCrdFile(filepath.Join("crds", "hostedclusters.hypershift.openshift.io.yaml")).
		Build()
	Expect(err).ToNot(HaveOccurred())
	err = tool.Setup(ctx)
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(func() {
		err := tool.Cleanup(ctx)
		Expect(err).ToNot(HaveOccurred())
	})
	DeferCleanup(func() {
		err := tool.Dump(ctx)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("Integration", func() {
	It("Setup", Label("setup"), func() {
		// This is a dummy test to have a mechanism to run the setup of the integration tests without running
		// any actual tests, with a command like this:
		//
		// ginkgo run --label-filter setup it
		//
		// This will create the kind cluster, install the dependencies, and deploy the application.
	})
})
