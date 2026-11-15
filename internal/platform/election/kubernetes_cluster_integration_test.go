//go:build integration

package election

import (
	"context"
	"crypto/rand"
	"os"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// The caller must supply a disposable cluster and an already-created namespace.
// This test creates only its uniquely named Lease and removes only that object.
// It validates real API failover, not operator scheduling or business recovery.
func TestKubernetesLeadershipFailover(t *testing.T) {
	kubeconfig, namespace := os.Getenv("ELECTION_TEST_KUBECONFIG"), os.Getenv("ELECTION_TEST_NAMESPACE")
	if kubeconfig == "" || namespace == "" {
		t.Fatal("explicit disposable ELECTION_TEST_KUBECONFIG and ELECTION_TEST_NAMESPACE are required")
	}
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		t.Fatal(err)
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	name := "acceptance-" + strings.ToLower(rand.Text())
	cfg := KubernetesConfig{Namespace: namespace, Identity: "first", Client: client, LeaseDuration: 3 * time.Second, RenewDeadline: time.Second, RetryPeriod: 200 * time.Millisecond}
	first, err := NewKubernetes(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	cfg.Identity = "second"
	second, err := NewKubernetes(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	// Clean up after both electors stop; never delete a possibly live Lease.
	defer func() {
		_ = first.Close()
		_ = second.Close()
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		lease, err := client.CoordinationV1().Leases(namespace).Get(cleanup, leaseNamePrefix+name, metav1.GetOptions{})
		if err == nil {
			_ = client.CoordinationV1().Leases(namespace).Delete(cleanup, lease.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &lease.UID, ResourceVersion: &lease.ResourceVersion}})
		}
	}()
	ctx, stop := context.WithTimeout(t.Context(), 20*time.Second)
	defer stop()
	leader, err := first.Campaign(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := second.Campaign(ctx, name); result <- err }()
	select {
	case err := <-result:
		t.Fatalf("candidate returned before first leader stopped: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	if leader.Err() == nil {
		t.Fatal("closed first leader still active")
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("real API successor failed to acquire")
	}
	lease, err := client.CoordinationV1().Leases(namespace).Get(ctx, leaseNamePrefix+name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if lease.Spec.HolderIdentity == nil || !strings.HasPrefix(*lease.Spec.HolderIdentity, "second_") {
		t.Fatal("successor Lease does not name the second holder")
	}
	t.Logf("Lease %s/%s transferred to %s at resourceVersion %s", namespace, lease.Name, *lease.Spec.HolderIdentity, lease.ResourceVersion)
}
