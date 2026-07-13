package controller

import (
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

func TestWorkloadStatusTransitions(t *testing.T) {
	t.Parallel()
	app := unitApplication()
	deployment := &appsv1.Deployment{}
	route := &gatewayv1.HTTPRoute{}

	status, rejected := workloadStatus(app, deployment, route)
	if rejected || status.Phase != platformv1alpha1.ApplicationPhaseProgressing || status.CandidateVersion != "v0.1.0" || meta.IsStatusConditionTrue(status.Conditions, conditionReady) {
		t.Fatalf("unexpected progressing status: %#v", status)
	}

	deployment.Status.AvailableReplicas = 1
	deployment.Status.UpdatedReplicas = 1
	deployment.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue}}
	gatewayNamespace := gatewayv1.Namespace("steadystate-system")
	route.Status.Parents = []gatewayv1.RouteParentStatus{{ParentRef: gatewayv1.ParentReference{Name: "steadystate", Namespace: &gatewayNamespace}, Conditions: []metav1.Condition{
		{Type: string(gatewayv1.RouteConditionAccepted), Status: metav1.ConditionTrue},
		{Type: string(gatewayv1.RouteConditionResolvedRefs), Status: metav1.ConditionTrue},
	}}}
	status, rejected = workloadStatus(app, deployment, route)
	if rejected || status.Phase != platformv1alpha1.ApplicationPhaseHealthy || status.ActiveVersion != "v0.1.0" || status.CandidateVersion != "" || !meta.IsStatusConditionTrue(status.Conditions, conditionReady) {
		t.Fatalf("unexpected healthy status: %#v", status)
	}

	route.Status.Parents[0].Conditions[0].Status = metav1.ConditionFalse
	status, rejected = workloadStatus(app, deployment, route)
	if !rejected || status.Phase != platformv1alpha1.ApplicationPhaseDegraded || meta.FindStatusCondition(status.Conditions, conditionReady).Reason != "RouteRejected" {
		t.Fatalf("unexpected rejected status: %#v", status)
	}

	deployment.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse, Reason: "ProgressDeadlineExceeded"}}
	route.Status.Parents = nil
	status, _ = workloadStatus(app, deployment, route)
	if status.Phase != platformv1alpha1.ApplicationPhaseDegraded || meta.FindStatusCondition(status.Conditions, conditionReady).Reason != "DeploymentFailed" {
		t.Fatalf("unexpected failed status: %#v", status)
	}
}

func TestUnsupportedStatusPreservesActiveVersion(t *testing.T) {
	t.Parallel()
	app := unitApplication()
	app.Status.ActiveVersion = "v0.0.9"
	app.Status.CandidateVersion = "v0.1.0"
	app.Spec.Security.NetworkIsolation = true
	status := degradedStatus(app, "UnsupportedFeature", "network isolation is not available")
	if status.ActiveVersion != "v0.0.9" || status.CandidateVersion != "" || status.Phase != platformv1alpha1.ApplicationPhaseDegraded {
		t.Fatalf("unexpected unsupported status: %#v", status)
	}
	if condition := meta.FindStatusCondition(status.Conditions, conditionSecurityPolicyReady); condition == nil || condition.Status != metav1.ConditionFalse {
		t.Fatalf("security condition does not expose unsupported capability: %#v", condition)
	}
}

func unitApplication() *platformv1alpha1.Application {
	return &platformv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "apps", Generation: 3},
		Spec: platformv1alpha1.ApplicationSpec{
			Owner: "platform-team", Image: platformv1alpha1.ApplicationImage{Repository: "example.test/demo", Tag: "v0.1.0"},
			Runtime: platformv1alpha1.ApplicationRuntime{Port: 8080, Replicas: platformv1alpha1.ReplicaBounds{Min: 1, Max: 3}},
			Resources: platformv1alpha1.ApplicationResources{
				Requests: platformv1alpha1.ResourceValues{CPU: resource.MustParse("50m"), Memory: resource.MustParse("32Mi")},
				Limits:   platformv1alpha1.ResourceValues{CPU: resource.MustParse("200m"), Memory: resource.MustParse("128Mi")},
			},
			Deployment:  platformv1alpha1.ApplicationDeployment{Strategy: platformv1alpha1.DeploymentStrategyRolling, AutomaticRollback: true},
			Reliability: platformv1alpha1.ReliabilityTargets{AvailabilityTarget: "99.9%", MaximumP95Latency: metav1.Duration{Duration: 250 * time.Millisecond}, MaximumErrorRate: "1%"},
			Security:    platformv1alpha1.SecurityOptions{RunAsNonRoot: true},
		},
	}
}
