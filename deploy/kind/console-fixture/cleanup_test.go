package main

import (
	"maps"
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/uuid"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	"orbitjob/internal/core/app/execution"
	"orbitjob/internal/core/domain/coordination"
	"orbitjob/internal/core/domain/function"
)

func TestMatchesExecutionPreservesCompleteOwnershipAcrossRunLabels(t *testing.T) {
	source := function.SourceUID(1)
	key := coordination.OccurrenceKey(source, 4, time.Date(2026, time.November, 17, 0, 0, 0, 0, time.UTC))
	for _, name := range []struct {
		kind  string
		value string
	}{
		{"legacy short name", v1alpha1.LegacyRunObjectName(source, key)},
		{"maximum label length", v1alpha1.LegacyRunObjectName(strings.Repeat("a", 54), key)},
		{"current full digest name", v1alpha1.RunObjectName(source, key)},
	} {
		t.Run(name.kind, func(t *testing.T) {
			run := ownedRun{Name: name.value, UID: uuid.NewUUID(), Revision: 4}
			m := &manifest{Runs: []ownedRun{run}}
			job := execution.BuildJob(execution.Identity{
				RunName: run.Name, RunUID: string(run.UID),
				RevisionID: strconv.FormatInt(run.Revision, 10), Attempt: 1,
			}, fixtureNamespace, execution.Template{})
			for _, resource := range []struct {
				kind   string
				labels map[string]string
			}{
				{"Job", job.Labels},
				{"Pod", job.Spec.Template.Labels},
			} {
				t.Run(resource.kind, func(t *testing.T) {
					for _, tc := range []struct {
						name    string
						key     string
						value   string
						ours    bool
						wantErr bool
					}{
						{name: "owned execution", ours: true},
						{name: "recreated run UID", key: execution.LabelRunUID, value: "new-uid"},
						{name: "missing run UID", key: execution.LabelRunUID},
						{name: "different run label", key: execution.LabelRunName, value: execution.RunLabel(v1alpha1.RunObjectName(source, "another-occurrence")), wantErr: true},
						{name: "missing run label", key: execution.LabelRunName, wantErr: true},
						{name: "different revision", key: execution.LabelRevisionID, value: "99", wantErr: true},
						{name: "missing revision", key: execution.LabelRevisionID, wantErr: true},
						{name: "invalid revision", key: execution.LabelRevisionID, value: "invalid", wantErr: true},
					} {
						t.Run(tc.name, func(t *testing.T) {
							labels := maps.Clone(resource.labels)
							if tc.key != "" {
								labels[tc.key] = tc.value
								if tc.value == "" {
									delete(labels, tc.key)
								}
							}
							ours, err := matchesExecution(labels, m)
							if ours != tc.ours || (err != nil) != tc.wantErr {
								t.Fatalf("matchesExecution() = (%v, %v), want (%v, error=%v)", ours, err, tc.ours, tc.wantErr)
							}
						})
					}
					if len(run.Name) > 63 {
						labels := maps.Clone(resource.labels)
						labels[execution.LabelRunName] = run.Name
						if ours, err := matchesExecution(labels, m); ours || err == nil {
							t.Fatal("unhashed long run label accepted")
						}
					}
				})
			}
		})
	}
}
