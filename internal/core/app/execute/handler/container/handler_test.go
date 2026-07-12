package container

import (
	"context"
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
