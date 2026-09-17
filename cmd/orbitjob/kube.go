package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// kubeRunner shells out to kubectl with the current kubeconfig context, the
// same posture the repo's scripts take. The interface keeps the cluster out of
// the unit tests.
type kubeRunner interface {
	run(ctx context.Context, args ...string) ([]byte, error)
}

// newKubeRunner is the seam the commands use; tests swap it for a fake.
var newKubeRunner = func() kubeRunner { return realKube{} }

// realKube runs the kubectl binary found on PATH.
type realKube struct{}

func (realKube) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("kubectl %s: %s", strings.Join(args, " "), firstLine(msg))
	}
	return stdout.Bytes(), nil
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}

// kubeJSON runs kubectl and decodes a -o json response.
func kubeJSON(ctx context.Context, kube kubeRunner, args ...string) (map[string]any, error) {
	full := append(append([]string{}, args...), "-o", "json")
	out, err := kube.run(ctx, full...)
	if err != nil {
		return nil, err
	}
	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		return nil, fatal("parse kubectl output for %s: %v", strings.Join(args, " "), err)
	}
	return decoded, nil
}

// resourceExists reports whether one cluster object is present. A not-found
// error is a clean no; anything else is surfaced.
func resourceExists(ctx context.Context, kube kubeRunner, args ...string) (bool, error) {
	full := append(append([]string{}, args...), "-o", "name")
	if _, err := kube.run(ctx, full...); err != nil {
		if isNotFoundKubeErr(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// isNotFoundKubeErr matches kubectl's "not found" failure without depending on
// its exact wording: kubectl exits non-zero and prints "NotFound" or
// "(NotFound)" from the API status.
func isNotFoundKubeErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") || strings.Contains(msg, "notfound")
}
