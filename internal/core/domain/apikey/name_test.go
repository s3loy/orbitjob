package apikey

import (
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	for _, tc := range []struct {
		name string
		ok   bool
	}{
		{"", true}, {"Daily operator", true}, {"\u5e73\u53f0\u8fd0\u7ef4 / \u591c\u95f4\u4efb\u52a1", true},
		{strings.Repeat("\u540d", 128), true}, {strings.Repeat("\u540d", 129), false},
		{" ", false}, {" label", false}, {"label\u00a0", false}, {"line\nlabel", false},
		{"tab\tlabel", false}, {"nul\x00label", false}, {"c1\u0085label", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateName(tc.name); (err == nil) != tc.ok {
				t.Fatalf("valid=%v err=%v", tc.ok, err)
			}
		})
	}
}
