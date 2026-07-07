// cmd/bootstrap is a standalone CLI to ensure the default tenant and initial
// API key exist. It persists the key to a SecretWriter backend (local file or
// Kubernetes Secret) and prints status messages to stderr, making it suitable
// for scripted first-time setup.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"orbitjob/internal/admin/bootstrap"
	adminpostgres "orbitjob/internal/admin/store/postgres"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Test seams for newSecretWriter.
var (
	inClusterConfigFn     = rest.InClusterConfig
	newKubernetesClientFn = func(cfg *rest.Config) (kubernetes.Interface, error) {
		return kubernetes.NewForConfig(cfg)
	}
)

const (
	defaultSecretRoot = "/run/secrets/orbitjob"
	defaultNamespace  = "orbitjob-system"
)

func main() {
	dsn := resolveDSN(os.Args)
	if dsn == "" {
		log.Fatal("DSN required: pass as first argument or set DATABASE_DSN / ADMIN_DSN")
	}

	db, err := adminpostgres.Open(dsn)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer func() { _ = db.Close() }()

	writer, err := newSecretWriter()
	if err != nil {
		log.Fatalf("secret writer: %v", err)
	}

	opts, err := resolveBootstrapOptions()
	if err != nil {
		log.Fatal(err)
	}
	opts.Writer = writer

	res, err := bootstrap.EnsureDefault(context.Background(), db, opts)
	if err != nil {
		log.Fatalf("bootstrap: %v", err)
	}

	if res.KeyCreated {
		fmt.Fprintln(os.Stderr, "Bootstrap API key created.")
		return
	}
	fmt.Fprintln(os.Stderr, "Default API key already exists.")
}

func resolveBootstrapOptions() (bootstrap.Options, error) {
	opts := bootstrap.Options{
		APIKey:             os.Getenv("ADMIN_BOOTSTRAP_API_KEY"),
		DisallowDefaultKey: os.Getenv("APP_ENV") == "production",
	}
	if opts.APIKey == "" && opts.DisallowDefaultKey {
		return bootstrap.Options{}, errors.New("ADMIN_BOOTSTRAP_API_KEY is required when APP_ENV=production")
	}
	return opts, nil
}

func resolveDSN(args []string) string {
	if len(args) > 1 && args[1] != "" {
		return args[1]
	}
	if d := os.Getenv("DATABASE_DSN"); d != "" {
		return d
	}
	return os.Getenv("ADMIN_DSN")
}

func newSecretWriter() (bootstrap.SecretWriter, error) {
	if os.Getenv("ADMIN_BOOTSTRAP_SECRET_BACKEND") == "local" || !inCluster() {
		root := os.Getenv("ADMIN_BOOTSTRAP_SECRET_ROOT")
		if root == "" {
			root = defaultSecretRoot
		}
		return &bootstrap.LocalFileSecretWriter{Root: root}, nil
	}

	cfg, err := inClusterConfigFn()
	if err != nil {
		return nil, fmt.Errorf("in-cluster config: %w", err)
	}
	client, err := newKubernetesClientFn(cfg)
	if err != nil {
		return nil, fmt.Errorf("k8s client: %w", err)
	}
	ns := os.Getenv("ADMIN_BOOTSTRAP_NAMESPACE")
	if ns == "" {
		ns = defaultNamespace
	}
	return &bootstrap.K8sSecretWriter{Client: client, Namespace: ns}, nil
}

func inCluster() bool {
	return os.Getenv("KUBERNETES_SERVICE_HOST") != ""
}
