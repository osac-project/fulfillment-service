/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package testing

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"embed"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

//go:embed examples/*.yaml
var examplesFS embed.FS

// KindBuilder contains the data and logic needed to create an object that helps manage a Kind cluster used for
// integration tests. Don't create instances of this type directly, use the NewKind function instead.
type KindBuilder struct {
	logger   *slog.Logger
	name     string
	home     string
	quiet    bool
	crdFiles []string
}

// Kind helps manage a Kind cluster used for integration tests. Don't create instances of this type directly, use the
// NewKind function instead.
type Kind struct {
	logger          *slog.Logger
	name            string
	home            string
	quiet           bool
	crdFiles        []string
	kubeconfigBytes []byte
	kubeconfigFile  string
	kubeClient      crclient.WithWatch
	kubeClientSet   *kubernetes.Clientset
}

// NewKind creates a builder that can then be used to configure and create a new Kind cluster used for integration
// tests.
func NewKind() *KindBuilder {
	return &KindBuilder{
		quiet: true,
	}
}

// SetLogger sets the logger. This is mandatory.
func (b *KindBuilder) SetLogger(value *slog.Logger) *KindBuilder {
	b.logger = value
	return b
}

// SetName sets the name of the Kind cluster. This is mandatory.
func (b *KindBuilder) SetName(name string) *KindBuilder {
	b.name = name
	return b
}

// AddCrdFile adds a file containing custom resource definition to be installed in the cluster.
func (b *KindBuilder) AddCrdFile(file string) *KindBuilder {
	b.crdFiles = append(b.crdFiles, file)
	return b
}

// SetHome sets the project home directory. This is optional, and it is used to shorten the directory in log
// messages when it is a subdirectory of the project home directory, replacing it with '~'. This is used to make
// log messages more readable.
func (b *KindBuilder) SetHome(value string) *KindBuilder {
	b.home = value
	return b
}

// SetQuiet sets whether the output of the command executed to manage the cluster should be quiet. When quiet is
// true (the default), the command output is buffered and only logged if the command fails. When quiet is false,
// each line of output is logged as it happens. This is useful to avoid flooding the logs with output that is not
// of interest.
func (b *KindBuilder) SetQuiet(value bool) *KindBuilder {
	b.quiet = value
	return b
}

// Build uses the configuration stored in the builder to create a new Kind cluster
func (b *KindBuilder) Build() (result *Kind, err error) {
	// Check parameters:
	if b.logger == nil {
		err = fmt.Errorf("logger is mandatory")
		return
	}
	if b.name == "" {
		err = fmt.Errorf("name is mandatory")
		return
	}

	// Add the name to the logger:
	logger := b.logger
	if !b.quiet {
		logger = logger.With(slog.String("name", b.name))
	}

	// Check that the command line tools that we need are available:
	_, err = exec.LookPath(helmCmd)
	if err != nil {
		err = fmt.Errorf("command line tool '%s' isn't available: %w", helmCmd, err)
		return
	}
	_, err = exec.LookPath(kindCmd)
	if err != nil {
		err = fmt.Errorf("command line tool '%s' isn't available: %w", kindCmd, err)
		return
	}
	_, err = exec.LookPath(kubectlCmd)
	if err != nil {
		err = fmt.Errorf("command line tool '%s' isn't available: %w", kubectlCmd, err)
		return
	}

	// Create and populate the object:
	result = &Kind{
		logger:   logger,
		name:     b.name,
		home:     b.home,
		quiet:    b.quiet,
		crdFiles: slices.Clone(b.crdFiles),
	}
	return
}

// Exists checks whether the cluster already exists.
func (k *Kind) Exists(ctx context.Context) (result bool, err error) {
	names, err := k.getClusters(ctx)
	if err != nil {
		return
	}
	result = slices.Contains(names, k.name)
	return
}

// Start makes sure that the cluster is created and ready to use. If the cluster already exists, it will use the
// existing one. If it doesn't exist, it will create a new one.
func (k *Kind) Start(ctx context.Context) error {
	// Check if the cluster already exists. If it does, use it. If not, create a new one.
	exists, err := k.Exists(ctx)
	if err != nil {
		return fmt.Errorf("failed to check if cluster exists: %w", err)
	}
	if exists {
		k.logger.LogAttrs(
			ctx,
			slog.LevelDebug,
			"Using existing kind cluster",
		)
	} else {
		k.logger.LogAttrs(
			ctx,
			slog.LevelDebug,
			"Creating new kind cluster",
		)
		err = k.createCluster(ctx)
		if err != nil {
			return fmt.Errorf("failed to create kind cluster '%s': %w", k.name, err)
		}
		k.logger.LogAttrs(
			ctx,
			slog.LevelDebug,
			"Created new kind cluster",
		)
	}

	// Create the kubeconfig and the Kubernetes client:
	err = k.createKubeconfig(ctx)
	if err != nil {
		return err
	}
	err = k.createKubeClient(ctx)
	if err != nil {
		return err
	}
	err = k.createKubeClientSet(ctx)
	if err != nil {
		return err
	}

	// If the cluster already existed then we don't need to do the rest of the setup, we will
	// assume that it has already been done.
	if exists {
		return nil
	}

	// Install custom resource definitions:
	err = k.installCrdFiles(ctx)
	if err != nil {
		return err
	}

	// Install cert-manager first, as other components (including trust-manager) depend on it:
	err = k.installCertManager(ctx)
	if err != nil {
		return fmt.Errorf("failed to install cert-manager: %w", err)
	}

	// Install trust-manager:
	err = k.installTrustManager(ctx)
	if err != nil {
		return fmt.Errorf("failed to install trust-manager: %w", err)
	}

	// Install CA:
	err = k.installCa(ctx)
	if err != nil {
		return fmt.Errorf("failed to install CA certificate: %w", err)
	}

	// Install Envoy gateway:
	err = k.installEnvoyGateway(ctx)
	if err != nil {
		return fmt.Errorf("failed to install Envoy Gateway: %w", err)
	}

	// Install default gateway:
	err = k.installDefaultGateway(ctx)
	if err != nil {
		return fmt.Errorf("failed to install default gateway: %w", err)
	}

	// Install authorino:
	err = k.installAuthorino(ctx)
	if err != nil {
		return fmt.Errorf("failed to install authorino: %w", err)
	}

	return nil
}

// Stop removes the Kind cluster.
func (k *Kind) Stop(ctx context.Context) error {
	k.logger.DebugContext(ctx, "Stopping kind cluster")
	var errs []error
	err := k.deleteCluster(ctx)
	if err != nil {
		errs = append(errs, fmt.Errorf("failed to delete kind cluster '%s': %w", k.name, err))
	}
	err = os.Remove(k.kubeconfigFile)
	if err != nil {
		errs = append(errs, fmt.Errorf("failed to remove kubeconfig file '%s': %w", k.kubeconfigFile, err))
	}
	if len(errs) > 0 {
		return fmt.Errorf("failed to stop kind cluster '%s': %v", k.name, errs)
	}
	k.logger.DebugContext(ctx, "Stopped kind cluster")
	return nil
}

// LoadImage loads the specified image into the Kind cluster.
//
// Note that this will take the image from the local Docker daemon, regardless of what provider was used to create the
// cluster. For example, if the cluster was created with the Podman provider this wills still load the image from
// Docker.
func (k *Kind) LoadImage(ctx context.Context, image string) error {
	if image == "" {
		return fmt.Errorf("image is mandatory")
	}
	logger := k.logger.With(
		slog.String("image", image),
	)
	logger.DebugContext(ctx, "Loading image into kind cluster")
	loadCmd, err := NewCommand().
		SetLogger(k.logger).
		SetName(kindCmd).
		SetHome(k.home).
		SetQuiet(k.quiet).
		SetArgs("load", "docker-image", "--name", k.name, image).
		Build()
	if err != nil {
		return fmt.Errorf(
			"failed to create command to load image '%s' into kind cluster '%s': %w",
			image, k.name, err,
		)
	}
	err = loadCmd.Execute(ctx)
	if err != nil {
		return fmt.Errorf(
			"failed to load image '%s' into kind cluster '%s': %w",
			image, k.name, err,
		)
	}
	logger.DebugContext(ctx, "Loaded image into kind cluster")
	return nil
}

// LoadArchive loads the specified image archive into the Kind cluster.
func (k *Kind) LoadArchive(ctx context.Context, archive string) error {
	if archive == "" {
		return fmt.Errorf("archive is mandatory")
	}
	logger := k.logger.With(
		slog.String("archive", archive),
	)
	logger.DebugContext(ctx, "Loading image archive into kind cluster")
	loadCmd, err := NewCommand().
		SetLogger(k.logger).
		SetName(kindCmd).
		SetHome(k.home).
		SetQuiet(k.quiet).
		SetArgs("load", "image-archive", "--name", k.name, archive).
		Build()
	if err != nil {
		return fmt.Errorf(
			"failed to create command to load image archive '%s' into kind cluster '%s': %w",
			archive, k.name, err,
		)
	}
	err = loadCmd.Execute(ctx)
	if err != nil {
		return fmt.Errorf(
			"failed to load image archive '%s' into kind cluster '%s': %w",
			archive, k.name, err,
		)
	}
	logger.DebugContext(ctx, "Loaded image archive into kind cluster")
	return nil
}

// Kubeconfig returns the kubeconfig bytes.
func (k *Kind) Kubeconfig() []byte {
	return slices.Clone(k.kubeconfigBytes)
}

// Client returns the controller runtime client for the cluster.
func (k *Kind) Client() crclient.WithWatch {
	return k.kubeClient
}

// ClientSet returns the Kubernetes set client for the cluster.
func (k *Kind) ClientSet() *kubernetes.Clientset {
	return k.kubeClientSet
}

// Dump uses 'kubectl cluster-info dump' to dump the state and logs of all namespaces to the specified directory.
// If the directory already exists, all its contents will be removed before dumping. If it doesn't exist, it will
// be created.
func (k *Kind) Dump(ctx context.Context, dir string) error {
	// Check parameters:
	if dir == "" {
		return fmt.Errorf("directory is mandatory")
	}
	logger := k.logger.With(
		slog.String("dir", dir),
	)
	logger.DebugContext(ctx, "Dumping cluster state and logs")

	// Remove the directory if it exists, then create it fresh:
	err := os.RemoveAll(dir)
	if err != nil {
		return fmt.Errorf("failed to remove dump directory '%s': %w", dir, err)
	}
	err = os.MkdirAll(dir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create dump directory '%s': %w", dir, err)
	}

	// Execute 'kubectl cluster-info' dump:
	dumpCmd, err := NewCommand().
		SetLogger(k.logger).
		SetName(kubectlCmd).
		SetHome(k.home).
		SetQuiet(k.quiet).
		SetArgs(
			"cluster-info",
			"dump",
			"--kubeconfig", k.kubeconfigFile,
			"--output", "yaml",
			"--output-directory", dir,
			"--all-namespaces",
		).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create command to dump cluster state: %w", err)
	}
	err = dumpCmd.Execute(ctx)
	if err != nil {
		return fmt.Errorf("failed to dump cluster state to directory '%s': %w", dir, err)
	}
	logger.DebugContext(ctx, "Dumped cluster state and logs")
	return nil
}

func (k *Kind) getClusters(ctx context.Context) (result []string, err error) {
	getCmd, err := NewCommand().
		SetLogger(k.logger).
		SetName(kindCmd).
		SetHome(k.home).
		SetQuiet(k.quiet).
		SetArgs("get", "clusters").
		Build()
	if err != nil {
		err = fmt.Errorf(
			"failed to create command to get existing kind clusters: %w",
			err,
		)
		return
	}
	getOut, _, err := getCmd.Evaluate(ctx)
	if err != nil {
		err = fmt.Errorf(
			"failed to get existing kind clusters: %w",
			err,
		)
		return
	}
	names := strings.Split(string(getOut), "\n")
	for len(names) > 0 && names[len(names)-1] == "" {
		names = names[:len(names)-1]
	}
	result = names
	return
}

func (k *Kind) createCluster(ctx context.Context) error {
	// Create a temporary directory to store the configuration file for the kind cluster:
	tmpDir, err := os.MkdirTemp("", "*.kind")
	if err != nil {
		return fmt.Errorf(
			"failed to create temporary directory for configuration of kind cluster '%s': %w",
			k.name, err,
		)
	}
	defer func() {
		err := os.RemoveAll(tmpDir)
		if err != nil {
			k.logger.LogAttrs(
				ctx,
				slog.LevelError,
				"Failed to remove temporary directory for configuration of kind cluster",
				slog.String("tmp", tmpDir),
				slog.Any("error", err),
			)
		}
	}()
	configData := map[string]any{
		"apiVersion": "kind.x-k8s.io/v1alpha4",
		"kind":       "Cluster",
		"name":       k.name,
		"nodes": []any{
			map[string]any{
				"role": "control-plane",
				"extraPortMappings": []any{
					map[string]any{
						"containerPort": internalIngressPort,
						"hostPort":      externalIngressPort,
						"listenAddress": "0.0.0.0",
					},
				},
			},
		},
	}
	configBytes, err := json.Marshal(configData)
	if err != nil {
		return fmt.Errorf("failed to marshal configuration for kind cluster '%s': %w", k.name, err)
	}
	configFile := filepath.Join(tmpDir, "config.yaml")
	err = os.WriteFile(configFile, configBytes, 0644)
	if err != nil {
		return fmt.Errorf(
			"failed to write configuration for kind cluster '%s' to file '%s': %w",
			k.name, configFile, err,
		)
	}
	k.logger.LogAttrs(
		ctx,
		slog.LevelDebug,
		"Creating kind cluster",
		slog.Any("config", configData),
	)

	// Create the cluster:
	createCmd, err := NewCommand().
		SetLogger(k.logger).
		SetName(kindCmd).
		SetHome(k.home).
		SetQuiet(k.quiet).
		SetArgs("create", "cluster", "--name", k.name, "--config", configFile).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create command to create kind cluster '%s': %w", k.name, err)
	}
	err = createCmd.Execute(ctx)
	if err != nil {
		return fmt.Errorf("failed to create kind cluster '%s': %w", k.name, err)
	}
	k.logger.DebugContext(ctx, "Created kind cluster")
	return nil
}

func (k *Kind) deleteCluster(ctx context.Context) error {
	k.logger.DebugContext(ctx, "Deleting kind cluster")
	deleteCmd, err := NewCommand().
		SetLogger(k.logger).
		SetName(kindCmd).
		SetHome(k.home).
		SetQuiet(k.quiet).
		SetArgs("delete", "cluster", "--name", k.name).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create command to delete kind cluster '%s': %w", k.name, err)
	}
	err = deleteCmd.Execute(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete kind cluster '%s': %w", k.name, err)
	}
	k.logger.DebugContext(ctx, "Deleted kind cluster")
	return err
}

func (k *Kind) createKubeconfig(ctx context.Context) error {
	k.logger.DebugContext(ctx, "Creating kubeconfig")
	getCmd, err := NewCommand().
		SetLogger(k.logger).
		SetName(kindCmd).
		SetHome(k.home).
		SetQuiet(k.quiet).
		SetArgs("get", "kubeconfig", "--name", k.name).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create command to get kubeconfig for kind cluster '%s': %w", k.name, err)
	}
	k.kubeconfigBytes, _, err = getCmd.Evaluate(ctx)
	if err != nil {
		return fmt.Errorf("failed to get kubeconfig for kind cluster '%s': %w", k.name, err)
	}

	err = func() error {
		tmpFile, err := os.CreateTemp("", "*.kubeconfig")
		if err != nil {
			return fmt.Errorf("failed to create kubeconfig file for kind cluster '%s': %w", k.name, err)
		}
		defer func() {
			err := tmpFile.Close()
			if err != nil {
				k.logger.LogAttrs(
					ctx,
					slog.LevelError,
					"Failed to close kubeconfig file",
					slog.String("file", tmpFile.Name()),
					slog.Any("error", err),
				)
			}
		}()
		_, err = tmpFile.Write(k.kubeconfigBytes)
		if err != nil {
			return fmt.Errorf(
				"failed to write kubeconfig file '%s' for kind cluster '%s': %w",
				tmpFile.Name(), k.name, err,
			)
		}
		k.kubeconfigFile = tmpFile.Name()
		return nil
	}()
	if err != nil {
		return err
	}
	k.logger.LogAttrs(
		ctx,
		slog.LevelDebug,
		"Created kubeconfig",
		slog.String("file", k.kubeconfigFile),
		slog.Int("size", len(k.kubeconfigBytes)),
	)
	return nil
}

func (k *Kind) createKubeClient(ctx context.Context) error {
	k.logger.DebugContext(ctx, "Creating Kubernetes client")
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(k.kubeconfigBytes)
	if err != nil {
		return fmt.Errorf("failed to create REST config for kind cluster '%s': %w", k.name, err)
	}
	k.kubeClient, err = crclient.NewWithWatch(restConfig, crclient.Options{})
	if err != nil {
		return fmt.Errorf("failed to create client for kind cluster '%s': %w", k.name, err)
	}
	k.logger.DebugContext(ctx, "Created Kubernetes client")
	return nil
}

func (k *Kind) createKubeClientSet(ctx context.Context) error {
	k.logger.DebugContext(ctx, "Creating Kubernetes client set")
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(k.kubeconfigBytes)
	if err != nil {
		return fmt.Errorf("failed to create REST config for kind cluster '%s': %w", k.name, err)
	}
	k.kubeClientSet, err = kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create client set for kind cluster '%s': %w", k.name, err)
	}
	k.logger.DebugContext(ctx, "Created Kubernetes client")
	return nil
}

func (k *Kind) installCertManager(ctx context.Context) (err error) {
	k.logger.DebugContext(ctx, "Installing cert-manager")
	installCmd, err := NewCommand().
		SetLogger(k.logger).
		SetName(helmCmd).
		SetHome(k.home).
		SetQuiet(k.quiet).
		SetArgs(
			"install",
			"cert-manager",
			"oci://quay.io/jetstack/charts/cert-manager",
			"--version", certManagerVersion,
			"--kubeconfig", k.kubeconfigFile,
			"--namespace", certManagerNamespace,
			"--create-namespace",
			"--set", "crds.enabled=true",
			"--wait",
		).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create command to install cert-manager: %w", err)
	}
	err = installCmd.Execute(ctx)
	if err != nil {
		return fmt.Errorf("failed to install cert-manager: %w", err)
	}

	// Wait for custom resource definition to be available:
	err = k.waitForCrd(ctx, "clusterissuer.yaml", time.Minute)
	if err != nil {
		return fmt.Errorf("failed to wait for issuer CRD: %w", err)
	}
	err = k.waitForCrd(ctx, "certificate.yaml", time.Minute)
	if err != nil {
		return fmt.Errorf("failed to wait for Certificate CRD: %w", err)
	}

	k.logger.DebugContext(ctx, "Installed cert-manager")
	return nil
}

func (k *Kind) installTrustManager(ctx context.Context) (err error) {
	k.logger.DebugContext(ctx, "Installing trust-manager")
	installCmd, err := NewCommand().
		SetLogger(k.logger).
		SetName(helmCmd).
		SetHome(k.home).
		SetQuiet(k.quiet).
		SetArgs(
			"install",
			"trust-manager",
			"oci://quay.io/jetstack/charts/trust-manager",
			"--version", trustManagerVersion,
			"--kubeconfig", k.kubeconfigFile,
			"--namespace", certManagerNamespace,
			"--set", "defaultPackage.enabled=false",
			"--wait",
		).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create command to install trust-manager: %w", err)
	}
	err = installCmd.Execute(ctx)
	if err != nil {
		return fmt.Errorf("failed to install trust-manager: %w", err)
	}
	err = k.waitForCrd(ctx, "bundle.yaml", time.Minute)
	if err != nil {
		return fmt.Errorf("failed to wait for bundle CRD: %w", err)
	}
	k.logger.DebugContext(ctx, "Installed trust-manager")
	return nil
}

func (k *Kind) installCa(ctx context.Context) (err error) {
	// Generate private key and certificate:
	k.logger.DebugContext(ctx, "Generating CA private key and certificate")
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	now := time.Now()
	crt := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: defaultCaCommonName,
		},
		NotBefore:             now,
		NotAfter:              now.AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	// Create the PEM encoding of the private key and the certificate:
	keyBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	keyPem := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyBytes,
	})
	crtBytes, err := x509.CreateCertificate(rand.Reader, &crt, &crt, &key.PublicKey, key)
	if err != nil {
		return err
	}
	crtPem := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: crtBytes,
	})

	// Create or update the secret:
	k.logger.DebugContext(ctx, "Creating or updating CA secret")
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: certManagerNamespace,
			Name:      defaultCaSecretName,
		},
	}
	_, err = controllerutil.CreateOrPatch(ctx, k.kubeClient, secret, func() error {
		secret.Type = corev1.SecretTypeTLS
		secret.Data = map[string][]byte{
			corev1.TLSCertKey:       crtPem,
			corev1.TLSPrivateKeyKey: keyPem,
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Create or update the issuer:
	k.logger.DebugContext(ctx, "Creating or updating CA issuer")
	issuer := &unstructured.Unstructured{}
	issuer.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "ClusterIssuer",
	})
	issuer.SetName(defaultCaIssuerName)
	_, err = controllerutil.CreateOrPatch(ctx, k.kubeClient, issuer, func() error {
		issuer.Object["spec"] = map[string]any{
			"ca": map[string]any{
				"secretName": defaultCaSecretName,
			},
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Create or update the bundle that will copy the CA certificate to all the namespaces:
	k.logger.DebugContext(ctx, "Creating or updating CA bundle")
	bundle := &unstructured.Unstructured{}
	bundle.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "trust.cert-manager.io",
		Version: "v1alpha1",
		Kind:    "Bundle",
	})
	bundle.SetName(defaultBundleName)
	_, err = controllerutil.CreateOrPatch(ctx, k.kubeClient, bundle, func() error {
		bundle.Object["spec"] = map[string]any{
			"sources": []any{
				map[string]any{
					"secret": map[string]any{
						"name": defaultCaSecretName,
						"key":  corev1.TLSCertKey,
					},
				},
			},
			"target": map[string]any{
				"configMap": map[string]any{
					"key": defaultBundleFile,
				},
				"namespaceSelector": map[string]any{
					"matchExpressions": []any{
						map[string]any{
							"key":      "kubernetes.io/metadata.name",
							"operator": "NotIn",
							"values": []string{
								"kube-node-lease",
								"kube-public",
								"kube-system",
								"local-path-storage",
								certManagerNamespace,
								envoyGatewayNamespace,
							},
						},
					},
				},
			},
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func (k *Kind) installAuthorino(ctx context.Context) (err error) {
	// The authorino operator doesn't currently support adding trusted CA certificates, due to a conflicting use of
	// the '/etc/ssl/certs' directory, see here for details:
	//
	// https://github.com/Kuadrant/authorino-operator/pull/282
	//
	// Till that is fixed we need to use an alternative container image that contains the fix. So we need to
	// download the manifests and then replace the image.
	response, err := http.Get(authorinoManifests)
	if err != nil {
		return
	}
	defer response.Body.Close()
	manifestsBytes, err := io.ReadAll(response.Body)
	if err != nil {
		return
	}
	manfiestsTexts := string(manifestsBytes)
	manfiestsTexts = strings.ReplaceAll(
		manfiestsTexts,
		"quay.io/kuadrant/authorino-operator:"+authorinoVersion,
		"quay.io/innabox/authorino-operator:latest",
	)
	manifestsDir, err := os.MkdirTemp("", "*.authorino")
	if err != nil {
		return
	}
	defer func() {
		err := os.RemoveAll(manifestsDir)
		if err != nil {
			k.logger.LogAttrs(
				ctx,
				slog.LevelError,
				"Failed to remove temporary manifests directory",
				slog.String("dir", manifestsDir),
				slog.Any("error", err),
			)
		}
	}()
	manifestsFile := filepath.Join(manifestsDir, "manifests.yaml")
	if err != nil {
		return
	}
	os.WriteFile(manifestsFile, []byte(manfiestsTexts), 0600)
	if err != nil {
		return
	}

	// Apply the authorino manifest:
	k.logger.DebugContext(ctx, "Applying authorino manifests")
	applyCmd, err := NewCommand().
		SetLogger(k.logger).
		SetName(kubectlCmd).
		SetHome(k.home).
		SetQuiet(k.quiet).
		SetArgs(
			"apply",
			"--kubeconfig", k.kubeconfigFile,
			"--filename", manifestsFile,
		).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create command to apply authorino manifests: %w", err)
	}
	err = applyCmd.Execute(ctx)
	if err != nil {
		return fmt.Errorf("failed to apply authorino manifests: %w", err)
	}
	k.logger.DebugContext(ctx, "Applied authorino manifests")

	// Wait for custom resource definition to be available:
	err = k.waitForCrd(ctx, "authorino.yaml", time.Minute)
	if err != nil {
		return fmt.Errorf("failed to wait for authorino CRD: %w", err)
	}
	err = k.waitForCrd(ctx, "authconfig.yaml", time.Minute)
	if err != nil {
		return fmt.Errorf("failed to wait for authconfig CRD: %w", err)
	}

	return nil
}

func (k *Kind) installEnvoyGateway(ctx context.Context) (err error) {
	k.logger.DebugContext(ctx, "Installing Envoy Gateway")
	installCmd, err := NewCommand().
		SetLogger(k.logger).
		SetName(helmCmd).
		SetArgs(
			"install",
			"envoy-gateway",
			"oci://docker.io/envoyproxy/gateway-helm",
			"--version", envoyGatewayVersion,
			"--kubeconfig", k.kubeconfigFile,
			"--namespace", envoyGatewayNamespace,
			"--create-namespace",
			"--wait",
		).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create command to install Envoy Gateway: %w", err)
	}
	err = installCmd.Execute(ctx)
	if err != nil {
		return fmt.Errorf("failed to install Envoy Gateway: %w", err)
	}
	err = k.waitForCrd(ctx, "envoyproxy.yaml", time.Minute)
	if err != nil {
		return fmt.Errorf("failed to wait for EnvoyProxy CRD: %w", err)
	}
	err = k.waitForCrd(ctx, "gatewayclass.yaml", time.Minute)
	if err != nil {
		return fmt.Errorf("failed to wait for GatewayClass CRD: %w", err)
	}
	err = k.waitForCrd(ctx, "gateway.yaml", time.Minute)
	if err != nil {
		return fmt.Errorf("failed to wait for Gateway CRD: %w", err)
	}
	k.logger.DebugContext(ctx, "Installed Envoy Gateway")
	return nil
}

func (k *Kind) installDefaultGateway(ctx context.Context) (err error) {
	// Create or update the Envoy proxy to specify additional details of the gateway class. Note that it isn't
	// possible to set the node port directly, but we can use a patch to set it.
	k.logger.DebugContext(ctx, "Creating or updating Envoy proxy")
	envoyProxyGvk := schema.GroupVersionKind{
		Group:   "gateway.envoyproxy.io",
		Version: "v1alpha1",
		Kind:    "EnvoyProxy",
	}
	envoyProxy := &unstructured.Unstructured{}
	envoyProxy.SetGroupVersionKind(envoyProxyGvk)
	envoyProxy.SetNamespace(envoyGatewayNamespace)
	envoyProxy.SetName(envoyProxyName)
	envoyPatch := map[string]any{
		"type": "StrategicMerge",
		"value": map[string]any{
			"spec": map[string]any{
				"ports": []any{
					map[string]any{
						"name":     "https",
						"port":     443,
						"nodePort": internalIngressPort,
					},
				},
			},
		},
	}
	_, err = controllerutil.CreateOrPatch(ctx, k.kubeClient, envoyProxy, func() error {
		envoyProxy.Object["spec"] = map[string]any{
			"provider": map[string]any{
				"type": "Kubernetes",
				"kubernetes": map[string]any{
					"envoyService": map[string]any{
						"type":  "NodePort",
						"patch": envoyPatch,
					},
				},
			},
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create or update Envoy proxy: %w", err)
	}

	// Create or update the default gateway class:
	k.logger.DebugContext(ctx, "Creating or updating default gateway class")
	gatewayClass := &unstructured.Unstructured{}
	gatewayClass.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "gateway.networking.k8s.io",
		Version: "v1",
		Kind:    "GatewayClass",
	})
	gatewayClass.SetName(envoyGatewayClass)
	_, err = controllerutil.CreateOrPatch(ctx, k.kubeClient, gatewayClass, func() error {
		gatewayClass.Object["spec"] = map[string]any{
			"controllerName": "gateway.envoyproxy.io/gatewayclass-controller",
			"parametersRef": map[string]any{
				"group":     envoyProxyGvk.Group,
				"kind":      envoyProxyGvk.Kind,
				"namespace": envoyProxy.GetNamespace(),
				"name":      envoyProxy.GetName(),
			},
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create or update default gateway class: %w", err)
	}

	// Create or update the default gateway:
	k.logger.DebugContext(ctx, "Creating or updating default gateway")
	gateway := &unstructured.Unstructured{}
	gateway.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "gateway.networking.k8s.io",
		Version: "v1",
		Kind:    "Gateway",
	})
	gateway.SetNamespace(envoyGatewayNamespace)
	gateway.SetName(envoyGatewayName)
	_, err = controllerutil.CreateOrPatch(ctx, k.kubeClient, gateway, func() error {
		gateway.Object["spec"] = map[string]any{
			"gatewayClassName": envoyGatewayClass,
			"listeners": []any{
				map[string]any{
					"name":     "tls",
					"protocol": "TLS",
					"port":     443,
					"tls": map[string]any{
						"mode": "Passthrough",
					},
					"allowedRoutes": map[string]any{
						"namespaces": map[string]any{
							"from": "All",
						},
					},
				},
			},
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create or update default gateway: %w", err)
	}

	return nil
}

func (k *Kind) installCrdFiles(ctx context.Context) error {
	for _, crdFile := range k.crdFiles {
		logger := k.logger.With(slog.String("file", crdFile))
		logger.DebugContext(ctx, "Applying CRD")
		applyCmd, err := NewCommand().
			SetLogger(k.logger).
			SetName(kubectlCmd).
			SetHome(k.home).
			SetQuiet(k.quiet).
			SetArgs(
				"apply",
				"--kubeconfig", k.kubeconfigFile,
				"--filename", crdFile,
			).
			Build()
		if err != nil {
			return err
		}
		err = applyCmd.Execute(ctx)
		if err != nil {
			return err
		}
		logger.DebugContext(ctx, "Applied CRD")
	}
	return nil
}

// waitForCrd waits for a custom resource definition to be available by attempting to create an example object
// loaded from the embedded YAML file using dry run. It will retry until the CRD is available or a timeout is reached.
func (k *Kind) waitForCrd(ctx context.Context, filename string, timeout time.Duration) error {
	// Load the YAML file from the embedded filesystem:
	data, err := examplesFS.ReadFile(filepath.Join("examples", filename))
	if err != nil {
		return fmt.Errorf("failed to read example file '%s': %w", filename, err)
	}

	// Deserialize the YAML into an unstructured object:
	decoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	object := &unstructured.Unstructured{}
	_, gvk, err := decoder.Decode(data, nil, object)
	if err != nil {
		return fmt.Errorf("failed to decode YAML from file '%s': %w", filename, err)
	}
	logger := k.logger.With(
		slog.String("file", filename),
		slog.String("group", gvk.Group),
		slog.String("version", gvk.Version),
		slog.String("kind", gvk.Kind),
	)

	// Wait for the custom resource definition to be available:
	logger.DebugContext(ctx, "Waiting for CRD to be available")
	start := time.Now()
	for {
		err := k.kubeClient.Create(ctx, object, crclient.DryRunAll)
		if err == nil {
			logger.DebugContext(ctx, "CRD is available")
			return nil
		}
		logger.LogAttrs(
			ctx,
			slog.LevelDebug,
			"CRD not available yet",
			slog.Any("error", err),
		)
		if time.Since(start) > timeout {
			return fmt.Errorf(
				"CRD %s/%s/%s not available after waiting for %v: %w",
				gvk.Group, gvk.Version, gvk.Kind, timeout, err,
			)
		}
		time.Sleep(5 * time.Second)
	}
}

// Name of objects related to the default CA:
const (
	defaultBundleFile   = "bundle.pem"
	defaultBundleName   = "ca-bundle"
	defaultCaCommonName = "Default CA"
	defaultCaIssuerName = "default-ca"
	defaultCaSecretName = "default-ca"
)

// Names of commands:
const (
	helmCmd    = "helm"
	kindCmd    = "kind"
	kubectlCmd = "kubectl"
)

// host ingress port is the port number on the host machine that maps to the internal ingress port. Traffic arriving on
// this port on the host will be forwarded to the internal ingress port in the cluster.
const externalIngressPort = 8000

// internalIngressPort is the port number used internally in the Kubernetes cluster for ingress traffic. This is the
// node port that Envoy Gateway's service will use, and it is also the container port mapped in the Kind cluster
// configuration.
const internalIngressPort = 30000

// Versions of components:
const (
	certManagerVersion  = "v1.19.2"
	trustManagerVersion = "v0.20.3"
	authorinoVersion    = "v0.22.0"
	envoyGatewayVersion = "v1.6.1"
)

// Details of the cert-manager installation:
const (
	certManagerNamespace = "cert-manager"
)

// Details of the Envoy gateway installation:
const (
	envoyGatewayClass     = "default"
	envoyGatewayName      = "default"
	envoyGatewayNamespace = "envoy-gateway"
	envoyProxyName        = "default"
)

// Details of the authorino installation:
const (
	authorinoManifests = "https://raw.githubusercontent.com/Kuadrant/authorino-operator/refs/heads/release-" +
		authorinoVersion + "/config/deploy/manifests.yaml"
)
