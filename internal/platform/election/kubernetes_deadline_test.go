package election

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestRenewalDeadlineCancelsHangingHTTPAuthority(t *testing.T) {
	stop := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","reason":"NotFound","code":404}`))
			return
		}
		if r.Method == http.MethodPut {
			_, _ = io.Copy(io.Discard, r.Body)
			select {
			case <-r.Context().Done():
			case <-stop:
			}
			return
		}
		var lease coordinationv1.Lease
		if err := json.NewDecoder(r.Body).Decode(&lease); err != nil {
			t.Error(err)
			return
		}
		lease.APIVersion, lease.Kind, lease.ResourceVersion = "coordination.k8s.io/v1", "Lease", "1"
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(lease)
	}))
	defer server.Close()
	defer close(stop)
	client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL, ContentConfig: rest.ContentConfig{ContentType: "application/json", AcceptContentTypes: "application/json"}})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewKubernetes(KubernetesConfig{Namespace: "test", Identity: "pod", Client: client, LeaseDuration: time.Second, RenewDeadline: 120 * time.Millisecond, RetryPeriod: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	leader, err := c.Campaign(t.Context(), "operator")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-leader.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("hanging HTTP extended leadership")
	}
}
func TestInitialCampaignTransportIsCancelable(t *testing.T) {
	stop := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-stop:
		}
	}))
	defer server.Close()
	defer close(stop)
	client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewKubernetes(KubernetesConfig{Namespace: "test", Identity: "pod", Client: client, LeaseDuration: time.Second, RenewDeadline: 100 * time.Millisecond, RetryPeriod: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	ctx, cancel := context.WithTimeout(t.Context(), 160*time.Millisecond)
	defer cancel()
	start := time.Now()
	leader, err := c.Campaign(ctx, "operator")
	if leader != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hanging initial acquisition: %v %v", leader, err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("initial transport ignored cancellation")
	}
}
