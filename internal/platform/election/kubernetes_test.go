package election

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func leaseForTest(name, holder string, at time.Time) *coordinationv1.Lease {
	duration := int32(1)
	stamp := metav1.NewMicroTime(at)
	return &coordinationv1.Lease{ObjectMeta: metav1.ObjectMeta{Name: leaseNamePrefix + name, Namespace: "test"},
		Spec: coordinationv1.LeaseSpec{HolderIdentity: &holder, LeaseDurationSeconds: &duration, RenewTime: &stamp}}
}
func testConfig(client *fake.Clientset) KubernetesConfig {
	return KubernetesConfig{Namespace: "test", Identity: "pod", Client: client, LeaseDuration: time.Second,
		RenewDeadline: 150 * time.Millisecond, RetryPeriod: 20 * time.Millisecond}
}
func TestConfigDefaultsAndOfficialValidation(t *testing.T) {
	if _, err := (KubernetesConfig{}).withDefaults(); err == nil {
		t.Fatal("namespace required")
	}
	cfg, err := (KubernetesConfig{Namespace: "test", Identity: "pod"}).withDefaults()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LeaseDuration != 15*time.Second || cfg.RenewDeadline != 10*time.Second || cfg.RetryPeriod != 2*time.Second {
		t.Fatalf("defaults: %+v", cfg)
	}
	cfg.RenewDeadline = 2 * time.Second
	cfg.RetryPeriod = 1900 * time.Millisecond
	if _, err := cfg.withDefaults(); err == nil {
		t.Fatal("official jitter constraint must be validated")
	}
}
func TestCampaignDoesNotTrustRemoteTimestamp(t *testing.T) {
	for _, offset := range []time.Duration{-time.Hour, time.Hour} {
		t.Run(offset.String(), func(t *testing.T) {
			client := fake.NewSimpleClientset(leaseForTest("operator", "other", time.Now().Add(offset)))
			var n atomic.Int64
			client.PrependReactor("get", "leases", func(action k8stesting.Action) (bool, runtime.Object, error) {
				// A live holder renews continually, despite arbitrary timestamp offset.
				return true, leaseForTest("operator", "other", time.Now().Add(offset).Add(time.Duration(n.Add(1)))), nil
			})
			c, err := NewKubernetes(testConfig(client))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = c.Close() }()
			ctx, cancel := context.WithTimeout(t.Context(), 180*time.Millisecond)
			defer cancel()
			leader, err := c.Campaign(ctx, "operator")
			if leader != nil || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("remote clock stole authority: %v %v", leader, err)
			}
			for _, a := range client.Actions() {
				if a.GetVerb() == "update" {
					t.Fatal("live foreign lease was updated")
				}
			}
		})
	}
}
func TestStableRecordWaitsLocalLeaseDuration(t *testing.T) {
	client := fake.NewSimpleClientset(leaseForTest("operator", "other", time.Now().Add(-time.Hour)))
	c, err := NewKubernetes(testConfig(client))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	start := time.Now()
	leader, err := c.Campaign(ctx, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < time.Second || leader.Err() != nil {
		t.Fatal("must wait locally observed lease duration")
	}
}
func TestCampaignLifecycleAndNoEarlyRelease(t *testing.T) {
	client := fake.NewSimpleClientset()
	c, err := NewKubernetes(testConfig(client))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	leader, err := c.Campaign(ctx, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Campaign(ctx, "operator"); err == nil {
		t.Fatal("overlapping campaign accepted")
	}
	cancel()
	select {
	case <-leader.Done():
	case <-time.After(time.Second):
		t.Fatal("leadership not canceled")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Campaign(t.Context(), "operator"); err == nil {
		t.Fatal("closed coordinator accepted campaign")
	}
	lease, err := client.CoordinationV1().Leases("test").Get(t.Context(), leaseNamePrefix+"operator", metav1.GetOptions{})
	if err != nil || lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity == "" {
		t.Fatalf("lease released before workers drain: %v %v", lease, err)
	}
	if len(c.(*kubernetesCoordinator).sessions) != 0 {
		t.Fatal("session leaked")
	}
}
func TestRenewalFailureCancelsAndCampaignCanRestart(t *testing.T) {
	client := fake.NewSimpleClientset()
	var fail atomic.Bool
	client.PrependReactor("update", "leases", func(k8stesting.Action) (bool, runtime.Object, error) {
		if fail.Load() {
			return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "leases"}, "operator", errors.New("partition"))
		}
		return false, nil, nil
	})
	c, err := NewKubernetes(testConfig(client))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	leader, err := c.Campaign(t.Context(), "operator")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := client.CoordinationV1().Leases("test").Get(t.Context(), leaseNamePrefix+"operator", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	first := *lease.Spec.HolderIdentity
	fail.Store(true)
	select {
	case <-leader.Done():
	case <-time.After(700 * time.Millisecond):
		t.Fatal("failed renewal kept authority")
	}
	k := c.(*kubernetesCoordinator)
	deadline := time.Now().Add(time.Second)
	for {
		k.mu.Lock()
		remaining := len(k.sessions)
		k.mu.Unlock()
		if remaining == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("session not reclaimed")
		}
		time.Sleep(time.Millisecond)
	}
	fail.Store(false)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if _, err := c.Campaign(ctx, "operator"); err != nil {
		t.Fatal(err)
	}
	lease, err = client.CoordinationV1().Leases("test").Get(t.Context(), lease.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if first == *lease.Spec.HolderIdentity {
		t.Fatal("new term reused holder token")
	}
}
func TestCloseWhileNeverElectedJoinsWaiter(t *testing.T) {
	client := fake.NewSimpleClientset(leaseForTest("operator", "other", time.Now()))
	c, err := NewKubernetes(testConfig(client))
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := c.Campaign(context.Background(), "operator"); result <- err }()
	deadline := time.Now().Add(time.Second)
	for len(client.Actions()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("campaign did not start")
		}
		time.Sleep(time.Millisecond)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("never-elected session returned authority")
		}
	case <-time.After(time.Second):
		t.Fatal("campaign leaked")
	}
}
func TestCampaignRequiresNameAndRespectsCanceledContext(t *testing.T) {
	c, err := NewKubernetes(testConfig(fake.NewSimpleClientset()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if _, err := c.Campaign(t.Context(), ""); err == nil {
		t.Fatal("empty name accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Campaign(ctx, "operator"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context: %v", err)
	}
}
