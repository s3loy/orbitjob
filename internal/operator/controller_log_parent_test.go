package operator

import (
	"orbitjob/internal/core/app/execution"
	"strings"
	"testing"
)

func TestLongLogRunAnnotationWakesFullParent(t *testing.T) {
	name := strings.Repeat("long-job-", 10) + "complete-digest"
	obj := managedJobEvent()
	obj.SetLabels(map[string]string{execution.LabelRunName: execution.RunLabel(name), execution.LabelRunUID: "immutable-run-uid"})
	obj.SetAnnotations(map[string]string{execution.AnnotationRunName: name})
	q := NewQueue()
	defer q.ShutDown()
	enqueueParentRun(q, obj)
	if q.Len() != 1 {
		t.Fatal("full parent was not queued")
	}
	key, _ := q.Get()
	q.Done(key)
	if key != "jobruns:finance/"+name {
		t.Fatalf("queued hash instead of actual name: %s", key)
	}
	obj.SetAnnotations(map[string]string{execution.AnnotationRunName: name + "-foreign"})
	enqueueParentRun(q, obj)
	if q.Len() != 0 {
		t.Fatal("inconsistent annotation was trusted")
	}
}
