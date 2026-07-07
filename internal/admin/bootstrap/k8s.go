package bootstrap

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// defaultK8sNamespace is the namespace used when K8sSecretWriter.Namespace
	// is empty.
	defaultK8sNamespace = "orbitjob-system"

	// k8sManagedByLabelKey is the standard label key identifying the controller
	// that manages a Kubernetes resource.
	k8sManagedByLabelKey = "app.kubernetes.io/managed-by"
	// k8sManagedByLabelValue identifies secrets created by the bootstrapper.
	k8sManagedByLabelValue = "orbitjob-bootstrap"
)

// K8sSecretWriter writes bootstrap secrets as Kubernetes Secret objects.
type K8sSecretWriter struct {
	Client    kubernetes.Interface
	Namespace string
}

func (w *K8sSecretWriter) namespace() string {
	if w.Namespace != "" {
		return w.Namespace
	}
	return defaultK8sNamespace
}

func encodeSecretData(data map[string]string) map[string][]byte {
	out := make(map[string][]byte, len(data))
	for k, v := range data {
		out[k] = []byte(v)
	}
	return out
}

// Write creates a Kubernetes Secret with the given name and string data. If a
// Secret with the same name already exists, Write returns nil without modifying
// the existing Secret.
func (w *K8sSecretWriter) Write(ctx context.Context, name string, data map[string]string) error {
	ns := w.namespace()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				k8sManagedByLabelKey: k8sManagedByLabelValue,
			},
		},
		Data: encodeSecretData(data),
		Type: corev1.SecretTypeOpaque,
	}

	_, err := w.Client.CoreV1().Secrets(ns).Create(ctx, secret, metav1.CreateOptions{})
	if errors.IsAlreadyExists(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create secret %s/%s: %w", ns, name, err)
	}
	return nil
}
