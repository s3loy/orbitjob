package logarchive

import (
	"context"
	"encoding/json"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (o *Observer) observePod(ctx context.Context, pod corev1.Pod) error {
	id, source, ok := o.Store.Source(pod.Namespace, pod.Name, string(pod.UID), "task", 0)
	var err error
	if !ok {
		id, source, err = o.ResolvePod(ctx, pod.Namespace, pod.Name, string(pod.UID), 0)
		if err != nil {
			return err
		}
	}
	command, _ := json.Marshal(pod.Spec.Containers[0].Command)
	args, _ := json.Marshal(pod.Spec.Containers[0].Args)
	records := []Record{{ID: eventID(source.PodUID, "configuration"), Kind: "event", Stage: "configuration", Attempt: source.Attempt, PodUID: source.PodUID, Text: fmt.Sprintf("Image: %s\nCommand: %s\nArguments: %s", pod.Spec.Containers[0].Image, command, args)}}
	for _, c := range pod.Status.Conditions {
		if c.Status == corev1.ConditionTrue {
			records = append(records, Record{ID: eventID(string(pod.UID), string(c.Type), c.LastTransitionTime.String()), Kind: "event", Stage: "preparing", Time: c.LastTransitionTime.Time, Attempt: source.Attempt, PodUID: source.PodUID, Text: string(c.Type) + ": " + c.Reason + " " + c.Message})
		}
	}
	for _, c := range pod.Status.ContainerStatuses {
		if c.State.Waiting != nil {
			w := c.State.Waiting
			records = append(records, Record{ID: eventID(string(pod.UID), w.Reason, w.Message), Kind: "event", Stage: "preparing", Attempt: source.Attempt, PodUID: source.PodUID, Text: w.Reason + ": " + w.Message})
		}
		if c.State.Running != nil {
			records = append(records, Record{ID: eventID(string(pod.UID), "running", c.State.Running.StartedAt.String()), Kind: "event", Stage: "running", Time: c.State.Running.StartedAt.Time, Attempt: source.Attempt, PodUID: source.PodUID, Text: "Container started: " + c.Name})
		}
		if c.State.Terminated != nil {
			v := c.State.Terminated
			code, signal := v.ExitCode, v.Signal
			records = append(records, Record{ID: eventID(string(pod.UID), "exit", v.FinishedAt.String()), Kind: "event", Stage: "completed", Time: v.FinishedAt.Time, Attempt: source.Attempt, PodUID: source.PodUID, Text: fmt.Sprintf("Container exited: %s; exit code %d; signal %d", v.Reason, code, signal), ExitCode: &code, Signal: &signal})
		}
	}
	events, err := o.Kube.CoreV1().Events(pod.Namespace).List(ctx, metav1.ListOptions{FieldSelector: "involvedObject.uid=" + string(pod.UID), Limit: 100})
	if err != nil {
		records = append(records, Record{ID: eventID(source.PodUID, "events-unavailable"), Kind: "event", Stage: "preparing", Attempt: source.Attempt, PodUID: source.PodUID, Text: "Kubernetes diagnostic events could not be read; independently observed Pod facts remain available."})
		return o.Store.Append(id, records)
	}
	for _, ev := range events.Items {
		at := ev.LastTimestamp.Time
		if at.IsZero() {
			at = ev.EventTime.Time
		}
		records = append(records, Record{ID: eventID(string(ev.UID), fmt.Sprint(ev.Count), ev.Message), Kind: "event", Stage: "preparing", Time: at, Attempt: source.Attempt, PodUID: source.PodUID, Text: ev.Reason + ": " + ev.Message})
	}
	if events.Continue != "" {
		records = append(records, Record{ID: eventID(source.PodUID, "event-limit"), Kind: "event", Stage: "preparing", Text: "Additional Kubernetes events were not observed because the bounded event page was exceeded."})
	}
	return o.Store.Append(id, records)
}
