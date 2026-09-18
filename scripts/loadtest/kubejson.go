package main

import (
	"encoding/json"
	"fmt"
)

// Minimal decode shapes for the kubectl reads this tool performs. They exist
// because a jsonpath template this repo once shipped read as empty on GitHub's
// runner while the same cluster answered the same query in JSON; structured
// decoding has no such variance, and a decode error is loud instead of a
// quietly empty read.

type kubeNameList struct {
	Items []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	} `json:"items"`
}

// decodeKubeNames extracts items[].metadata.name from a `kubectl get -o json`
// response. An empty items array is a valid result and decodes to no names.
func decodeKubeNames(out []byte) ([]string, error) {
	var list kubeNameList
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("decode kubectl item names: %w", err)
	}
	names := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		names = append(names, item.Metadata.Name)
	}
	return names, nil
}

// decodeReadyReplicas reads status.readyReplicas from a Deployment's JSON.
// Absent means zero: a deployment that has not scheduled anything is not
// ready.
func decodeReadyReplicas(out []byte) (int32, error) {
	var deployment struct {
		Status struct {
			ReadyReplicas int32 `json:"readyReplicas"`
		} `json:"status"`
	}
	if err := json.Unmarshal(out, &deployment); err != nil {
		return 0, fmt.Errorf("decode deployment status: %w", err)
	}
	return deployment.Status.ReadyReplicas, nil
}
