package resources

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

func TestTeamNamingAndOwnershipMetadata(t *testing.T) {
	t.Parallel()
	team := testTeam()

	namespace := TeamNamespace(team)
	if namespace.Name != "team-payments" || namespace.Labels[TeamLabelKey] != "payments" || namespace.Labels[GatewayAccessLabelKey] != "true" {
		t.Fatalf("unexpected Team Namespace: %#v", namespace.ObjectMeta)
	}
	if namespace.Annotations[TeamUIDAnnotationKey] != "team-uid" {
		t.Fatalf("Team UID annotation=%q", namespace.Annotations[TeamUIDAnnotationKey])
	}
	if len(namespace.OwnerReferences) != 0 {
		t.Fatal("Team-generated resources must not use owner references")
	}
}

func TestTeamResourcePolicies(t *testing.T) {
	t.Parallel()
	team := testTeam()

	quota := TeamResourceQuota(team)
	for _, name := range []corev1.ResourceName{corev1.ResourceRequestsCPU, corev1.ResourceRequestsMemory, corev1.ResourceLimitsCPU, corev1.ResourceLimitsMemory} {
		if _, exists := quota.Spec.Hard[name]; !exists {
			t.Fatalf("ResourceQuota is missing %s", name)
		}
	}
	cpu := quota.Spec.Hard[corev1.ResourceRequestsCPU]
	memory := quota.Spec.Hard[corev1.ResourceRequestsMemory]
	if cpu.Cmp(resource.MustParse("2")) != 0 || memory.Cmp(resource.MustParse("2Gi")) != 0 {
		t.Fatalf("unexpected ResourceQuota hard limits: %#v", quota.Spec.Hard)
	}

	limit := TeamLimitRange(team).Spec.Limits[0]
	if limit.Type != corev1.LimitTypeContainer || limit.DefaultRequest.Cpu().Cmp(resource.MustParse("50m")) != 0 || limit.Default.Memory().Cmp(resource.MustParse("512Mi")) != 0 {
		t.Fatalf("unexpected LimitRange defaults: %#v", limit)
	}
	if limit.Max != nil {
		t.Fatal("LimitRange must leave aggregate maximum enforcement to ResourceQuota")
	}
}

func TestTeamRBACIsSortedAndProtectsPlatformGuardrails(t *testing.T) {
	t.Parallel()
	team := testTeam()
	team.Spec.Owners = []platformv1alpha1.TeamOwner{"zoe", "alice"}

	binding := TeamRoleBinding(team)
	if len(binding.Subjects) != 3 || binding.Subjects[0].Kind != rbacv1.ServiceAccountKind || binding.Subjects[1].Name != "alice" || binding.Subjects[2].Name != "zoe" {
		t.Fatalf("RoleBinding subjects are not deterministic: %#v", binding.Subjects)
	}
	if binding.Namespace != TeamNamespaceName(team) {
		t.Fatalf("Team owner RoleBinding escaped its namespace: %q", binding.Namespace)
	}
	if binding.RoleRef.APIGroup != rbacv1.GroupName || binding.RoleRef.Name != TeamOwnerName || binding.RoleRef.Kind != "ClusterRole" {
		t.Fatalf("unexpected RoleRef: %#v", binding.RoleRef)
	}

	role := &rbacv1.Role{Rules: TeamOwnerRules()}
	for _, rule := range role.Rules {
		for _, protected := range []string{"clusterrolebindings", "clusterroles", "limitranges", "namespaces", "networkpolicies", "resourcequotas", "rolebindings", "roles", "serviceaccounts"} {
			if slices.Contains(rule.Resources, protected) {
				t.Fatalf("Team owner ClusterRole grants access to protected resource %q", protected)
			}
		}
	}
	if !roleAllows(role, "", "secrets", "get") || !roleAllows(role, "platform.steadystate.dev", "applications", "create") {
		t.Fatal("Team owner ClusterRole is missing required own-namespace permissions")
	}
	if *TeamServiceAccount(team).AutomountServiceAccountToken {
		t.Fatal("Team ServiceAccount must not automount credentials")
	}
}

func TestInstalledTeamOwnerClusterRoleMatchesBuilder(t *testing.T) {
	t.Parallel()

	installed := readClusterRole(t, "../../config/rbac/team_owner_role.yaml")
	if installed.Name != "team-owner" {
		t.Fatalf("install-time ClusterRole name=%q", installed.Name)
	}
	want, err := json.Marshal(TeamOwnerRules())
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(installed.Rules)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("install-time ClusterRole rules differ from the deterministic builder:\ngot  %s\nwant %s", got, want)
	}
}

func TestManagerCanOnlyBindInstalledTeamOwnerClusterRole(t *testing.T) {
	t.Parallel()

	manager := readClusterRole(t, "../../config/rbac/role.yaml")
	foundBind := false
	for _, rule := range manager.Rules {
		if slices.Contains(rule.Verbs, "escalate") {
			t.Fatal("manager ClusterRole must never grant escalate")
		}
		if slices.Contains(rule.Resources, "clusterrolebindings") {
			t.Fatal("manager ClusterRole must never grant ClusterRoleBinding access")
		}
		if !slices.Contains(rule.Verbs, "bind") {
			continue
		}
		foundBind = true
		if len(rule.APIGroups) != 1 || rule.APIGroups[0] != rbacv1.GroupName || len(rule.Resources) != 1 || rule.Resources[0] != "clusterroles" || len(rule.ResourceNames) != 1 || rule.ResourceNames[0] != TeamOwnerName || len(rule.Verbs) != 1 {
			t.Fatalf("manager bind authority is not restricted to %s: %#v", TeamOwnerName, rule)
		}
	}
	if !foundBind {
		t.Fatalf("manager ClusterRole cannot bind %s", TeamOwnerName)
	}
}

func readClusterRole(t *testing.T, path string) *rbacv1.ClusterRole {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	role := &rbacv1.ClusterRole{}
	if err := yaml.NewYAMLOrJSONDecoder(file, 4096).Decode(role); err != nil {
		t.Fatal(err)
	}
	return role
}

func TestTeamNetworkPolicies(t *testing.T) {
	t.Parallel()
	team := testTeam()

	deny := TeamDefaultDenyNetworkPolicy(team)
	if len(deny.Spec.Ingress) != 0 || len(deny.Spec.Egress) != 0 || len(deny.Spec.PolicyTypes) != 2 {
		t.Fatalf("default-deny policy is incomplete: %#v", deny.Spec)
	}

	dns := TeamAllowDNSNetworkPolicy(team)
	peer := dns.Spec.Egress[0].To[0]
	if peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] != "kube-system" || peer.PodSelector.MatchLabels["k8s-app"] != "kube-dns" || len(dns.Spec.Egress[0].Ports) != 2 {
		t.Fatalf("DNS policy is not restricted to CoreDNS: %#v", dns.Spec)
	}

	envoy := TeamAllowEnvoyNetworkPolicy(team)
	envoyPeer := envoy.Spec.Ingress[0].From[0]
	if envoyPeer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] != "envoy-gateway-system" || envoyPeer.PodSelector.MatchLabels["gateway.envoyproxy.io/owning-gateway-name"] != "steadystate" || envoyPeer.PodSelector.MatchLabels["gateway.envoyproxy.io/owning-gateway-namespace"] != "steadystate-system" || envoyPeer.PodSelector.MatchLabels["app.kubernetes.io/component"] != "proxy" {
		t.Fatalf("Envoy policy has an unsafe peer selector: %#v", envoy.Spec)
	}
	if len(envoy.Spec.Ingress[0].Ports) != 0 {
		t.Fatal("Team-level Envoy ingress must support each Application's declared TCP port")
	}

	monitoring := TeamAllowMonitoringNetworkPolicy(team)
	monitoringRule := monitoring.Spec.Ingress[0]
	monitoringPeer := monitoringRule.From[0]
	if monitoringPeer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"] != "monitoring" || monitoringPeer.PodSelector.MatchLabels["app.kubernetes.io/name"] != "prometheus" {
		t.Fatalf("monitoring policy has an unsafe peer selector: %#v", monitoring.Spec)
	}
	if len(monitoringRule.Ports) != 1 || monitoringRule.Ports[0].Port == nil || monitoringRule.Ports[0].Port.StrVal != "http" || monitoringRule.Ports[0].Protocol == nil || *monitoringRule.Ports[0].Protocol != corev1.ProtocolTCP {
		t.Fatalf("monitoring policy must permit only the named TCP http port: %#v", monitoring.Spec)
	}

	applications := TeamAllowApplicationsNetworkPolicy(team)
	if applications.Spec.PodSelector.MatchLabels[NetworkIsolationLabelKey] != "false" ||
		applications.Spec.Ingress[0].From[0].PodSelector.MatchLabels[NetworkIsolationLabelKey] != "false" ||
		applications.Spec.Egress[0].To[0].PodSelector.MatchLabels[NetworkIsolationLabelKey] != "false" {
		t.Fatalf("same-Team communication must exclude isolated Applications: %#v", applications.Spec)
	}
}

func TestTeamBuildersAreByteStableAndIndependent(t *testing.T) {
	t.Parallel()
	team := testTeam()
	builders := []func() any{
		func() any { return TeamNamespace(team) },
		func() any { return TeamResourceQuota(team) },
		func() any { return TeamLimitRange(team) },
		func() any { return TeamServiceAccount(team) },
		func() any { return TeamOwnerRules() },
		func() any { return TeamRoleBinding(team) },
		func() any { return TeamDefaultDenyNetworkPolicy(team) },
		func() any { return TeamAllowDNSNetworkPolicy(team) },
		func() any { return TeamAllowEnvoyNetworkPolicy(team) },
		func() any { return TeamAllowMonitoringNetworkPolicy(team) },
		func() any { return TeamAllowApplicationsNetworkPolicy(team) },
	}
	for _, build := range builders {
		first, err := json.Marshal(build())
		if err != nil {
			t.Fatal(err)
		}
		second, err := json.Marshal(build())
		if err != nil {
			t.Fatal(err)
		}
		if string(first) != string(second) {
			t.Fatalf("builder output changed:\n%s\n%s", first, second)
		}
	}

	first := TeamNamespace(team)
	first.Labels[TeamLabelKey] = "mutated"
	if second := TeamNamespace(team); second.Labels[TeamLabelKey] != team.Name {
		t.Fatal("Team builder returned shared mutable labels")
	}
}

func roleAllows(role *rbacv1.Role, apiGroup, resourceName, verb string) bool {
	for _, rule := range role.Rules {
		if slices.Contains(rule.APIGroups, apiGroup) && slices.Contains(rule.Resources, resourceName) && slices.Contains(rule.Verbs, verb) {
			return true
		}
	}
	return false
}

func testTeam() *platformv1alpha1.Team {
	return &platformv1alpha1.Team{
		ObjectMeta: metav1.ObjectMeta{Name: "payments", UID: types.UID("team-uid")},
		Spec: platformv1alpha1.TeamSpec{
			Owners: []platformv1alpha1.TeamOwner{"payments-owner"},
			Quota: platformv1alpha1.TeamQuota{
				CPU:    resource.MustParse("2"),
				Memory: resource.MustParse("2Gi"),
			},
			AllowedRepositories: []platformv1alpha1.RepositoryPattern{"ghcr.io/saadabdullaah/payments-*"},
		},
	}
}
