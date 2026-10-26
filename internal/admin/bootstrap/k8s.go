package bootstrap

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
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

// Write creates a Kubernetes Secret with the given name and string data. If a
// Secret with the same name already exists, Write verifies the requested data
// without modifying it. A stale or unrelated Secret is an explicit conflict.
func (w *K8sSecretWriter) Write(ctx context.Context, name string, data map[string]string) error {
	if w.Namespace == "" {
		return errors.New("kubernetes namespace is required")
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: w.Namespace,
			Labels: map[string]string{
				k8sManagedByLabelKey: k8sManagedByLabelValue,
			},
		},
		StringData: data,
		Type:       corev1.SecretTypeOpaque,
	}

	_, err := w.Client.CoreV1().Secrets(w.Namespace).Create(ctx, secret, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, getErr := w.Client.CoreV1().Secrets(w.Namespace).Get(ctx, name, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("read existing secret %s/%s: %w", w.Namespace, name, getErr)
		}
		for key, expected := range data {
			actual, present := existing.Data[key]
			if !present {
				value, ok := existing.StringData[key]
				actual, present = []byte(value), ok
			}
			if !present || string(actual) != expected {
				return fmt.Errorf("existing secret %s/%s has conflicting data", w.Namespace, name)
			}
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("create secret %s/%s: %w", w.Namespace, name, err)
	}
	return nil
}
