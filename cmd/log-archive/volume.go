package main

import (
	"context"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"orbitjob/internal/logarchive"
	"os"
)

func boundVolume(ctx context.Context, kube kubernetes.Interface, l logarchive.Limits) (logarchive.Limits, error) {
	namespace, pod := os.Getenv("POD_NAMESPACE"), os.Getenv("POD_NAME")
	if namespace == "" || pod == "" {
		return l, fmt.Errorf("archive Pod identity is required to verify its persistent volume")
	}
	claim, e := kube.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, "data-"+pod, metav1.GetOptions{})
	if e != nil {
		return l, fmt.Errorf("read archive volume allocation: %w", e)
	}
	capacity := claim.Spec.Resources.Requests.Storage().Value()
	if capacity < 10*logarchive.GiB {
		return l, fmt.Errorf("archive volume needs at least 10 GiB in the supported profile")
	}
	if capacity < l.DeclaredBytes {
		l.DeclaredBytes = capacity
	}
	if ceiling := capacity * 7 / 10; ceiling < l.TotalBytes {
		l.TotalBytes = ceiling
	}
	return l, l.Validate()
}
