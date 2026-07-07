package main

import (
	"errors"
	"testing"

	"orbitjob/internal/admin/bootstrap"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

func TestResolveBootstrapOptions_ProductionRequiresKey(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("ADMIN_BOOTSTRAP_API_KEY", "")

	_, err := resolveBootstrapOptions()
	if err == nil {
		t.Fatal("expected error in production without ADMIN_BOOTSTRAP_API_KEY")
	}
}

func TestResolveBootstrapOptions_ProductionWithKey(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("ADMIN_BOOTSTRAP_API_KEY", "otj_prod_key_12345")

	opts, err := resolveBootstrapOptions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.APIKey != "otj_prod_key_12345" {
		t.Fatalf("expected api key, got %q", opts.APIKey)
	}
	if !opts.DisallowDefaultKey {
		t.Fatal("expected default key to be disallowed in production")
	}
}

func TestResolveBootstrapOptions_DevelopmentAllowsDefault(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("ADMIN_BOOTSTRAP_API_KEY", "")

	opts, err := resolveBootstrapOptions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.DisallowDefaultKey {
		t.Fatal("expected default key to be allowed in development")
	}
	if opts.APIKey != "" {
		t.Fatal("expected empty api key to fall back to default")
	}
}

func TestResolveBootstrapOptions_DevelopmentUsesExplicitKey(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("ADMIN_BOOTSTRAP_API_KEY", "otj_dev_key_12345")

	opts, err := resolveBootstrapOptions()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.APIKey != "otj_dev_key_12345" {
		t.Fatalf("expected explicit api key, got %q", opts.APIKey)
	}
	if opts.DisallowDefaultKey {
		t.Fatal("expected default key to be allowed in development")
	}
}

func TestResolveDSN_ArgumentFirst(t *testing.T) {
	t.Setenv("DATABASE_DSN", "")
	t.Setenv("ADMIN_DSN", "")

	got := resolveDSN([]string{"cmd", "postgres://arg"})
	if got != "postgres://arg" {
		t.Fatalf("expected arg dsn, got %q", got)
	}
}

func TestResolveDSN_DatabaseDSNFallback(t *testing.T) {
	t.Setenv("DATABASE_DSN", "postgres://database")
	t.Setenv("ADMIN_DSN", "postgres://admin")

	got := resolveDSN([]string{"cmd"})
	if got != "postgres://database" {
		t.Fatalf("expected DATABASE_DSN, got %q", got)
	}
}

func TestResolveDSN_AdminDSNFallback(t *testing.T) {
	t.Setenv("DATABASE_DSN", "")
	t.Setenv("ADMIN_DSN", "postgres://admin")

	got := resolveDSN([]string{"cmd"})
	if got != "postgres://admin" {
		t.Fatalf("expected ADMIN_DSN, got %q", got)
	}
}

func TestResolveDSN_Empty(t *testing.T) {
	t.Setenv("DATABASE_DSN", "")
	t.Setenv("ADMIN_DSN", "")

	got := resolveDSN([]string{"cmd"})
	if got != "" {
		t.Fatalf("expected empty dsn, got %q", got)
	}
}

func TestResolveDSN_ArgumentOverridesEnvironment(t *testing.T) {
	t.Setenv("DATABASE_DSN", "postgres://database")
	t.Setenv("ADMIN_DSN", "postgres://admin")

	got := resolveDSN([]string{"cmd", "postgres://arg"})
	if got != "postgres://arg" {
		t.Fatalf("expected arg dsn to override env, got %q", got)
	}
}

func TestInCluster_True(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	if !inCluster() {
		t.Fatal("expected inCluster to be true")
	}
}

func TestInCluster_False(t *testing.T) {
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	if inCluster() {
		t.Fatal("expected inCluster to be false")
	}
}

func TestNewSecretWriter_LocalDefaultRoot(t *testing.T) {
	t.Setenv("ADMIN_BOOTSTRAP_SECRET_BACKEND", "local")
	t.Setenv("ADMIN_BOOTSTRAP_SECRET_ROOT", "")
	t.Setenv("KUBERNETES_SERVICE_HOST", "")

	writer, err := newSecretWriter()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	local, ok := writer.(*bootstrap.LocalFileSecretWriter)
	if !ok {
		t.Fatalf("expected *LocalFileSecretWriter, got %T", writer)
	}
	if local.Root != defaultSecretRoot {
		t.Fatalf("expected root %q, got %q", defaultSecretRoot, local.Root)
	}
}

func TestNewSecretWriter_LocalCustomRoot(t *testing.T) {
	t.Setenv("ADMIN_BOOTSTRAP_SECRET_BACKEND", "local")
	t.Setenv("ADMIN_BOOTSTRAP_SECRET_ROOT", "/tmp/secrets")
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")

	writer, err := newSecretWriter()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	local, ok := writer.(*bootstrap.LocalFileSecretWriter)
	if !ok {
		t.Fatalf("expected *LocalFileSecretWriter, got %T", writer)
	}
	if local.Root != "/tmp/secrets" {
		t.Fatalf("expected root %q, got %q", "/tmp/secrets", local.Root)
	}
}

func TestNewSecretWriter_DefaultsToLocalWhenNotInCluster(t *testing.T) {
	t.Setenv("ADMIN_BOOTSTRAP_SECRET_BACKEND", "")
	t.Setenv("ADMIN_BOOTSTRAP_SECRET_ROOT", "")
	t.Setenv("KUBERNETES_SERVICE_HOST", "")

	writer, err := newSecretWriter()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := writer.(*bootstrap.LocalFileSecretWriter); !ok {
		t.Fatalf("expected *LocalFileSecretWriter, got %T", writer)
	}
}

func TestNewSecretWriter_RespectsBackendOverride(t *testing.T) {
	t.Setenv("ADMIN_BOOTSTRAP_SECRET_BACKEND", "local")
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")

	writer, err := newSecretWriter()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := writer.(*bootstrap.LocalFileSecretWriter); !ok {
		t.Fatalf("expected *LocalFileSecretWriter when backend forced, got %T", writer)
	}
}

func TestNewSecretWriter_K8sDefaultNamespace(t *testing.T) {
	origConfig := inClusterConfigFn
	origClient := newKubernetesClientFn
	defer func() {
		inClusterConfigFn = origConfig
		newKubernetesClientFn = origClient
	}()

	inClusterConfigFn = func() (*rest.Config, error) {
		return &rest.Config{Host: "https://localhost:443"}, nil
	}
	newKubernetesClientFn = func(_ *rest.Config) (kubernetes.Interface, error) {
		return fake.NewSimpleClientset(), nil
	}

	t.Setenv("ADMIN_BOOTSTRAP_SECRET_BACKEND", "")
	t.Setenv("ADMIN_BOOTSTRAP_NAMESPACE", "")
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")

	writer, err := newSecretWriter()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	k8s, ok := writer.(*bootstrap.K8sSecretWriter)
	if !ok {
		t.Fatalf("expected *K8sSecretWriter, got %T", writer)
	}
	if k8s.Namespace != defaultNamespace {
		t.Fatalf("expected namespace %q, got %q", defaultNamespace, k8s.Namespace)
	}
}

func TestNewSecretWriter_K8sCustomNamespace(t *testing.T) {
	origConfig := inClusterConfigFn
	origClient := newKubernetesClientFn
	defer func() {
		inClusterConfigFn = origConfig
		newKubernetesClientFn = origClient
	}()

	inClusterConfigFn = func() (*rest.Config, error) {
		return &rest.Config{Host: "https://localhost:443"}, nil
	}
	newKubernetesClientFn = func(_ *rest.Config) (kubernetes.Interface, error) {
		return fake.NewSimpleClientset(), nil
	}

	t.Setenv("ADMIN_BOOTSTRAP_SECRET_BACKEND", "")
	t.Setenv("ADMIN_BOOTSTRAP_NAMESPACE", "custom-ns")
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")

	writer, err := newSecretWriter()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	k8s, ok := writer.(*bootstrap.K8sSecretWriter)
	if !ok {
		t.Fatalf("expected *K8sSecretWriter, got %T", writer)
	}
	if k8s.Namespace != "custom-ns" {
		t.Fatalf("expected namespace %q, got %q", "custom-ns", k8s.Namespace)
	}
}

func TestNewSecretWriter_K8sConfigError(t *testing.T) {
	origConfig := inClusterConfigFn
	origClient := newKubernetesClientFn
	defer func() {
		inClusterConfigFn = origConfig
		newKubernetesClientFn = origClient
	}()

	inClusterConfigFn = func() (*rest.Config, error) {
		return nil, errors.New("no in-cluster config")
	}
	newKubernetesClientFn = func(_ *rest.Config) (kubernetes.Interface, error) {
		t.Fatal("client constructor should not be called")
		return nil, nil
	}

	t.Setenv("ADMIN_BOOTSTRAP_SECRET_BACKEND", "")
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")

	_, err := newSecretWriter()
	if err == nil {
		t.Fatal("expected error from in-cluster config")
	}
}

func TestNewSecretWriter_K8sClientError(t *testing.T) {
	origConfig := inClusterConfigFn
	origClient := newKubernetesClientFn
	defer func() {
		inClusterConfigFn = origConfig
		newKubernetesClientFn = origClient
	}()

	inClusterConfigFn = func() (*rest.Config, error) {
		return &rest.Config{Host: "https://localhost:443"}, nil
	}
	newKubernetesClientFn = func(_ *rest.Config) (kubernetes.Interface, error) {
		return nil, errors.New("client creation failed")
	}

	t.Setenv("ADMIN_BOOTSTRAP_SECRET_BACKEND", "")
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")

	_, err := newSecretWriter()
	if err == nil {
		t.Fatal("expected error from k8s client creation")
	}
}
