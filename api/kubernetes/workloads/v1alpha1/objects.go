package v1alpha1

import (
	"encoding/json"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var GroupVersion = schema.GroupVersion{Group: "workloads.orbitjob.io", Version: "v1alpha1"}

type ScheduledJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ScheduledJobSpec   `json:"spec"`
	Status            ScheduledJobStatus `json:"status,omitempty"`
}
type ScheduledJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ScheduledJob `json:"items"`
}
type JobRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              JobRunSpec   `json:"spec"`
	Status            JobRunStatus `json:"status,omitempty"`
}
type JobRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []JobRun `json:"items"`
}
type WorkflowJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              WorkflowJobSpec   `json:"spec"`
	Status            WorkflowJobStatus `json:"status,omitempty"`
}
type WorkflowJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorkflowJob `json:"items"`
}
type WorkflowRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              WorkflowRunSpec   `json:"spec"`
	Status            WorkflowRunStatus `json:"status,omitempty"`
}
type WorkflowRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorkflowRun `json:"items"`
}

func clone[T any](in T) T {
	b, _ := json.Marshal(in)
	var out T
	_ = json.Unmarshal(b, &out)
	return out
}
func (in *ScheduledJob) DeepCopyObject() runtime.Object          { return clone(*in).asScheduledJob() }
func (x ScheduledJob) asScheduledJob() *ScheduledJob             { return &x }
func (in *ScheduledJobList) DeepCopyObject() runtime.Object      { return clone(*in).asScheduledJobList() }
func (x ScheduledJobList) asScheduledJobList() *ScheduledJobList { return &x }
func (in *JobRun) DeepCopyObject() runtime.Object                { return clone(*in).asJobRun() }
func (x JobRun) asJobRun() *JobRun                               { return &x }
func (in *JobRunList) DeepCopyObject() runtime.Object            { return clone(*in).asJobRunList() }
func (x JobRunList) asJobRunList() *JobRunList                   { return &x }
func (in *WorkflowJob) DeepCopyObject() runtime.Object           { return clone(*in).asWorkflowJob() }
func (x WorkflowJob) asWorkflowJob() *WorkflowJob                { return &x }
func (in *WorkflowJobList) DeepCopyObject() runtime.Object       { return clone(*in).asWorkflowJobList() }
func (x WorkflowJobList) asWorkflowJobList() *WorkflowJobList {
	return &x
}
func (in *WorkflowRun) DeepCopyObject() runtime.Object     { return clone(*in).asWorkflowRun() }
func (x WorkflowRun) asWorkflowRun() *WorkflowRun          { return &x }
func (in *WorkflowRunList) DeepCopyObject() runtime.Object { return clone(*in).asWorkflowRunList() }
func (x WorkflowRunList) asWorkflowRunList() *WorkflowRunList {
	return &x
}
func AddToScheme(s *runtime.Scheme) error {
	s.AddKnownTypes(GroupVersion,
		&ScheduledJob{}, &ScheduledJobList{},
		&JobRun{}, &JobRunList{},
		&WorkflowJob{}, &WorkflowJobList{},
		&WorkflowRun{}, &WorkflowRunList{})
	metav1.AddToGroupVersion(s, GroupVersion)
	return nil
}
