package election

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// This server models API storage concurrency, not election. Both independent
// clients execute the real client-go election and REST transport against it.
type leaseAPIFixture struct {
	mu             sync.Mutex
	lease          *coordinationv1.Lease
	version        int
	partitionFirst atomic.Bool
	writes         atomic.Int64
	deletes        atomic.Int64
}

func (f *leaseAPIFixture) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	status := func(code int, reason metav1.StatusReason) {
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(metav1.Status{TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"}, Status: "Failure", Reason: reason, Code: int32(code)})
	}
	if f.partitionFirst.Load() && r.Header.Get("Authorization") == "Bearer first" {
		status(503, metav1.StatusReasonServiceUnavailable)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method == http.MethodDelete {
		f.deletes.Add(1)
		status(405, metav1.StatusReasonMethodNotAllowed)
		return
	}
	if r.Method == http.MethodGet {
		if f.lease == nil {
			status(404, metav1.StatusReasonNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(f.lease)
		return
	}
	var candidate coordinationv1.Lease
	if err := json.NewDecoder(r.Body).Decode(&candidate); err != nil {
		status(400, metav1.StatusReasonBadRequest)
		return
	}
	if r.Method == http.MethodPost && f.lease != nil {
		status(409, metav1.StatusReasonAlreadyExists)
		return
	}
	if r.Method == http.MethodPut && (f.lease == nil || candidate.ResourceVersion != f.lease.ResourceVersion) {
		status(409, metav1.StatusReasonConflict)
		return
	}
	f.version++
	candidate.TypeMeta = metav1.TypeMeta{Kind: "Lease", APIVersion: "coordination.k8s.io/v1"}
	candidate.ResourceVersion = strconv.Itoa(f.version)
	f.lease = candidate.DeepCopy()
	f.writes.Add(1)
	if r.Method == http.MethodPost {
		w.WriteHeader(http.StatusCreated)
	}
	_ = json.NewEncoder(w).Encode(f.lease)
}
func httpElectionClient(t *testing.T, url, token string) kubernetes.Interface {
	t.Helper()
	client, err := kubernetes.NewForConfig(&rest.Config{Host: url, BearerToken: token, QPS: -1, ContentConfig: rest.ContentConfig{ContentType: "application/json", AcceptContentTypes: "application/json"}})
	if err != nil {
		t.Fatal(err)
	}
	return client
}
func TestIndependentHTTPClientsFailOverAfterPartitionAndRecover(t *testing.T) {
	fixture := &leaseAPIFixture{}
	server := httptest.NewServer(http.HandlerFunc(fixture.serve))
	defer server.Close()
	cfg := KubernetesConfig{Namespace: "test", LeaseDuration: time.Second, RenewDeadline: 150 * time.Millisecond, RetryPeriod: 20 * time.Millisecond}
	cfg.Identity = "first"
	cfg.Client = httpElectionClient(t, server.URL, "first")
	first, err := NewKubernetes(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	cfg.Identity = "second"
	cfg.Client = httpElectionClient(t, server.URL, "second")
	second, err := NewKubernetes(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	leader, err := first.Campaign(ctx, "operator")
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		ctx context.Context
		err error
	}
	successor := make(chan result, 1)
	go func() { c, err := second.Campaign(ctx, "operator"); successor <- result{c, err} }()
	// Make sure the first leader has renewed, rather than merely created a row.
	deadline := time.Now().Add(time.Second)
	for fixture.writes.Load() < 3 {
		if time.Now().After(deadline) {
			t.Fatal("leader did not renew")
		}
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case <-successor:
		t.Fatal("candidate stole live leader")
	default:
	}
	fixture.partitionFirst.Store(true)
	select {
	case <-leader.Done():
	case <-time.After(600 * time.Millisecond):
		t.Fatal("partitioned leader kept authority")
	}
	var next result
	select {
	case next = <-successor:
	case <-ctx.Done():
		t.Fatal("successor did not take over")
	}
	if next.err != nil || next.ctx.Err() != nil {
		t.Fatalf("successor: %v", next.err)
	}
	fixture.partitionFirst.Store(false)
	// The recovered process must observe the successor's progress, not reuse its
	// old holder identity or declare itself leader from a stale local record.
	probe, stopProbe := context.WithTimeout(ctx, 180*time.Millisecond)
	defer stopProbe()
	renewed, err := first.Campaign(probe, "operator")
	if renewed != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("old term bypassed new holder: %v %v", renewed, err)
	}
	fixture.mu.Lock()
	holder := *fixture.lease.Spec.HolderIdentity
	fixture.mu.Unlock()
	if !strings.HasPrefix(holder, "second_") {
		t.Fatalf("unexpected holder: %s", holder)
	}
	if fixture.deletes.Load() != 0 {
		t.Fatal("a leadership transition deleted the Lease")
	}
}
func TestImmediateCloseAndCampaignRacesLeaveNoSessions(t *testing.T) {
	for i := 0; i < 20; i++ {
		fixture := &leaseAPIFixture{}
		server := httptest.NewServer(http.HandlerFunc(fixture.serve))
		cfg := KubernetesConfig{Namespace: "test", Identity: "racer", Client: httpElectionClient(t, server.URL, "race"), LeaseDuration: time.Second, RenewDeadline: 100 * time.Millisecond, RetryPeriod: 10 * time.Millisecond}
		c, err := NewKubernetes(cfg)
		if err != nil {
			server.Close()
			t.Fatal(err)
		}
		result := make(chan context.Context, 1)
		go func() { leader, _ := c.Campaign(t.Context(), "race"); result <- leader }()
		closed := make(chan struct{})
		go func() { _ = c.Close(); close(closed) }()
		if err = c.Close(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-closed:
		case <-time.After(time.Second):
			t.Fatal("concurrent Close blocked")
		}
		select {
		case leader := <-result:
			if leader != nil && leader.Err() == nil {
				t.Fatal("closed coordinator returned live authority")
			}
		case <-time.After(time.Second):
			t.Fatal("Campaign leaked")
		}
		k := c.(*kubernetesCoordinator)
		k.mu.Lock()
		remaining := len(k.sessions)
		k.mu.Unlock()
		server.Close()
		if remaining != 0 {
			t.Fatal("session map leaked")
		}
	}
}
