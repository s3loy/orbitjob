package policy

import (
	"strings"
	"testing"
)

func TestParseDocumentRejectsUnsupportedAndAmbiguousSemantics(t *testing.T) {
	const statement = `{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}`
	for name, raw := range map[string]string{
		"conditional allow":   `{"statement":[` + strings.TrimSuffix(statement, "}") + `,"Condition":{"StringEquals":{"environment":"prod"}}}]}`,
		"not action":          `{"statement":[` + strings.TrimSuffix(statement, "}") + `,"NotAction":["job:Trigger"]}]}`,
		"lowercase condition": `{"statement":[` + strings.TrimSuffix(statement, "}") + `,"condition":{}}]}`,
		"unknown root":        `{"condition":{},"statement":[` + statement + `]}`,
		"duplicate effect":    `{"statement":[{"effect":"Deny","effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}`,
		"case variant effect": `{"statement":[{"effect":"Deny","Effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]}]}`,
		"case variant root":   `{"Statement":[` + statement + `]}`,
		"duplicate statement": `{"statement":[` + statement + `],"statement":[` + statement + `]}`,
		"trailing json":       `{"statement":[` + statement + `]} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			if doc, err := ParseDocument([]byte(raw)); err == nil {
				t.Fatalf("unsupported semantics accepted %+v", doc)
			}
		})
	}
}

func TestStrictDocumentKeepsSupportedDenySemantics(t *testing.T) {
	raw := []byte(`{"version":"1","statement":[{"effect":"Allow","action":["job:Get"],"resource":["orbitjob:self:*:job/*"]},{"effect":"Deny","action":["job:Get"],"resource":["orbitjob:self:*:job/blocked"]}]}`)
	doc, err := ParseDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]Decision{"allowed": DecisionAllow, "blocked": DecisionDeny} {
		resolved := ResolveSelf(doc, "home")
		if got := Evaluate([]Document{resolved}, Request{Action: "job:Get", Resource: ARN{Tenant: "home", Group: "*", Type: "job", ID: id}}); got != want {
			t.Fatalf("id=%s decision=%v want=%v", id, got, want)
		}
	}
}
