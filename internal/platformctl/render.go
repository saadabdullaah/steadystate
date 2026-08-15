package platformctl

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

type FileChange struct {
	Path    string `json:"path" yaml:"path"`
	Action  string `json:"action" yaml:"action"`
	Content []byte `json:"-" yaml:"-"`
	Before  []byte `json:"-" yaml:"-"`
}

type ChangeSet struct {
	RequestID      string       `json:"requestID" yaml:"requestID"`
	Operation      string       `json:"operation" yaml:"operation"`
	ProposalDigest string       `json:"proposalDigest" yaml:"proposalDigest"`
	RenderDigest   string       `json:"renderDigest" yaml:"renderDigest"`
	Files          []FileChange `json:"files" yaml:"files"`
}

type changeRenderer struct {
	repository *os.Root
	request    ChangeRequest
	catalog    TenantCatalog
	changes    map[string]FileChange
}

func RenderChange(root string, request ChangeRequest) (ChangeSet, error) {
	if err := request.Validate(); err != nil {
		return ChangeSet{}, err
	}
	catalog, err := LoadCatalog(root)
	if err != nil {
		return ChangeSet{}, err
	}
	repository, err := os.OpenRoot(root)
	if err != nil {
		return ChangeSet{}, err
	}
	defer func() { _ = repository.Close() }()
	renderer := &changeRenderer{repository: repository, request: request, catalog: catalog, changes: map[string]FileChange{}}
	if err := renderer.render(); err != nil {
		return ChangeSet{}, err
	}
	catalogData, err := yaml.Marshal(renderer.catalog)
	if err != nil {
		return ChangeSet{}, err
	}
	if err := renderer.set(CatalogRelativePath, catalogData); err != nil {
		return ChangeSet{}, err
	}
	if len(renderer.changes) == 0 {
		return ChangeSet{}, exitError(ExitConflict, "operation produces no repository change")
	}
	paths := make([]string, 0, len(renderer.changes))
	for path := range renderer.changes {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	files := make([]FileChange, 0, len(paths))
	hasher := sha256.New()
	for _, path := range paths {
		change := renderer.changes[path]
		files = append(files, change)
		_, _ = hasher.Write([]byte(change.Action + "\x00" + change.Path + "\x00"))
		_, _ = hasher.Write(change.Content)
	}
	proposalDigest, err := request.Digest()
	if err != nil {
		return ChangeSet{}, err
	}
	return ChangeSet{
		RequestID: request.RequestID, Operation: request.Operation, ProposalDigest: proposalDigest,
		RenderDigest: "sha256:" + hex.EncodeToString(hasher.Sum(nil)), Files: files,
	}, nil
}

func (r *changeRenderer) render() error {
	p := r.request.Parameters
	switch r.request.Operation {
	case "team.create":
		if r.tenantExists(p.Name) {
			return exitError(ExitConflict, "Team %q already exists", p.Name)
		}
		tenant := CatalogTenant{Name: p.Name, TeamPath: teamPath(p.Name), Owners: normalizeStrings(p.Owners), Lifecycle: "Active", Applications: []CatalogApplication{}, Databases: []CatalogDatabase{}}
		r.catalog.Tenants = append(r.catalog.Tenants, tenant)
		return r.writeTeam(p.Name, p)
	case "team.update":
		tenant, err := r.activeTenant(p.Name)
		if err != nil {
			return err
		}
		tenant.Owners = normalizeStrings(p.Owners)
		return r.writeTeam(p.Name, p)
	case "team.delete":
		return r.approveTeamDeletion(p)
	case "team.finalize":
		return r.finalizeTeam(p)
	case "app.create":
		return r.createApplication(p)
	case "app.update":
		return r.updateApplication(p)
	case "app.delete":
		return r.approveApplicationDeletion(p)
	case "app.finalize":
		return r.finalizeApplication(p)
	case "database.create", "database.restore":
		return r.createDatabase(p)
	case "database.update":
		return r.updateDatabase(p)
	case "database.delete":
		return r.approveDatabaseDeletion(p)
	case "database.finalize":
		return r.finalizeDatabase(p)
	case "service.scaffold":
		return r.scaffoldService(p)
	case "service.activate":
		return r.activateService(p)
	case "service.retire":
		return r.retireService(p)
	case "service.finalize":
		return r.finalizeService(p)
	default:
		return exitError(ExitUsage, "unsupported operation %q", r.request.Operation)
	}
}

func (r *changeRenderer) scaffoldService(p ChangeParameters) error {
	for _, tenant := range r.catalog.Tenants {
		for _, service := range tenant.Services {
			if service.Name == p.Name {
				return exitError(ExitConflict, "service %q already exists", p.Name)
			}
		}
	}
	tenant, err := r.tenant(p.Team)
	if err != nil {
		if !p.CreateTeam || p.Team != p.Name {
			return err
		}
		teamParameters := ChangeParameters{Name: p.Team, Owners: []string{p.Team + "-owners"}, AllowedRepositories: []string{"ghcr.io/saadabdullaah/steadystate-services"}, CPUQuota: "2", MemoryQuota: "4Gi"}
		tenantValue := CatalogTenant{Name: p.Team, TeamPath: teamPath(p.Team), Owners: normalizeStrings(teamParameters.Owners), Lifecycle: "Active", Applications: []CatalogApplication{}, Databases: []CatalogDatabase{}, Services: []CatalogService{}}
		r.catalog.Tenants = append(r.catalog.Tenants, tenantValue)
		sort.Slice(r.catalog.Tenants, func(i, j int) bool { return r.catalog.Tenants[i].Name < r.catalog.Tenants[j].Name })
		tenant, _ = r.tenant(p.Team)
		if err := r.writeTeam(p.Team, teamParameters); err != nil {
			return err
		}
	} else if p.CreateTeam {
		return exitError(ExitConflict, "Team %q already exists; omit --create-team", p.Team)
	}
	if tenant.Lifecycle != "Active" {
		return exitError(ExitConflict, "Team %q is retiring", p.Team)
	}
	databaseRef := ""
	if p.WithDatabase {
		databaseRef = p.Name
		if databaseExists(tenant, databaseRef) {
			return exitError(ExitConflict, "Database %q already exists", databaseRef)
		}
		databaseParameters := ChangeParameters{Team: p.Team, Name: databaseRef, Instances: 1, StorageSize: "1Gi", BackupSchedule: "0 0 2 * * *", BackupRetention: "7d"}
		tenant.Databases = append(tenant.Databases, CatalogDatabase{Name: databaseRef, Path: databasePath(databaseRef), Lifecycle: "Active"})
		sort.Slice(tenant.Databases, func(i, j int) bool { return tenant.Databases[i].Name < tenant.Databases[j].Name })
		if err := r.writeDatabase(p.Team, databaseParameters); err != nil {
			return err
		}
	}
	service := CatalogService{Name: p.Name, Path: servicePath(p.Name), Template: p.Template, Version: p.Version, Components: templateComponents(p.Name, p.Template), DatabaseRef: databaseRef, OwnsTeam: p.CreateTeam, OwnsDatabase: p.WithDatabase, Lifecycle: "Scaffolded"}
	tenant.Services = append(tenant.Services, service)
	sort.Slice(tenant.Services, func(i, j int) bool { return tenant.Services[i].Name < tenant.Services[j].Name })
	return r.writeServiceSource(service)
}

func (r *changeRenderer) activateService(p ChangeParameters) error {
	tenant, err := r.activeTenant(p.Team)
	if err != nil {
		return err
	}
	service, err := serviceEntry(tenant, p.Name)
	if err != nil {
		return err
	}
	if service.Lifecycle == "Retiring" {
		return exitError(ExitConflict, "service %q is retiring", p.Name)
	}
	service.Version = p.Version
	for _, component := range service.Components {
		parameters := ChangeParameters{Team: p.Team, Name: component, Owner: p.Team + "-owners", ImageRepository: serviceImageRepository, ImageTag: serviceImageTag(*service, component, p.Version), Port: 8080, MinReplicas: 1, MaxReplicas: 3}
		if service.Template == "full-stack" && component == p.Name+"-api" {
			parameters.DatabaseRef = service.DatabaseRef
		}
		entry, entryErr := applicationEntry(tenant, component)
		if entryErr != nil {
			tenant.Applications = append(tenant.Applications, CatalogApplication{Name: component, Path: applicationPath(component), DatabaseRef: parameters.DatabaseRef, Lifecycle: "Active"})
		} else {
			if entry.Lifecycle != "Active" {
				return exitError(ExitConflict, "Application %q is retiring", component)
			}
			entry.DatabaseRef = parameters.DatabaseRef
		}
		if err := r.writeApplicationWithIsolation(p.Team, parameters, service.Template != "full-stack"); err != nil {
			return err
		}
	}
	sort.Slice(tenant.Applications, func(i, j int) bool { return tenant.Applications[i].Name < tenant.Applications[j].Name })
	service.Lifecycle = "Active"
	return r.writeServiceVersion(service)
}

func (r *changeRenderer) retireService(p ChangeParameters) error {
	tenant, err := r.activeTenant(p.Team)
	if err != nil {
		return err
	}
	service, err := serviceEntry(tenant, p.Name)
	if err != nil {
		return err
	}
	if service.Lifecycle == "Retiring" {
		return exitError(ExitConflict, "service %q is already retiring", p.Name)
	}
	service.Lifecycle, service.DeletionRequest = "Retiring", r.request.RequestID
	for _, component := range service.Components {
		application, entryErr := applicationEntry(tenant, component)
		if entryErr == nil {
			application.Lifecycle, application.DeletionRequest = "Retiring", r.request.RequestID
			if err := r.approveManifest(applicationManifestPath(component), r.request.RequestID, p.Force); err != nil {
				return err
			}
		}
	}
	if service.OwnsDatabase && service.DatabaseRef != "" {
		database, entryErr := databaseEntry(tenant, service.DatabaseRef)
		if entryErr == nil {
			database.Lifecycle, database.DeletionRequest = "Retiring", r.request.RequestID
			if err := r.approveManifest(databaseManifestPath(database.Name), r.request.RequestID, p.Force); err != nil {
				return err
			}
		}
	}
	if service.OwnsTeam {
		tenant.Lifecycle, tenant.DeletionRequest = "Retiring", r.request.RequestID
		if err := r.approveManifest(teamManifestPath(tenant.Name), r.request.RequestID, p.Force); err != nil {
			return err
		}
	}
	return nil
}

func (r *changeRenderer) finalizeService(p ChangeParameters) error {
	tenant, err := r.tenant(p.Team)
	if err != nil {
		return err
	}
	service, err := serviceEntry(tenant, p.Name)
	if err != nil {
		return err
	}
	if err := requireRetiring(service.Lifecycle, service.DeletionRequest, p); err != nil {
		return err
	}
	for _, component := range service.Components {
		if entry, entryErr := applicationEntry(tenant, component); entryErr == nil {
			if err := requireRetiring(entry.Lifecycle, entry.DeletionRequest, p); err != nil {
				return err
			}
			if err := r.deleteLeaf(entry.Path, "application.yaml"); err != nil {
				return err
			}
		}
	}
	if service.OwnsDatabase && service.DatabaseRef != "" {
		if entry, entryErr := databaseEntry(tenant, service.DatabaseRef); entryErr == nil {
			if err := requireRetiring(entry.Lifecycle, entry.DeletionRequest, p); err != nil {
				return err
			}
			if err := r.deleteLeaf(entry.Path, "database.yaml"); err != nil {
				return err
			}
		}
	}
	if err := r.deleteServiceSource(*service); err != nil {
		return err
	}
	if service.OwnsTeam {
		if err := requireRetiring(tenant.Lifecycle, tenant.DeletionRequest, p); err != nil {
			return err
		}
		if err := r.deleteLeaf(tenant.TeamPath, "team.yaml"); err != nil {
			return err
		}
		filtered := r.catalog.Tenants[:0]
		for _, candidate := range r.catalog.Tenants {
			if candidate.Name != tenant.Name {
				filtered = append(filtered, candidate)
			}
		}
		r.catalog.Tenants = filtered
		return nil
	}
	apps := tenant.Applications[:0]
	for _, app := range tenant.Applications {
		if !contains(service.Components, app.Name) {
			apps = append(apps, app)
		}
	}
	tenant.Applications = apps
	if service.OwnsDatabase {
		databases := tenant.Databases[:0]
		for _, db := range tenant.Databases {
			if db.Name != service.DatabaseRef {
				databases = append(databases, db)
			}
		}
		tenant.Databases = databases
	}
	services := tenant.Services[:0]
	for _, candidate := range tenant.Services {
		if candidate.Name != service.Name {
			services = append(services, candidate)
		}
	}
	tenant.Services = services
	return nil
}

func (r *changeRenderer) tenant(name string) (*CatalogTenant, error) {
	for index := range r.catalog.Tenants {
		if r.catalog.Tenants[index].Name == name {
			return &r.catalog.Tenants[index], nil
		}
	}
	return nil, exitError(ExitNotFound, "Team %q is not present in the catalog", name)
}

func (r *changeRenderer) tenantExists(name string) bool {
	tenant, _ := r.tenant(name)
	return tenant != nil
}

func (r *changeRenderer) activeTenant(name string) (*CatalogTenant, error) {
	tenant, err := r.tenant(name)
	if err != nil {
		return nil, err
	}
	if tenant.Lifecycle != "Active" {
		return nil, exitError(ExitConflict, "Team %q is retiring", name)
	}
	return tenant, nil
}

func (r *changeRenderer) createApplication(p ChangeParameters) error {
	tenant, err := r.activeTenant(p.Team)
	if err != nil {
		return err
	}
	for _, catalogTenant := range r.catalog.Tenants {
		for _, item := range catalogTenant.Applications {
			if item.Name == p.Name {
				return exitError(ExitConflict, "Application %q already exists", p.Name)
			}
		}
	}
	for _, item := range tenant.Applications {
		if item.Name == p.Name {
			return exitError(ExitConflict, "Application %q already exists", p.Name)
		}
	}
	if p.DatabaseRef != "" && !databaseExists(tenant, p.DatabaseRef) {
		return exitError(ExitNotFound, "Database %q is not active in Team %q", p.DatabaseRef, p.Team)
	}
	tenant.Applications = append(tenant.Applications, CatalogApplication{Name: p.Name, Path: applicationPath(p.Name), DatabaseRef: p.DatabaseRef, Lifecycle: "Active"})
	sort.Slice(tenant.Applications, func(i, j int) bool { return tenant.Applications[i].Name < tenant.Applications[j].Name })
	return r.writeApplication(p.Team, p)
}

func (r *changeRenderer) updateApplication(p ChangeParameters) error {
	tenant, err := r.activeTenant(p.Team)
	if err != nil {
		return err
	}
	application, err := applicationEntry(tenant, p.Name)
	if err != nil {
		return err
	}
	if application.Lifecycle != "Active" {
		return exitError(ExitConflict, "Application %q is retiring", p.Name)
	}
	if p.DatabaseRef != "" && !databaseExists(tenant, p.DatabaseRef) {
		return exitError(ExitNotFound, "Database %q is not active in Team %q", p.DatabaseRef, p.Team)
	}
	application.DatabaseRef = p.DatabaseRef
	return r.writeApplication(p.Team, p)
}

func (r *changeRenderer) createDatabase(p ChangeParameters) error {
	tenant, err := r.activeTenant(p.Team)
	if err != nil {
		return err
	}
	for _, catalogTenant := range r.catalog.Tenants {
		for _, item := range catalogTenant.Databases {
			if item.Name == p.Name {
				return exitError(ExitConflict, "Database %q already exists", p.Name)
			}
		}
	}
	for _, item := range tenant.Databases {
		if item.Name == p.Name {
			return exitError(ExitConflict, "Database %q already exists", p.Name)
		}
	}
	tenant.Databases = append(tenant.Databases, CatalogDatabase{Name: p.Name, Path: databasePath(p.Name), Lifecycle: "Active"})
	sort.Slice(tenant.Databases, func(i, j int) bool { return tenant.Databases[i].Name < tenant.Databases[j].Name })
	return r.writeDatabase(p.Team, p)
}

func (r *changeRenderer) updateDatabase(p ChangeParameters) error {
	tenant, err := r.activeTenant(p.Team)
	if err != nil {
		return err
	}
	database, err := databaseEntry(tenant, p.Name)
	if err != nil {
		return err
	}
	if database.Lifecycle != "Active" {
		return exitError(ExitConflict, "Database %q is retiring", p.Name)
	}
	return r.writeDatabase(p.Team, p)
}

func (r *changeRenderer) approveTeamDeletion(p ChangeParameters) error {
	tenant, err := r.activeTenant(p.Name)
	if err != nil {
		return err
	}
	tenant.Lifecycle, tenant.DeletionRequest = "Retiring", r.request.RequestID
	if err := r.approveManifest(teamManifestPath(p.Name), r.request.RequestID, p.Force); err != nil {
		return err
	}
	for index := range tenant.Applications {
		tenant.Applications[index].Lifecycle, tenant.Applications[index].DeletionRequest = "Retiring", r.request.RequestID
		if err := r.approveManifest(applicationManifestPath(tenant.Applications[index].Name), r.request.RequestID, p.Force); err != nil {
			return err
		}
	}
	for index := range tenant.Databases {
		tenant.Databases[index].Lifecycle, tenant.Databases[index].DeletionRequest = "Retiring", r.request.RequestID
		if err := r.approveManifest(databaseManifestPath(tenant.Databases[index].Name), r.request.RequestID, p.Force); err != nil {
			return err
		}
	}
	for index := range tenant.Services {
		tenant.Services[index].Lifecycle, tenant.Services[index].DeletionRequest = "Retiring", r.request.RequestID
	}
	return nil
}

func (r *changeRenderer) approveApplicationDeletion(p ChangeParameters) error {
	tenant, err := r.activeTenant(p.Team)
	if err != nil {
		return err
	}
	application, err := applicationEntry(tenant, p.Name)
	if err != nil {
		return err
	}
	if application.Lifecycle != "Active" {
		return exitError(ExitConflict, "Application %q is already retiring", p.Name)
	}
	application.Lifecycle, application.DeletionRequest = "Retiring", r.request.RequestID
	return r.approveManifest(applicationManifestPath(p.Name), r.request.RequestID, p.Force)
}

func (r *changeRenderer) approveDatabaseDeletion(p ChangeParameters) error {
	tenant, err := r.activeTenant(p.Team)
	if err != nil {
		return err
	}
	for _, application := range tenant.Applications {
		if application.DatabaseRef == p.Name && application.Lifecycle == "Active" {
			return exitError(ExitConflict, "Database %q is still referenced by Application %q", p.Name, application.Name)
		}
	}
	database, err := databaseEntry(tenant, p.Name)
	if err != nil {
		return err
	}
	if database.Lifecycle != "Active" {
		return exitError(ExitConflict, "Database %q is already retiring", p.Name)
	}
	database.Lifecycle, database.DeletionRequest = "Retiring", r.request.RequestID
	return r.approveManifest(databaseManifestPath(p.Name), r.request.RequestID, p.Force)
}

func (r *changeRenderer) finalizeTeam(p ChangeParameters) error {
	tenant, err := r.tenant(p.Name)
	if err != nil {
		return err
	}
	if err := requireRetiring(tenant.Lifecycle, tenant.DeletionRequest, p); err != nil {
		return err
	}
	if err := r.deleteLeaf(teamPath(p.Name), "team.yaml"); err != nil {
		return err
	}
	for _, application := range tenant.Applications {
		if err := r.deleteLeaf(application.Path, "application.yaml"); err != nil {
			return err
		}
	}
	for _, database := range tenant.Databases {
		if err := r.deleteLeaf(database.Path, "database.yaml"); err != nil {
			return err
		}
	}
	for _, service := range tenant.Services {
		if err := r.deleteServiceSource(service); err != nil {
			return err
		}
	}
	filtered := r.catalog.Tenants[:0]
	for _, item := range r.catalog.Tenants {
		if item.Name != p.Name {
			filtered = append(filtered, item)
		}
	}
	r.catalog.Tenants = filtered
	return nil
}

func (r *changeRenderer) finalizeApplication(p ChangeParameters) error {
	tenant, err := r.activeTenant(p.Team)
	if err != nil {
		return err
	}
	application, err := applicationEntry(tenant, p.Name)
	if err != nil {
		return err
	}
	if err := requireRetiring(application.Lifecycle, application.DeletionRequest, p); err != nil {
		return err
	}
	if err := r.deleteLeaf(application.Path, "application.yaml"); err != nil {
		return err
	}
	filtered := tenant.Applications[:0]
	for _, item := range tenant.Applications {
		if item.Name != p.Name {
			filtered = append(filtered, item)
		}
	}
	tenant.Applications = filtered
	return nil
}

func (r *changeRenderer) finalizeDatabase(p ChangeParameters) error {
	tenant, err := r.activeTenant(p.Team)
	if err != nil {
		return err
	}
	database, err := databaseEntry(tenant, p.Name)
	if err != nil {
		return err
	}
	if err := requireRetiring(database.Lifecycle, database.DeletionRequest, p); err != nil {
		return err
	}
	if err := r.deleteLeaf(database.Path, "database.yaml"); err != nil {
		return err
	}
	filtered := tenant.Databases[:0]
	for _, item := range tenant.Databases {
		if item.Name != p.Name {
			filtered = append(filtered, item)
		}
	}
	tenant.Databases = filtered
	return nil
}

func requireRetiring(lifecycle, request string, p ChangeParameters) error {
	if lifecycle != "Retiring" || request != p.DeletionRequest {
		return exitError(ExitConflict, "resource is not retiring under deletion request %q", p.DeletionRequest)
	}
	return nil
}

func (r *changeRenderer) approveManifest(path, requestID string, force bool) error {
	if err := validateChangePath(path); err != nil {
		return err
	}
	data, err := r.repository.ReadFile(filepath.FromSlash(path))
	if err != nil {
		return exitError(ExitNotFound, "read protected manifest %s: %v", path, err)
	}
	var object map[string]any
	if err := yaml.Unmarshal(data, &object); err != nil {
		return err
	}
	metadata, _ := object["metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
		object["metadata"] = metadata
	}
	annotations, _ := metadata["annotations"].(map[string]any)
	if annotations == nil {
		annotations = map[string]any{}
		metadata["annotations"] = annotations
	}
	delete(annotations, "argocd.argoproj.io/sync-options")
	annotations["steadystate.dev/deletion-request"] = requestID
	if force {
		annotations["steadystate.dev/force-delete"] = "true"
	}
	output, err := yaml.Marshal(object)
	if err != nil {
		return err
	}
	return r.set(path, output)
}

func (r *changeRenderer) writeTeam(name string, p ChangeParameters) error {
	object := map[string]any{
		"apiVersion": "platform.steadystate.dev/v1alpha1", "kind": "Team",
		"metadata": metadata(name, "", "-1"),
		"spec":     map[string]any{"owners": normalizeStrings(p.Owners), "quota": map[string]any{"cpu": p.CPUQuota, "memory": p.MemoryQuota}, "allowedRepositories": normalizeStrings(p.AllowedRepositories)},
	}
	return r.writeLeaf(teamPath(name), "team.yaml", object)
}

func (r *changeRenderer) writeApplication(team string, p ChangeParameters) error {
	return r.writeApplicationWithIsolation(team, p, true)
}

func (r *changeRenderer) writeApplicationWithIsolation(team string, p ChangeParameters, networkIsolation bool) error {
	spec := map[string]any{
		"owner": p.Owner, "image": map[string]any{"repository": p.ImageRepository, "tag": p.ImageTag},
		"runtime":       map[string]any{"port": p.Port, "replicas": map[string]any{"min": p.MinReplicas, "max": p.MaxReplicas}},
		"resources":     map[string]any{"requests": map[string]any{"cpu": "50m", "memory": "32Mi"}, "limits": map[string]any{"cpu": "200m", "memory": "128Mi"}},
		"deployment":    map[string]any{"strategy": "canary", "automaticRollback": true, "steps": []any{canaryStep(10), canaryStep(25), canaryStep(50), canaryStep(100)}},
		"reliability":   map[string]any{"availabilityTarget": "99.9%", "maximumP95Latency": "250ms", "maximumErrorRate": "1%"},
		"observability": map[string]any{"metrics": true, "logs": true, "traces": true},
		"security":      map[string]any{"requireSignedImage": true, "runAsNonRoot": true, "networkIsolation": networkIsolation},
	}
	// Database bindings are profile-dependent. The catalog records the desired
	// relationship and the root chart adds databaseRef only when the data
	// foundation is enabled. Keeping the leaf profile-neutral lets the same
	// service run in standard mode without waiting forever for a Database that
	// is intentionally not installed.
	object := map[string]any{"apiVersion": "platform.steadystate.dev/v1alpha1", "kind": "Application", "metadata": metadata(p.Name, "team-"+team, "1"), "spec": spec}
	return r.writeLeaf(applicationPath(p.Name), "application.yaml", object)
}

func (r *changeRenderer) writeDatabase(team string, p ChangeParameters) error {
	spec := map[string]any{
		"engine": "postgres", "instances": p.Instances,
		"storage": map[string]any{"size": p.StorageSize, "storageClassName": "local-path"},
		"backups": map[string]any{"enabled": true, "schedule": p.BackupSchedule, "retention": p.BackupRetention},
	}
	switch r.request.Operation {
	case "database.restore":
		recovery := map[string]any{"sourceServerName": p.SourceServerName}
		if p.TargetTime != "" {
			recovery["targetTime"] = p.TargetTime
		}
		spec["recovery"] = recovery
	case "database.update":
		manifestPath := databaseManifestPath(p.Name)
		if err := validateChangePath(manifestPath); err != nil {
			return err
		}
		data, err := r.repository.ReadFile(filepath.FromSlash(manifestPath))
		if err != nil {
			return err
		}
		var existing map[string]any
		if err := yaml.Unmarshal(data, &existing); err != nil {
			return err
		}
		existingSpec, _, _ := unstructured.NestedMap(existing, "spec")
		if recovery, found, _ := unstructured.NestedMap(existingSpec, "recovery"); found {
			spec["recovery"] = recovery
		}
		if storage, found, _ := unstructured.NestedMap(existingSpec, "storage"); found {
			if currentClass, ok := storage["storageClassName"].(string); ok && currentClass != "" {
				spec["storage"].(map[string]any)["storageClassName"] = currentClass
			}
			if currentSize, ok := storage["size"].(string); ok {
				oldQuantity, oldErr := resource.ParseQuantity(currentSize)
				newQuantity, newErr := resource.ParseQuantity(p.StorageSize)
				if oldErr != nil || newErr != nil || newQuantity.Cmp(oldQuantity) < 0 {
					return exitError(ExitUsage, "Database storage size may only increase from %s", currentSize)
				}
			}
		}
	}
	object := map[string]any{"apiVersion": "platform.steadystate.dev/v1alpha1", "kind": "Database", "metadata": metadata(p.Name, "team-"+team, "0"), "spec": spec}
	return r.writeLeaf(databasePath(p.Name), "database.yaml", object)
}

func metadata(name, namespace, wave string) map[string]any {
	value := map[string]any{"name": name, "annotations": map[string]any{"argocd.argoproj.io/sync-wave": wave, "argocd.argoproj.io/sync-options": "Prune=false"}}
	if namespace != "" {
		value["namespace"] = namespace
	}
	return value
}

func canaryStep(weight int) map[string]any { return map[string]any{"weight": weight, "pause": "30s"} }

func (r *changeRenderer) writeLeaf(path, manifest string, object map[string]any) error {
	data, err := yaml.Marshal(object)
	if err != nil {
		return err
	}
	if err := r.set(path+"/"+manifest, data); err != nil {
		return err
	}
	kustomization := []byte("apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n  - " + manifest + "\n")
	return r.set(path+"/kustomization.yaml", kustomization)
}

func (r *changeRenderer) deleteLeaf(path, manifest string) error {
	if err := validateChangePath(path + "/" + manifest); err != nil {
		return err
	}
	directory, err := r.repository.Open(filepath.FromSlash(path))
	if err != nil {
		return exitError(ExitNotFound, "cannot inspect leaf %s: %v", path, err)
	}
	defer func() { _ = directory.Close() }()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return exitError(ExitNotFound, "cannot inspect leaf %s: %v", path, err)
	}
	allowed := map[string]bool{manifest: true, "kustomization.yaml": true}
	for _, entry := range entries {
		if !allowed[entry.Name()] {
			return exitError(ExitConflict, "leaf %s contains unexpected file %s", path, entry.Name())
		}
	}
	for _, file := range []string{manifest, "kustomization.yaml"} {
		if err := r.remove(path + "/" + file); err != nil {
			return err
		}
	}
	return nil
}

func (r *changeRenderer) set(path string, content []byte) error {
	if err := validateChangePath(path); err != nil {
		return err
	}
	before, err := r.repository.ReadFile(filepath.FromSlash(path))
	action := "update"
	if os.IsNotExist(err) {
		before, action = nil, "create"
	} else if err != nil {
		return err
	}
	if string(before) == string(content) {
		return nil
	}
	r.changes[path] = FileChange{Path: path, Action: action, Content: content, Before: before}
	return nil
}

func (r *changeRenderer) remove(path string) error {
	if err := validateChangePath(path); err != nil {
		return err
	}
	before, err := r.repository.ReadFile(filepath.FromSlash(path))
	if err != nil {
		return exitError(ExitNotFound, "cannot finalize missing file %s", path)
	}
	r.changes[path] = FileChange{Path: path, Action: "delete", Before: before}
	return nil
}

func validateChangePath(path string) error {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean != path || strings.HasPrefix(clean, "../") || filepath.IsAbs(path) {
		return exitError(ExitUsage, "unsafe rendered path %q", path)
	}
	if path == CatalogRelativePath {
		return nil
	}
	parts := strings.Split(path, "/")
	if len(parts) >= 3 && parts[0] == "services" && validName(parts[1], 48) {
		relative := strings.Join(parts[2:], "/")
		allowed := map[string]bool{
			"README.md": true, "VERSION": true, "service.yaml": true,
			"api/Dockerfile": true, "api/main.go": true, "api/main_test.go": true,
			"web/Dockerfile": true, "web/package.json": true, "web/package-lock.json": true,
			"web/index.html": true, "web/tsconfig.json": true, "web/vite.config.ts": true,
			"web/src/main.tsx": true, "web/src/style.css": true,
			"web/test/template.test.js": true,
			"web/server/main.go":        true, "web/server/main_test.go": true,
			"web/server/static/index.html": true,
		}
		if !allowed[relative] {
			return exitError(ExitUsage, "rendered service file %q is outside the broker allowlist", path)
		}
		return nil
	}
	if len(parts) != 4 || parts[0] != "gitops" || (parts[1] != "teams" && parts[1] != "applications" && parts[1] != "databases") || !validName(parts[2], 63) {
		return exitError(ExitUsage, "rendered path %q is outside the broker allowlist", path)
	}
	allowedFile := map[string]map[string]bool{"teams": {"team.yaml": true, "kustomization.yaml": true}, "applications": {"application.yaml": true, "kustomization.yaml": true}, "databases": {"database.yaml": true, "kustomization.yaml": true}}
	if !allowedFile[parts[1]][parts[3]] {
		return exitError(ExitUsage, "rendered file %q is outside the broker allowlist", path)
	}
	return nil
}

func ApplyChangeSet(root string, set ChangeSet) error {
	repository, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = repository.Close() }()
	for _, change := range set.Files {
		if err := validateChangePath(change.Path); err != nil {
			return err
		}
		relative := filepath.FromSlash(change.Path)
		current, readErr := repository.ReadFile(relative)
		if change.Action == "create" {
			if readErr == nil || !os.IsNotExist(readErr) {
				return exitError(ExitConflict, "create target %s changed after rendering", change.Path)
			}
		} else if readErr != nil || string(current) != string(change.Before) {
			return exitError(ExitConflict, "target %s changed after rendering", change.Path)
		}
		switch change.Action {
		case "create", "update":
			if err := repository.MkdirAll(filepath.Dir(relative), 0o755); err != nil {
				return err
			}
			if err := repository.WriteFile(relative, change.Content, 0o644); err != nil {
				return err
			}
		case "delete":
			if err := repository.Remove(relative); err != nil {
				return err
			}
		default:
			return exitError(ExitUsage, "unsupported file action %q", change.Action)
		}
	}
	for _, change := range set.Files {
		if change.Action == "delete" {
			_ = repository.Remove(filepath.Dir(filepath.FromSlash(change.Path)))
		}
	}
	return nil
}

func ChangeSetDiff(set ChangeSet) (string, error) {
	var builder strings.Builder
	for _, change := range set.Files {
		diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{A: difflib.SplitLines(string(change.Before)), B: difflib.SplitLines(string(change.Content)), FromFile: "a/" + change.Path, ToFile: "b/" + change.Path, Context: 3})
		if err != nil {
			return "", err
		}
		builder.WriteString(diff)
		if diff != "" && !strings.HasSuffix(diff, "\n") {
			builder.WriteByte('\n')
		}
	}
	return builder.String(), nil
}

func ChangeSetJSON(set ChangeSet) ([]byte, error) {
	type file struct {
		Path   string `json:"path"`
		Action string `json:"action"`
	}
	files := make([]file, 0, len(set.Files))
	for _, item := range set.Files {
		files = append(files, file{Path: item.Path, Action: item.Action})
	}
	value := struct {
		RequestID      string `json:"requestID"`
		Operation      string `json:"operation"`
		ProposalDigest string `json:"proposalDigest"`
		RenderDigest   string `json:"renderDigest"`
		Files          []file `json:"files"`
	}{set.RequestID, set.Operation, set.ProposalDigest, set.RenderDigest, files}
	return json.Marshal(value)
}

func teamPath(name string) string                { return "gitops/teams/" + name }
func applicationPath(name string) string         { return "gitops/applications/" + name }
func databasePath(name string) string            { return "gitops/databases/" + name }
func teamManifestPath(name string) string        { return teamPath(name) + "/team.yaml" }
func applicationManifestPath(name string) string { return applicationPath(name) + "/application.yaml" }
func databaseManifestPath(name string) string    { return databasePath(name) + "/database.yaml" }
func servicePath(name string) string             { return "services/" + name }

func applicationEntry(tenant *CatalogTenant, name string) (*CatalogApplication, error) {
	for index := range tenant.Applications {
		if tenant.Applications[index].Name == name {
			return &tenant.Applications[index], nil
		}
	}
	return nil, exitError(ExitNotFound, "Application %q is not present in Team %q", name, tenant.Name)
}
func databaseEntry(tenant *CatalogTenant, name string) (*CatalogDatabase, error) {
	for index := range tenant.Databases {
		if tenant.Databases[index].Name == name {
			return &tenant.Databases[index], nil
		}
	}
	return nil, exitError(ExitNotFound, "Database %q is not present in Team %q", name, tenant.Name)
}
func databaseExists(tenant *CatalogTenant, name string) bool {
	item, _ := databaseEntry(tenant, name)
	return item != nil && item.Lifecycle == "Active"
}
func serviceEntry(tenant *CatalogTenant, name string) (*CatalogService, error) {
	for index := range tenant.Services {
		if tenant.Services[index].Name == name {
			return &tenant.Services[index], nil
		}
	}
	return nil, exitError(ExitNotFound, "service %q is not present in Team %q", name, tenant.Name)
}
func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
