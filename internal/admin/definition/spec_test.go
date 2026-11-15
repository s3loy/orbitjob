package definition

import (
	"strings"
	"testing"
)

func TestManualOnlyJobSpec(t *testing.T) {
	if _, _, err := CanonicalSpec("jobs", `{"schedule":"","jobTemplate":{"image":"task"}}`); err != nil {
		t.Fatalf("manual-only job rejected: %v", err)
	}
}

func TestCanonicalSpecStrictAndLossless(t *testing.T) {
	for _, input := range []string{`null`, `{"schedule":"* * * * *","jobTemplate":{"image":"task"},"status":{}}`, `{"schedule":"x","jobTemplate":{"image":"task","privileged":true}}`, `{"schedule":"x","jobTemplate":{"image":"task"}} {}`} {
		if _, _, err := CanonicalSpec("jobs", input); err == nil {
			t.Fatalf("accepted invalid spec: %s", input)
		}
	}
	input := `{"schedule":"* * * * *","tasks":[{"name":"a","jobRef":{"name":"a"}},{"name":"b","jobRef":{"name":"b"},"dependsOn":["a"],"condition":{"type":"all","rules":[{"task":"a","field":"attempts","operator":"gt","number":9007199254740993}]}}]}`
	out, digest, err := CanonicalSpec("workflows", input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "9007199254740993") || len(digest) != 64 {
		t.Fatalf("lost integer precision: %s", out)
	}
	again, d2, err := CanonicalSpec("workflows", out)
	if err != nil || again != out || d2 != digest {
		t.Fatal("unstable canonical spec")
	}
}
