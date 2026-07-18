package controller

import (
	"testing"
	"time"

	rolloutsv1alpha1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
	"github.com/saadabdullaah/steadystate/internal/resources"
)

func TestCanaryStatusPromotionAndRollback(t *testing.T) {
	t.Parallel()
	app := unitCanaryApplication()
	app.Status.ActiveVersion = "v0.3.0"
	app.Status.ResolvedImageDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	app.Status.ResolvedGitRevision = "fedcba9876543210fedcba9876543210fedcba98"
	route := resources.CanaryHTTPRoute(app)
	route.Generation = 4
	setUnitRouteReady(route)
	rollout := unitHealthyRollout(app)
	state := &applicationRuntimeState{route: route, rollout: rollout}

	status, rejected := canaryWorkloadStatus(app, state, imageDigestResolution{state: imageDigestResolved, digest: testImageDigest}, testGitRevision)
	if rejected || status.Phase != platformv1alpha1.ApplicationPhaseHealthy || status.ActiveVersion != "v0.4.0" || status.CandidateVersion != "" || status.ResolvedImageDigest != testImageDigest || status.ResolvedGitRevision != testGitRevision {
		t.Fatalf("canary promotion was not atomic: %#v", status)
	}

	rollout.Status.Abort = true
	rollout.Status.Phase = rolloutsv1alpha1.RolloutPhaseDegraded
	state.analysisFailure = "metric candidate-success-rate is Failed: threshold exceeded"
	route.Labels[gatewayPluginInProgressLabel] = gatewayPluginInProgressValue
	*route.Spec.Rules[0].BackendRefs[0].Weight = 90
	*route.Spec.Rules[0].BackendRefs[1].Weight = 10
	status, _ = canaryWorkloadStatus(app, state, imageDigestResolution{state: imageDigestResolved, digest: testImageDigest}, testGitRevision)
	if status.Phase != platformv1alpha1.ApplicationPhaseRollingBack || meta.FindStatusCondition(status.Conditions, conditionReady).Reason != "CanaryRollbackInProgress" {
		t.Fatalf("aborted canary did not enter RollingBack: %#v", status)
	}
	assertActiveTuplePreserved(t, app, status)

	delete(route.Labels, gatewayPluginInProgressLabel)
	*route.Spec.Rules[0].BackendRefs[0].Weight = 100
	*route.Spec.Rules[0].BackendRefs[1].Weight = 0
	status, _ = canaryWorkloadStatus(app, state, imageDigestResolution{state: imageDigestResolved, digest: testImageDigest}, testGitRevision)
	ready := meta.FindStatusCondition(status.Conditions, conditionReady)
	if status.Phase != platformv1alpha1.ApplicationPhaseDegraded || ready == nil || ready.Reason != "CanaryAnalysisFailed" || ready.Message != state.analysisFailure {
		t.Fatalf("stable rollback was not truthfully Degraded: %#v", status)
	}
	assertActiveTuplePreserved(t, app, status)
}

func TestCanaryStatusProgressAndManualIntervention(t *testing.T) {
	t.Parallel()
	app := unitCanaryApplication()
	route := resources.CanaryHTTPRoute(app)
	route.Generation = 2
	setUnitRouteReady(route)
	rollout := unitHealthyRollout(app)
	rollout.Status.Phase = rolloutsv1alpha1.RolloutPhaseProgressing
	rollout.Status.Canary.CurrentStepAnalysisRunStatus = &rolloutsv1alpha1.RolloutAnalysisRunStatus{
		Name: "demo-analysis", Status: rolloutsv1alpha1.AnalysisPhaseRunning,
	}
	state := &applicationRuntimeState{route: route, rollout: rollout}
	status, _ := canaryWorkloadStatus(app, state, imageDigestResolution{state: imageDigestPending, message: "pending"}, testGitRevision)
	if status.Phase != platformv1alpha1.ApplicationPhaseProgressing || status.CandidateVersion != "v0.4.0" || meta.FindStatusCondition(status.Conditions, conditionReady).Reason != "CanaryProgressing" {
		t.Fatalf("unexpected canary progress: %#v", status)
	}

	app.Spec.Deployment.AutomaticRollback = false
	rollout.Status.Canary.CurrentStepAnalysisRunStatus.Status = rolloutsv1alpha1.AnalysisPhaseInconclusive
	state.analysisFailure = "metric candidate-p95-latency is Inconclusive: manual policy"
	status, _ = canaryWorkloadStatus(app, state, imageDigestResolution{state: imageDigestPending}, testGitRevision)
	if status.Phase != platformv1alpha1.ApplicationPhaseProgressing || meta.FindStatusCondition(status.Conditions, conditionReady).Reason != "CanaryAnalysisInconclusive" {
		t.Fatalf("manual analysis did not pause truthfully: %#v", status)
	}
}

func TestStrategyMigrationAndPluginWeightOwnership(t *testing.T) {
	t.Parallel()
	app := unitCanaryApplication()
	desired := resources.CanaryHTTPRoute(app)
	current := resources.CanaryHTTPRoute(app)
	current.Labels[gatewayPluginInProgressLabel] = gatewayPluginInProgressValue
	*current.Spec.Rules[0].BackendRefs[0].Weight = 75
	*current.Spec.Rules[0].BackendRefs[1].Weight = 25
	preserveRouteWeights(desired, current)
	if *desired.Spec.Rules[0].BackendRefs[0].Weight != 75 || *desired.Spec.Rules[0].BackendRefs[1].Weight != 25 {
		t.Fatalf("plugin-owned weights were overwritten: %#v", desired.Spec.Rules[0].BackendRefs)
	}

	status := strategyMigrationStatus(app, "cutover")
	if status.Phase != platformv1alpha1.ApplicationPhaseProgressing || meta.FindStatusCondition(status.Conditions, conditionReady).Reason != "StrategyMigration" {
		t.Fatalf("unexpected migration status: %#v", status)
	}

	currentDeployment := resources.Deployment(app)
	replicas := int32(2)
	currentDeployment.Spec.Replicas = &replicas
	desiredDeployment := resources.Deployment(app)
	if err := mutateDeployment(currentDeployment, desiredDeployment, false); err != nil {
		t.Fatal(err)
	}
	if *currentDeployment.Spec.Replicas != 2 {
		t.Fatal("operator changed Rollouts-owned Deployment replicas")
	}
	if err := mutateDeployment(currentDeployment, desiredDeployment, true); err != nil {
		t.Fatal(err)
	}
	if *currentDeployment.Spec.Replicas != 0 {
		t.Fatal("rolling migration did not restore operator replica ownership")
	}
}

func TestRolloutReadinessRequiresCurrentObservedGeneration(t *testing.T) {
	t.Parallel()
	app := unitCanaryApplication()
	rollout := unitHealthyRollout(app)
	if !rolloutHealthy(rollout, 1) {
		t.Fatal("current healthy Rollout was not accepted")
	}
	rollout.Status.ObservedGeneration = "6"
	if rolloutHealthy(rollout, 1) {
		t.Fatal("stale Rollout status was accepted")
	}
}

func TestBootstrapRolloutUsesRouterFreePinnedContract(t *testing.T) {
	t.Parallel()
	object := resources.RolloutObject(unitCanaryApplication())
	if err := configureBootstrapRollout(object); err != nil {
		t.Fatal(err)
	}
	rollout := &rolloutsv1alpha1.Rollout{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, rollout); err != nil {
		t.Fatal(err)
	}
	canary := rollout.Spec.Strategy.Canary
	if canary == nil || len(canary.Steps) != 0 || canary.TrafficRouting != nil || canary.ScaleDownDelaySeconds != nil || canary.AbortScaleDownDelaySeconds != nil || canary.MinPodsPerReplicaSet != nil {
		t.Fatalf("bootstrap Rollout retained traffic-routing-only fields: %#v", canary)
	}
	if rollout.Spec.WorkloadRef == nil || rollout.Spec.WorkloadRef.ScaleDown != rolloutsv1alpha1.ScaleDownNever {
		t.Fatalf("bootstrap Rollout did not retain the serving Deployment: %#v", rollout.Spec.WorkloadRef)
	}
}

func TestActivatedRolloutDoesNotReturnToBootstrapWhenRouteStatusLags(t *testing.T) {
	t.Parallel()
	app := unitCanaryApplication()
	app.Status.ActiveVersion = "v0.3.0"
	rollout := resources.Rollout(app)
	rollout.Status.StableRS = "stable-hash"

	if shouldHoldServingDeployment(app, rollout, true, false) {
		t.Fatal("route generation lag incorrectly returned an active Rollout to bootstrap")
	}

	rollout.Spec.Strategy.Canary.TrafficRouting = nil
	rollout.Spec.Strategy.Canary.Steps = nil
	if !shouldHoldServingDeployment(app, rollout, true, false) {
		t.Fatal("router-free Rollout did not retain the serving Deployment while route acceptance was pending")
	}
	if shouldHoldServingDeployment(app, rollout, true, true) {
		t.Fatal("accepted canary route did not release the router-free bootstrap hold")
	}
	if !shouldHoldServingDeployment(app, rollout, false, true) {
		t.Fatal("missing Rollout did not preserve the last healthy Deployment during reconstruction")
	}
}

func unitCanaryApplication() *platformv1alpha1.Application {
	app := unitApplication()
	app.Spec.Image.Tag = "v0.4.0"
	app.Spec.Observability.Metrics = true
	app.Spec.Deployment = platformv1alpha1.ApplicationDeployment{
		Strategy:          platformv1alpha1.DeploymentStrategyCanary,
		AutomaticRollback: true,
		Steps: []platformv1alpha1.CanaryStep{
			{Weight: 10, Pause: metav1.Duration{Duration: 30 * time.Second}},
			{Weight: 25, Pause: metav1.Duration{Duration: 30 * time.Second}},
			{Weight: 50, Pause: metav1.Duration{Duration: 30 * time.Second}},
			{Weight: 100, Pause: metav1.Duration{Duration: 30 * time.Second}},
		},
	}
	return app
}

func unitHealthyRollout(app *platformv1alpha1.Application) *rolloutsv1alpha1.Rollout {
	return &rolloutsv1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{Name: app.Name, Namespace: app.Namespace, Generation: 5},
		Status: rolloutsv1alpha1.RolloutStatus{
			ObservedGeneration: "5", Phase: rolloutsv1alpha1.RolloutPhaseHealthy,
			StableRS: "stable-hash", AvailableReplicas: app.Spec.Runtime.Replicas.Min,
		},
	}
}

func setUnitRouteReady(route *gatewayv1.HTTPRoute) {
	gatewayNamespace := gatewayv1.Namespace("steadystate-system")
	route.Status.Parents = []gatewayv1.RouteParentStatus{{
		ParentRef: gatewayv1.ParentReference{Name: "steadystate", Namespace: &gatewayNamespace},
		Conditions: []metav1.Condition{
			{Type: string(gatewayv1.RouteConditionAccepted), Status: metav1.ConditionTrue, ObservedGeneration: route.Generation},
			{Type: string(gatewayv1.RouteConditionResolvedRefs), Status: metav1.ConditionTrue, ObservedGeneration: route.Generation},
		},
	}}
}

func assertActiveTuplePreserved(t *testing.T, app *platformv1alpha1.Application, status platformv1alpha1.ApplicationStatus) {
	t.Helper()
	if status.ActiveVersion != app.Status.ActiveVersion || status.ResolvedImageDigest != app.Status.ResolvedImageDigest || status.ResolvedGitRevision != app.Status.ResolvedGitRevision {
		t.Fatalf("active release tuple changed: %#v", status)
	}
}
