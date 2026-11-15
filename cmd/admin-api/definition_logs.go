package main

import (
	"database/sql"
	"fmt"
	"orbitjob/internal/admin/definition"
	"os"
	"strings"

	"k8s.io/client-go/kubernetes"
	adminhttp "orbitjob/internal/admin/http"
	"orbitjob/internal/admin/kube"
	adminpostgres "orbitjob/internal/admin/store/postgres"
	"orbitjob/internal/operator"
	"orbitjob/internal/platform/imagepin"
)

// No mapping means no additional routes or Kubernetes grants. Helm supplies
// the same authoritative table to Admin and Operator; no CR label sets tenancy.
func wireDefinitionLogs(h *adminhttp.Handler, db *sql.DB, publisher kube.JobRunPublisher) error {
	raw := os.Getenv(operator.NamespaceTenantsTableEnv)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	mapping, err := operator.ParseNamespaceTenants(raw)
	if err != nil {
		return err
	}
	cfg, err := inClusterConfigFn()
	if err != nil {
		return fmt.Errorf("log in-cluster config: %w", err)
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("log Kubernetes client: %w", err)
	}
	h.SetDefinitionBackend(definition.Service{
		Backend:              kube.DefinitionClient{Client: publisher.Client, NamespaceTenants: mapping},
		Store:                adminpostgres.DefinitionOperations{DB: db},
		Images:               imagepin.New(strings.Split(os.Getenv("IMAGE_APPROVED_REGISTRIES"), ","), strings.Split(os.Getenv("IMAGE_PRIVATE_REGISTRIES"), ",")),
		AllowLegacyAuthoring: os.Getenv("ALLOW_LEGACY_AUTHORING") == "true",
	})
	h.SetLogBackend(adminpostgres.LogLocatorRepository{DB: db}, kube.LogClient{Client: client, Dynamic: publisher.Client, NamespaceTenants: mapping})
	return nil
}
