package resources

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	rolloutsv1alpha1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

func TestProgressiveDeliveryResourceContracts(t *testing.T) {
	t.Parallel()
	application := testCanaryApplication()

	anchor := Deployment(application)
	if anchor.Spec.Replicas == nil || *anchor.Spec.Replicas != 0 || anchor.Spec.Template.Labels[VersionLabelKey] != "v0.4.0" {
		t.Fatalf("canary Deployment is not a zero-replica workloadRef anchor: %#v", anchor.Spec)
	}

	stable := StableService(application)
	canary := CanaryService(application)
	if stable.Name != "payments-stable" || stable.Labels[ServiceRoleLabelKey] != "stable" || canary.Name != "payments-canary" || canary.Labels[ServiceRoleLabelKey] != "canary" {
		t.Fatalf("unexpected rollout Services: stable=%#v canary=%#v", stable.ObjectMeta, canary.ObjectMeta)
	}
	if stable.Spec.Selector["app.kubernetes.io/instance"] != application.Name || canary.Spec.Selector["app.kubernetes.io/instance"] != application.Name {
		t.Fatal("rollout Services must start from the stable Application selector")
	}

	route := CanaryHTTPRoute(application)
	backends := route.Spec.Rules[0].BackendRefs
	if len(backends) != 2 || backends[0].Name != "payments-stable" || *backends[0].Weight != 100 || backends[1].Name != "payments-canary" || *backends[1].Weight != 0 {
		t.Fatalf("unexpected canary HTTPRoute backends: %#v", backends)
	}

	rollout := Rollout(application)
	if rollout.Spec.WorkloadRef == nil || rollout.Spec.WorkloadRef.Name != application.Name || rollout.Spec.WorkloadRef.Kind != "Deployment" || rollout.Spec.WorkloadRef.ScaleDown != rolloutsv1alpha1.ScaleDownOnSuccess {
		t.Fatalf("Rollout does not use the Deployment anchor: %#v", rollout.Spec.WorkloadRef)
	}
	if rollout.Spec.Strategy.Canary == nil || rollout.Spec.Strategy.Canary.StableService != stable.Name || rollout.Spec.Strategy.Canary.CanaryService != canary.Name {
		t.Fatalf("Rollout does not reference both traffic Services: %#v", rollout.Spec.Strategy.Canary)
	}
	plugin := map[string]string{}
	if err := json.Unmarshal(rollout.Spec.Strategy.Canary.TrafficRouting.Plugins[GatewayPluginName], &plugin); err != nil {
		t.Fatal(err)
	}
	if plugin["httpRoute"] != route.Name || plugin["namespace"] != application.Namespace {
		t.Fatalf("unexpected Gateway plugin contract: %#v", plugin)
	}
	rolloutObject := RolloutObject(application)
	if _, found, err := unstructured.NestedFieldNoCopy(rolloutObject.Object, "spec", "template"); err != nil || found {
		t.Fatalf("workloadRef transport must omit the empty typed template: found=%t err=%v", found, err)
	}
	workloadName, found, err := unstructured.NestedString(rolloutObject.Object, "spec", "workloadRef", "name")
	if err != nil || !found || workloadName != application.Name {
		t.Fatalf("workloadRef transport lost the typed reference: name=%q found=%t err=%v", workloadName, found, err)
	}
	steps := rollout.Spec.Strategy.Canary.Steps
	if len(steps) != 12 {
		t.Fatalf("Rollout has %d steps, want weight/pause/analysis for four weights", len(steps))
	}
	for index, weight := range []int32{10, 25, 50, 100} {
		setWeight, pause, analysis := steps[index*3], steps[index*3+1], steps[index*3+2]
		if setWeight.SetWeight == nil || *setWeight.SetWeight != weight || pause.Pause == nil || pause.Pause.DurationSeconds() != 30 || analysis.Analysis == nil {
			t.Fatalf("invalid step triplet at weight %d: %#v %#v %#v", weight, setWeight, pause, analysis)
		}
		if analysis.Analysis.Templates[0].TemplateName != AnalysisTemplateName(application) || analysis.Analysis.Args[0].Value != application.Spec.Image.Tag || analysis.Analysis.Args[1].ValueFrom == nil || *analysis.Analysis.Args[1].ValueFrom.PodTemplateHashValue != rolloutsv1alpha1.Latest {
			t.Fatalf("analysis step at weight %d lacks candidate identity: %#v", weight, analysis.Analysis)
		}
	}
}

func TestAnalysisTemplateMetricGates(t *testing.T) {
	t.Parallel()
	application := testCanaryApplication()
	template := AnalysisTemplate(application)
	if template.Name != "payments-analysis" || len(template.Spec.Metrics) != 3 || len(template.Spec.Args) != 2 {
		t.Fatalf("unexpected AnalysisTemplate: %#v", template.Spec)
	}
	for _, metric := range template.Spec.Metrics {
		if metric.InitialDelay != "30s" || metric.Interval != "30s" || metric.Count == nil || metric.Count.IntValue() != 3 || metric.FailureLimit == nil || metric.FailureLimit.IntValue() != 1 || metric.ConsecutiveErrorLimit == nil || metric.ConsecutiveErrorLimit.IntValue() != 1 || metric.ConsecutiveSuccessLimit == nil || metric.ConsecutiveSuccessLimit.IntValue() != 2 {
			t.Fatalf("metric %s does not have the fixed analysis bounds: %#v", metric.Name, metric)
		}
		if metric.Provider.Prometheus == nil || metric.Provider.Prometheus.Address != PrometheusAddress {
			t.Fatalf("metric %s does not use pinned in-cluster Prometheus", metric.Name)
		}
	}
	if !strings.Contains(template.Spec.Metrics[0].Provider.Prometheus.Query, "or vector(-1)") || template.Spec.Metrics[0].SuccessCondition != "result[0] >= 0.99" {
		t.Fatalf("success-rate metric is not fail-safe: %#v", template.Spec.Metrics[0])
	}
	if !strings.Contains(template.Spec.Metrics[1].Provider.Prometheus.Query, "or vector(1000000000)") || template.Spec.Metrics[1].SuccessCondition != "result[0] <= 0.25" {
		t.Fatalf("latency metric is not fail-safe: %#v", template.Spec.Metrics[1])
	}
	if !strings.Contains(template.Spec.Metrics[2].Provider.Prometheus.Query, `label_rollouts_pod_template_hash="{{args.candidate-hash}}"`) || !strings.Contains(template.Spec.Metrics[2].Provider.Prometheus.Query, "or vector(0)") {
		t.Fatalf("restart metric does not isolate the candidate ReplicaSet: %#v", template.Spec.Metrics[2])
	}

	application.Spec.Deployment.AutomaticRollback = false
	manual := AnalysisTemplate(application)
	for _, metric := range manual.Spec.Metrics {
		if metric.FailureCondition != "false" || metric.InconclusiveLimit == nil || metric.InconclusiveLimit.IntValue() != 1 {
			t.Fatalf("manual metric %s does not become bounded Inconclusive: %#v", metric.Name, metric)
		}
	}
}

func TestMonitoringResourceContracts(t *testing.T) {
	t.Parallel()
	application := testCanaryApplication()
	monitor := ServiceMonitor(application)
	if monitor.GetAPIVersion() != serviceMonitorAPIVersion || monitor.GetKind() != "ServiceMonitor" || monitor.GetName() != "payments-monitor" {
		t.Fatalf("unexpected ServiceMonitor identity: %#v", monitor.Object)
	}
	role, found, err := unstructured.NestedString(monitor.Object, "spec", "selector", "matchLabels", ServiceRoleLabelKey)
	if err != nil || !found || role != "base" {
		t.Fatalf("ServiceMonitor must select only the base Service: role=%q found=%t err=%v", role, found, err)
	}
	endpoints, found, err := unstructured.NestedSlice(monitor.Object, "spec", "endpoints")
	if err != nil || !found || len(endpoints) != 1 || endpoints[0].(map[string]any)["port"] != "http" || endpoints[0].(map[string]any)["path"] != "/metrics" {
		t.Fatalf("unexpected ServiceMonitor endpoint: %#v err=%v", endpoints, err)
	}

	rule := PrometheusRule(application)
	groups, found, err := unstructured.NestedSlice(rule.Object, "spec", "groups")
	if err != nil || !found || len(groups) != 1 {
		t.Fatalf("unexpected PrometheusRule groups: %#v err=%v", groups, err)
	}
	rules := groups[0].(map[string]any)["rules"].([]any)
	if len(rules) != 3 {
		t.Fatalf("PrometheusRule has %d candidate alerts", len(rules))
	}
	rendered, err := json.Marshal(rules)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"SteadyStateCandidateHighErrorRate", "SteadyStateCandidateHighP95Latency", "SteadyStateCandidateRestarts", `"version":"v0.4.0"`, "0.01", "0.25", "label_steadystate_dev_version"} {
		if !strings.Contains(string(rendered), token) {
			t.Errorf("PrometheusRule is missing %q: %s", token, rendered)
		}
	}
}

func TestProgressiveBuildersAreByteStableAndIndependent(t *testing.T) {
	t.Parallel()
	application := testCanaryApplication()
	builders := []func() any{
		func() any { return Rollout(application) },
		func() any { return RolloutObject(application) },
		func() any { return StableService(application) },
		func() any { return CanaryService(application) },
		func() any { return CanaryHTTPRoute(application) },
		func() any { return AnalysisTemplate(application) },
		func() any { return ServiceMonitor(application) },
		func() any { return PrometheusRule(application) },
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

	first := StableService(application)
	first.Labels[ServiceRoleLabelKey] = "mutated"
	if second := StableService(application); second.Labels[ServiceRoleLabelKey] != "stable" {
		t.Fatal("rollout Service builder returned shared mutable labels")
	}
}

func TestProgressiveResourcesGoldenDigest(t *testing.T) {
	t.Parallel()
	application := testCanaryApplication()
	payload, err := json.Marshal([]any{
		Deployment(application), Service(application), StableService(application), CanaryService(application),
		CanaryHTTPRoute(application), Rollout(application), AnalysisTemplate(application), ServiceMonitor(application), PrometheusRule(application),
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	const want = "6cdd3d3af44836550b8778ed7dbb03837180290d1c2e4f8ddb427903c8574562"
	if digest != want {
		t.Fatalf("progressive resource golden digest=%s, want %s", digest, want)
	}
}

func TestManagerProgressiveDeliveryRBACIsLeastPrivilege(t *testing.T) {
	t.Parallel()
	role := readClusterRole(t, "../../config/rbac/role.yaml")
	for _, resource := range []string{"rollouts", "analysistemplates"} {
		if verbs := clusterRoleVerbs(role, "argoproj.io", resource); !slices.Equal(verbs, []string{"create", "delete", "get", "list", "patch", "update", "watch"}) {
			t.Fatalf("unexpected %s verbs: %#v", resource, verbs)
		}
	}
	if verbs := clusterRoleVerbs(role, "argoproj.io", "analysisruns"); !slices.Equal(verbs, []string{"get", "list", "watch"}) {
		t.Fatalf("AnalysisRuns must remain Rollouts-owned and read-only: %#v", verbs)
	}
	for _, resource := range []string{"servicemonitors", "prometheusrules"} {
		if verbs := clusterRoleVerbs(role, "monitoring.coreos.com", resource); !slices.Equal(verbs, []string{"create", "delete", "get", "list", "patch", "update", "watch"}) {
			t.Fatalf("unexpected %s verbs: %#v", resource, verbs)
		}
	}
	if verbs := clusterRoleVerbs(role, "apps", "replicasets"); !slices.Equal(verbs, []string{"get", "list", "watch"}) {
		t.Fatalf("ReplicaSets must remain Rollouts-owned and read-only: %#v", verbs)
	}
}

func clusterRoleVerbs(role *rbacv1.ClusterRole, group, resource string) []string {
	for _, rule := range role.Rules {
		if slices.Contains(rule.APIGroups, group) && slices.Contains(rule.Resources, resource) {
			return rule.Verbs
		}
	}
	return nil
}

func testCanaryApplication() *platformv1alpha1.Application {
	application := testApplication()
	application.Spec.Image.Tag = "v0.4.0"
	application.Spec.Deployment = platformv1alpha1.ApplicationDeployment{
		Strategy:          platformv1alpha1.DeploymentStrategyCanary,
		AutomaticRollback: true,
		Steps: []platformv1alpha1.CanaryStep{
			{Weight: 10, Pause: metav1.Duration{Duration: 30 * time.Second}},
			{Weight: 25, Pause: metav1.Duration{Duration: 30 * time.Second}},
			{Weight: 50, Pause: metav1.Duration{Duration: 30 * time.Second}},
			{Weight: 100, Pause: metav1.Duration{Duration: 30 * time.Second}},
		},
	}
	application.Spec.Observability.Metrics = true
	return application
}
