package strictjson

import (
	"strings"
	"testing"
)

func TestStrictJSONRejectsAmbiguousAndUnboundedInput(t *testing.T) {
	for _, input := range []string{
		`{"image":"safe","image":"other"}`,
		`{"a":1,"\u0061":2}`,
		`{"x":{"a":1,"a":2}}`,
		`{"x":1} {"y":2}`,
		strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65),
		`[0` + strings.Repeat(",0", MaxNodes) + `]`,
		`"` + strings.Repeat("\u754c", MaxBytes/3) + `"`,
	} {
		if err := Validate([]byte(input)); err == nil {
			t.Fatalf("accepted ambiguous or excessive JSON %.80s", input)
		}
	}
	for _, input := range []string{`{"x":9007199254740993}`, `{"a":1,"x":{"a":2}}`, `[]`, `{"zero":0}`, strings.Repeat("[", 64) + "0" + strings.Repeat("]", 64)} {
		if err := Validate([]byte(input)); err != nil {
			t.Fatalf("rejected valid JSON %.80s: %v", input, err)
		}
	}
}
