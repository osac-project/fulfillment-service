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
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"time"

	"github.com/onsi/gomega/ghttp"
	"github.com/osac-project/fulfillment-common/auth"
	"github.com/osac-project/fulfillment-common/network"
	"github.com/osac-project/fulfillment-common/oauth"
	"github.com/osac-project/fulfillment-common/testing"
	"go.yaml.in/yaml/v2"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	grpcstatus "google.golang.org/grpc/status"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/private/v1"
	"github.com/osac-project/fulfillment-service/internal/version"
)

// Deplyment modes:
const (
	deployModeHelm      = "helm"
	deployModeKustomize = "kustomize"
)

var ServiceAccountTenants = map[string]string{
	"alice": "a",
	"bob":   "a",
	"carol": "b",
	"dave":  "b",
}

var OIDCTenants = map[string][]string{
	"adam":    {"engineering"},
	"ben":     {"engineering", "sales"},
	"charles": {"sales"},
}

// ToolBuilder contains the data and logic needed to create an instance of the integration test tool. Don't create
// instances of this directly, use the NewTool function instead.
type ToolBuilder struct {
	logger      *slog.Logger
	projectDir  string
	crdFiles    []string
	keepCluster bool
	keepService bool
	deployMode  string
}

// Tool is an instance of the integration test tool that sets up the test environment. Don't create instances of this
// directly, use the NewTool function instead.
type Tool struct {
	logger          *slog.Logger
	projectDir      string
	crdFiles        []string
	keepKind        bool
	keepService     bool
	deploymentMode  string
	tmpDir          string
	cluster         *testing.Kind
	kubeClient      crclient.Client
	kubeClientSet   *kubernetes.Clientset
	caPool          *x509.CertPool
	kcFile          string
	emergencyConn   *grpc.ClientConn
	adminConn       *grpc.ClientConn
	userConn        *grpc.ClientConn
	emergencyClient *http.Client
	adminClient     *http.Client
	userClient      *http.Client
}

// NewTool creates a builder that can then be used to configure and create an instance of the integration test tool.
func NewTool() *ToolBuilder {
	return &ToolBuilder{}
}

// SetLogger sets the logger that the tool will use to write messages to the log. This is mandatory.
func (b *ToolBuilder) SetLogger(value *slog.Logger) *ToolBuilder {
	b.logger = value
	return b
}

// SetProjectDir sets the root directory of the project. This is optional, if not specified, the tool will search for
// the 'go.mod' file starting from the current directory.
func (b *ToolBuilder) SetProjectDir(value string) *ToolBuilder {
	b.projectDir = value
	return b
}

// AddCrdFile adds a CRD file to be installed in the cluster.
func (b *ToolBuilder) AddCrdFile(value string) *ToolBuilder {
	b.crdFiles = append(b.crdFiles, value)
	return b
}

// AddCrdFiles adds multiple CRD files to be installed in the cluster.
func (b *ToolBuilder) AddCrdFiles(values ...string) *ToolBuilder {
	b.crdFiles = append(b.crdFiles, values...)
	return b
}

// SetKeepCluster sets whether to keep the cluster after the tests complete. The default is to destroy the cluster.
func (b *ToolBuilder) SetKeepCluster(value bool) *ToolBuilder {
	b.keepCluster = value
	return b
}

// SetKeepService sets whether to keep the service after the tests complete. The default is to undeploy the service.
func (b *ToolBuilder) SetKeepService(value bool) *ToolBuilder {
	b.keepService = value
	return b
}

// SetDeployMode sets how the service should be deployed. Valid values are 'heml' and 'kustomize'. The default is 'helm'.
func (b *ToolBuilder) SetDeployMode(value string) *ToolBuilder {
	b.deployMode = value
	return b
}

// Build uses the data stored in the builder to create a new instance of the integration test tool.
func (b *ToolBuilder) Build() (result *Tool, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.deployMode != deployModeHelm && b.deployMode != deployModeKustomize {
		err = fmt.Errorf(
			"invalid deploy mode '%s'i must be '%s' or '%s'",
			b.deployMode, deployModeHelm, deployModeKustomize,
		)
		return
	}

	// Find the project directory if not specified:
	projectDir := b.projectDir
	if projectDir == "" {
		projectDir, err = b.findProjectDir()
		if err != nil {
			return
		}
	}

	// Create and populate the object:
	result = &Tool{
		logger:         b.logger,
		projectDir:     projectDir,
		crdFiles:       slices.Clone(b.crdFiles),
		keepKind:       b.keepCluster,
		keepService:    b.keepService,
		deploymentMode: b.deployMode,
	}
	return
}

// findProjectDir finds the project directory by searching for the go.mod file starting from the current directory.
func (b *ToolBuilder) findProjectDir() (result string, err error) {
	currentDir, err := os.Getwd()
	if err != nil {
		err = fmt.Errorf("failed to get current directory: %w", err)
		return
	}
	for {
		modFile := filepath.Join(currentDir, "go.mod")
		_, statErr := os.Stat(modFile)
		if statErr == nil {
			result = currentDir
			return
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			err = fmt.Errorf("failed to stat '%s': %w", modFile, statErr)
			return
		}
		parentDir := filepath.Dir(currentDir)
		if parentDir == currentDir {
			err = fmt.Errorf("failed to find 'go.mod' file starting from '%s'", currentDir)
			return
		}
		currentDir = parentDir
	}
}

// Setup prepares the integration test environment. This includes building the binary and container image, starting
// the Kind cluster, installing Keycloak and the service, and creating the necessary clients.
func (t *Tool) Setup(ctx context.Context) error {
	var err error

	// Check that the required host names are resolvable:
	err = t.checkAddress(ctx, keycloakAddr)
	if err != nil {
		return err
	}
	err = t.checkAddress(ctx, serviceAddr)
	if err != nil {
		return err
	}

	// Create a temporary directory:
	t.tmpDir, err = os.MkdirTemp("", "*.it")
	if err != nil {
		return fmt.Errorf("failed to create temporary directory: %w", err)
	}

	// Check that the required command line tools are available:
	err = t.checkCommands(ctx)
	if err != nil {
		return err
	}

	// Build the binary:
	err = t.buildBinary(ctx)
	if err != nil {
		return err
	}

	// Build the container image:
	imageRef, err := t.buildImage(ctx)
	if err != nil {
		return err
	}

	// Save the container image to a tar file:
	imageTar, err := t.saveImage(ctx, imageRef)
	if err != nil {
		return err
	}

	// Start the cluster:
	if err = t.startCluster(ctx); err != nil {
		return err
	}

	// Load the container image into the cluster:
	err = t.cluster.LoadArchive(ctx, imageTar)
	if err != nil {
		return fmt.Errorf("failed to load container image into cluster: %w", err)
	}

	// Write the kubeconfig file:
	t.kcFile = filepath.Join(t.tmpDir, "kubeconfig")
	err = os.WriteFile(t.kcFile, t.cluster.Kubeconfig(), 0400)
	if err != nil {
		return fmt.Errorf("failed to write kubeconfig file: %w", err)
	}

	// Get the clients:
	t.kubeClient = t.cluster.Client()
	t.kubeClientSet = t.cluster.ClientSet()

	// Load the CA bundle:
	err = t.loadCaBundle(ctx)
	if err != nil {
		return err
	}

	// Install Keycloak:
	err = t.deployKeycloak(ctx)
	if err != nil {
		return err
	}

	// Install the service:
	err = t.deployService(ctx, imageRef)
	if err != nil {
		return err
	}

	// Create the gRPC and HTTP clients:
	err = t.createClients(ctx)
	if err != nil {
		return err
	}

	// Wait for the service to be healthy:
	err = t.waitForHealth(ctx)
	if err != nil {
		return err
	}

	// Create the hub namespace:
	err = t.createHubNamespace(ctx)
	if err != nil {
		return err
	}

	// Create the test user service accounts:
	err = t.createUserServiceAccounts(ctx)
	if err != nil {
		return err
	}

	// Register the hub:
	if err = t.registerHub(ctx); err != nil {
		return err
	}

	return nil
}

// checkAddress checks that the given address is resolvable.
func (t *Tool) checkAddress(ctx context.Context, addr string) error {
	t.logger.DebugContext(ctx, "Checking address", "address", addr)
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("failed to split host and port from '%s': %w", addr, err)
	}
	_, err = net.LookupHost(host)
	if err != nil {
		return fmt.Errorf(
			"failed to lookup host '%s', you may need to add a '127.0.0.1 %[1]s' entry to the "+
				"'/etc/hosts' file: %w",
			addr, err,
		)
	}
	return nil
}

// checkCommands checks that the required command line tools are available.
func (t *Tool) checkCommands(ctx context.Context) error {
	t.logger.DebugContext(ctx, "Checking command line tools")
	commands := []string{
		kubectlCmd,
		podmanCmd,
		helmCmd,
		kustomizeCmd,
	}
	for _, command := range commands {
		_, err := exec.LookPath(command)
		if err != nil {
			return fmt.Errorf("command '%s' is not available: %w", command, err)
		}
	}
	return nil
}

func (t *Tool) buildBinary(ctx context.Context) error {
	t.logger.DebugContext(ctx, "Building binary")

	// Get the version from git:
	versionCmd, err := testing.NewCommand().
		SetLogger(t.logger).
		SetHome(t.projectDir).
		SetDir(t.projectDir).
		SetName("git").
		SetArgs(
			"describe",
			"--tags",
			"--always",
		).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create command to get version from git: %w", err)
	}
	versionBytes, _, err := versionCmd.Evaluate(ctx)
	if err != nil {
		return fmt.Errorf("failed to get version from git: %w", err)
	}
	version := string(versionBytes)

	// Build the binary:
	buildCmd, err := testing.NewCommand().
		SetLogger(t.logger).
		SetHome(t.projectDir).
		SetDir(t.projectDir).
		SetName("go").
		SetArgs(
			"build",
			"-ldflags",
			fmt.Sprintf("-X github.com/osac-project/fulfillment-service/internal.id=%s", version),
		).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create command to build binary: %w", err)
	}
	err = buildCmd.Execute(ctx)
	if err != nil {
		return fmt.Errorf("failed to build binary: %w", err)
	}

	return nil
}

// buildImage builds the container image and returns the full image reference.
func (t *Tool) buildImage(ctx context.Context) (result string, err error) {
	t.logger.DebugContext(ctx, "Building image")
	imageTag := time.Now().Format("20060102150405")
	imageRef := fmt.Sprintf("%s:%s", imageName, imageTag)
	buildCmd, err := testing.NewCommand().
		SetLogger(t.logger).
		SetHome(t.projectDir).
		SetDir(t.projectDir).
		SetName(podmanCmd).
		SetArgs(
			"build",
			"--tag", imageRef,
			"--file", filepath.Join("it", "Containerfile"),
			".",
		).
		Build()
	if err != nil {
		err = fmt.Errorf("failed to create command to build image: %w", err)
		return
	}
	err = buildCmd.Execute(ctx)
	if err != nil {
		err = fmt.Errorf("failed to build image: %w", err)
		return
	}
	result = imageRef
	return
}

// saveImage saves the given container image to a tar file and returns the path to that tar file.
func (t *Tool) saveImage(ctx context.Context, imageRef string) (result string, err error) {
	t.logger.DebugContext(ctx, "Saving container image to tar file")
	imageTar := filepath.Join(t.tmpDir, "image.tar")
	saveCmd, err := testing.NewCommand().
		SetLogger(t.logger).
		SetHome(t.projectDir).
		SetDir(t.projectDir).
		SetName(podmanCmd).
		SetArgs(
			"save",
			"--output", imageTar,
			imageRef,
		).
		Build()
	if err != nil {
		err = fmt.Errorf("failed to create command to save image: %w", err)
		return
	}
	err = saveCmd.Execute(ctx)
	if err != nil {
		err = fmt.Errorf("failed to save container image: %w", err)
		return
	}
	result = imageTar
	return
}

func (t *Tool) startCluster(ctx context.Context) error {
	t.logger.DebugContext(ctx, "Starting cluster")
	builder := testing.NewKind()
	builder.SetLogger(t.logger)
	builder.SetHome(t.projectDir)
	builder.SetName("fulfillment-service-it")
	for _, crdFile := range t.crdFiles {
		builder.AddCrdFile(crdFile)
	}
	var err error
	t.cluster, err = builder.Build()
	if err != nil {
		return fmt.Errorf("failed to create cluster: %w", err)
	}
	err = t.cluster.Start(ctx)
	if err != nil {
		return fmt.Errorf("failed to start cluster: %w", err)
	}
	return nil
}

func (t *Tool) loadCaBundle(ctx context.Context) error {
	t.logger.DebugContext(ctx, "Loading CA bundle")

	// Wait for the CA bundle to be available:
	caBundleKey := crclient.ObjectKey{
		Namespace: "default",
		Name:      "ca-bundle",
	}
	caBundleMap := &corev1.ConfigMap{}
	var err error
	for i := 0; i < 60; i++ {
		err = t.kubeClient.Get(ctx, caBundleKey, caBundleMap)
		if err == nil {
			break
		}
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to get CA bundle: %w", err)
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		return fmt.Errorf("CA bundle not available after waiting: %w", err)
	}

	// Write CA files:
	var caFiles []string
	for caKey, caText := range caBundleMap.Data {
		caFile := filepath.Join(t.tmpDir, caKey)
		err = os.WriteFile(caFile, []byte(caText), 0400)
		if err != nil {
			return fmt.Errorf("failed to write CA file: %w", err)
		}
		caFiles = append(caFiles, caFile)
	}

	// Create the CA pool:
	t.caPool, err = network.NewCertPool().
		SetLogger(t.logger).
		AddFiles(caFiles...).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create CA pool: %w", err)
	}
	return nil
}

// deployKeycloak installs the Keycloak chart.
func (t *Tool) deployKeycloak(ctx context.Context) error {
	// Get the host name:
	t.logger.DebugContext(ctx, "Installing Keycloak chart")
	host, _, err := net.SplitHostPort(keycloakAddr)
	if err != nil {
		return fmt.Errorf("failed to split host and port from '%s': %w", keycloakAddr, err)
	}

	// Prepare a map containing the values for the chart:
	valuesData := map[string]any{
		"variant":  "kind",
		"hostname": host,
		"certs": map[string]any{
			"issuerRef": map[string]any{
				"kind": "ClusterIssuer",
				"name": "default-ca",
			},
		},
		"groups": []any{
			map[string]any{
				"name": adminsGroup,
				"path": fmt.Sprintf("/%s", adminsGroup),
			},
			map[string]any{
				"name": usersGroup,
				"path": fmt.Sprintf("/%s", usersGroup),
			},
		},
		"users": []any{
			map[string]any{
				"username":      adminUsername,
				"enabled":       true,
				"firstName":     "Ms.",
				"lastName":      "Admin",
				"email":         fmt.Sprintf("%s@example.com", adminUsername),
				"emailVerified": true,
				"groups": []string{
					fmt.Sprintf("/%s", adminsGroup),
				},
				"credentials": []any{
					map[string]any{
						"type":      "password",
						"value":     adminsPassword,
						"temporary": false,
					},
				},
			},
			map[string]any{
				"username":      userUsername,
				"enabled":       true,
				"firstName":     "Mr.",
				"lastName":      "User",
				"email":         fmt.Sprintf("%s@example.com", userUsername),
				"emailVerified": true,
				"groups": []string{
					fmt.Sprintf("/%s", usersGroup),
				},
				"credentials": []any{
					map[string]any{
						"type":      "password",
						"value":     usersPassword,
						"temporary": false,
					},
				},
			},
		},
	}

	// Add the OIDC tenants
	groupsAdded := []string{}
	for oidcUser, oidcGroups := range OIDCTenants {
		newUser := map[string]any{
			"username":      oidcUser,
			"enabled":       true,
			"firstName":     oidcUser,
			"lastName":      oidcUser,
			"email":         fmt.Sprintf("%s@example.com", oidcUser),
			"emailVerified": true,
			"credentials": []any{
				map[string]any{
					"type":      "password",
					"value":     usersPassword,
					"temporary": false,
				},
			},
			"groups": []string{},
		}

		for _, oidcGroup := range oidcGroups {
			newUser["groups"] = append(newUser["groups"].([]string), fmt.Sprintf("/%s", oidcGroup))

			if !slices.Contains(groupsAdded, oidcGroup) {
				valuesData["groups"] = append(valuesData["groups"].([]any), map[string]any{
					"name": oidcGroup,
					"path": fmt.Sprintf("/%s", oidcGroup),
				})
				groupsAdded = append(groupsAdded, oidcGroup)
			}
		}

		valuesData["users"] = append(valuesData["users"].([]any), newUser)
	}

	valuesBytes, err := yaml.Marshal(valuesData)
	if err != nil {
		return fmt.Errorf("failed to marshal values to YAML: %w", err)
	}

	// Write the values to a temporary file:
	valuesFile := filepath.Join(t.tmpDir, "keycloak-values.yaml")
	err = os.WriteFile(valuesFile, valuesBytes, 0400)
	if err != nil {
		return fmt.Errorf("failed to write values to file: %w", err)
	}

	// Install the chart:
	installCmd, err := testing.NewCommand().
		SetLogger(t.logger).
		SetHome(t.projectDir).
		SetDir(t.projectDir).
		SetName(helmCmd).
		SetArgs(
			"upgrade",
			"--install",
			"keycloak",
			"charts/keycloak",
			"--kubeconfig", t.kcFile,
			"--namespace", "keycloak",
			"--create-namespace",
			"--values", valuesFile,
			"--wait",
		).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create Keycloak install command: %w", err)
	}
	if err = installCmd.Execute(ctx); err != nil {
		return fmt.Errorf("failed to install Keycloak: %w", err)
	}
	return nil
}

func (t *Tool) deployService(ctx context.Context, imageRef string) error {
	switch t.deploymentMode {
	case deployModeHelm:
		return t.deployServiceWithHelm(ctx, imageRef)
	case deployModeKustomize:
		return t.deployServiceWithKustomize(ctx, imageRef)
	default:
		return fmt.Errorf("unknown deploy mode '%s'", t.deploymentMode)
	}
}

func (t *Tool) deployServiceWithHelm(ctx context.Context, imageRef string) error {
	// Prepare the values:
	valuesData := map[string]any{
		"variant": "kind",
		"log": map[string]any{
			"level":   "debug",
			"headers": true,
			"bodies":  true,
		},
		"images": map[string]any{
			"service": imageRef,
		},
		"certs": map[string]any{
			"issuerRef": map[string]any{
				"kind": "ClusterIssuer",
				"name": "default-ca",
			},
			"caBundle": map[string]any{
				"configMap": "ca-bundle",
			},
		},
		"auth": map[string]any{
			"issuerUrl": fmt.Sprintf("https://%s/realms/osac", keycloakAddr),
		},
	}
	valuesBytes, err := yaml.Marshal(valuesData)
	if err != nil {
		return fmt.Errorf("failed to marshal values to YAML: %w", err)
	}
	valuesFile := filepath.Join(t.tmpDir, "service-values.yaml")
	err = os.WriteFile(valuesFile, valuesBytes, 0400)
	if err != nil {
		return fmt.Errorf("failed to write values to file: %w", err)
	}
	t.logger.DebugContext(
		ctx,
		"Service chart values",
		slog.Any("values", valuesData),
	)

	// Deploy the service:
	t.logger.DebugContext(ctx, "Deploying service with Helm")
	installCmd, err := testing.NewCommand().
		SetLogger(t.logger).
		SetHome(t.projectDir).
		SetDir(t.projectDir).
		SetName(helmCmd).
		SetArgs(
			"upgrade",
			"--install",
			"fulfillment-service",
			"charts/service",
			"--kubeconfig", t.kcFile,
			"--namespace", "osac",
			"--create-namespace",
			"--values", valuesFile,
			"--wait",
		).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create service install command: %w", err)
	}
	if err = installCmd.Execute(ctx); err != nil {
		return fmt.Errorf("failed to install service: %w", err)
	}
	return nil
}

func (t *Tool) deployServiceWithKustomize(ctx context.Context, imageRef string) error {
	t.logger.DebugContext(ctx, "Deploying service with Kustomize")

	// Copy manifests to temporary directory:
	srcDir := filepath.Join(t.projectDir, "manifests")
	tmpDir := filepath.Join(t.tmpDir, "manifests")
	err := t.copyDir(srcDir, tmpDir)
	if err != nil {
		return fmt.Errorf("failed to copy manifests to temporary directory: %w", err)
	}

	// Update the image reference using kustomize edit:
	baseDir := filepath.Join(tmpDir, "base")
	editCmd, err := testing.NewCommand().
		SetLogger(t.logger).
		SetHome(t.projectDir).
		SetDir(baseDir).
		SetName(kustomizeCmd).
		SetArgs(
			"edit",
			"set",
			"image",
			fmt.Sprintf("fulfillment-service=%s", imageRef),
		).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create kustomize edit command: %w", err)
	}
	err = editCmd.Execute(ctx)
	if err != nil {
		return fmt.Errorf("failed to set image with kustomize edit: %w", err)
	}

	// Apply the manifests:
	overlayDir := filepath.Join(tmpDir, "overlays", "kind")
	applyCmd, err := testing.NewCommand().
		SetLogger(t.logger).
		SetHome(t.projectDir).
		SetDir(t.projectDir).
		SetName(kubectlCmd).
		SetArgs(
			"apply",
			"--kubeconfig", t.kcFile,
			"--kustomize", overlayDir,
		).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create kubectl apply command: %w", err)
	}
	err = applyCmd.Execute(ctx)
	if err != nil {
		return fmt.Errorf("failed to apply manifests: %w", err)
	}

	// Wait for all deployments to be ready:
	waitCmd, err := testing.NewCommand().
		SetLogger(t.logger).
		SetHome(t.projectDir).
		SetDir(t.projectDir).
		SetName(kubectlCmd).
		SetArgs(
			"wait",
			"--kubeconfig", t.kcFile,
			"--namespace", "osac",
			"--for=condition=available",
			"--timeout=5m",
			"deployment", "--all",
		).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create kubectl wait command: %w", err)
	}
	err = waitCmd.Execute(ctx)
	if err != nil {
		return fmt.Errorf("failed to wait for deployments: %w", err)
	}

	return nil
}

func (t *Tool) copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, relPath)
		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dstPath, data, info.Mode())
	})
}

func (t *Tool) undeployService(ctx context.Context) error {
	switch t.deploymentMode {
	case deployModeHelm:
		return t.undeployServiceWithHelm(ctx)
	case deployModeKustomize:
		return t.undeployServiceWithKustomize(ctx)
	default:
		return fmt.Errorf("unknown deploy mode '%s'", t.deploymentMode)
	}
}

func (t *Tool) undeployServiceWithHelm(ctx context.Context) error {
	t.logger.DebugContext(ctx, "Undeploying service with Helm")
	uninstallCmd, err := testing.NewCommand().
		SetLogger(t.logger).
		SetHome(t.projectDir).
		SetDir(t.projectDir).
		SetName(helmCmd).
		SetArgs(
			"uninstall",
			"fulfillment-service",
			"--kubeconfig", t.kcFile,
			"--namespace", "osac",
		).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create service uninstall command: %w", err)
	}
	if err = uninstallCmd.Execute(ctx); err != nil {
		return fmt.Errorf("failed to uninstall service: %w", err)
	}
	return nil
}

func (t *Tool) undeployServiceWithKustomize(ctx context.Context) error {
	t.logger.DebugContext(ctx, "Undeploying service with Kustomize")
	err := t.cluster.Client().Delete(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "osac",
		},
	})
	if err != nil {
		return fmt.Errorf("failed to delete namespace: %w", err)
	}
	return nil
}

func (t *Tool) createClients(ctx context.Context) error {
	// Create token sources:
	emergencyTokenSource, err := t.makeKubernetesTokenSource(ctx, emergencyServiceAccount, "osac")
	if err != nil {
		return err
	}
	adminTokenSource, err := t.makeKeycloakTokenSource(ctx, adminUsername, adminsPassword)
	if err != nil {
		return err
	}
	userTokenSource, err := t.makeKeycloakTokenSource(ctx, userUsername, usersPassword)
	if err != nil {
		return err
	}

	// Create gRPC clients:
	t.emergencyConn, err = t.makeGrpcConn(emergencyTokenSource)
	if err != nil {
		return err
	}
	t.adminConn, err = t.makeGrpcConn(adminTokenSource)
	if err != nil {
		return err
	}
	t.userConn, err = t.makeGrpcConn(userTokenSource)
	if err != nil {
		return err
	}

	// Create HTTP clients:
	t.emergencyClient = t.makeHttpClient(emergencyTokenSource)
	t.adminClient = t.makeHttpClient(adminTokenSource)
	t.userClient = t.makeHttpClient(userTokenSource)

	return nil
}

func (t *Tool) createUserServiceAccounts(ctx context.Context) error {
	var tenantNamespaces []string
	for user, group := range ServiceAccountTenants {
		if !slices.Contains(tenantNamespaces, group) {
			err := t.kubeClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: group,
				},
			})

			if err != nil && !apierrors.IsAlreadyExists(err) {
				return fmt.Errorf("failed to create namespace '%s': %w", group, err)
			}

			tenantNamespaces = append(tenantNamespaces, group)
		}
		err := t.kubeClient.Create(ctx, &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      user,
				Namespace: group,
			},
		})
		if err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create service account '%s': %w", user, err)
		}
	}
	return nil
}

func (t *Tool) makeKubernetesTokenSource(ctx context.Context, sa, namespace string) (result auth.TokenSource, err error) {
	response, err := t.kubeClientSet.CoreV1().ServiceAccounts(namespace).CreateToken(
		ctx,
		sa,
		&authenticationv1.TokenRequest{
			Spec: authenticationv1.TokenRequestSpec{
				ExpirationSeconds: ptr.To(int64(3600)),
			},
		},
		metav1.CreateOptions{},
	)
	if err != nil {
		err = fmt.Errorf("failed to create token for service account '%s': %w", sa, err)
		return
	}
	token := &auth.Token{
		Access: response.Status.Token,
	}
	result, err = auth.NewStaticTokenSource().
		SetLogger(t.logger).
		SetToken(token).
		Build()
	return
}

func (t *Tool) makeKeycloakTokenSource(ctx context.Context, username, password string) (result auth.TokenSource, err error) {
	store, err := auth.NewMemoryTokenStore().
		SetLogger(t.logger).
		Build()
	if err != nil {
		return
	}
	result, err = oauth.NewTokenSource().
		SetLogger(t.logger).
		SetStore(store).
		SetCaPool(t.caPool).
		SetIssuer(fmt.Sprintf("https://%s/realms/osac", keycloakAddr)).
		SetFlow(oauth.PasswordFlow).
		SetClientId("fulfillment-cli").
		SetUsername(username).
		SetPassword(password).
		SetScopes("openid").
		Build()
	return
}

func (t *Tool) makeGrpcConn(tokenSource auth.TokenSource) (result *grpc.ClientConn, err error) {
	userAgent := fmt.Sprintf("%s/%s", userAgent, version.Get())
	result, err = network.NewGrpcClient().
		SetLogger(t.logger).
		SetCaPool(t.caPool).
		SetAddress(serviceAddr).
		SetTokenSource(tokenSource).
		SetUserAgent(userAgent).
		Build()
	return
}

func (t *Tool) makeHttpClient(tokenSource auth.TokenSource) *http.Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs: t.caPool,
		},
	}
	return &http.Client{
		Transport: ghttp.RoundTripperFunc(
			func(request *http.Request) (response *http.Response, err error) {
				token, err := tokenSource.Token(request.Context())
				if err != nil {
					return nil, err
				}
				request.Header.Set(
					"Authorization",
					fmt.Sprintf("Bearer %s", token.Access),
				)
				response, err = transport.RoundTrip(request)
				return
			},
		),
	}
}

func (t *Tool) waitForHealth(ctx context.Context) error {
	t.logger.DebugContext(ctx, "Waiting for service to be healthy")
	healthClient := healthv1.NewHealthClient(t.adminConn)
	healthRequest := &healthv1.HealthCheckRequest{}
	var lastErr error
	for i := 0; i < 12; i++ {
		healthResponse, err := healthClient.Check(ctx, healthRequest)
		if err == nil && healthResponse.Status == healthv1.HealthCheckResponse_SERVING {
			return nil
		}
		lastErr = err
		time.Sleep(5 * time.Second)
	}
	if lastErr != nil {
		return fmt.Errorf("service not healthy after waiting: %w", lastErr)
	}
	return fmt.Errorf("service not healthy after waiting")
}

func (t *Tool) createHubNamespace(ctx context.Context) error {
	t.logger.DebugContext(ctx, "Creating hub namespace")
	hubNamespaceObject := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: hubNamespace,
		},
	}
	err := t.kubeClient.Create(ctx, hubNamespaceObject)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create hub namespace: %w", err)
	}
	return nil
}

func (t *Tool) registerHub(ctx context.Context) error {
	t.logger.DebugContext(ctx, "Registering hub")

	// Prepare the kubeconfig for the hub:
	hubKcBytes := t.cluster.Kubeconfig()
	hubKcObject, err := clientcmd.Load(hubKcBytes)
	if err != nil {
		return fmt.Errorf("failed to load hub kubeconfig: %w", err)
	}
	for clusterKey := range hubKcObject.Clusters {
		hubKcObject.Clusters[clusterKey].Server = "https://kubernetes.default.svc"
	}
	hubKcBytes, err = clientcmd.Write(*hubKcObject)
	if err != nil {
		return fmt.Errorf("failed to write hub Kc: %w", err)
	}

	// Create the hubs client:
	hubsClient := privatev1.NewHubsClient(t.adminConn)

	// Wait for Authorino authorization to be ready:
	for range 30 {
		_, err = hubsClient.List(ctx, privatev1.HubsListRequest_builder{}.Build())
		if err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		return fmt.Errorf("authorization not ready after waiting: %w", err)
	}

	// Create the hub:
	_, err = hubsClient.Create(ctx, privatev1.HubsCreateRequest_builder{
		Object: privatev1.Hub_builder{
			Id:         hubId,
			Kubeconfig: hubKcBytes,
			Namespace:  hubNamespace,
		}.Build(),
	}.Build())
	if err != nil {
		status, ok := grpcstatus.FromError(err)
		if ok && status.Code() == grpccodes.AlreadyExists {
			return nil
		}
		return fmt.Errorf("failed to create hub: %w", err)
	}
	return nil
}

func (t *Tool) Cleanup(ctx context.Context) error {
	var errs []error

	// Close gRPC connections:
	if t.emergencyConn != nil {
		err := t.emergencyConn.Close()
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to close emergency adminstrator connection: %w", err))
		}
	}
	if t.adminConn != nil {
		err := t.adminConn.Close()
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to close administrator connection: %w", err))
		}
	}
	if t.userConn != nil {
		err := t.userConn.Close()
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to close regular connection: %w", err))
		}
	}

	// Undeploy the service:
	if t.cluster != nil && t.keepKind && !t.keepService {
		err := t.undeployService(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to undeploy service: %w", err))
		}
	}

	// Stop the cluster:
	if t.cluster != nil && !t.keepKind {
		err := t.cluster.Stop(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to stop cluster: %w", err))
		}
	}

	// Remove temporary directory:
	if t.tmpDir != "" {
		err := os.RemoveAll(t.tmpDir)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to remove temporary directory: %w", err))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (t *Tool) Dump(ctx context.Context) error {
	if t.cluster == nil {
		return nil
	}
	logsDir := filepath.Join(t.projectDir, "logs")
	return t.cluster.Dump(ctx, logsDir)
}

// Cluster returns the Kind cluster.
func (t *Tool) Cluster() *testing.Kind {
	return t.cluster
}

// KubeClient returns the Kubernetes client.
func (t *Tool) KubeClient() crclient.Client {
	return t.kubeClient
}

// KubeClientSet returns the Kubernetes clientset.
func (t *Tool) KubeClientSet() *kubernetes.Clientset {
	return t.kubeClientSet
}

// EmergencyConn returns the gRPC client connection for the emergency administration service account.
func (t *Tool) EmergencyConn() *grpc.ClientConn {
	return t.emergencyConn
}

// AdminConn returns the gRPC client connection for admnistration user.
func (t *Tool) AdminConn() *grpc.ClientConn {
	return t.adminConn
}

// UserConn returns the gRPC client connection for the regular user.
func (t *Tool) UserConn() *grpc.ClientConn {
	return t.userConn
}

// EmergencyClient returns the HTTP client for the emergency administration service account.
func (t *Tool) EmergencyClient() *http.Client {
	return t.emergencyClient
}

// AdminClient returns the HTTP client for the administrator user.
func (t *Tool) AdminClient() *http.Client {
	return t.adminClient
}

// UserClient returns the HTTP client for the regular user.
func (t *Tool) UserClient() *http.Client {
	return t.userClient
}

// ProjectDir returns the project directory.
func (t *Tool) ProjectDir() string {
	return t.projectDir
}

// Names of the command line tools:
const (
	helmCmd      = "helm"
	kubectlCmd   = "kubectl"
	kustomizeCmd = "kustomize"
	podmanCmd    = "podman"
)

// Name and namespace of the hub:
const hubId = "local"
const hubNamespace = "osac-operator-system"

// Image details:
const imageName = "ghcr.io/osac/fulfillment-service"

// userAgent is the user agent string for the integration test tool.
const userAgent = "fulfillment-it-tool"

// Service host name and address:
const (
	keycloakAddr = "keycloak.keycloak.svc.cluster.local:8000"
	serviceAddr  = "fulfillment-api.osac.svc.cluster.local:8000"
)

// Name of the Kubernetes service account that is used for emergency administration access.
const emergencyServiceAccount = "admin"

// Details of the Keycloak administrator user:
const (
	adminUsername  = "admin"
	adminsPassword = "password"
	adminsGroup    = "admins"
)

// Details of the Keycloak regular user:
const (
	userUsername  = "user"
	usersPassword = "password"
	usersGroup    = "users"
)
