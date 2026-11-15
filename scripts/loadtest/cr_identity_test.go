package main

import (
	"testing"

	workloadsv1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
)

func TestRunCRPresenceRequiresNamespaceAndFullImmutableIdentity(t *testing.T) {
	run := ledgerRun{revisionID: 7, trigger: "Manual", sourceUID: "source-uid", occurrenceKey: "same-prefix-long-occurrence"}
	for _, name := range workloadsv1alpha1.RunNameCandidates("definition", run.occurrenceKey) {
		obj := observedRunCR{}
		obj.Metadata.Name = name
		obj.Spec.OccurrenceKey = run.occurrenceKey
		obj.Spec.RevisionID = run.revisionID
		obj.Spec.Source.UID = run.sourceUID
		obj.Spec.Trigger = run.trigger
		objects := map[string]observedRunCR{"tenant-a/" + name: obj}
		if !runCRPresent(objects, "tenant-a", "definition", run) {
			t.Errorf("matching modern/legacy run %s was not found", name)
		}
		if runCRPresent(objects, "tenant-b", "definition", run) {
			t.Error("an object in another tenant namespace satisfied the evidence")
		}
		for _, mutate := range []func(*observedRunCR){
			func(obj *observedRunCR) { obj.Spec.OccurrenceKey = "another-occurrence" },
			func(obj *observedRunCR) { obj.Spec.RevisionID++ },
			func(obj *observedRunCR) { obj.Spec.Source.UID = "another-source" },
			func(obj *observedRunCR) { obj.Spec.Trigger = "Schedule" },
		} {
			foreign := obj
			mutate(&foreign)
			objects["tenant-a/"+name] = foreign
			if runCRPresent(objects, "tenant-a", "definition", run) {
				t.Errorf("a name collision satisfied immutable identity: %+v", foreign.Spec)
			}
		}
	}
}
