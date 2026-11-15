package execution

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestJobCreateDoesNotReplayRetryAfterResponse(t *testing.T) {
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost || req.URL.Path != "/apis/batch/v1/namespaces/finance/jobs" {
			t.Errorf("unexpected request: %s %s", req.Method, req.URL.Path)
		}
		posts.Add(1)
		w.Header().Set("Retry-After", "0")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: "Failure", Reason: metav1.StatusReasonServiceUnavailable, Message: "create outcome unavailable", Code: 503})
	}))
	defer server.Close()
	client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = (KubernetesJobClient{Client: client.BatchV1()}).Create(t.Context(), &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "billing-extract", Namespace: "finance"}}, metav1.CreateOptions{})
	if !apierrors.IsServiceUnavailable(err) || posts.Load() != 1 {
		t.Fatalf("possibly accepted POST replayed: requests=%d err=%v", posts.Load(), err)
	}
}
