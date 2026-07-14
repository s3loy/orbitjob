package container

import (
	"context"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"orbitjob/internal/core/app/execute"
)

func TestHandlerCreatesSecureJobAndCompletes(t *testing.T) {
	client := fake.NewSimpleClientset()
	h := New(Config{
		Client:           client,
		Namespace:        "orbitjob-tasks",
		PollInterval:     time.Millisecond,
		TTLAfterFinished: time.Minute,
		RequireDigest:    true,
	})
	task := containerTask(map[string]any{
		"image":   "busybox@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"command": []any{"/bin/true"},
	})

	done := make(chan execute.Result, 1)
	go func() { done <- h.Execute(context.Background(), task) }()

	var job *batchv1.Job
	for i := 0; i < 100; i++ {
		jobs, err := client.BatchV1().Jobs("orbitjob-tasks").List(context.Background(), metav1.ListOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(jobs.Items) == 1 {
			job = &jobs.Items[0]
			break
		}
		time.Sleep(time.Millisecond)
	}
	if job == nil {
		t.Fatal("Kubernetes Job was not created")
	}

	container := job.Spec.Template.Spec.Containers[0]
	if container.Image != task.HandlerPayload["image"] {
		t.Fatalf("expected image %q, got %q", task.HandlerPayload["image"], container.Image)
	}
	security := container.SecurityContext
	if security == nil || security.RunAsNonRoot == nil || !*security.RunAsNonRoot {
		t.Fatal("expected runAsNonRoot=true")
	}
	if security.AllowPrivilegeEscalation == nil || *security.AllowPrivilegeEscalation {
		t.Fatal("expected allowPrivilegeEscalation=false")
	}
	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 {
		t.Fatalf("expected backoffLimit=0, got %v", job.Spec.BackoffLimit)
	}

	job.Status.Conditions = []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: "True"}}
	if _, err := client.BatchV1().Jobs(job.Namespace).UpdateStatus(context.Background(), job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	select {
	case result := <-done:
		if !result.Success || result.ResultCode != "completed" {
			t.Fatalf("unexpected result: %+v", result)
		}
	case <-time.After(time.Second):
		t.Fatal("handler did not finish")
	}
}

func TestParsePayloadQualificationFields(t *testing.T) {
	got, err := parsePayload(map[string]any{
		"image": "registry.example/tool:1@sha256:" + strings.Repeat("a", 64),
		"env":   map[string]any{"CASE_ID": "data-json-0001"},
		"resources": map[string]any{
			"requests": map[string]any{"cpu": "25m", "memory": "32Mi"},
			"limits":   map[string]any{"cpu": "250m", "memory": "128Mi"},
		},
		"automount_service_account_token": true,
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Env) != 1 || got.Env[0].Name != "CASE_ID" || got.Env[0].Value != "data-json-0001" {
		t.Fatalf("env = %#v", got.Env)
	}
	if got.Resources.Requests.Cpu().String() != "25m" || got.Resources.Limits.Memory().String() != "128Mi" {
		t.Fatalf("resources = %#v", got.Resources)
	}
	if !got.AutomountServiceAccountToken {
		t.Fatal("automount token should be enabled")
	}
}

func TestParsePayloadDefaultsTokenAutomountOff(t *testing.T) {
	got, err := parsePayload(map[string]any{"image": "alpine:3.22"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.AutomountServiceAccountToken {
		t.Fatal("token automount must default to false")
	}
}

func TestParsePayloadRejectsResourceRequestAboveLimit(t *testing.T) {
	_, err := parsePayload(map[string]any{
		"image": "alpine:3.22",
		"resources": map[string]any{
			"requests": map[string]any{"cpu": "500m", "memory": "64Mi"},
			"limits":   map[string]any{"cpu": "100m", "memory": "64Mi"},
		},
	}, false)
	if err == nil || !strings.Contains(err.Error(), "cpu request exceeds limit") {
		t.Fatalf("error = %v", err)
	}
}

func TestHandlerRejectsMutableImageWhenDigestRequired(t *testing.T) {
	h := New(Config{Client: fake.NewSimpleClientset(), Namespace: "tasks", RequireDigest: true})
	result := h.Execute(context.Background(), containerTask(map[string]any{"image": "busybox:latest"}))
	if result.Success || result.ResultCode != "invalid_payload" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func containerTask(payload map[string]any) execute.AssignedTask {
	return execute.AssignedTask{
		InstanceID:     42,
		RunID:          "0ca24961-9e0a-443e-8cf0-845e4699e63d",
		TenantID:       "tenant-a",
		JobID:          7,
		HandlerType:    "container",
		HandlerPayload: payload,
		TimeoutSec:     30,
		Attempt:        1,
	}
}
