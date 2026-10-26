package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSuccessResponsesRequireTheExpectedResourceShape(t *testing.T) {
	old := stdoutWriter
	t.Cleanup(func() { stdoutWriter = old })
	for _, body := range []string{"null", "{}", `{"items":[{}]}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		for _, command := range [][]string{
			{"jobs", "list"}, {"jobs", "get", "7"}, {"jobs", "trigger", "7"},
			{"checks", "list"}, {"checks", "get", "7"},
			{"runs", "list"}, {"runs", "get", "7"}, {"runs", "cancel", "7"},
		} {
			for _, jsonOutput := range []bool{false, true} {
				output := new(bytes.Buffer)
				stdoutWriter = output
				args := append(append([]string{}, command...), "--api-url", server.URL, "--api-key", "test")
				if jsonOutput {
					args = append(args, "--json")
				}
				if code := run(context.Background(), args); code == 0 || strings.Contains(output.String(), "[OK]") {
					t.Errorf("body=%s %v json=%v: exit=%d output=%s", body, command, jsonOutput, code, output)
				}
			}
		}
		server.Close()
	}
}

func TestNewRunReferenceCanPrecedeItsFirstStatus(t *testing.T) {
	ref, err := decodeOne[runRef]([]byte(`{"namespace":"tasks","name":"created-run","occurrence_key":"occurrence","phase":""}`))
	if err != nil || ref.Name != "created-run" {
		t.Fatalf("a valid asynchronous run reference was rejected: %+v %v", ref, err)
	}
}
