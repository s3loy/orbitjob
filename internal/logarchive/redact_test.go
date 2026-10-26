package logarchive

import (
	"strings"
	"testing"
	"time"
)

func TestPlatformEventsRedactCredentialsWithoutRewritingWorkloadOutput(t *testing.T) {
	now := time.Now()
	s := openTest(t, &now)
	id := identity(71)
	if err := s.Register(id); err != nil {
		t.Fatal(err)
	}
	secret := "otj_synthetic_credential_for_redaction_test"
	text := "Arguments: Authorization: Bearer " + secret + " postgres://orbitjob_admin:synthetic_password@database/orbitjob"
	if err := s.Append(id, []Record{{ID: "event-redaction", Kind: "event", Text: text}, {ID: "raw-workload", Kind: "stdout", Text: secret}}); err != nil {
		t.Fatal(err)
	}
	p, err := s.Read(id, Query{})
	if err != nil || len(p.Records) != 2 {
		t.Fatalf("read: %+v %v", p, err)
	}
	if strings.Contains(p.Records[0].Text, secret) || strings.Contains(p.Records[0].Text, "synthetic_password") || !strings.Contains(p.Records[0].Text, "[REDACTED]") {
		t.Fatalf("platform event exposed credential: %s", p.Records[0].Text)
	}
	if p.Records[1].Text != secret {
		t.Fatal("raw workload output was silently changed")
	}
	p, err = s.Read(id, Query{Search: secret})
	if err != nil || len(p.Records) != 1 || p.Records[0].Kind != "stdout" {
		t.Fatal("event search still exposes credential")
	}
}
