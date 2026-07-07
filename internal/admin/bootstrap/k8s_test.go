package bootstrap

import (
	"errors"
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

	if string(got.Data["api-key"]) != "otj_k8skey_000" {
		t.Fatalf("unexpected data: %s", got.Data["api-key"])
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
			Data: map[string][]byte{"api-key": []byte("existing")},
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
	if string(got.Data["api-key"]) != "existing" {
		t.Fatalf("existing secret was overwritten: %s", got.Data["api-key"])
	}
}

func TestK8sSecretWriter_CreateError(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "secrets", func(action testcore.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, errors.New("apiserver unavailable")
	})
	w := &K8sSecretWriter{Client: client, Namespace: "orbitjob-system"}

	if err := w.Write(t.Context(), "bootstrap-api-key", map[string]string{"api-key": "x"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestK8sSecretWriter_UsesDefaultNamespace(t *testing.T) {
	client := fake.NewSimpleClientset()
	w := &K8sSecretWriter{Client: client}

	if err := w.Write(t.Context(), "bootstrap-api-key", map[string]string{"api-key": "otj_k8skey_000"}); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := client.CoreV1().Secrets(defaultK8sNamespace).Get(t.Context(), "bootstrap-api-key", metav1.GetOptions{}); err != nil {
		t.Fatalf("get: %v", err)
	}
}
