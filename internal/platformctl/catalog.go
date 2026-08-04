package platformctl

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"sigs.k8s.io/yaml"
)

const CatalogRelativePath = "gitops/clusters/local/catalog/tenants.yaml"

var dnsLabelPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

type TenantCatalog struct {
	APIVersion string          `json:"apiVersion" yaml:"apiVersion"`
	Kind       string          `json:"kind" yaml:"kind"`
	Tenants    []CatalogTenant `json:"tenants" yaml:"tenants"`
}

type CatalogTenant struct {
	Name         string               `json:"name" yaml:"name"`
	TeamPath     string               `json:"teamPath" yaml:"teamPath"`
	Applications []CatalogApplication `json:"applications" yaml:"applications"`
	Databases    []CatalogDatabase    `json:"databases" yaml:"databases"`
}

type CatalogApplication struct {
	Name        string `json:"name" yaml:"name"`
	Path        string `json:"path" yaml:"path"`
	DatabaseRef string `json:"databaseRef,omitempty" yaml:"databaseRef,omitempty"`
}

type CatalogDatabase struct {
	Name string `json:"name" yaml:"name"`
	Path string `json:"path" yaml:"path"`
}

func LoadCatalog(root string) (TenantCatalog, error) {
	path := filepath.Join(root, filepath.FromSlash(CatalogRelativePath))
	data, err := os.ReadFile(path)
	if err != nil {
		return TenantCatalog{}, fmt.Errorf("read tenant catalog: %w", err)
	}
	var catalog TenantCatalog
	if err := yaml.UnmarshalStrict(data, &catalog); err != nil {
		return TenantCatalog{}, exitError(ExitUsage, "parse tenant catalog: %v", err)
	}
	if err := catalog.Validate(root); err != nil {
		return TenantCatalog{}, exitError(ExitUsage, "invalid tenant catalog: %v", err)
	}
	return catalog, nil
}

func (c TenantCatalog) Validate(root string) error {
	if c.APIVersion != CatalogAPIVersion || c.Kind != CatalogKind {
		return fmt.Errorf("expected %s %s", CatalogAPIVersion, CatalogKind)
	}
	seenTeams := map[string]struct{}{}
	seenApplications := map[string]struct{}{}
	seenDatabases := map[string]struct{}{}
	for _, tenant := range c.Tenants {
		if !validName(tenant.Name, 58) {
			return fmt.Errorf("invalid Team name %q", tenant.Name)
		}
		if _, exists := seenTeams[tenant.Name]; exists {
			return fmt.Errorf("duplicate Team %q", tenant.Name)
		}
		seenTeams[tenant.Name] = struct{}{}
		expectedTeamPath := "gitops/teams/" + tenant.Name
		if tenant.TeamPath != expectedTeamPath {
			return fmt.Errorf("team %q path must be %q", tenant.Name, expectedTeamPath)
		}
		if err := requireDirectory(root, tenant.TeamPath); err != nil {
			return err
		}
		tenantDatabases := map[string]struct{}{}
		for _, database := range tenant.Databases {
			if !validName(database.Name, 63) {
				return fmt.Errorf("invalid Database name %q", database.Name)
			}
			if _, exists := seenDatabases[database.Name]; exists {
				return fmt.Errorf("duplicate Database %q", database.Name)
			}
			seenDatabases[database.Name] = struct{}{}
			tenantDatabases[database.Name] = struct{}{}
			expected := "gitops/databases/" + database.Name
			if database.Path != expected {
				return fmt.Errorf("database %q path must be %q", database.Name, expected)
			}
			if err := requireDirectory(root, database.Path); err != nil {
				return err
			}
		}
		for _, application := range tenant.Applications {
			if !validName(application.Name, 63) {
				return fmt.Errorf("invalid Application name %q", application.Name)
			}
			if _, exists := seenApplications[application.Name]; exists {
				return fmt.Errorf("duplicate Application %q", application.Name)
			}
			seenApplications[application.Name] = struct{}{}
			expected := "gitops/applications/" + application.Name
			if application.Path != expected {
				return fmt.Errorf("application %q path must be %q", application.Name, expected)
			}
			if err := requireDirectory(root, application.Path); err != nil {
				return err
			}
			if application.DatabaseRef != "" {
				if _, exists := tenantDatabases[application.DatabaseRef]; !exists {
					return fmt.Errorf("application %q references unknown database %q", application.Name, application.DatabaseRef)
				}
			}
		}
	}
	return nil
}

func validName(name string, max int) bool {
	return len(name) > 0 && len(name) <= max && dnsLabelPattern.MatchString(name)
}

func requireDirectory(root, slashPath string) error {
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(slashPath)))
	if err != nil || !info.IsDir() {
		return fmt.Errorf("catalog path %q is not a directory", slashPath)
	}
	return nil
}

func (c TenantCatalog) SortedTenants() []CatalogTenant {
	tenants := append([]CatalogTenant(nil), c.Tenants...)
	sort.Slice(tenants, func(i, j int) bool { return tenants[i].Name < tenants[j].Name })
	return tenants
}
