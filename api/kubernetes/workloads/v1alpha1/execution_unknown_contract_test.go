package v1alpha1

import "testing"

func TestExecutionUnknownSurvivesCRDStatusValidation(t *testing.T) {
	for _, resource := range []string{"jobruns", "workflowruns"} {
		t.Run(resource, func(t *testing.T) {
			doc := loadCRDDoc(t, "../../../../charts/orbitjob/crds/workloads.orbitjob.io_"+resource+".yaml")
			versions := doc["spec"].(map[string]any)["versions"].([]any)
			version := versions[0].(map[string]any)
			w := &crdWalk{}
			schema := w.mapAt(version, "schema", "openAPIV3Schema", "properties", "status", "properties")
			phase := w.mapAt(schema, "phase")
			if !w.setOf(phase, "enum")["ExecutionUnknown"] {
				t.Fatal("unknown phase rejected by admission")
			}
			if resource == "jobruns" {
				if w.mapAt(schema, "executionUnknownSince")["format"] != "date-time" {
					t.Fatal("unknown timestamp missing")
				}
				reasons := w.setOf(w.mapAt(schema, "executionUnknownReason"), "enum")
				if !reasons["JobNotFound"] || !reasons["JobUIDMismatch"] || !reasons["PodIdentityAmbiguous"] {
					t.Fatal("bounded unknown reasons missing")
				}
			} else {
				stepPhase := w.mapAt(schema, "steps", "items", "properties", "phase")
				if !w.setOf(stepPhase, "enum")["ExecutionUnknown"] {
					t.Fatal("unknown step phase rejected")
				}
			}
			if len(w.vs) > 0 {
				t.Fatal(w.vs)
			}
		})
	}
}
