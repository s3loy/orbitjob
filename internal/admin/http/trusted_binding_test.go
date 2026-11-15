package http

import (
	"encoding/json"
	"net/url"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin/binding"

	"orbitjob/internal/platform/scan"
)

func TestCheckStatusBodyCannotChangeTrustedFields(t *testing.T) {
	tenantID, groupID := scan.GenerateID(), scan.GenerateID()
	in := ChangeCheckStatusRequest{ID: 7, TenantID: tenantID, ResourceGroupID: groupID}
	if err := json.Unmarshal([]byte(`{"ID":9,"TenantID":"other","ResourceGroupID":"other","version":2}`), &in); err != nil {
		t.Fatal(err)
	}
	if in.ID != 7 || in.TenantID != tenantID || in.ResourceGroupID != groupID || in.Version != 2 {
		t.Fatalf("body changed trusted fields: %+v", in)
	}
}

func TestQueryCannotChangePathIdentity(t *testing.T) {
	models := []any{&GetJobRequest{}, &GetCheckRequest{}, &GetFunctionRequest{},
		&InvokeFunctionRequest{}, &ListFunctionRunsRequest{}, &GetWorkflowRequest{}, &ListWorkflowRunsRequest{}}
	for _, model := range models {
		t.Run(reflect.TypeOf(model).Elem().Name(), func(t *testing.T) {
			id := reflect.ValueOf(model).Elem().FieldByName("ID")
			id.SetInt(7)
			if err := binding.MapFormWithTag(model, url.Values{"ID": {"9"}}, "form"); err != nil {
				t.Fatal(err)
			}
			if id.Int() != 7 {
				t.Fatalf("query replaced the path identity: %d", id.Int())
			}
		})
	}
}
