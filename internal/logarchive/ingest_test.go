package logarchive

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	dfake "k8s.io/client-go/dynamic/fake"
	kfake "k8s.io/client-go/kubernetes/fake"
	kt "k8s.io/client-go/testing"
	api "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/execution"
)

func collectorRequest(in Input) *http.Request {
	body, _ := json.Marshal(in)
	r := httptest.NewRequest(http.MethodPost, "/v1/ingest", strings.NewReader(string(body)))
	r.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{{}}}, PeerCertificates: []*x509.Certificate{{Subject: pkix.Name{CommonName: "orbitjob-log-collector"}}}}
	return r
}

func sourceInput(podUID string, text string) Input {
	return Input{ID: uuid.NewString(), File: "/var/log/pods/own_pod_" + podUID + "/task/0.log", Log: time.Now().UTC().Format(time.RFC3339Nano) + " stdout F " + text}
}

func TestCollectorDependencyFailureRetries(t *testing.T) {
	now := time.Now()
	s := openTest(t, &now)
	k := kfake.NewSimpleClientset()
	k.PrependReactor("get", "pods", func(kt.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewServiceUnavailable("transient")
	})
	o := &Observer{Store: s, Kube: k, Namespaces: map[string]string{"own": "tenant"}}
	w := httptest.NewRecorder()
	o.ingestHTTP(w, collectorRequest(sourceInput(uuid.NewString(), "must retry")))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("transient failure ACKed: %d %s", w.Code, w.Body.String())
	}
}

func TestCollectorVerifiesPinnedRevisionBeforeRegistration(t *testing.T) {
	now := time.Now()
	s := openTest(t, &now)
	name := api.RunObjectName("hello", uuid.NewString())
	runUID := uuid.NewString()
	job := execution.BuildJob(execution.Identity{RunName: name, RunUID: runUID, RevisionID: "11", Attempt: 3}, "own", execution.Template{Image: "busybox:1.37"})
	job.UID = types.UID(uuid.NewString())
	controller := true
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "own", UID: types.UID(uuid.NewString()), Labels: job.Spec.Template.Labels, OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: job.Name, UID: job.UID, Controller: &controller}}}, Spec: job.Spec.Template.Spec}
	run := &api.JobRun{TypeMeta: metav1.TypeMeta{APIVersion: api.GroupVersion.String(), Kind: "JobRun"}, ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "own", UID: types.UID(runUID), CreationTimestamp: metav1.NewTime(now)}, Spec: api.JobRunSpec{ScheduledJobRef: api.ObjectReference{Name: "hello", UID: uuid.NewString()}, DefinitionRevision: 9, OccurrenceKey: uuid.NewString(), Trigger: api.Trigger("Manual")}}
	scheme := runtime.NewScheme()
	if e := api.AddToScheme(scheme); e != nil {
		t.Fatal(e)
	}
	o := &Observer{Store: s, Kube: kfake.NewSimpleClientset(job, pod), Dynamic: dfake.NewSimpleDynamicClient(scheme, run), Namespaces: map[string]string{"own": "tenant"}}
	if _, _, e := o.ResolvePod(context.Background(), "own", "pod", string(pod.UID), 0); e == nil {
		t.Fatal("accepted Job revision differing from immutable run")
	}
}

func TestLateCollectorOutputRemainsReadable(t *testing.T) {
	now := time.Now()
	s := openTest(t, &now)
	id := identity(1)
	if e := s.Register(id); e != nil {
		t.Fatal(e)
	}
	podUID := uuid.NewString()
	source := Source{Namespace: "own", PodName: "pod", PodUID: podUID, JobUID: uuid.NewString(), Attempt: 1, Container: "task"}
	if e := s.Bind(id, source); e != nil {
		t.Fatal(e)
	}
	if e := s.Complete(id, "Succeeded", now); e != nil {
		t.Fatal(e)
	}
	if e := s.Seal(id); e != nil {
		t.Fatal(e)
	}
	o := &Observer{Store: s}
	w := httptest.NewRecorder()
	o.ingestHTTP(w, collectorRequest(sourceInput(podUID, "late buffered output")))
	p, e := s.Read(id, Query{})
	if w.Code != http.StatusOK || e != nil || len(p.Records) != 1 || !strings.Contains(p.Records[0].Text, "late buffered output") {
		t.Fatalf("late output lost: HTTP%d %+v %v", w.Code, p, e)
	}
}

func TestManifestCapacityPersistsLossAcrossRestart(t *testing.T) {
	now := time.Now()
	l := testLimits(&now)
	l.ManifestBytes = 4096
	directory := t.TempDir()
	s, e := Open(directory, l)
	if e != nil {
		t.Fatal(e)
	}
	id := identity(1)
	if e = s.Register(id); e != nil {
		t.Fatal(e)
	}
	podUID := uuid.NewString()
	if e = s.Bind(id, Source{Namespace: "own", PodName: "pod", PodUID: podUID, JobUID: uuid.NewString(), Attempt: 1, Container: "task"}); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 200; i++ {
		e = s.Append(id, []Record{{ID: uuid.NewString(), Kind: "stdout", Text: fmt.Sprint(i)}})
		if e != nil {
			break
		}
	}
	if e != ErrCapacity {
		t.Fatalf("did not reach controlled metadata budget: %v", e)
	}
	o := &Observer{Store: s}
	w := httptest.NewRecorder()
	o.ingestHTTP(w, collectorRequest(sourceInput(podUID, "cannot fit")))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"rejected":1`) {
		t.Fatalf("capacity refusal misreported: %d %s", w.Code, w.Body.String())
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(directory, l)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = s.Close() }()
	p, e := s.Read(id, Query{})
	if e != nil || p.State != "partial" || len(p.Gaps) == 0 {
		t.Fatalf("persisted loss marker missing: %+v %v", p, e)
	}
}

func TestReplayAfterManyChunksIsIdempotent(t *testing.T) {
	now := time.Now()
	s := openTest(t, &now)
	id := identity(1)
	if e := s.Register(id); e != nil {
		t.Fatal(e)
	}
	first := []Record{{ID: uuid.NewString(), Kind: "stdout", Text: "unique archived needle"}}
	if e := s.AppendBatch(id, "first", first); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 300; i++ {
		if e := s.AppendBatch(id, fmt.Sprint("batch-", i), []Record{{ID: uuid.NewString(), Kind: "stdout", Text: "other output"}}); e != nil {
			t.Fatal(e)
		}
	}
	before, _ := s.Snapshot(id)
	if e := s.AppendBatch(id, "first", first); e != nil {
		t.Fatal(e)
	}
	p, e := s.Read(id, Query{Search: "unique archived needle"})
	after, _ := s.Snapshot(id)
	if e != nil || len(p.Records) != 1 || after.OutputBytes != before.OutputBytes {
		t.Fatalf("old chunk replay changed archive: %+v %v", p, e)
	}
}
