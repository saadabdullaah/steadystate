package platformctl

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

func newInitCommand(options *Options) *cobra.Command {
	var template, team string
	var createTeam, withDatabase, plan bool
	command := &cobra.Command{Use: "init NAME", Short: "Scaffold a golden-path service through a reviewed Git proposal", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		_, selected, err := options.loadContext()
		if err != nil {
			return err
		}
		if team == "" {
			team = args[0]
		}
		ctx, cancel := options.commandContext(cmd.Context())
		defer cancel()
		baseSHA, err := repositoryBaseSHA(ctx, selected)
		if err != nil {
			return err
		}
		request := NewChangeRequest("service.scaffold", baseSHA, ChangeParameters{Team: team, Name: args[0], Template: template, Version: "v0.1.0", CreateTeam: createTeam, WithDatabase: withDatabase})
		return planOrSubmit(options, ctx, selected, request, plan)
	}}
	command.Flags().StringVar(&template, "template", "", "golden template: go-api, react-static, or full-stack")
	command.Flags().StringVar(&team, "team", "", "existing Team name (defaults to NAME)")
	command.Flags().BoolVar(&createTeam, "create-team", false, "create an isolated Team owned by this service")
	command.Flags().BoolVar(&withDatabase, "with-database", false, "create and attach PostgreSQL (full-stack only)")
	command.Flags().BoolVar(&plan, "plan", false, "render and show the deterministic change without submitting")
	_ = command.MarkFlagRequired("template")
	return command
}

func newServiceCommand(options *Options) *cobra.Command {
	command := &cobra.Command{Use: "service", Short: "Manage generated-service lifecycle"}
	command.AddCommand(newTypedWriteCommand(options, "service.retire", "retire NAME", "Approve protected service retirement"))
	command.AddCommand(newTypedWriteCommand(options, "service.finalize", "finalize NAME", "Finalize an approved service retirement"))
	command.AddCommand(newServiceReleasePlanCommand(options))
	command.AddCommand(newServiceActivationProposalCommand(options))
	return command
}

func newServiceActivationProposalCommand(options *Options) *cobra.Command {
	var team, name, version, baseSHA string
	command := &cobra.Command{Use: "activation-proposal", Hidden: true, Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		requestID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("steadystate-service-activation:"+name+":"+baseSHA)).String()
		request := ChangeRequest{APIVersion: ChangeRequestAPIVersion, Kind: ChangeRequestKind, SchemaVersion: ChangeRequestSchema, RequestID: requestID, BaseSHA: baseSHA, RendererVersion: RendererVersion, Operation: "service.activate", Parameters: ChangeParameters{Team: team, Name: name, Version: version}}
		encoded, err := request.Encode()
		if err != nil {
			return err
		}
		root, err := os.Getwd()
		if err != nil {
			return err
		}
		set, err := RenderChange(root, request)
		if err != nil {
			return err
		}
		data, err := ChangeSetJSON(set)
		if err != nil {
			return err
		}
		var validation any
		if err := json.Unmarshal(data, &validation); err != nil {
			return err
		}
		output, err := json.Marshal(map[string]any{"requestID": requestID, "encoded": encoded, "validation": validation})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(options.Stdout, string(output))
		return err
	}}
	command.Flags().StringVar(&team, "team", "", "owning Team")
	command.Flags().StringVar(&name, "name", "", "generated service")
	command.Flags().StringVar(&version, "version", "", "released service version")
	command.Flags().StringVar(&baseSHA, "base-sha", "", "exact activation base")
	for _, flag := range []string{"team", "name", "version", "base-sha"} {
		_ = command.MarkFlagRequired(flag)
	}
	return command
}

type serviceReleaseItem struct {
	Service     string `json:"service"`
	Team        string `json:"team"`
	Component   string `json:"component"`
	Application string `json:"application"`
	Version     string `json:"version"`
	Path        string `json:"path"`
	Dockerfile  string `json:"dockerfile"`
	SemverTag   string `json:"semverTag"`
	SHATag      string `json:"shaTag"`
}

func newServiceReleasePlanCommand(options *Options) *cobra.Command {
	var baseSHA, sourceSHA string
	command := &cobra.Command{Use: "release-plan", Hidden: true, Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		root, err := os.Getwd()
		if err != nil {
			return err
		}
		items, err := buildServiceReleasePlan(root, baseSHA, sourceSHA)
		if err != nil {
			return err
		}
		data, err := json.Marshal(map[string]any{"include": items})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(options.Stdout, string(data))
		return err
	}}
	command.Flags().StringVar(&baseSHA, "base-sha", "", "previous main commit")
	command.Flags().StringVar(&sourceSHA, "source-sha", "", "exact source commit")
	_ = command.MarkFlagRequired("base-sha")
	_ = command.MarkFlagRequired("source-sha")
	return command
}

func buildServiceReleasePlan(root, baseSHA, sourceSHA string) ([]serviceReleaseItem, error) {
	if !gitObjectPattern.MatchString(baseSHA) || !gitObjectPattern.MatchString(sourceSHA) {
		return nil, exitError(ExitUsage, "release SHAs must be full lowercase Git object IDs")
	}
	head, err := runExternal(context.Background(), root, "git", "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	if head != sourceSHA {
		return nil, exitError(ExitConflict, "release source SHA does not match checkout HEAD")
	}
	changed, err := runExternal(context.Background(), root, "git", "diff", "--name-only", baseSHA, sourceSHA, "--", "services")
	if err != nil {
		return nil, err
	}
	serviceNames := map[string]bool{}
	for _, path := range strings.Split(changed, "\n") {
		parts := strings.Split(filepath.ToSlash(strings.TrimSpace(path)), "/")
		if len(parts) >= 3 && parts[0] == "services" && validName(parts[1], 48) {
			serviceNames[parts[1]] = true
		}
	}
	catalog, err := LoadCatalog(root)
	if err != nil {
		return nil, err
	}
	items := []serviceReleaseItem{}
	for serviceName := range serviceNames {
		if _, statErr := os.Stat(filepath.Join(root, "services", serviceName, "service.yaml")); os.IsNotExist(statErr) {
			continue
		} else if statErr != nil {
			return nil, statErr
		}
		tenant, service, findErr := findCatalogService(catalog, serviceName)
		if findErr != nil {
			return nil, findErr
		}
		descriptorData, readErr := os.ReadFile(filepath.Join(root, "services", serviceName, "service.yaml"))
		if readErr != nil {
			return nil, readErr
		}
		var descriptor serviceDescriptor
		if err := yaml.UnmarshalStrict(descriptorData, &descriptor); err != nil {
			return nil, exitError(ExitUsage, "invalid service descriptor %s: %v", serviceName, err)
		}
		versionData, readErr := os.ReadFile(filepath.Join(root, "services", serviceName, "VERSION"))
		if readErr != nil {
			return nil, readErr
		}
		version := strings.TrimSpace(string(versionData))
		if descriptor.Name != serviceName || descriptor.Version != version || !semverPattern.MatchString(version) {
			return nil, exitError(ExitUsage, "service %q descriptor and VERSION must agree", serviceName)
		}
		expectedService := *service
		expectedService.Version = version
		expectedData, expectedErr := serviceDescriptorFor(expectedService)
		if expectedErr != nil {
			return nil, expectedErr
		}
		var expectedDescriptor serviceDescriptor
		if err := yaml.UnmarshalStrict(expectedData, &expectedDescriptor); err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(descriptor, expectedDescriptor) {
			return nil, exitError(ExitUsage, "service %q descriptor paths and components must remain renderer-derived", serviceName)
		}
		oldVersion, oldErr := runExternal(context.Background(), root, "git", "show", baseSHA+":services/"+serviceName+"/VERSION")
		if oldErr == nil && oldVersion == version && runtimeServiceChange(changed, serviceName) {
			return nil, exitError(ExitConflict, "runtime changes for service %q require a VERSION bump", serviceName)
		}
		for _, component := range descriptor.Components {
			if component.Name != "api" && component.Name != "web" {
				return nil, exitError(ExitUsage, "service %q has invalid component", serviceName)
			}
			prefix := serviceName + "-" + component.Name
			items = append(items, serviceReleaseItem{Service: serviceName, Team: tenant.Name, Component: component.Name, Application: component.Application, Version: version, Path: "services/" + serviceName + "/" + component.Path, Dockerfile: "services/" + serviceName + "/" + component.Dockerfile, SemverTag: prefix + "-" + version, SHATag: prefix + "-sha-" + sourceSHA})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Service == items[j].Service {
			return items[i].Component < items[j].Component
		}
		return items[i].Service < items[j].Service
	})
	return items, nil
}

func runtimeServiceChange(changed, service string) bool {
	prefix := "services/" + service + "/"
	for _, path := range strings.Split(changed, "\n") {
		relative := strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(path)), prefix)
		if relative != path && relative != "README.md" && relative != "VERSION" && relative != "service.yaml" {
			return true
		}
	}
	return false
}

func planOrSubmit(options *Options, ctx context.Context, selected Context, request ChangeRequest, plan bool) error {
	set, err := RenderChange(selected.CheckoutPath, request)
	if err != nil {
		return err
	}
	diff, err := ChangeSetDiff(set)
	if err != nil {
		return err
	}
	if !options.Quiet && options.Format == "table" {
		_, _ = fmt.Fprint(options.Stdout, diff)
	}
	if plan {
		return printChangeSummary(options, set, "planned", "", diff)
	}
	confirmed, err := confirmSubmission(options, request.Operation, request.Parameters.Name)
	if err != nil {
		return err
	}
	if !confirmed {
		return exitError(ExitUsage, "proposal was not submitted")
	}
	encoded, err := request.Encode()
	if err != nil {
		return err
	}
	runURL, err := dispatchChange(ctx, selected, request, encoded)
	if err != nil {
		return err
	}
	return printChangeSummary(options, set, "submitted", runURL, diff)
}

func newDevCommand(options *Options) *cobra.Command {
	var bootstrap, databaseTunnel bool
	command := &cobra.Command{Use: "dev NAME", Short: "Run a generated service with a host-native edit loop", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		_, selected, err := options.loadContext()
		if err != nil {
			return err
		}
		catalog, err := LoadCatalog(selected.CheckoutPath)
		if err != nil {
			return err
		}
		tenant, service, err := findCatalogService(catalog, args[0])
		if err != nil {
			return err
		}
		if bootstrap {
			ctx, cancel := options.commandContext(cmd.Context())
			_, err = runExternal(ctx, selected.CheckoutPath, "pwsh", "-NoProfile", "-File", filepath.Join(selected.CheckoutPath, "scripts", "dev.ps1"), "bootstrap", "-Profile", selected.Profile)
			cancel()
			if err != nil {
				return err
			}
		}
		return runDevelopment(cmd.Context(), options, selected, tenant, *service, databaseTunnel)
	}}
	command.Flags().BoolVar(&bootstrap, "bootstrap", false, "bootstrap the configured local profile before starting")
	command.Flags().BoolVar(&databaseTunnel, "database-tunnel", false, "forward and inject the configured PostgreSQL connection")
	return command
}

func findCatalogService(catalog TenantCatalog, name string) (*CatalogTenant, *CatalogService, error) {
	for tenantIndex := range catalog.Tenants {
		for serviceIndex := range catalog.Tenants[tenantIndex].Services {
			if catalog.Tenants[tenantIndex].Services[serviceIndex].Name == name {
				return &catalog.Tenants[tenantIndex], &catalog.Tenants[tenantIndex].Services[serviceIndex], nil
			}
		}
	}
	return nil, nil, exitError(ExitNotFound, "service %q is not present in the catalog", name)
}

func runDevelopment(ctx context.Context, options *Options, selected Context, tenant *CatalogTenant, service CatalogService, databaseTunnel bool) error {
	parent := ctx
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	baseEnv := append([]string(nil), os.Environ()...)
	apiEnv := []string(nil)
	apiPath := ""
	if service.Template == "go-api" || service.Template == "full-stack" {
		apiPort := "8080"
		if service.Template == "full-stack" {
			apiPort = "8081"
		}
		apiPath = "./services/" + service.Name + "/api"
		apiEnv = append(baseEnv, "PORT="+apiPort, "STEADYSTATE_APP_NAME="+service.Components[len(service.Components)-1], "STEADYSTATE_APP_NAMESPACE=team-"+tenant.Name, "STEADYSTATE_APP_VERSION="+service.Version)
	}
	var tunnel *exec.Cmd
	if databaseTunnel {
		if service.DatabaseRef == "" {
			return exitError(ExitUsage, "service %q has no Database", service.Name)
		}
		arguments := kubeArguments(selected, "-n", "team-"+tenant.Name, "port-forward", "service/"+service.DatabaseRef+"-rw", "5432:5432")
		tunnel = exec.CommandContext(ctx, "kubectl", arguments...) // #nosec G204 -- validated catalog and config values are passed as arguments, not through a shell.
		tunnel.Dir, tunnel.Stdout, tunnel.Stderr = selected.CheckoutPath, options.Stdout, options.Stderr
		if err := tunnel.Start(); err != nil {
			return exitError(ExitRemote, "start database tunnel: %v", err)
		}
		defer terminateProcess(tunnel)
		uri, err := databaseURI(ctx, selected, tenant.Name, service.DatabaseRef)
		if err != nil {
			return err
		}
		apiEnv = append(apiEnv, "DATABASE_URL="+uri)
	}
	results := make(chan error, 2)
	processes := 0
	if apiPath != "" {
		processes++
		go func() { results <- watchGoDevelopment(ctx, options, selected.CheckoutPath, apiPath, apiEnv) }()
	}
	if service.Template == "react-static" || service.Template == "full-stack" {
		web := exec.CommandContext(ctx, "npm", "--prefix", filepath.Join("services", service.Name, "web"), "run", "dev") // #nosec G204 -- service is a validated catalog DNS label.
		web.Dir, web.Env = selected.CheckoutPath, baseEnv
		web.Stdout, web.Stderr, web.Stdin = options.Stdout, options.Stderr, options.Stdin
		if err := web.Start(); err != nil {
			return exitError(ExitRemote, "start frontend development process: %v", err)
		}
		processes++
		go func() { results <- web.Wait() }()
	}
	if processes == 0 {
		return exitError(ExitUsage, "service template has no development process")
	}
	firstErr := <-results
	cancel()
	for index := 1; index < processes; index++ {
		<-results
	}
	if parent.Err() != nil {
		return nil
	}
	if firstErr != nil {
		return exitError(ExitRemote, "development process stopped: %v", firstErr)
	}
	return nil
}

func watchGoDevelopment(ctx context.Context, options *Options, root, packagePath string, environment []string) error {
	watchRoot := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(packagePath, "./")))
	stamp, err := sourceStamp(watchRoot)
	if err != nil {
		return err
	}
	for {
		process := exec.CommandContext(ctx, "go", "run", packagePath) // #nosec G204 -- packagePath is derived from a validated catalog DNS label.
		process.Dir, process.Env, process.Stdout, process.Stderr, process.Stdin = root, environment, options.Stdout, options.Stderr, options.Stdin
		if err := process.Start(); err != nil {
			return err
		}
		completed := make(chan error, 1)
		go func() { completed <- process.Wait() }()
		ticker := time.NewTicker(750 * time.Millisecond)
		waitingForChange := false
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				if process.Process != nil {
					_ = process.Process.Kill()
				}
				return nil
			case runErr := <-completed:
				if ctx.Err() != nil {
					ticker.Stop()
					return nil
				}
				waitingForChange = true
				_, _ = fmt.Fprintf(options.Stderr, "Go service stopped (%v); waiting for a source change.\n", runErr)
			case <-ticker.C:
				current, stampErr := sourceStamp(watchRoot)
				if stampErr != nil {
					ticker.Stop()
					terminateProcess(process)
					return stampErr
				}
				if current == stamp {
					continue
				}
				stamp = current
				ticker.Stop()
				if !waitingForChange && process.Process != nil {
					_ = process.Process.Kill()
					<-completed
				}
				timer := time.NewTimer(500 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil
				case <-timer.C:
				}
				_, _ = fmt.Fprintln(options.Stderr, "Go source changed; rebuilding.")
				goto restart
			}
		}
	restart:
	}
}

func sourceStamp(root string) (string, error) {
	var builder strings.Builder
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(&builder, "%s:%d:%d\n", filepath.ToSlash(path), info.Size(), info.ModTime().UnixNano())
		return nil
	})
	return builder.String(), err
}

func databaseURI(ctx context.Context, selected Context, team, database string) (string, error) {
	encoded, err := runExternal(ctx, selected.CheckoutPath, "kubectl", kubeArguments(selected, "-n", "team-"+team, "get", "secret", database+"-postgres-app", "-o", "jsonpath={.data.uri}")...)
	if err != nil {
		return "", exitError(ExitRemote, "read database connection Secret: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", exitError(ExitRemote, "decode database connection Secret")
	}
	uri := string(decoded)
	if index := strings.Index(uri, "@"); index >= 0 {
		if slash := strings.Index(uri[index:], "/"); slash >= 0 {
			uri = uri[:index+1] + "127.0.0.1:5432" + uri[index+slash:]
		}
	}
	return uri, nil
}

func kubeArguments(selected Context, arguments ...string) []string {
	result := []string{}
	if selected.Kubeconfig != "" {
		result = append(result, "--kubeconfig", selected.Kubeconfig)
	}
	if selected.KubeContext != "" {
		result = append(result, "--context", selected.KubeContext)
	}
	return append(result, arguments...)
}

func terminateProcess(command *exec.Cmd) {
	if command != nil && command.Process != nil {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
	}
}
