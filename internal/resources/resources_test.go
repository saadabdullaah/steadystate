package resources

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

func TestNamingAndHostname(t *testing.T) {
	t.Parallel()
	app := testApplication()
	if got, want := ConfigMapName(app), "payments-config"; got != want {
		t.Fatalf("ConfigMapName=%q, want %q", got, want)
	}
	if got, want := StableServiceName(app), "payments-stable"; got != want {
		t.Fatalf("StableServiceName=%q, want %q", got, want)
	}
	if got, want := CanaryServiceName(app), "payments-canary"; got != want {
		t.Fatalf("CanaryServiceName=%q, want %q", got, want)
	}
	if got, want := Hostname(app), "payments.demo.steadystate.localtest.me"; got != want {
		t.Fatalf("Hostname=%q, want %q", got, want)
	}
}

func TestSuffixedNamesAreDNSLabelSafe(t *testing.T) {
	t.Parallel()
	app := testApplication()
	app.Name = strings.Repeat("a", 63)
	for _, named := range []struct {
		name   string
		suffix string
	}{
		{name: ConfigMapName(app), suffix: "-config"},
		{name: StableServiceName(app), suffix: "-stable"},
		{name: CanaryServiceName(app), suffix: "-canary"},
		{name: AnalysisTemplateName(app), suffix: "-analysis"},
		{name: ServiceMonitorName(app), suffix: "-monitor"},
		{name: PrometheusRuleName(app), suffix: "-alerts"},
	} {
		if len(named.name) > 63 || !strings.HasSuffix(named.name, named.suffix) {
			t.Fatalf("suffix-safe name %q is invalid", named.name)
		}
		if !regexp.MustCompile(`-[0-9a-f]{8}` + regexp.QuoteMeta(named.suffix) + `$`).MatchString(named.name) {
			t.Fatalf("suffix-safe name %q lacks its eight-character hash", named.name)
		}
	}
}

func TestGeneratedResources(t *testing.T) {
	t.Parallel()
	app := testApplication()

	config := ConfigMap(app)
	if config.Name != "payments-config" || config.Data["PORT"] != "9090" || config.Data["STEADYSTATE_APP_VERSION"] != "v1.2.3" || len(config.Data) != 5 {
		t.Fatalf("unexpected ConfigMap: %#v", config)
	}

	deployment := Deployment(app)
	container := deployment.Spec.Template.Spec.Containers[0]
	if *deployment.Spec.Replicas != 2 || deployment.Spec.Strategy.Type != appsv1.RollingUpdateDeploymentStrategyType || container.Image != "example.test/payments:v1.2.3" {
		t.Fatalf("unexpected Deployment: %#v", deployment.Spec)
	}
	if deployment.Spec.Template.Labels[VersionLabelKey] != "v1.2.3" {
		t.Fatalf("Deployment template lacks version identity: %#v", deployment.Spec.Template.Labels)
	}
	if container.LivenessProbe.HTTPGet.Path != "/healthz" || container.ReadinessProbe.HTTPGet.Path != "/readyz" || *container.SecurityContext.AllowPrivilegeEscalation || !*container.SecurityContext.ReadOnlyRootFilesystem || !*container.SecurityContext.RunAsNonRoot {
		t.Fatalf("Deployment hardening or probes are incomplete: %#v", container)
	}
	if *deployment.Spec.Template.Spec.AutomountServiceAccountToken || deployment.Spec.Template.Spec.SecurityContext.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Fatal("pod-level hardening is incomplete")
	}

	service := Service(app)
	if service.Spec.Ports[0].Port != 80 || service.Spec.Ports[0].TargetPort.IntVal != 9090 {
		t.Fatalf("unexpected Service: %#v", service.Spec)
	}
	if service.Labels[ServiceRoleLabelKey] != "base" {
		t.Fatalf("base Service lacks its monitoring role: %#v", service.Labels)
	}

	route := HTTPRoute(app)
	if route.Spec.Hostnames[0] != gatewayv1.Hostname("payments.demo.steadystate.localtest.me") || route.Spec.ParentRefs[0].Name != "steadystate" || *route.Spec.ParentRefs[0].Namespace != "steadystate-system" || route.Spec.Rules[0].BackendRefs[0].Name != "payments" {
		t.Fatalf("unexpected HTTPRoute: %#v", route.Spec)
	}
}

func TestBuildersAreByteStableAndIndependent(t *testing.T) {
	t.Parallel()
	app := testApplication()
	builders := []func() any{
		func() any { return Deployment(app) },
		func() any { return Service(app) },
		func() any { return ConfigMap(app) },
		func() any { return HTTPRoute(app) },
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
}

func testApplication() *platformv1alpha1.Application {
	return &platformv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "payments", Namespace: "demo"},
		Spec: platformv1alpha1.ApplicationSpec{
			Owner: "payments-team", Image: platformv1alpha1.ApplicationImage{Repository: "example.test/payments", Tag: "v1.2.3"},
			Runtime: platformv1alpha1.ApplicationRuntime{Port: 9090, Replicas: platformv1alpha1.ReplicaBounds{Min: 2, Max: 5}},
			Resources: platformv1alpha1.ApplicationResources{
				Requests: platformv1alpha1.ResourceValues{CPU: resource.MustParse("100m"), Memory: resource.MustParse("64Mi")},
				Limits:   platformv1alpha1.ResourceValues{CPU: resource.MustParse("500m"), Memory: resource.MustParse("256Mi")},
			},
			Deployment:  platformv1alpha1.ApplicationDeployment{Strategy: platformv1alpha1.DeploymentStrategyRolling, AutomaticRollback: true},
			Reliability: platformv1alpha1.ReliabilityTargets{AvailabilityTarget: "99.9%", MaximumP95Latency: metav1.Duration{Duration: 250 * time.Millisecond}, MaximumErrorRate: "1%"},
			Security:    platformv1alpha1.SecurityOptions{RunAsNonRoot: true},
		},
	}
}
