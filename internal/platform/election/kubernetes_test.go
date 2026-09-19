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

func micro(t time.Time) *metav1.MicroTime {
	m := metav1.NewMicroTime(t.UTC())
	return &m
}

func seconds(v int32) *int32 { return &v }

func ptrStr(v string) *string { return &v }

func leaseWith(holder string, renewAt time.Time, durationSeconds int32) *coordinationv1.Lease {
	return namedLease("operator", holder, renewAt, durationSeconds)
}

func namedLease(name, holder string, renewAt time.Time, durationSeconds int32) *coordinationv1.Lease {
	return &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{Name: leaseNamePrefix + name, Namespace: "orbitjob-system"},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity:       &holder,
			RenewTime:            micro(renewAt),
			LeaseDurationSeconds: seconds(durationSeconds),
		},
	}
}

func TestConfigDefaultsAndValidation(t *testing.T) {
	if _, err := (KubernetesConfig{}).withDefaults(); err == nil {
		t.Fatal("namespace is required")
	}
	resolved, err := KubernetesConfig{Namespace: "orbitjob-system", Identity: "pod-0"}.withDefaults()
	if err != nil {
		t.Fatal(err)
	}
	if resolved.LeaseDuration != 15*time.Second || resolved.RenewDeadline != 10*time.Second || resolved.RetryPeriod != 2*time.Second {
		t.Fatalf("unexpected defaults: %+v", resolved)
	}
	// A renew deadline at or beyond the lease duration lets a stale holder keep
	// believing it is leader after another instance took over.
	if _, err := (KubernetesConfig{
		Namespace: "ns", Identity: "x", LeaseDuration: 5 * time.Second, RenewDeadline: 5 * time.Second,
	}).withDefaults(); err == nil {
		t.Fatal("renew deadline must be shorter than lease duration")
	}
}

func TestLeaseIsFree(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		lease    *coordinationv1.Lease
		identity string
		want     bool
	}{
		{"never held", &coordinationv1.Lease{}, "pod-0", true},
		{"held by us", leaseWith("pod-0", now, 15), "pod-0", true},
		{"held by another and fresh", leaseWith("pod-1", now.Add(-time.Second), 15), "pod-0", false},
		{"held by another and expired", leaseWith("pod-1", now.Add(-time.Minute), 15), "pod-0", true},
		{"no renew time recorded", &coordinationv1.Lease{Spec: coordinationv1.LeaseSpec{HolderIdentity: ptrStr("pod-1")}}, "pod-0", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := leaseIsFree(tt.lease, now, tt.identity); got != tt.want {
				t.Fatalf("free = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCampaignTakesExpiredLease(t *testing.T) {
	client := fake.NewSimpleClientset(leaseWith("pod-1", time.Now().Add(-time.Hour), 15))
	coordinator, err := NewKubernetes(KubernetesConfig{
		Namespace: "orbitjob-system", Identity: "pod-0", Client: client,
		RetryPeriod: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = coordinator.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	leaderCtx, err := coordinator.Campaign(ctx, "operator")
	if err != nil {
		t.Fatalf("expected to take over an expired lease: %v", err)
	}
	if leaderCtx.Err() != nil {
		t.Fatal("leader context must be live")
	}

	lease, err := client.CoordinationV1().Leases("orbitjob-system").Get(ctx, leaseNamePrefix+"operator", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity != "pod-0" {
		t.Fatalf("holder = %v", lease.Spec.HolderIdentity)
	}
}

func TestTryLockReportsLockedWhenAnotherHoldsTheLease(t *testing.T) {
	client := fake.NewSimpleClientset(namedLease("dispatch", "pod-1", time.Now(), 60))
	coordinator, err := NewKubernetes(KubernetesConfig{
		Namespace: "orbitjob-system", Identity: "pod-0", Client: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = coordinator.Close() }()

	if _, err := coordinator.TryLock(context.Background(), "dispatch"); !errors.Is(err, ErrLocked) {
		t.Fatalf("error = %v, want ErrLocked", err)
	}
}

func TestTryLockReleasesOnUnlock(t *testing.T) {
	client := fake.NewSimpleClientset()
	coordinator, err := NewKubernetes(KubernetesConfig{
		Namespace: "orbitjob-system", Identity: "pod-0", Client: client,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = coordinator.Close() }()

	unlock, err := coordinator.TryLock(context.Background(), "dispatch")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.TryLock(context.Background(), "dispatch"); err != nil {
		t.Fatalf("the same holder must be able to re-enter: %v", err)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	// Idempotent: a second release must not error.
	if err := unlock(); err != nil {
		t.Fatalf("unlock is not idempotent: %v", err)
	}
	if _, err := client.CoordinationV1().Leases("orbitjob-system").Get(
		context.Background(), leaseNamePrefix+"dispatch", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("lease still present: %v", err)
	}
}

func TestCampaignStopsOnContextCancellation(t *testing.T) {
	// Another instance holds a fresh lease, so the campaign can never win.
	client := fake.NewSimpleClientset(leaseWith("pod-1", time.Now(), 600))
	coordinator, err := NewKubernetes(KubernetesConfig{
		Namespace: "orbitjob-system", Identity: "pod-0", Client: client,
		RetryPeriod: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = coordinator.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := coordinator.Campaign(ctx, "operator"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
}

func TestCloseIsIdempotentAndStopsCampaigns(t *testing.T) {
	client := fake.NewSimpleClientset()
	coordinator, err := NewKubernetes(KubernetesConfig{
		Namespace: "orbitjob-system", Identity: "pod-0", Client: client,
		RetryPeriod: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	leaderCtx, err := coordinator.Campaign(context.Background(), "operator")
	if err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Close(); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Close(); err != nil {
		t.Fatalf("close is not idempotent: %v", err)
	}
	select {
	case <-leaderCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("closing the coordinator must release leadership")
	}
	if _, err := coordinator.Campaign(context.Background(), "operator"); err == nil {
		t.Fatal("a closed coordinator must refuse new campaigns")
	}
}

func TestNewKubernetesRequiresNamespaceAndElectionName(t *testing.T) {
	if _, err := NewKubernetes(KubernetesConfig{}); err == nil {
		t.Fatal("expected namespace validation")
	}
	coordinator, err := NewKubernetes(KubernetesConfig{
		Namespace: "orbitjob-system", Identity: "pod-0", Client: fake.NewSimpleClientset(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = coordinator.Close() }()
	if _, err := coordinator.Campaign(context.Background(), ""); err == nil {
		t.Fatal("expected election name validation")
	}
	if _, err := coordinator.TryLock(context.Background(), ""); err == nil {
		t.Fatal("expected lock name validation")
	}
}

func TestTryAcquireSurfacesApiErrors(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("get", "leases", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "leases"}, "x", errors.New("denied"))
	})
	coordinator, err := NewKubernetes(KubernetesConfig{
		Namespace: "orbitjob-system", Identity: "pod-0", Client: client,
		RetryPeriod: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = coordinator.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// A permission error will never resolve by retrying, so it must fail fast
	// rather than spinning until the caller's deadline.
	if _, err := coordinator.Campaign(ctx, "operator"); err == nil {
		t.Fatal("expected the API error to surface")
	}
}

func TestAcquireCreatesLeaseWhenAbsent(t *testing.T) {
	client := fake.NewSimpleClientset()
	coordinator, err := NewKubernetes(KubernetesConfig{
		Namespace: "orbitjob-system", Identity: "pod-0", Client: client,
		RetryPeriod: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = coordinator.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := coordinator.Campaign(ctx, "operator"); err != nil {
		t.Fatal(err)
	}
	lease, err := client.CoordinationV1().Leases("orbitjob-system").Get(ctx, leaseNamePrefix+"operator", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if lease.Spec.LeaseDurationSeconds == nil || *lease.Spec.LeaseDurationSeconds != 15 {
		t.Fatalf("lease duration = %v", lease.Spec.LeaseDurationSeconds)
	}
}

func TestRenewLoopKeepsLeadershipWhileTheLeaseIsHeld(t *testing.T) {
	client := fake.NewSimpleClientset()
	coordinator, err := NewKubernetes(KubernetesConfig{
		Namespace: "orbitjob-system", Identity: "pod-0", Client: client,
		RetryPeriod: 5 * time.Millisecond, RenewDeadline: time.Second, LeaseDuration: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = coordinator.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leaderCtx, err := coordinator.Campaign(ctx, "operator")
	if err != nil {
		t.Fatal(err)
	}

	// Several renew intervals pass; leadership must survive them.
	time.Sleep(40 * time.Millisecond)
	if leaderCtx.Err() != nil {
		t.Fatal("leadership was lost while the lease was renewable")
	}
	lease, err := client.CoordinationV1().Leases("orbitjob-system").Get(ctx, leaseNamePrefix+"operator", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if lease.Spec.RenewTime == nil {
		t.Fatal("renew time was never refreshed")
	}
}

func TestRenewLoopSurvivesTransientApiErrorsWithinDeadline(t *testing.T) {
	client := fake.NewSimpleClientset()
	// The reactor is installed before the coordinator starts: the fake
	// clientset is not safe to reconfigure while a renew loop is running.
	var failing atomic.Bool
	failing.Store(true)
	client.PrependReactor("update", "leases", func(k8stesting.Action) (bool, runtime.Object, error) {
		if failing.Load() {
			return true, nil, errors.New("apiserver unavailable")
		}
		return false, nil, nil
	})

	coordinator, err := NewKubernetes(KubernetesConfig{
		Namespace: "orbitjob-system", Identity: "pod-0", Client: client,
		RetryPeriod: 5 * time.Millisecond, RenewDeadline: time.Second, LeaseDuration: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = coordinator.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leaderCtx, err := coordinator.Campaign(ctx, "operator")
	if err != nil {
		t.Fatal(err)
	}

	// Renewals fail for a while, then recover. An outage shorter than the renew
	// deadline must not cost leadership.
	time.Sleep(30 * time.Millisecond)
	failing.Store(false)
	time.Sleep(30 * time.Millisecond)

	if leaderCtx.Err() != nil {
		t.Fatal("leadership must survive an outage shorter than the renew deadline")
	}
}

func TestRenewLoopGivesUpAuthorityAfterRepeatedFailures(t *testing.T) {
	client := fake.NewSimpleClientset()
	// Every renewal fails from the outset, so leadership cannot be sustained.
	client.PrependReactor("update", "leases", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("apiserver unavailable")
	})
	coordinator, err := NewKubernetes(KubernetesConfig{
		Namespace: "orbitjob-system", Identity: "pod-0", Client: client,
		RetryPeriod: 2 * time.Millisecond, RenewDeadline: 10 * time.Millisecond, LeaseDuration: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = coordinator.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leaderCtx, err := coordinator.Campaign(ctx, "operator")
	if err != nil {
		t.Fatal(err)
	}

	// A leader that cannot renew must stop acting rather than keep working from
	// stale authority.
	select {
	case <-leaderCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("leadership was not surrendered after the renew deadline passed")
	}
}
