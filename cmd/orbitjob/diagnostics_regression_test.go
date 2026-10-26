package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type unavailableKube struct{}

func (unavailableKube) run(_ context.Context, args ...string) ([]byte, error) {
	return nil, fmt.Errorf("kubectl unavailable: %s", strings.Join(args, " "))
}

func TestJSONDiagnosticsPreserveFailureExitCode(t *testing.T) {
	t.Setenv("ORBITJOB_API_KEY", "")
	oldKube, oldOut := newKubeRunner, stdoutWriter
	t.Cleanup(func() { newKubeRunner, stdoutWriter = oldKube, oldOut })
	newKubeRunner = func() kubeRunner { return unavailableKube{} }
	for _, name := range []string{"status", "doctor"} {
		output := new(bytes.Buffer)
		stdoutWriter = output
		code := run(context.Background(), []string{name, "--json"})
		if code == 0 || !strings.Contains(output.String(), "[FAIL]") {
			t.Errorf("%s: exit=%d output=%s", name, code, output)
		}
	}
}

func TestMalformedIDCannotCancelAnotherRun(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"namespace":"test","name":"test","phase":"Running"}`))
	}))
	t.Cleanup(server.Close)
	old := stdoutWriter
	stdoutWriter = new(bytes.Buffer)
	t.Cleanup(func() { stdoutWriter = old })
	for _, value := range []string{"7oops", "7.5", "7/8", " 7", "7\n"} {
		err := runsCancel(context.Background(), []string{value, "--api-url", server.URL, "--api-key", "test"})
		if err == nil {
			t.Errorf("accepted malformed run ID %q", value)
		}
	}
	if requests != 0 {
		t.Fatalf("sent %d destructive requests for invalid IDs", requests)
	}
}

func TestMalformedSuccessResponseFailsEveryAPICommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not-json"))
	}))
	t.Cleanup(server.Close)
	old := stdoutWriter
	t.Cleanup(func() { stdoutWriter = old })
	commands := [][]string{
		{"jobs", "list"}, {"jobs", "get", "7"}, {"jobs", "trigger", "7"},
		{"checks", "list"}, {"checks", "get", "7"},
		{"runs", "list"}, {"runs", "get", "7"}, {"runs", "cancel", "7"},
	}
	for _, command := range commands {
		for _, jsonOutput := range []bool{false, true} {
			output := new(bytes.Buffer)
			stdoutWriter = output
			args := append(append([]string{}, command...), "--api-url", server.URL, "--api-key", "test")
			if jsonOutput {
				args = append(args, "--json")
			}
			if code := run(context.Background(), args); code == 0 || strings.Contains(output.String(), "[OK]") {
				t.Errorf("%v json=%v: exit=%d output=%s", command, jsonOutput, code, output)
			}
		}
	}
}
