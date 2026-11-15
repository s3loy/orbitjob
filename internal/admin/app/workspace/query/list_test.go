package query

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"orbitjob/internal/core/domain/policy"
	"orbitjob/internal/domain/validation"
)

type directoryFake struct {
	in    DirectoryInput
	calls int
	rows  []WorkspaceListItem
	err   error
}

func TestDirectoryRejectsNULBeforeReading(t *testing.T) {
	for _, search := range []string{"\x00", "before\x00after"} {
		reader := &directoryFake{}
		_, err := NewLister(reader).List(context.Background(), ListInput{Search: search, Caller: Caller{TenantID: own}})
		var invalid *validation.Error
		if !errors.As(err, &invalid) || invalid.Field != "search" || reader.calls != 0 {
			t.Fatalf("search=%q calls=%d err=%v", search, reader.calls, err)
		}
	}
}

func (f *directoryFake) ListWorkspaceDirectory(_ context.Context, in DirectoryInput) ([]WorkspaceListItem, error) {
	f.in = in
	f.calls++
	return f.rows, f.err
}

const own = "00000000000000000000000001"
const other = "00000000000000000000000002"

func allow(action, resource string) policy.Document {
	return policy.Document{Statement: []policy.Statement{{Effect: policy.EffectAllow, Action: []string{action}, Resource: []string{resource}}}}
}
func TestDirectoryScopeUsesEffectiveGrantsAndImplicitOwnMetadata(t *testing.T) {
	global := allow("*", "orbitjob:*:*:*/*")
	for _, tc := range []struct {
		name    string
		caller  Caller
		all     bool
		include string
		exclude string
	}{
		{"global administrator", Caller{TenantID: own, Documents: []policy.Document{global}}, true, "", ""},
		{"ordinary operator", Caller{TenantID: own, Documents: []policy.Document{allow("job:Get", "orbitjob:self:*:job/*")}}, false, own, ""},
		{"tenant admin self", Caller{TenantID: own, Documents: []policy.Document{policy.ResolveSelf(allow("tenant:Get", "orbitjob:self:*:*/*"), own)}}, false, own, ""},
		{"group bound global", Caller{TenantID: own, ResourceGroupID: "group", Documents: []policy.Document{global}}, false, own, ""},
		{"exact platform", Caller{IsPlatform: true, Documents: []policy.Document{allow("tenant:Get", "orbitjob:"+other+":*:tenant/"+other)}}, false, other, ""},
		{"boundary caps platform", Caller{IsPlatform: true, Documents: policy.Intersect([]policy.Document{global}, allow("tenant:Get", "orbitjob:"+other+":*:tenant/*"))}, false, other, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &directoryFake{}
			_, err := NewLister(reader).List(context.Background(), ListInput{Caller: tc.caller})
			if err != nil || reader.calls != 1 {
				t.Fatalf("read=%d err=%v", reader.calls, err)
			}
			if reader.in.Scope.All != tc.all || (tc.include != "" && !slices.Equal(reader.in.Scope.Include, []string{tc.include})) {
				t.Fatalf("wrong scope %+v", reader.in.Scope)
			}
			if reader.in.Limit != 21 || reader.in.Offset != 0 {
				t.Fatalf("wrong page %+v", reader.in)
			}
		})
	}
	// A Deny removes foreign metadata, while the pre-existing own-label exception
	// remains identity information even if tenant:Get itself is denied.
	deny := policy.Document{Statement: []policy.Statement{{Effect: policy.EffectDeny, Action: []string{"tenant:Get"}, Resource: []string{"orbitjob:" + own + ":*:tenant/*", "orbitjob:" + other + ":*:tenant/*"}}}}
	reader := &directoryFake{}
	_, err := NewLister(reader).List(context.Background(), ListInput{Caller: Caller{TenantID: own, Documents: []policy.Document{global, deny}}})
	if err != nil || slices.Contains(reader.in.Scope.Exclude, own) || !slices.Contains(reader.in.Scope.Exclude, other) {
		t.Fatalf("own/foreign distinction %+v %v", reader.in.Scope, err)
	}
}
func TestDirectoryPaginationSearchAndFailures(t *testing.T) {
	reader := &directoryFake{rows: []WorkspaceListItem{{ID: own}, {ID: other}, {ID: "third"}}}
	out, err := NewLister(reader).List(context.Background(), ListInput{Search: "  \u540d\u79f0%_\\  ", Limit: 2, Offset: 7, Caller: Caller{TenantID: own}})
	if err != nil || !out.HasMore || len(out.Items) != 2 || reader.in.Limit != 3 || reader.in.Offset != 7 || reader.in.Search != "\u540d\u79f0%_\\" {
		t.Fatalf("page %+v input %+v err=%v", out, reader.in, err)
	}
	reader.rows = nil
	out, err = NewLister(reader).List(context.Background(), ListInput{Caller: Caller{TenantID: own}, Search: strings.Repeat("\u4e2d", 128)})
	if err != nil || out.Items == nil || out.HasMore {
		t.Fatalf("empty page %+v %v", out, err)
	}
	for _, in := range []ListInput{{Limit: -1}, {Limit: 101}, {Offset: -1}, {Offset: 2147483648}, {Search: strings.Repeat("\u4e2d", 129)}, {Search: string([]byte{255})}} {
		in.Caller = Caller{TenantID: own}
		if _, err := NewLister(reader).List(context.Background(), in); err == nil {
			t.Fatalf("accepted invalid input %+v", in)
		}
	}
	if _, err := NewLister(reader).List(context.Background(), ListInput{}); err == nil {
		t.Fatal("missing caller accepted")
	}
	out, err = NewLister(reader).List(context.Background(), ListInput{Caller: Caller{IsPlatform: true}})
	if err != nil || len(out.Items) != 0 || out.HasMore {
		t.Fatalf("ungranted platform %+v %v", out, err)
	}
	reader.err = errors.New("database unavailable")
	if _, err := NewLister(reader).List(context.Background(), ListInput{Caller: Caller{TenantID: own}}); err == nil {
		t.Fatal("directory failure became successful empty list")
	}
}
