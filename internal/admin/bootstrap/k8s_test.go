package bootstrap

import (
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	testcore "k8s.io/client-go/testing"
)

func TestK8sSecretWriter_CreatesSecret(t *testing.T) {
	client := fake.NewSimpleClientset()
	w := &K8sSecretWriter{Client: client, Namespace: "orbitjob-system"}

	if err := w.Write(t.Context(), "bootstrap-api-key", map[string]string{"api-key": "otj_k8skey_000"}); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := client.CoreV1().Secrets("orbitjob-system").Get(t.Context(), "bootstrap-api-key", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.StringData["api-key"] != "otj_k8skey_000" {
		t.Fatalf("unexpected string data: %s", got.StringData["api-key"])
	}
	if got.Labels[k8sManagedByLabelKey] != k8sManagedByLabelValue {
		t.Fatalf("unexpected managed-by label: %s", got.Labels[k8sManagedByLabelKey])
	}
	if got.Type != corev1.SecretTypeOpaque {
		t.Fatalf("unexpected secret type: %s", got.Type)
	}
}

func TestK8sSecretWriter_AlreadyExistsReturnsNil(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bootstrap-api-key",
				Namespace: "orbitjob-system",
			},
			StringData: map[string]string{"api-key": "existing"},
		},
	)
	w := &K8sSecretWriter{Client: client, Namespace: "orbitjob-system"}

	if err := w.Write(t.Context(), "bootstrap-api-key", map[string]string{"api-key": "otj_k8skey_000"}); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := client.CoreV1().Secrets("orbitjob-system").Get(t.Context(), "bootstrap-api-key", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.StringData["api-key"] != "existing" {
		t.Fatalf("existing secret was overwritten: %s", got.StringData["api-key"])
	}
}

func TestK8sSecretWriter_CreateError(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "secrets", func(action testcore.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, errors.New("apiserver unavailable")
	})
	w := &K8sSecretWriter{Client: client, Namespace: "orbitjob-system"}

	err := w.Write(t.Context(), "bootstrap-api-key", map[string]string{"api-key": "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "apiserver unavailable") {
		t.Fatalf("error does not wrap underlying cause: %v", err)
	}
	if !strings.Contains(err.Error(), "create secret orbitjob-system/bootstrap-api-key") {
		t.Fatalf("error does not identify secret: %v", err)
	}
}

func TestK8sSecretWriter_EmptyNamespaceReturnsError(t *testing.T) {
	client := fake.NewSimpleClientset()
	w := &K8sSecretWriter{Client: client}

	err := w.Write(t.Context(), "bootstrap-api-key", map[string]string{"api-key": "x"})
	if err == nil {
		t.Fatal("expected error for empty namespace")
	}
	if !strings.Contains(err.Error(), "namespace") {
		t.Fatalf("error does not mention namespace: %v", err)
	}
}
