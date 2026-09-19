package policy

import "testing"

func TestParseARN(t *testing.T) {
	cases := []struct {
		in                     string
		tenant, group, typ, id string
	}{
		{"orbitjob:01ABC:ci:job/01JOB", "01ABC", "ci", "job", "01JOB"},
		{"orbitjob:01ABC:ci:job/*", "01ABC", "ci", "job", "*"},
		{"orbitjob:01ABC:*:job/*", "01ABC", "*", "job", "*"},
		{"orbitjob:*:*:tenant/*", "*", "*", "tenant", "*"},
	}
	for _, c := range cases {
		got, err := ParseARN(c.in)
		if err != nil {
			t.Fatalf("ParseARN(%q): %v", c.in, err)
		}
		if got.Tenant != c.tenant || got.Group != c.group || got.Type != c.typ || got.ID != c.id {
			t.Fatalf("ParseARN(%q) = %+v", c.in, got)
		}
	}
}

func TestParseARNRejectsMalformed(t *testing.T) {
	for _, in := range []string{"", "orbitjob:01ABC:ci:job", "orbitjob:a:b:c:d:e", "job/01JOB"} {
		if _, err := ParseARN(in); err == nil {
			t.Fatalf("ParseARN(%q) should fail", in)
		}
	}
}
