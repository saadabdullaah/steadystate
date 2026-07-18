package gitops_test

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kyaml "k8s.io/apimachinery/pkg/util/yaml"
)

const (
	repositoryURL = "https://github.com/saadabdullaah/steadystate.git"
	testRevision  = "0123456789abcdef0123456789abcdef01234567"
)

func TestGitOpsRendersDeterministically(t *testing.T) {
	root := repositoryRoot(t)
	helmArguments := []string{
		"template", "steadystate-root", filepath.Join(root, "gitops", "clusters", "local"),
		"--namespace", "argocd",
		"--set-string", "gitRevision=" + testRevision,
	}
	first := run(t, root, "helm", helmArguments...)
	second := run(t, root, "helm", helmArguments...)
	if !bytes.Equal(first, second) {
		t.Fatal("Helm root rendering is not deterministic")
	}
	decodeManifests(t, first)

	for _, path := range []string{
		"gitops/platform",
		"gitops/teams/payments",
		"gitops/applications/demo",
	} {
		arguments := []string{"build", filepath.Join(root, filepath.FromSlash(path))}
		first = run(t, root, "kustomize", arguments...)
		second = run(t, root, "kustomize", arguments...)
		if !bytes.Equal(first, second) {
			t.Fatalf("Kustomize rendering is not deterministic for %s", path)
		}
		decodeManifests(t, first)
	}
}

func TestRootChartRevisionOrderingAndSyncBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	rendered := run(t, root, "helm",
		"template", "steadystate-root", filepath.Join(root, "gitops", "clusters", "local"),
		"--namespace", "argocd",
		"--set-string", "gitRevision="+testRevision,
	)
	objects := decodeManifests(t, rendered)
	if len(objects) != 8 {
		t.Fatalf("root chart rendered %d objects, want 8", len(objects))
	}

	for _, name := range []string{"root", "tenant"} {
		project := findObject(t, objects, "AppProject", name)
		assertAnnotation(t, project, "argocd.argoproj.io/sync-wave", "-30")
		assertOnlyRepository(t, project)
	}
	platformProject := findObject(t, objects, "AppProject", "platform")
	assertAnnotation(t, platformProject, "argocd.argoproj.io/sync-wave", "-30")
	assertExactSet(t, stringSlice(t, platformProject, "spec", "sourceRepos"), []string{
		repositoryURL,
		"https://argoproj.github.io/argo-helm",
		"https://prometheus-community.github.io/helm-charts",
	})

	expectedWaves := map[string]string{
		"argocd-configuration": "-20",
		"monitoring":           "-18",
		"argo-rollouts":        "-17",
		"steadystate-operator": "-10",
		"payments":             "0",
	}
	for name, wave := range expectedWaves {
		application := findObject(t, objects, "Application", name)
		assertAnnotation(t, application, "argocd.argoproj.io/sync-wave", wave)
	}

	argocd := findObject(t, objects, "Application", "argocd-configuration")
	assertString(t, argocd, repositoryURL, "spec", "source", "repoURL")
	assertString(t, argocd, "gitops/platform", "spec", "source", "path")
	assertString(t, argocd, testRevision, "spec", "source", "targetRevision")
	assertAutomated(t, argocd, true)

	assertExternalChartApplication(t, objects, "monitoring", "https://prometheus-community.github.io/helm-charts", "kube-prometheus-stack", "87.16.1", "gitops/platform/monitoring/values.yaml", "monitoring")
	assertExternalChartApplication(t, objects, "argo-rollouts", "https://argoproj.github.io/argo-helm", "argo-rollouts", "2.41.0", "gitops/platform/rollouts/values.yaml", "argo-rollouts")

	operator := findObject(t, objects, "Application", "steadystate-operator")
	assertString(t, operator, repositoryURL, "spec", "source", "repoURL")
	assertString(t, operator, "config/default", "spec", "source", "path")
	assertString(t, operator, testRevision, "spec", "source", "targetRevision")
	assertAutomated(t, operator, true)
	images, found, err := unstructured.NestedStringSlice(operator, "spec", "source", "kustomize", "images")
	if err != nil || !found || len(images) != 1 || images[0] != "ghcr.io/saadabdullaah/steadystate-operator:v0.3.0" {
		t.Fatalf("operator image override is invalid: %#v, found=%v, err=%v", images, found, err)
	}

	payments := findObject(t, objects, "Application", "payments")
	assertAutomated(t, payments, false)
	sources := nestedSlice(t, payments, "spec", "sources")
	if len(sources) != 2 {
		t.Fatalf("payments has %d sources, want 2", len(sources))
	}
	expectedPaths := []string{"gitops/teams/payments", "gitops/applications/demo"}
	for index, rawSource := range sources {
		source, ok := rawSource.(map[string]any)
		if !ok {
			t.Fatalf("payments source %d is %T", index, rawSource)
		}
		assertString(t, source, repositoryURL, "repoURL")
		assertString(t, source, testRevision, "targetRevision")
		assertString(t, source, expectedPaths[index], "path")
		envsubst, found, err := unstructured.NestedBool(source, "kustomize", "commonAnnotationsEnvsubst")
		if err != nil || !found || !envsubst {
			t.Fatalf("source %s does not enable commonAnnotationsEnvsubst", expectedPaths[index])
		}
		annotations, found, err := unstructured.NestedStringMap(source, "kustomize", "commonAnnotations")
		if err != nil || !found || annotations["steadystate.dev/source-revision"] != "$ARGOCD_APP_REVISION" {
			t.Fatalf("source %s has invalid revision annotations: %#v", expectedPaths[index], annotations)
		}
	}
	options, found, err := unstructured.NestedStringSlice(payments, "spec", "syncPolicy", "syncOptions")
	if err != nil || !found || !contains(options, "RespectIgnoreDifferences=true") {
		t.Fatalf("payments does not respect ignored operator fields: %#v", options)
	}
	encoded := string(rendered)
	for _, pointer := range []string{"/metadata/finalizers", "/status"} {
		if strings.Count(encoded, pointer) < 2 {
			t.Fatalf("operator-owned field %s is not ignored for Team and Application", pointer)
		}
	}
	if strings.Contains(encoded, "CreateNamespace=") {
		t.Fatal("CreateNamespace must not be enabled")
	}
}

func TestProjectRestrictions(t *testing.T) {
	root := repositoryRoot(t)
	objects := decodeManifests(t, run(t, root, "helm",
		"template", "steadystate-root", filepath.Join(root, "gitops", "clusters", "local"),
		"--set-string", "gitRevision="+testRevision,
	))

	rootProject := findObject(t, objects, "AppProject", "root")
	rootKinds := resourceKinds(t, rootProject, "namespaceResourceWhitelist")
	assertExactSet(t, rootKinds, []string{"argoproj.io/AppProject", "argoproj.io/Application"})
	if _, found, _ := unstructured.NestedSlice(rootProject, "spec", "clusterResourceWhitelist"); found {
		t.Fatal("root project must not permit cluster-scoped resources")
	}

	platform := findObject(t, objects, "AppProject", "platform")
	platformKinds := append(
		resourceKinds(t, platform, "clusterResourceWhitelist"),
		resourceKinds(t, platform, "namespaceResourceWhitelist")...,
	)
	for _, forbidden := range []string{"platform.steadystate.dev/Team", "platform.steadystate.dev/Application"} {
		if contains(platformKinds, forbidden) {
			t.Fatalf("platform project unexpectedly permits %s", forbidden)
		}
	}
	assertExactSet(t, platformKinds, []string{
		"/Namespace",
		"apiextensions.k8s.io/CustomResourceDefinition",
		"rbac.authorization.k8s.io/ClusterRole",
		"rbac.authorization.k8s.io/ClusterRoleBinding",
		"/ConfigMap",
		"/Secret",
		"/ServiceAccount",
		"/Service",
		"apps/Deployment",
		"gateway.networking.k8s.io/HTTPRoute",
		"networking.k8s.io/NetworkPolicy",
		"rbac.authorization.k8s.io/Role",
		"rbac.authorization.k8s.io/RoleBinding",
		"monitoring.coreos.com/Alertmanager",
		"monitoring.coreos.com/Prometheus",
		"monitoring.coreos.com/ServiceMonitor",
	})
	platformDestinations := nestedSlice(t, platform, "spec", "destinations")
	actualDestinations := make([]string, 0, len(platformDestinations))
	for _, raw := range platformDestinations {
		actualDestinations = append(actualDestinations, raw.(map[string]any)["namespace"].(string))
	}
	assertExactSet(t, actualDestinations, []string{"argocd", "steadystate-system", "monitoring", "argo-rollouts"})

	tenant := findObject(t, objects, "AppProject", "tenant")
	assertExactSet(t, resourceKinds(t, tenant, "clusterResourceWhitelist"), []string{"platform.steadystate.dev/Team"})
	assertExactSet(t, resourceKinds(t, tenant, "namespaceResourceWhitelist"), []string{"platform.steadystate.dev/Application"})
	warn, found, err := unstructured.NestedBool(tenant, "spec", "orphanedResources", "warn")
	if err != nil || !found || warn {
		t.Fatalf("tenant orphan warning must be explicitly false: found=%v value=%v err=%v", found, warn, err)
	}
	destinations := nestedSlice(t, tenant, "spec", "destinations")
	if len(destinations) != 1 {
		t.Fatalf("tenant has %d destinations, want 1", len(destinations))
	}
	destination := destinations[0].(map[string]any)
	assertString(t, destination, "team-*", "namespace")
}

func TestTenantLeavesContainOnlyOwnedCustomResources(t *testing.T) {
	root := repositoryRoot(t)
	teamObjects := decodeManifests(t, run(t, root, "kustomize", "build", filepath.Join(root, "gitops", "teams", "payments")))
	if len(teamObjects) != 1 || objectString(teamObjects[0], "kind") != "Team" {
		t.Fatalf("Team leaf rendered unexpected objects: %#v", objectIdentities(teamObjects))
	}
	assertAnnotation(t, teamObjects[0], "argocd.argoproj.io/sync-wave", "-1")

	applicationObjects := decodeManifests(t, run(t, root, "kustomize", "build", filepath.Join(root, "gitops", "applications", "demo")))
	if len(applicationObjects) != 1 || objectString(applicationObjects[0], "kind") != "Application" ||
		objectString(applicationObjects[0], "apiVersion") != "platform.steadystate.dev/v1alpha1" {
		t.Fatalf("Application leaf rendered unexpected objects: %#v", objectIdentities(applicationObjects))
	}
	assertAnnotation(t, applicationObjects[0], "argocd.argoproj.io/sync-wave", "0")
}

func TestArgoConfigurationContracts(t *testing.T) {
	root := repositoryRoot(t)
	objects := decodeManifests(t, run(t, root, "kustomize", "build", filepath.Join(root, "gitops", "platform")))
	namespace := findObject(t, objects, "Namespace", "argocd")
	labels, found, err := unstructured.NestedStringMap(namespace, "metadata", "labels")
	if err != nil || !found || labels["steadystate.dev/gateway-access"] != "true" {
		t.Fatalf("argocd namespace does not opt into the shared Gateway: %#v", labels)
	}

	config := findObject(t, objects, "ConfigMap", "argocd-cm")
	data, found, err := unstructured.NestedStringMap(config, "data")
	if err != nil || !found {
		t.Fatalf("argocd-cm data missing: %v", err)
	}
	required := []string{
		"application.resourceTrackingMethod",
		"resource.customizations.health.platform.steadystate.dev_Application",
		"resource.customizations.health.platform.steadystate.dev_Team",
		"resource.customizations.health.argoproj.io_Application",
	}
	for _, key := range required {
		if data[key] == "" {
			t.Errorf("argocd-cm is missing %s", key)
		}
	}
	healthContracts := map[string][]string{
		"resource.customizations.health.platform.steadystate.dev_Application": {
			"observedGeneration", `phase == "Degraded"`, `phase == "Healthy"`, `condition.type == "Ready"`,
		},
		"resource.customizations.health.platform.steadystate.dev_Team": {
			"observedGeneration", `condition.status == "True"`, `condition.status == "False"`,
		},
		"resource.customizations.health.argoproj.io_Application": {
			"obj.status.health.status",
		},
	}
	for key, tokens := range healthContracts {
		for _, token := range tokens {
			if !strings.Contains(data[key], token) {
				t.Errorf("health customization %s is missing %q", key, token)
			}
		}
	}
	if data["application.resourceTrackingMethod"] != "annotation" {
		t.Fatal("Argo resource tracking must be annotation-based")
	}

	parameters := findObject(t, objects, "ConfigMap", "argocd-cmd-params-cm")
	assertString(t, parameters, "true", "data", "server.insecure")

	route := findObject(t, objects, "HTTPRoute", "argocd")
	hostnames, found, err := unstructured.NestedStringSlice(route, "spec", "hostnames")
	if err != nil || !found || len(hostnames) != 1 || hostnames[0] != "argocd.localtest.me" {
		t.Fatalf("unexpected Argo hostnames: %#v", hostnames)
	}
	parentRefs := nestedSlice(t, route, "spec", "parentRefs")
	if len(parentRefs) != 1 {
		t.Fatalf("Argo route has %d parentRefs", len(parentRefs))
	}
	parent := parentRefs[0].(map[string]any)
	assertString(t, parent, "steadystate", "name")
	assertString(t, parent, "steadystate-system", "namespace")
	findObject(t, objects, "Namespace", "monitoring")
	findObject(t, objects, "Namespace", "argo-rollouts")
}

func TestProgressiveDeliveryValuesAreFrozenAndMinimal(t *testing.T) {
	root := repositoryRoot(t)
	rollouts := string(readFile(t, filepath.Join(root, "gitops", "platform", "rollouts", "values.yaml")))
	for _, token := range []string{
		"gatewayapi-plugin-linux-amd64",
		"v0.16.0",
		"3f129e6a1ea948932f440b16dfb9eae636a065857eb27311928e5790593103c1",
		"gatewayAPI: false",
		"dashboard:\n  enabled: false",
		"- httproutes",
		"- patch",
	} {
		if !strings.Contains(rollouts, token) {
			t.Errorf("Rollouts values are missing %q", token)
		}
	}
	for _, provider := range []string{"istio", "smi", "ambassador", "awsLoadBalancerController", "awsAppMesh", "traefik", "apisix", "contour", "glooPlatform"} {
		if !strings.Contains(rollouts, provider+": false") {
			t.Errorf("provider %s is not disabled", provider)
		}
	}

	monitoring := string(readFile(t, filepath.Join(root, "gitops", "platform", "monitoring", "values.yaml")))
	for _, token := range []string{
		"retention: 6h",
		"scrapeInterval: 15s",
		"evaluationInterval: 15s",
		"serviceMonitorNamespaceSelector: {}",
		"ruleNamespaceSelector: {}",
		"nodeExporter:\n  enabled: false",
		"prometheus-node-exporter:\n  enabled: false",
		"defaultRules:\n  create: false",
		"GF_SECURITY_DISABLE_INITIAL_ADMIN_CREATION: \"true\"",
		"auth.anonymous:",
		"prometheusOperator:\n  tls:\n    enabled: false",
		"memory: 320Mi",
		"memory: 448Mi",
	} {
		if !strings.Contains(monitoring, token) {
			t.Errorf("monitoring values are missing %q", token)
		}
	}
}

func TestBootstrapRootResolvesRevisionOnce(t *testing.T) {
	root := repositoryRoot(t)
	rendered := run(t, root, "helm",
		"template", "steadystate-root", filepath.Join(root, "gitops", "clusters", "local"),
		"--set", "bootstrapRoot=true",
		"--set-string", "rootTargetRevision=checkpoint-branch",
		"--show-only", "templates/root-application.yaml.tpl",
	)
	objects := decodeManifests(t, rendered)
	rootApplication := findObject(t, objects, "Application", "steadystate-root")
	assertString(t, rootApplication, "root", "spec", "project")
	assertString(t, rootApplication, "checkpoint-branch", "spec", "source", "targetRevision")
	parameters := nestedSlice(t, rootApplication, "spec", "source", "helm", "parameters")
	if len(parameters) != 1 {
		t.Fatalf("root application has %d Helm parameters", len(parameters))
	}
	parameter := parameters[0].(map[string]any)
	assertString(t, parameter, "gitRevision", "name")
	assertString(t, parameter, "$ARGOCD_APP_REVISION", "value")
	assertAutomated(t, rootApplication, true)
}

func TestGitOpsCommandsAreMirrored(t *testing.T) {
	root := repositoryRoot(t)
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	devScript, err := os.ReadFile(filepath.Join(root, "scripts", "dev.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"deploy-gitops", "test-gitops", "undeploy-gitops", "verify-gitops", "verify-progressive-delivery", "test-progressive-delivery"} {
		if !strings.Contains(string(makefile), command) {
			t.Errorf("Makefile is missing %s", command)
		}
		if !strings.Contains(string(devScript), "'"+command+"'") {
			t.Errorf("scripts/dev.ps1 is missing %s", command)
		}
	}
}

func TestGitOpsAcceptanceAndTeardownRegressions(t *testing.T) {
	root := repositoryRoot(t)
	content, err := os.ReadFile(filepath.Join(root, "scripts", "gitops.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)

	if !strings.Contains(text, "[AllowEmptyCollection()]") {
		t.Fatal("the acceptance evidence check list must accept its initially empty collection")
	}

	rootDelete := strings.Index(text, "delete application.argoproj.io steadystate-root")
	paymentsDelete := strings.Index(text, "delete application.argoproj.io payments")
	remainingDelete := strings.Index(text, "delete application.argoproj.io argocd-configuration steadystate-operator")
	if rootDelete < 0 || rootDelete >= paymentsDelete || paymentsDelete >= remainingDelete {
		t.Fatal("GitOps teardown must delete root, payments, then the remaining child Applications")
	}

	for _, lookup := range []string{"'applications.platform.steadystate.dev'", "'teams.platform.steadystate.dev'"} {
		if !strings.Contains(text, lookup) {
			t.Fatalf("GitOps acceptance is missing fully qualified lookup %s", lookup)
		}
	}

	for _, command := range []string{
		"steadystate-root -n argocd --ignore-not-found=true --wait=true --timeout=60s",
		"payments monitoring argo-rollouts -n argocd --ignore-not-found=true --wait=true --timeout=120s",
		"applications.platform.steadystate.dev --all --all-namespaces --ignore-not-found=true --wait=true --timeout=180s",
		"teams.platform.steadystate.dev --all --ignore-not-found=true --wait=true --timeout=180s",
		"namespace steadystate-unmanaged --ignore-not-found=true --wait=true --timeout=120s",
		"argocd-configuration steadystate-operator -n argocd --ignore-not-found=true --wait=true --timeout=60s",
		"config/default') --ignore-not-found=true --wait=true --timeout=180s",
	} {
		if !strings.Contains(text, command) {
			t.Fatalf("GitOps teardown is missing bounded delete %q", command)
		}
	}
	devScript, err := os.ReadFile(filepath.Join(root, "scripts", "dev.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{
		"applications.platform.steadystate.dev --all --all-namespaces --ignore-not-found=true --wait=true --timeout=180s",
		"config/default') --ignore-not-found=true --wait=true --timeout=180s",
	} {
		if !strings.Contains(string(devScript), command) {
			t.Fatalf("operator teardown is missing bounded cleanup %q", command)
		}
	}
}

func TestPhase3HostedAcceptanceContracts(t *testing.T) {
	root := repositoryRoot(t)
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(content)
	}
	workflow := read(filepath.Join(root, ".github", "workflows", "nightly.yml"))
	script := read(filepath.Join(root, "scripts", "phase3-acceptance.ps1"))
	tape := read(filepath.Join(root, "docs", "demonstrations", "phase3-gitops-delivery.tape"))

	workflowTokens := []string{
		"timeout-minutes: 90",
		"persist-credentials: false",
		"actions/create-github-app-token@bcd2ba49218906704ab6c1aa796996da409d3eb1",
		"client-id: ${{ vars.STEADYSTATE_BOT_CLIENT_ID }}",
		"private-key: ${{ secrets.STEADYSTATE_BOT_PRIVATE_KEY }}",
		"permission-contents: write",
		"phase3-acceptance-${{ github.sha }}",
		"if-no-files-found: error",
		"Delete Phase 3 acceptance branch",
	}
	for _, token := range workflowTokens {
		if !strings.Contains(workflow, token) {
			t.Errorf("Phase 3 Nightly workflow is missing %q", token)
		}
	}

	requiredChecks := []string{
		"pinned-argocd-installed-dex-absent",
		"argocd-ui-route-reachable",
		"root-platform-team-applications-healthy",
		"baseline-version-reachable",
		"git-commit-detected-without-kubectl-delivery-mutation",
		"candidate-synchronized-and-served",
		"runtime-digest-matches-ghcr-linux-amd64",
		"resolved-git-revision-matches-candidate",
		"kubernetes-and-argo-degraded-on-rejection",
		"recovery-restores-healthy",
		"argo-health-matches-kubernetes-status",
		"operator-outage-preserves-resource-uids",
		"operator-restart-reconciles-without-drift",
		"argo-ownership-boundary",
	}
	for _, check := range requiredChecks {
		if !strings.Contains(script, check) || !strings.Contains(workflow, check) {
			t.Errorf("Phase 3 acceptance check %q is not implemented and verified", check)
		}
	}

	for _, token := range []string{
		"acceptance/phase3-",
		"docs/demonstrations/phase1-self-heal.gif",
		"schemaVersion = 1",
		"sourceSHA = $SourceRevision",
		"baselineCommit = $baselineCommit",
		"candidateCommit = $candidateCommit",
		"rejectionCommit = $rejectionCommit",
		"recoveryCommit = $recoveryCommit",
		"anonymousPull = $true",
		"linux/amd64",
		"RepositoryNotAllowed",
		"control-plane=controller-manager",
		"Assert-ArgoOwnershipBoundary",
		"[Alias('o')]",
		"Invoke-External git commit -m $Message | Out-Host",
		"Write-Evidence -EvidenceResult failed",
	} {
		if !strings.Contains(script, token) {
			t.Errorf("Phase 3 acceptance script is missing %q", token)
		}
	}

	candidateStart := strings.Index(script, "$candidateCommit = New-AcceptanceCommit")
	outageStart := strings.Index(script, "$outageBefore = Get-ResourceSnapshot")
	if candidateStart < 0 || outageStart <= candidateStart {
		t.Fatal("could not locate the Git-only delivery interval")
	}
	deliveryInterval := script[candidateStart:outageStart]
	for _, mutation := range []string{"kubectl apply", "kubectl patch", "kubectl delete"} {
		if strings.Contains(deliveryInterval, mutation) {
			t.Errorf("delivery interval contains Kubernetes mutation %q", mutation)
		}
	}

	diagnostics := strings.Index(workflow, "Capture Phase 3 acceptance diagnostics")
	branchCleanup := strings.Index(workflow, "Delete Phase 3 acceptance branch")
	clusterCleanup := strings.Index(workflow, "Undeploy GitOps foundation")
	if diagnostics < 0 || diagnostics >= branchCleanup || branchCleanup >= clusterCleanup {
		t.Fatal("Phase 3 diagnostics and cleanup ordering is invalid")
	}
	if strings.Count(workflow, "timeout-minutes: 5") < 3 {
		t.Fatal("every destructive Nightly cleanup step must have a five-minute outer timeout")
	}
	if strings.Count(workflow, "docs/demonstrations/phase3-gitops-delivery.gif") < 3 {
		t.Fatal("Phase 3 GIF must be verified, completeness-checked, and uploaded")
	}
	for _, token := range []string{
		"Output docs/demonstrations/phase3-gitops-delivery.gif",
		"scripts/phase3-acceptance.ps1",
		"Set WaitTimeout 20m",
		"Set Framerate 2",
		"Set PlaybackSpeed 8.0",
		"Wait+Screen /PHASE3_ACCEPTANCE_RESULT_(PASSED|FAILED)/",
	} {
		if !strings.Contains(tape, token) {
			t.Errorf("Phase 3 VHS tape is missing %q", token)
		}
	}
	for _, token := range []string{
		"-not $_.metadata.deletionTimestamp",
		"$_.status.phase -eq 'Running'",
		"$_.type -eq 'Ready' -and $_.status -eq 'True'",
		"PHASE3_ACCEPTANCE_RESULT_PASSED",
		"PHASE3_ACCEPTANCE_RESULT_FAILED",
	} {
		if !strings.Contains(script, token) {
			t.Errorf("Phase 3 acceptance script is missing %q", token)
		}
	}
	if strings.Count(script, "Clear-Host") < 2 {
		t.Error("Phase 3 acceptance must present a deterministic final result screen on success and failure")
	}
}

func TestArgoVersionAndChecksumPins(t *testing.T) {
	content, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts", "versions.env"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	if !regexp.MustCompile(`(?m)^ARGO_CD_VERSION=3\.4\.2$`).MatchString(text) {
		t.Fatal("ARGO_CD_VERSION is not pinned to 3.4.2")
	}
	if !regexp.MustCompile("(?m)^ARGO_CD_MANIFEST_SHA256=69114b8c9eb48a1d08598e6f654a0869b10ae902456ea4b70796cb563760f5ec$").MatchString(text) {
		t.Fatal("Argo CD manifest checksum is not pinned")
	}
	for _, pin := range []string{
		`ARGO_ROLLOUTS_CHART_VERSION=2.41.0`,
		`ARGO_ROLLOUTS_VERSION=1.9.0`,
		`GATEWAY_API_PLUGIN_VERSION=0.16.0`,
		`KUBE_PROMETHEUS_STACK_VERSION=87.16.1`,
		`K6_VERSION=2.1.0`,
		`ARGO_ROLLOUTS_CHART_SHA256=ff53d617efb369cd07acc595309e7fa73a602576375e0a52a78dab1e2d970df5`,
		`GATEWAY_API_PLUGIN_LINUX_AMD64_SHA256=3f129e6a1ea948932f440b16dfb9eae636a065857eb27311928e5790593103c1`,
		`KUBE_PROMETHEUS_STACK_CHART_SHA256=153c69faae66a313dc07ed36f77741fd17cc7da86d3d7790b34bf4d4902fe7f4`,
	} {
		if !regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(pin) + `$`).MatchString(text) {
			t.Errorf("missing frozen Phase 4 pin %s", pin)
		}
	}
}

func TestPhase4AcceptanceWorkflowContracts(t *testing.T) {
	root := repositoryRoot(t)
	workflow := string(readFile(t, filepath.Join(root, ".github", "workflows", "phase4.yml")))
	script := string(readFile(t, filepath.Join(root, "scripts", "phase4-acceptance.ps1")))
	promotionTape := string(readFile(t, filepath.Join(root, "docs", "demonstrations", "phase4-canary-promotion.tape")))
	rollbackTape := string(readFile(t, filepath.Join(root, "docs", "demonstrations", "phase4-automatic-rollback.tape")))
	for _, token := range []string{
		"name: Phase 4 acceptance",
		"timeout-minutes: 75",
		"cancel-in-progress: false",
		"phase4-acceptance-${{ github.sha }}",
		"if-no-files-found: error",
		"Capture acceptance diagnostics",
		"Delete ephemeral acceptance branch",
		"Undeploy GitOps",
		"Destroy cluster",
		"permission-contents: write",
		"docs/demonstrations/phase4-canary-promotion.gif",
		"docs/demonstrations/phase4-automatic-rollback.gif",
	} {
		if !strings.Contains(workflow, token) {
			t.Errorf("Phase 4 workflow is missing %q", token)
		}
	}
	for _, token := range []string{
		"acceptance/phase4-",
		"[ValidateSet('Prepare','Promote','Rollback','CaptureFailure')]",
		"sha-$sourceCommit",
		"This delivery commit must change only spec.image.tag.",
		"Invoke-External kubectl kustomize (Split-Path -Parent $ManifestPath) | Out-Null",
		"Invoke-External git commit -m $Message | Out-Host",
		"Measure-Traffic $GoodTag $weight",
		"Measure-Traffic $BadTag 10",
		"Samples = 500",
		"[Math]::Abs($observed - $ExpectedPercent) -gt 8",
		"CanaryAnalysisFailed",
		"Wait-CandidateAlert",
		"Measure-StableWindow",
		"Assert-K6NoFailures 'promotion'",
		"Assert-K6NoFailures 'final-migration'",
		"monitoringWorkingSetBytes",
		"Save-FinalEvidence $state passed",
		"Save-FinalEvidence $state failed $failure",
	} {
		if !strings.Contains(script, token) {
			t.Errorf("Phase 4 acceptance script is missing %q", token)
		}
	}
	diagnostics := strings.Index(workflow, "Capture acceptance diagnostics")
	upload := strings.Index(workflow, "Upload Phase 4 acceptance artifact")
	branchCleanup := strings.Index(workflow, "Delete ephemeral acceptance branch")
	clusterCleanup := strings.Index(workflow, "Undeploy GitOps")
	if diagnostics < 0 || diagnostics >= upload || upload >= branchCleanup || branchCleanup >= clusterCleanup {
		t.Fatal("Phase 4 diagnostics, artifact, and cleanup ordering is invalid")
	}
	gitDelivery := strings.Index(script, "} elseif ($Stage -eq 'Promote')")
	if gitDelivery < 0 {
		t.Fatal("could not locate the Phase 4 Git-only delivery interval")
	}
	for _, mutation := range []string{"kubectl apply", "kubectl patch", "kubectl delete"} {
		if strings.Contains(script[gitDelivery:], mutation) {
			t.Errorf("Phase 4 delivery interval contains Kubernetes mutation %q", mutation)
		}
	}
	for _, tape := range []struct {
		content string
		result  string
	}{
		{promotionTape, "PHASE4_PROMOTION_RESULT_(PASSED|FAILED)"},
		{rollbackTape, "PHASE4_ROLLBACK_RESULT_(PASSED|FAILED)"},
	} {
		for _, token := range []string{"Set WaitTimeout 20m", "Set Framerate 2", "Set PlaybackSpeed 8.0", tape.result} {
			if !strings.Contains(tape.content, token) {
				t.Errorf("Phase 4 VHS tape is missing %q", token)
			}
		}
	}
}

func run(t *testing.T, directory, name string, arguments ...string) []byte {
	t.Helper()
	command := exec.Command(name, arguments...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(arguments, " "), err, output)
	}
	return output
}

func decodeManifests(t *testing.T, content []byte) []map[string]any {
	t.Helper()
	decoder := kyaml.NewYAMLOrJSONDecoder(bytes.NewReader(content), 4096)
	objects := []map[string]any{}
	for {
		object := map[string]any{}
		err := decoder.Decode(&object)
		if err == io.EOF {
			return objects
		}
		if err != nil {
			t.Fatalf("decode rendered YAML: %v", err)
		}
		if len(object) > 0 {
			objects = append(objects, object)
		}
	}
}

func findObject(t *testing.T, objects []map[string]any, kind, name string) map[string]any {
	t.Helper()
	for _, object := range objects {
		if objectString(object, "kind") == kind && objectString(object, "metadata", "name") == name {
			return object
		}
	}
	t.Fatalf("missing %s/%s in %#v", kind, name, objectIdentities(objects))
	return nil
}

func objectIdentities(objects []map[string]any) []string {
	identities := make([]string, 0, len(objects))
	for _, object := range objects {
		identities = append(identities, objectString(object, "apiVersion")+"/"+objectString(object, "kind")+"/"+objectString(object, "metadata", "name"))
	}
	return identities
}

func objectString(object map[string]any, fields ...string) string {
	value, _, _ := unstructured.NestedString(object, fields...)
	return value
}

func assertString(t *testing.T, object map[string]any, expected string, fields ...string) {
	t.Helper()
	actual, found, err := unstructured.NestedString(object, fields...)
	if err != nil || !found || actual != expected {
		t.Fatalf("%s: got %q, found=%v, err=%v, want %q", strings.Join(fields, "."), actual, found, err, expected)
	}
}

func assertAnnotation(t *testing.T, object map[string]any, key, expected string) {
	t.Helper()
	annotations, found, err := unstructured.NestedStringMap(object, "metadata", "annotations")
	if err != nil || !found || annotations[key] != expected {
		t.Fatalf("annotation %s: got %#v, found=%v, err=%v", key, annotations, found, err)
	}
}

func assertOnlyRepository(t *testing.T, project map[string]any) {
	t.Helper()
	repositories, found, err := unstructured.NestedStringSlice(project, "spec", "sourceRepos")
	if err != nil || !found || len(repositories) != 1 || repositories[0] != repositoryURL {
		t.Fatalf("project repositories are not restricted: %#v", repositories)
	}
}

func assertExternalChartApplication(t *testing.T, objects []map[string]any, name, chartRepo, chart, revision, valuesPath, namespace string) {
	t.Helper()
	application := findObject(t, objects, "Application", name)
	assertAutomated(t, application, true)
	assertString(t, application, namespace, "spec", "destination", "namespace")
	sources := nestedSlice(t, application, "spec", "sources")
	if len(sources) != 2 {
		t.Fatalf("%s has %d sources, want 2", name, len(sources))
	}
	chartSource := sources[0].(map[string]any)
	assertString(t, chartSource, chartRepo, "repoURL")
	assertString(t, chartSource, chart, "chart")
	assertString(t, chartSource, revision, "targetRevision")
	valueFiles := stringSlice(t, chartSource, "helm", "valueFiles")
	assertExactSet(t, valueFiles, []string{"$values/" + valuesPath})
	valuesSource := sources[1].(map[string]any)
	assertString(t, valuesSource, repositoryURL, "repoURL")
	assertString(t, valuesSource, testRevision, "targetRevision")
	assertString(t, valuesSource, "values", "ref")
	if name == "monitoring" {
		options := stringSlice(t, application, "spec", "syncPolicy", "syncOptions")
		assertExactSet(t, options, []string{"ServerSideApply=true"})
	}
}

func assertAutomated(t *testing.T, application map[string]any, expectPrune bool) {
	t.Helper()
	selfHeal, found, err := unstructured.NestedBool(application, "spec", "syncPolicy", "automated", "selfHeal")
	if err != nil || !found || !selfHeal {
		t.Fatalf("%s does not enable automated self-heal", objectString(application, "metadata", "name"))
	}
	prune, found, err := unstructured.NestedBool(application, "spec", "syncPolicy", "automated", "prune")
	if err != nil {
		t.Fatal(err)
	}
	if expectPrune && (!found || !prune) {
		t.Fatalf("%s must enable prune", objectString(application, "metadata", "name"))
	}
	if !expectPrune && found && prune {
		t.Fatalf("%s must not enable prune", objectString(application, "metadata", "name"))
	}
}

func nestedSlice(t *testing.T, object map[string]any, fields ...string) []any {
	t.Helper()
	value, found, err := unstructured.NestedSlice(object, fields...)
	if err != nil || !found {
		t.Fatalf("%s missing: found=%v err=%v", strings.Join(fields, "."), found, err)
	}
	return value
}

func stringSlice(t *testing.T, object map[string]any, fields ...string) []string {
	t.Helper()
	value, found, err := unstructured.NestedStringSlice(object, fields...)
	if err != nil || !found {
		t.Fatalf("%s missing: found=%v err=%v", strings.Join(fields, "."), found, err)
	}
	return value
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func resourceKinds(t *testing.T, project map[string]any, field string) []string {
	t.Helper()
	resources := nestedSlice(t, project, "spec", field)
	kinds := make([]string, 0, len(resources))
	for _, raw := range resources {
		resource := raw.(map[string]any)
		kinds = append(kinds, objectString(resource, "group")+"/"+objectString(resource, "kind"))
	}
	return kinds
}

func assertExactSet(t *testing.T, actual, expected []string) {
	t.Helper()
	for _, value := range expected {
		if !contains(actual, value) {
			t.Fatalf("missing %s from %#v", value, actual)
		}
	}
	if len(actual) != len(expected) {
		t.Fatalf("got %#v, want exactly %#v", actual, expected)
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("could not find repository root")
		}
		directory = parent
	}
}
