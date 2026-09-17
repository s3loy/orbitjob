//go:build integration

package http

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	v1alpha1 "orbitjob/api/kubernetes/workloads/v1alpha1"
	apikeycommand "orbitjob/internal/admin/app/apikey/command"
	apikeyquery "orbitjob/internal/admin/app/apikey/query"
	checkcommand "orbitjob/internal/admin/app/check/command"
	checkquery "orbitjob/internal/admin/app/check/query"
	checkrunquery "orbitjob/internal/admin/app/checkrun/query"
	jobcommand "orbitjob/internal/admin/app/job/command"
	jobquery "orbitjob/internal/admin/app/job/query"
	policycommand "orbitjob/internal/admin/app/policy/command"
	policyquery "orbitjob/internal/admin/app/policy/query"
	resourcegroupcommand "orbitjob/internal/admin/app/resourcegroup/command"
	resourcegroupquery "orbitjob/internal/admin/app/resourcegroup/query"
	runcommand "orbitjob/internal/admin/app/run/command"
	runquery "orbitjob/internal/admin/app/run/query"
	slicommand "orbitjob/internal/admin/app/sli/command"
	sliquery "orbitjob/internal/admin/app/sli/query"
	slocommand "orbitjob/internal/admin/app/slo/command"
	sloquery "orbitjob/internal/admin/app/slo/query"
	sloalertquery "orbitjob/internal/admin/app/sloalert/query"
	slobudgetquery "orbitjob/internal/admin/app/slobudget/query"
	tenantcommand "orbitjob/internal/admin/app/tenant/command"
	tenantquery "orbitjob/internal/admin/app/tenant/query"
	"orbitjob/internal/admin/bootstrap"
	"orbitjob/internal/admin/http/middleware"
	"orbitjob/internal/admin/kube"
	adminpostgres "orbitjob/internal/admin/store/postgres"
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/platform/migrate"
	"orbitjob/internal/platform/postgrestest"
)

// The password the db/migrations qualification suite provisions for the
// deployment roles. Reusing it means the two suites never fight over the role
// password on a shared cluster.
const itestAdminPassword = "itest-admin"

// newIntegrationServer assembles the admin API exactly as cmd/admin-api does,
// with two stand-ins. The Kubernetes client the trigger and cancel paths
// publish through is a dynamic fake, so a manual trigger creates a real JobRun
// object in an in-memory cluster without needing a control plane. And the
// server runs on an orbitjob_admin connection: the baseline lets
// orbitjob_bootstrap_default execute only for that identity, and the
// repositories set app.tenant_id for it, so it is the identity the API server
// uses in production. Everything else -- routes, middleware, repositories, use
// cases -- is the production wiring.
//
// The returned *sql.DB is the superuser handle the schema was built with, not
// the server's handle: the tests use it for direct SQL assertions, which rely
// on the superuser's RLS bypass to read rows across tenants.
func newIntegrationServer(t *testing.T) (*httptest.Server, *sql.DB, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := postgrestest.Open(t)

	ctx := context.Background()

	// Provision the deployment role's password, then open the server's handle
	// as that role, keeping this test's schema as the search path.
	if err := migrate.EnsureRolePasswords(ctx, db, map[string]string{
		"orbitjob_migrator": "itest-migrator",
		"orbitjob_admin":    itestAdminPassword,
		"orbitjob_runtime":  "itest-runtime",
	}); err != nil {
		t.Fatalf("provision deployment role passwords: %v", err)
	}
	var searchPath string
	if err := db.QueryRowContext(ctx, "SHOW search_path").Scan(&searchPath); err != nil {
		t.Fatalf("read test search path: %v", err)
	}
	parsed, err := url.Parse(os.Getenv("TEST_DATABASE_DSN"))
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_DSN: %v", err)
	}
	parsed.User = url.UserPassword(migrate.RoleAdmin, itestAdminPassword)
	query := parsed.Query()
	query.Set("search_path", searchPath)
	parsed.RawQuery = query.Encode()

	adminDB, err := sql.Open("postgres", parsed.String())
	if err != nil {
		t.Fatalf("open orbitjob_admin connection: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	if err := adminDB.PingContext(ctx); err != nil {
		t.Fatalf("ping orbitjob_admin connection: %v", err)
	}

	// Restore the presets the schema harness truncated away, so the bootstrap
	// function and the tests' policy bindings find them. Seeding runs on the
	// superuser handle: a preset carries no tenant id, and the RLS policy on
	// policies governs tenant-owned rows, not platform ones.
	if err := postgrestest.SeedPresetPolicies(ctx, db); err != nil {
		t.Fatalf("seed platform preset policies: %v", err)
	}

	// Bootstrap default tenant and key so the auth middleware has credentials.
	bootstrapKey := "otj_integration_bootstrap_key"
	if _, err := bootstrap.EnsureDefault(ctx, adminDB, bootstrap.Options{APIKey: bootstrapKey}); err != nil {
		t.Fatalf("bootstrap default tenant: %v", err)
	}

	// Job definitions are read-only here: they are ScheduledJob Custom Resources
	// projected into revisions. A manual trigger publishes a JobRun through the
	// same publisher a cancel patches, so both routes are live.
	jobRepo := adminpostgres.NewJobRepository(adminDB)
	listJobsUC := jobquery.NewListJobsUseCase(jobRepo)
	getJobUC := jobquery.NewGetJobUseCase(jobRepo)
	publisherScheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(publisherScheme); err != nil {
		t.Fatalf("build publisher scheme: %v", err)
	}
	publisher := kube.JobRunPublisher{Client: dynamicfake.NewSimpleDynamicClient(publisherScheme)}
	triggerJobUC := jobcommand.NewTriggerJobUseCase(jobRepo, publisher)

	runRepo := adminpostgres.NewRunRepository(adminDB)
	handler := NewHandler(listJobsUC, getJobUC, triggerJobUC)
	handler.SetListInstancesUseCase(runquery.NewListRunsUseCase(runRepo))
	handler.SetGetInstanceUseCase(runquery.NewGetRunUseCase(runRepo))
	handler.SetCancelRunUseCase(runcommand.NewCancelRunUseCase(runRepo, publisher))
	handler.SetListAttemptsUseCase(runquery.NewListAttemptsUseCase(runRepo))

	tenantRepo := adminpostgres.NewTenantRepository(adminDB)
	handler.SetCreateTenantUseCase(tenantcommand.NewCreator(tenantRepo))
	handler.SetListTenantsUseCase(tenantquery.NewLister(tenantRepo))
	handler.SetGetTenantUseCase(tenantquery.NewGetter(tenantRepo))

	apiKeyRepo := adminpostgres.NewAPIKeyRepository(adminDB)
	policyRepo := adminpostgres.NewPolicyRepository(adminDB)
	groupRepo := adminpostgres.NewResourceGroupRepository(adminDB)

	createAPIKeyUC := apikeycommand.NewCreator(apiKeyRepo).WithPolicies(policyRepo).WithGroups(groupRepo)
	handler.SetCreateAPIKeyUseCase(createAPIKeyUC)
	handler.SetListAPIKeysUseCase(apikeyquery.NewLister(apiKeyRepo))
	handler.SetRevokeAPIKeyUseCase(apikeycommand.NewRevoker(apiKeyRepo))

	handler.SetCreatePolicyUseCase(policycommand.NewCreator(policyRepo))
	handler.SetListPoliciesUseCase(policyquery.NewLister(policyRepo))
	handler.SetGetPolicyUseCase(policyquery.NewGetter(policyRepo))
	handler.SetDeletePolicyUseCase(policycommand.NewDeleter(policyRepo))

	handler.SetCreateGroupUseCase(resourcegroupcommand.NewCreator(groupRepo))
	handler.SetListGroupsUseCase(resourcegroupquery.NewLister(groupRepo))

	// Check, check-run, SLI and SLO use cases. Their by-id routes are
	// group-scoped too, so the integration server has to be able to reach them.
	checkWriteRepo := corepostgres.NewCheckRepository(adminDB)
	checkReadRepo := adminpostgres.NewCheckRepository(adminDB)
	handler.SetCreateCheckUseCase(checkcommand.NewCreateCheckUseCase(checkWriteRepo))
	handler.SetListChecksUseCase(checkquery.NewListChecksUseCase(checkReadRepo))
	handler.SetGetCheckUseCase(checkquery.NewGetCheckUseCase(checkReadRepo))
	handler.SetPauseCheckUseCase(checkcommand.NewPauseCheckUseCase(checkWriteRepo))
	handler.SetResumeCheckUseCase(checkcommand.NewResumeCheckUseCase(checkWriteRepo))
	handler.SetDeleteCheckUseCase(checkcommand.NewDeleteCheckUseCase(checkWriteRepo))

	checkRunReadRepo := adminpostgres.NewCheckRunRepository(adminDB)
	handler.SetListCheckRunsUseCase(checkrunquery.NewListCheckRunsUseCase(checkRunReadRepo))
	handler.SetGetCheckRunUseCase(checkrunquery.NewGetCheckRunUseCase(checkRunReadRepo))

	sliWriteRepo := corepostgres.NewSLIRepository(adminDB)
	sliReadRepo := adminpostgres.NewSLIReadRepository(adminDB)
	handler.SetCreateSLIUseCase(slicommand.NewCreateSLIUseCase(sliWriteRepo))
	handler.SetListSLIsUseCase(sliquery.NewListSLIsUseCase(sliReadRepo))
	handler.SetGetSLIUseCase(sliquery.NewGetSLIUseCase(sliReadRepo))
	handler.SetDeleteSLIUseCase(slicommand.NewDeleteSLIUseCase(sliWriteRepo))

	sloWriteRepo := corepostgres.NewSLORepository(adminDB)
	sloReadRepo := adminpostgres.NewSLOReadRepository(adminDB)
	handler.SetCreateSLOUseCase(slocommand.NewCreateSLOUseCase(sloWriteRepo))
	handler.SetListSLOsUseCase(sloquery.NewListSLOsUseCase(sloReadRepo))
	handler.SetGetSLOUseCase(sloquery.NewGetSLOUseCase(sloReadRepo, adminpostgres.NewBudgetReadRepository(adminDB)))
	handler.SetChangeSLOStatusUseCase(slocommand.NewChangeSLOStatusUseCase(sloWriteRepo))
	handler.SetDeleteSLOUseCase(slocommand.NewDeleteSLOUseCase(sloWriteRepo))

	budgetReadRepo := adminpostgres.NewBudgetReadRepository(adminDB)
	handler.SetGetBudgetUseCase(slobudgetquery.NewGetBudgetUseCase(budgetReadRepo))
	handler.SetListBudgetHistoryUseCase(slobudgetquery.NewListBudgetHistoryUseCase(budgetReadRepo))

	alertReadRepo := adminpostgres.NewBudgetAlertReadRepository(adminDB)
	handler.SetGetAlertUseCase(sloalertquery.NewGetAlertUseCase(alertReadRepo))
	handler.SetListAlertsUseCase(sloalertquery.NewListAlertsUseCase(alertReadRepo))

	auth := middleware.NewAuth(adminDB)
	// Wire the policy loader: without it, requests authenticate but carry no
	// grants, so every guarded route denies.
	auth.Documents = policyRepo

	r := gin.Default()
	r.Use(auth.Middleware())
	handler.Register(r)

	return httptest.NewServer(r), db, bootstrapKey
}

func postJSON(t *testing.T, client *http.Client, url, authKey string, body any) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(http.MethodPost, url, bodyReader)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if authKey != "" {
		req.Header.Set("Authorization", "Bearer "+authKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func get(t *testing.T, client *http.Client, url, authKey string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if authKey != "" {
		req.Header.Set("Authorization", "Bearer "+authKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func requireStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("expected status %d, got %d: %s", want, resp.StatusCode, string(body))
	}
}

func decodeJSON(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("decode response: %v (body: %s)", err, string(body))
	}
}
