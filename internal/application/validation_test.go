package application

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

func TestParsePercentage(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"0%", "1%", "99.9%", "100.0%"} {
		if _, err := ParsePercentage(value); err != nil {
			t.Errorf("ParsePercentage(%q) returned %v", value, err)
		}
	}
	for _, value := range []string{"", "1", "-1%", "100.1%", "nope%"} {
		if _, err := ParsePercentage(value); err == nil {
			t.Errorf("ParsePercentage(%q) succeeded unexpectedly", value)
		}
	}
}

func TestParseDuration(t *testing.T) {
	t.Parallel()
	if got, err := ParseDuration("250ms"); err != nil || got != 250*time.Millisecond {
		t.Fatalf("ParseDuration returned %v, %v", got, err)
	}
	for _, value := range []string{"", "0s", "-1s", "tomorrow"} {
		if _, err := ParseDuration(value); err == nil {
			t.Errorf("ParseDuration(%q) succeeded unexpectedly", value)
		}
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()
	if err := Validate(validApplication()); err != nil {
		t.Fatalf("valid Application rejected: %v", err)
	}

	tests := map[string]func(*platformv1alpha1.Application){
		"empty owner": func(app *platformv1alpha1.Application) { app.Spec.Owner = "" },
		"digest":      func(app *platformv1alpha1.Application) { app.Spec.Image.Repository += "@sha256:deadbeef" },
		"latest":      func(app *platformv1alpha1.Application) { app.Spec.Image.Tag = "LATEST" },
		"bad port":    func(app *platformv1alpha1.Application) { app.Spec.Runtime.Port = 0 },
		"bad replicas": func(app *platformv1alpha1.Application) {
			app.Spec.Runtime.Replicas.Min = 4
			app.Spec.Runtime.Replicas.Max = 3
		},
		"zero resource": func(app *platformv1alpha1.Application) { app.Spec.Resources.Requests.CPU = resource.Quantity{} },
		"request above limit": func(app *platformv1alpha1.Application) {
			app.Spec.Resources.Requests.Memory = resource.MustParse("1Gi")
		},
		"zero availability": func(app *platformv1alpha1.Application) { app.Spec.Reliability.AvailabilityTarget = "0%" },
		"bad error rate":    func(app *platformv1alpha1.Application) { app.Spec.Reliability.MaximumErrorRate = "101%" },
		"bad latency":       func(app *platformv1alpha1.Application) { app.Spec.Reliability.MaximumP95Latency.Duration = 0 },
		"rolling steps": func(app *platformv1alpha1.Application) {
			app.Spec.Deployment.Steps = []platformv1alpha1.CanaryStep{{Weight: 10, Pause: metav1.Duration{Duration: time.Second}}}
		},
		"canary no steps": func(app *platformv1alpha1.Application) {
			app.Spec.Deployment.Strategy = platformv1alpha1.DeploymentStrategyCanary
		},
		"canary without metrics": func(app *platformv1alpha1.Application) {
			app.Spec.Deployment.Strategy = platformv1alpha1.DeploymentStrategyCanary
			app.Spec.Deployment.Steps = []platformv1alpha1.CanaryStep{{Weight: 10, Pause: metav1.Duration{Duration: time.Second}}}
		},
		"decreasing canary": func(app *platformv1alpha1.Application) {
			app.Spec.Deployment.Strategy = platformv1alpha1.DeploymentStrategyCanary
			app.Spec.Deployment.Steps = []platformv1alpha1.CanaryStep{{Weight: 50, Pause: metav1.Duration{Duration: time.Second}}, {Weight: 10, Pause: metav1.Duration{Duration: time.Second}}}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			app := validApplication()
			mutate(app)
			if err := Validate(app); err == nil {
				t.Fatal("invalid Application accepted")
			}
		})
	}
}

func TestUnsupportedFeaturesAreOrdered(t *testing.T) {
	t.Parallel()
	app := validApplication()
	app.Spec.Deployment.Strategy = platformv1alpha1.DeploymentStrategyCanary
	app.Spec.Deployment.Steps = []platformv1alpha1.CanaryStep{{Weight: 10, Pause: metav1.Duration{Duration: time.Second}}}
	app.Spec.Observability.Metrics = true
	app.Spec.Security.NetworkIsolation = true
	got := UnsupportedFeatures(app)
	want := []string{"security.networkIsolation"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func validApplication() *platformv1alpha1.Application {
	return &platformv1alpha1.Application{Spec: platformv1alpha1.ApplicationSpec{
		Owner:   "platform-team",
		Image:   platformv1alpha1.ApplicationImage{Repository: "example.test/demo", Tag: "v0.1.0"},
		Runtime: platformv1alpha1.ApplicationRuntime{Port: 8080, Replicas: platformv1alpha1.ReplicaBounds{Min: 1, Max: 3}},
		Resources: platformv1alpha1.ApplicationResources{
			Requests: platformv1alpha1.ResourceValues{CPU: resource.MustParse("50m"), Memory: resource.MustParse("32Mi")},
			Limits:   platformv1alpha1.ResourceValues{CPU: resource.MustParse("200m"), Memory: resource.MustParse("128Mi")},
		},
		Deployment:  platformv1alpha1.ApplicationDeployment{Strategy: platformv1alpha1.DeploymentStrategyRolling, AutomaticRollback: true},
		Reliability: platformv1alpha1.ReliabilityTargets{AvailabilityTarget: "99.9%", MaximumP95Latency: metav1.Duration{Duration: 250 * time.Millisecond}, MaximumErrorRate: "1%"},
		Security:    platformv1alpha1.SecurityOptions{RunAsNonRoot: true},
	}}
}
