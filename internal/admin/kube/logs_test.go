package kube

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	dfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	v1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/admin/logs"
	"orbitjob/internal/core/app/execution"
)

func TestLogSnapshotOwnershipAndBoundaries(t *testing.T) {
	for _, scenario := range []string{"ready", "long-name", "annotation-mismatch", "job-replaced", "pod-foreign", "label-mismatch", "run-replaced", "wrong-tenant", "starting", "cleaned", "unavailable", "legacy-owner", "job-owner-foreign", "pod-replaced-during-read", "failed-without-pod", "no-pod", "inactive-failure-without-pod", "pod-vanished-during-read", "pod-vanished-after-read", "log-forbidden-with-failure"} {
		t.Run(scenario, func(t *testing.T) {
			controller := true
			labels := map[string]string{execution.LabelRunName: "task-key", execution.LabelRunUID: "run-uid", execution.LabelRevisionID: "9", execution.LabelAttempt: "2"}
			job := batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "job", Namespace: "own", UID: types.UID("job-uid"), Labels: labels, OwnerReferences: []metav1.OwnerReference{{APIVersion: "workloads.orbitjob.io/v1alpha1", Kind: "JobRun", Name: "task-key", UID: "run-uid", Controller: &controller}}}}
			pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "own", UID: types.UID("pod-uid"), Labels: labels, OwnerReferences: []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "Job", Name: "job", UID: "job-uid", Controller: &controller}}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "task"}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
			runName := "task-key"
			if scenario == "long-name" || scenario == "annotation-mismatch" {
				runName = v1.RunObjectName("task", "occurrence")
				labels[execution.LabelRunName] = execution.RunLabel(runName)
				job.Annotations = map[string]string{execution.AnnotationRunName: runName}
				job.OwnerReferences[0].Name = runName
				if scenario == "annotation-mismatch" {
					job.Annotations[execution.AnnotationRunName] = "another-run"
				}
			}
			if scenario == "legacy-owner" {
				job.OwnerReferences = nil
			}
			if scenario == "job-owner-foreign" {
				job.OwnerReferences[0].UID = "other-run"
			}
			if scenario == "job-replaced" {
				job.UID = "new-job"
			}
			if scenario == "pod-foreign" {
				pod.OwnerReferences[0].UID = "foreign-job"
			}
			if scenario == "label-mismatch" {
				pod.Labels = map[string]string{}
			}
			if scenario == "starting" {
				pod.Status.Phase = corev1.PodPending
			}
			if scenario == "failed-without-pod" || scenario == "inactive-failure-without-pod" || scenario == "job-owner-foreign" || scenario == "pod-vanished-during-read" || scenario == "pod-vanished-after-read" || scenario == "log-forbidden-with-failure" {
				status := corev1.ConditionTrue
				if scenario == "inactive-failure-without-pod" {
					status = corev1.ConditionFalse
				}
				job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: status, Reason: "DeadlineExceeded", Message: "Job was active longer than specified deadline"}}
			}
			requestedLogs := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/apis/batch/v1/namespaces/own/jobs/job":
					if scenario == "cleaned" {
						w.WriteHeader(404)
						_, _ = io.WriteString(w, `{"kind":"Status","apiVersion":"v1","reason":"NotFound","code":404}`)
						return
					}
					_ = jsonWrite(w, job)
				case "/api/v1/namespaces/own/pods":
					if r.URL.Query().Get("labelSelector") != "batch.kubernetes.io/controller-uid=job-uid" {
						t.Error("unbounded Pod selector")
					}
					if strings.HasSuffix(scenario, "without-pod") || scenario == "no-pod" {
						_ = jsonWrite(w, corev1.PodList{})
					} else {
						_ = jsonWrite(w, corev1.PodList{Items: []corev1.Pod{pod}})
					}
				case "/api/v1/namespaces/own/pods/pod":
					if scenario == "pod-vanished-after-read" {
						w.WriteHeader(404)
						_, _ = io.WriteString(w, `{"kind":"Status","apiVersion":"v1","reason":"NotFound","code":404}`)
						return
					}
					if scenario == "pod-replaced-during-read" {
						pod.UID = "new-pod"
					}
					_ = jsonWrite(w, pod)
				case "/api/v1/namespaces/own/pods/pod/log":
					requestedLogs = true
					if scenario == "pod-vanished-during-read" || scenario == "log-forbidden-with-failure" {
						code, reason := 404, "NotFound"
						if scenario == "log-forbidden-with-failure" {
							code, reason = 403, "Forbidden"
						}
						w.WriteHeader(code)
						_ = jsonWrite(w, metav1.Status{TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"}, Reason: metav1.StatusReason(reason), Code: int32(code)})
						return
					}
					if r.URL.Query().Get("container") != "task" || r.URL.Query().Get("limitBytes") != "9" || r.URL.Query().Get("tailLines") != "5" || r.URL.Query().Get("follow") == "true" {
						t.Errorf("unexpected log query: %s", r.URL.RawQuery)
					}
					if scenario == "unavailable" {
						w.WriteHeader(503)
						return
					}
					w.Header().Set("Content-Type", "text/plain")
					_, _ = io.WriteString(w, "123456789abcdef")
				default:
					t.Errorf("unexpected kube request: %s", r.URL.String())
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			run := &v1.JobRun{TypeMeta: metav1.TypeMeta{APIVersion: v1.GroupVersion.String(), Kind: "JobRun"}, ObjectMeta: metav1.ObjectMeta{Name: "task-key", Namespace: "own", UID: "run-uid"}, Spec: v1.JobRunSpec{ScheduledJobRef: v1.ObjectReference{Name: "task", UID: "source-uid"}, DefinitionRevision: 9, OccurrenceKey: "occurrence"}}
			run.Name = runName
			if scenario == "run-replaced" {
				run.UID = "new-run"
			}
			dyn := dfake.NewSimpleDynamicClient(runtime.NewScheme())
			raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(run)
			if err != nil {
				t.Fatal(err)
			}
			_, err = dyn.Resource(jobRunGVR).Namespace("own").Create(context.Background(), &unstructured.Unstructured{Object: raw}, metav1.CreateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			adapter := LogClient{Client: client, Dynamic: dyn, NamespaceTenants: map[string]string{"own": "tenant"}}
			loc := logs.Locator{TenantID: "tenant", Namespace: "own", SourceUID: "source-uid", SourceName: "task", RevisionID: 9, OccurrenceKey: "occurrence", JobName: "job", JobUID: "job-uid", Attempt: 2}
			if scenario == "wrong-tenant" {
				loc.TenantID = "foreign"
			}
			out, err := adapter.Read(context.Background(), loc, logs.Request{TailLines: 5, LimitBytes: 8})
			denied := strings.Contains(scenario, "replaced") || scenario == "annotation-mismatch" || scenario == "pod-foreign" || scenario == "label-mismatch" || scenario == "wrong-tenant" || scenario == "job-owner-foreign" || scenario == "pod-replaced-during-read"
			if scenario == "log-forbidden-with-failure" {
				if err == nil || strings.Contains(out.Reason, "DeadlineExceeded") || out.Text != "" {
					t.Fatalf("forbidden read released diagnostics: %+v %v", out, err)
				}
				return
			}
			if denied {
				if err == nil || (requestedLogs && scenario != "pod-replaced-during-read") {
					t.Fatalf("ownership denial failed: out=%+v err=%v logs=%v", out, err, requestedLogs)
				}
				if strings.Contains(out.Reason, "DeadlineExceeded") {
					t.Fatal("foreign Job diagnostics released before ownership validation")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "ready", "long-name":
				if out.State != "ready" || out.Text != "12345678" || !out.Truncated {
					t.Fatalf("unbounded/bad snapshot: %+v", out)
				}
			case "starting":
				if out.State != "starting" || requestedLogs {
					t.Fatalf("starting: %+v", out)
				}
			case "cleaned":
				if out.State != "absent" || requestedLogs {
					t.Fatalf("absent: %+v", out)
				}
			case "failed-without-pod":
				if out.State != "absent" || out.Text != "" || requestedLogs || out.Reason != "no retained Pod for this attempt. Kubernetes Job failed: DeadlineExceeded: Job was active longer than specified deadline" {
					t.Fatalf("missing Pod hid retained Job failure: %+v", out)
				}
			case "pod-vanished-during-read", "pod-vanished-after-read":
				if out.State != "absent" || out.Text != "" || !requestedLogs || out.Reason != "Pod logs are no longer retained. Kubernetes Job failed: DeadlineExceeded: Job was active longer than specified deadline" {
					t.Fatalf("Pod deletion race hid retained Job failure: %+v", out)
				}
			case "no-pod", "inactive-failure-without-pod":
				if out.State != "absent" || out.Reason != "no retained Pod for this attempt" || out.Text != "" || requestedLogs {
					t.Fatalf("missing Pod must not imply an unobserved failure: %+v", out)
				}
			case "legacy-owner":
				if out.State != "unavailable" || requestedLogs {
					t.Fatalf("legacy owner: %+v", out)
				}
			case "unavailable":
				if out.State != "unavailable" {
					t.Fatalf("unavailable: %+v", out)
				}
			}
		})
	}
}
func jsonWrite(w io.Writer, v any) error { return json.NewEncoder(w).Encode(v) }
