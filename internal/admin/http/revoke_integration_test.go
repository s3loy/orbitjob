//go:build integration

package http

import (
	"fmt"
	"net/http"
	"testing"

	"orbitjob/internal/admin/bootstrap"
)

func TestRevokePersistsActualOwnerAndAuthenticatedActor(t *testing.T) {
	for _, crossTenant := range []bool{false, true} {
		t.Run(fmt.Sprint(crossTenant), func(t *testing.T) {
			server, db, bootstrapKey := newIntegrationServer(t)
			defer server.Close()
			tenant := createTenant(t, server, bootstrapKey, "revoke-owner")
			actorID, actorKey := createKey(t, server, bootstrapKey, tenant, []string{tenantAdminPolicyID}, "")
			if crossTenant {
				// FullAccess is an explicit grant covering the actual other owner.
				actorID, actorKey = bootstrap.DefaultAPIKeyID, bootstrapKey
			}
			targetID, _ := createKey(t, server, bootstrapKey, tenant, []string{readOnlyPolicyID}, "")
			response := postJSON(t, server.Client(), server.URL+"/api/v1/api_keys/"+targetID+"/revoke", actorKey, nil)
			requireStatus(t, response, http.StatusOK)
			_ = response.Body.Close()
			var revoked bool
			if err := db.QueryRowContext(t.Context(), `SELECT revoked_at IS NOT NULL FROM api_keys WHERE id=$1 AND tenant_id=$2`, targetID, tenant).Scan(&revoked); err != nil || !revoked {
				t.Fatalf("revocation did not persist: revoked=%t err=%v", revoked, err)
			}
			var recordedTenant, recordedActor, recordedResource string
			if err := db.QueryRowContext(t.Context(), `SELECT tenant_id,actor_id,resource_id FROM audit_events WHERE event_type='revoke' AND resource_type='apikey' AND resource_id=$1`, targetID).Scan(&recordedTenant, &recordedActor, &recordedResource); err != nil {
				t.Fatal(err)
			}
			if recordedTenant != tenant || recordedActor != actorID || recordedResource != targetID {
				t.Fatalf("audit owner=%s actor=%s resource=%s", recordedTenant, recordedActor, recordedResource)
			}
		})
	}
}
