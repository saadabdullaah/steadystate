package platformctl

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepositoryCatalogPreservesReleasedTopology(t *testing.T) {
	root := repositoryRoot(t)
	catalog, err := LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Tenants) == 0 {
		t.Fatal("repository catalog has no tenants")
	}
	var tenant *CatalogTenant
	for index := range catalog.Tenants {
		if catalog.Tenants[index].Name == "payments" {
			tenant = &catalog.Tenants[index]
			break
		}
	}
	if tenant == nil {
		t.Fatalf("released payments tenant is missing: %#v", catalog.Tenants)
	}
	if len(tenant.Applications) != 1 || tenant.Applications[0].Name != "demo" || tenant.Applications[0].DatabaseRef != "orders" {
		t.Fatalf("released demo topology changed: %#v", tenant.Applications)
	}
	if len(tenant.Databases) != 1 || tenant.Databases[0].Name != "orders" {
		t.Fatalf("released database topology changed: %#v", tenant.Databases)
	}
}

func TestCatalogRejectsDerivedPathMismatch(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"gitops/teams/payments", "gitops/applications/demo", "gitops/databases/orders"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(path)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	catalog := TenantCatalog{
		APIVersion: CatalogAPIVersion, Kind: CatalogKind,
		Tenants: []CatalogTenant{{
			Name: "payments", TeamPath: "gitops/teams/payments",
			Applications: []CatalogApplication{{Name: "demo", Path: "gitops/applications/wrong"}},
			Databases:    []CatalogDatabase{{Name: "orders", Path: "gitops/databases/orders"}},
		}},
	}
	if err := catalog.Validate(root); err == nil {
		t.Fatal("expected a derived-path validation failure")
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(directory, "..", ".."))
}
