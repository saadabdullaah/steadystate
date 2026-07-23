package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
	"github.com/saadabdullaah/steadystate/internal/resources"
)

const (
	testImageDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testGitRevision = "0123456789abcdef0123456789abcdef01234567"
)

func TestWorkloadStatusTransitions(t *testing.T) {
	t.Parallel()
	app := unitApplication()
	deployment := &appsv1.Deployment{}
	route := &gatewayv1.HTTPRoute{}

	digest := imageDigestResolution{state: imageDigestResolved, digest: testImageDigest}
	status, rejected := workloadStatus(app, deployment, route, digest, testGitRevision)
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
	status, rejected = workloadStatus(app, deployment, route, digest, testGitRevision)
	if rejected || status.Phase != platformv1alpha1.ApplicationPhaseHealthy || status.ActiveVersion != "v0.1.0" || status.CandidateVersion != "" || status.ResolvedImageDigest != testImageDigest || status.ResolvedGitRevision != testGitRevision || !meta.IsStatusConditionTrue(status.Conditions, conditionReady) {
		t.Fatalf("unexpected healthy status: %#v", status)
	}
	if !meta.IsStatusConditionTrue(status.Conditions, conditionServiceHealth) {
		t.Fatalf("healthy workload does not report ServiceHealth=True: %#v", status)
	}

	route.Status.Parents[0].Conditions[0].Status = metav1.ConditionFalse
	status, rejected = workloadStatus(app, deployment, route, digest, testGitRevision)
	if !rejected || status.Phase != platformv1alpha1.ApplicationPhaseDegraded || meta.FindStatusCondition(status.Conditions, conditionReady).Reason != "RouteRejected" {
		t.Fatalf("unexpected rejected status: %#v", status)
	}
	if condition := meta.FindStatusCondition(status.Conditions, conditionServiceHealth); condition == nil || condition.Status != metav1.ConditionFalse || condition.Reason != "RouteRejected" {
		t.Fatalf("route rejection does not report ServiceHealth=False: %#v", condition)
	}

	deployment.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse, Reason: "ProgressDeadlineExceeded"}}
	route.Status.Parents = nil
	status, _ = workloadStatus(app, deployment, route, digest, testGitRevision)
	if status.Phase != platformv1alpha1.ApplicationPhaseDegraded || meta.FindStatusCondition(status.Conditions, conditionReady).Reason != "DeploymentFailed" {
		t.Fatalf("unexpected failed status: %#v", status)
	}
}

func TestSourceRevisionValidation(t *testing.T) {
	t.Parallel()
	app := unitApplication()
	if revision, err := resolvedSourceRevision(app); err != nil || revision != "" {
		t.Fatalf("unexpected absent revision result: revision=%q err=%v", revision, err)
	}
	app.Annotations = map[string]string{platformv1alpha1.SourceRevisionAnnotationKey: testGitRevision}
	if revision, err := resolvedSourceRevision(app); err != nil || revision != testGitRevision {
		t.Fatalf("unexpected valid revision result: revision=%q err=%v", revision, err)
	}
	app.Annotations[platformv1alpha1.SourceRevisionAnnotationKey] = "ABC123"
	if _, err := resolvedSourceRevision(app); err == nil {
		t.Fatal("invalid source revision was accepted")
	}
}

func TestOptionalResourceAvailability(t *testing.T) {
	t.Parallel()
	groupVersion := schema.GroupVersion{Group: "argoproj.io", Version: "v1alpha1"}
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{groupVersion})
	rolloutGVK := groupVersion.WithKind("Rollout")

	available, err := optionalResourceAvailable(mapper, rolloutGVK)
	if err != nil || available {
		t.Fatalf("absent optional Rollout mapping: available=%t err=%v", available, err)
	}

	mapper.Add(rolloutGVK, meta.RESTScopeNamespace)
	available, err = optionalResourceAvailable(mapper, rolloutGVK)
	if err != nil || !available {
		t.Fatalf("installed optional Rollout mapping: available=%t err=%v", available, err)
	}

	monitoringGVK := schema.GroupVersionKind{Group: "monitoring.coreos.com", Version: "v1", Kind: "ServiceMonitor"}
	available, err = optionalResourceAvailable(mapper, monitoringGVK)
	if err != nil || available {
		t.Fatalf("absent optional ServiceMonitor mapping: available=%t err=%v", available, err)
	}

	reconciler := &ApplicationReconciler{}
	if !reconciler.hasProgressiveDelivery() {
		t.Fatal("direct unit/envtest reconcilers must assume their installed CRD fixture is available")
	}
	reconciler.progressiveDeliveryAvailable = &available
	if reconciler.hasProgressiveDelivery() {
		t.Fatal("manager discovery must disable progressive delivery when any required CRD is absent")
	}
}

func TestNormalizeImageDigest(t *testing.T) {
	t.Parallel()
	for _, imageID := range []string{
		testImageDigest,
		"containerd://" + testImageDigest,
		"docker-pullable://example.test/demo@" + testImageDigest,
	} {
		if digest, ok := normalizeImageDigest(imageID); !ok || digest != testImageDigest {
			t.Fatalf("normalizeImageDigest(%q)=%q,%v", imageID, digest, ok)
		}
	}
	for _, imageID := range []string{"", "sha256:short", "containerd://SHA256:AAAAAAAA", "garbagesha256:" + strings.Repeat("a", 64)} {
		if _, ok := normalizeImageDigest(imageID); ok {
			t.Fatalf("malformed imageID %q was accepted", imageID)
		}
	}
}

func TestResolvePodImageDigestStates(t *testing.T) {
	t.Parallel()
	image := "example.test/demo:v0.1.0"
	if result := resolvePodImageDigest(nil, image); result.state != imageDigestPending {
		t.Fatalf("empty Pods result=%#v", result)
	}
	first := unitReadyPod("first", image, "containerd://"+testImageDigest)
	if result := resolvePodImageDigest([]corev1.Pod{first}, image); result.state != imageDigestResolved || result.digest != testImageDigest {
		t.Fatalf("resolved result=%#v", result)
	}
	digestPinned := unitReadyPod("digest-pinned", "example.test/demo@"+testImageDigest, "containerd://"+testImageDigest)
	if result := resolvePodImageDigest([]corev1.Pod{digestPinned}, image); result.state != imageDigestResolved || result.digest != testImageDigest {
		t.Fatalf("Kyverno-mutated digest reference was not accepted: %#v", result)
	}
	malformed := unitReadyPod("malformed", image, "containerd://sha256:short")
	if result := resolvePodImageDigest([]corev1.Pod{malformed}, image); result.state != imageDigestInvalid {
		t.Fatalf("invalid result=%#v", result)
	}
	secondDigest := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	second := unitReadyPod("second", image, "containerd://"+secondDigest)
	if result := resolvePodImageDigest([]corev1.Pod{first, second}, image); result.state != imageDigestConflict {
		t.Fatalf("conflict result=%#v", result)
	}
	missing := unitReadyPod("missing", image, "")
	if result := resolvePodImageDigest([]corev1.Pod{first, missing}, image); result.state != imageDigestPending {
		t.Fatalf("missing imageID result=%#v", result)
	}
	old := unitReadyPod("old", "example.test/demo:v0.0.9", "containerd://"+secondDigest)
	if result := resolvePodImageDigest([]corev1.Pod{old, first}, image); result.state != imageDigestResolved || result.digest != testImageDigest {
		t.Fatalf("old version was not ignored: %#v", result)
	}
}

func TestDigestFailurePreservesActiveRelease(t *testing.T) {
	t.Parallel()
	app := unitApplication()
	app.Status.ActiveVersion = "v0.0.9"
	app.Status.ResolvedImageDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	app.Status.ResolvedGitRevision = "fedcba9876543210fedcba9876543210fedcba98"
	deployment, route := unitReadyWorkload()
	for _, digest := range []imageDigestResolution{
		{state: imageDigestPending, message: "pending"},
		{state: imageDigestInvalid, message: "invalid"},
		{state: imageDigestConflict, message: "conflict"},
	} {
		status, _ := workloadStatus(app, deployment, route, digest, testGitRevision)
		if status.ActiveVersion != app.Status.ActiveVersion || status.ResolvedImageDigest != app.Status.ResolvedImageDigest || status.ResolvedGitRevision != app.Status.ResolvedGitRevision {
			t.Fatalf("active release was not preserved for %s: %#v", digest.state, status)
		}
	}
}

func TestSuccessfulPromotionUpdatesActiveRelease(t *testing.T) {
	t.Parallel()
	app := unitApplication()
	app.Status.ActiveVersion = "v0.0.9"
	app.Status.ResolvedImageDigest = testImageDigest
	app.Status.ResolvedGitRevision = "fedcba9876543210fedcba9876543210fedcba98"
	app.Spec.Image.Tag = "v0.2.0"
	deployment, route := unitReadyWorkload()
	candidateDigest := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	status, _ := workloadStatus(app, deployment, route, imageDigestResolution{state: imageDigestResolved, digest: candidateDigest}, testGitRevision)
	if status.ActiveVersion != "v0.2.0" || status.ResolvedImageDigest != candidateDigest || status.ResolvedGitRevision != testGitRevision || status.CandidateVersion != "" {
		t.Fatalf("candidate was not promoted atomically: %#v", status)
	}
	if !meta.IsStatusConditionTrue(status.Conditions, conditionReady) {
		t.Fatalf("promoted release is not Ready: %#v", status)
	}
}

func TestApplicationRequestForPod(t *testing.T) {
	t.Parallel()
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "demo-pod", Namespace: "team-apps", Labels: map[string]string{
		"app.kubernetes.io/instance":   "demo",
		"app.kubernetes.io/managed-by": resources.ManagedBy,
	}}}
	requests := applicationRequestForPod(context.Background(), pod)
	if len(requests) != 1 || requests[0].Name != "demo" || requests[0].Namespace != "team-apps" {
		t.Fatalf("unexpected Pod mapping: %#v", requests)
	}
	pod.Labels["app.kubernetes.io/managed-by"] = "someone-else"
	if requests := applicationRequestForPod(context.Background(), pod); len(requests) != 0 {
		t.Fatalf("foreign Pod mapped to Application: %#v", requests)
	}
}
func TestSecurityRejectionStatusPreservesActiveVersion(t *testing.T) {
	t.Parallel()
	app := unitApplication()
	app.Status.ActiveVersion = "v0.0.9"
	app.Status.CandidateVersion = "v0.1.0"
	app.Status.ResolvedImageDigest = testImageDigest
	app.Status.ResolvedGitRevision = testGitRevision
	app.Spec.Security.NetworkIsolation = true
	status := degradedStatus(app, "SecurityPolicyRejected", "workload admission was rejected")
	if status.ActiveVersion != "v0.0.9" || status.ResolvedImageDigest != testImageDigest || status.ResolvedGitRevision != testGitRevision || status.CandidateVersion != "" || status.Phase != platformv1alpha1.ApplicationPhaseDegraded {
		t.Fatalf("unexpected security rejection status: %#v", status)
	}
	if condition := meta.FindStatusCondition(status.Conditions, conditionSecurityPolicyReady); condition == nil || condition.Status != metav1.ConditionFalse {
		t.Fatalf("security condition does not expose admission rejection: %#v", condition)
	}
}

func TestValidateApplicationTenancy(t *testing.T) {
	t.Parallel()

	app := unitApplication()
	team := unitTeam()
	namespace := resources.TeamNamespace(team)
	app.Namespace = namespace.Name
	app.Spec.Image.Repository = "example.test/apps/demo"
	if failure := validateApplicationTenancy(app, namespace, team); failure != nil {
		t.Fatalf("valid tenancy was rejected: %#v", failure)
	}

	app.Spec.Image.Repository = "example.test/blocked"
	if failure := validateApplicationTenancy(app, namespace, team); failure == nil || failure.reason != "RepositoryNotAllowed" {
		t.Fatalf("disallowed repository failure=%#v", failure)
	}

	app.Spec.Image.Repository = "example.test/apps/demo"
	namespace.Annotations[resources.TeamUIDAnnotationKey] = "different-uid"
	if failure := validateApplicationTenancy(app, namespace, team); failure == nil || failure.reason != "NamespaceOwnershipMismatch" {
		t.Fatalf("namespace ownership failure=%#v", failure)
	}
}

func unitTeam() *platformv1alpha1.Team {
	return &platformv1alpha1.Team{
		ObjectMeta: metav1.ObjectMeta{Name: "apps", UID: types.UID("team-uid")},
		Spec: platformv1alpha1.TeamSpec{
			Owners: []platformv1alpha1.TeamOwner{"apps-owner"},
			Quota: platformv1alpha1.TeamQuota{
				CPU:    resource.MustParse("2"),
				Memory: resource.MustParse("2Gi"),
			},
			AllowedRepositories: []platformv1alpha1.RepositoryPattern{"example.test/apps/*"},
		},
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

func unitReadyPod(name, image, imageID string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "application", Image: image}}},
		Status: corev1.PodStatus{
			Conditions:        []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{{Name: "application", Image: image, ImageID: imageID, Ready: true}},
		},
	}
}

func unitReadyWorkload() (*appsv1.Deployment, *gatewayv1.HTTPRoute) {
	deployment := &appsv1.Deployment{Status: appsv1.DeploymentStatus{
		AvailableReplicas: 1,
		UpdatedReplicas:   1,
		Conditions:        []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue}},
	}}
	gatewayNamespace := gatewayv1.Namespace("steadystate-system")
	route := &gatewayv1.HTTPRoute{Status: gatewayv1.HTTPRouteStatus{RouteStatus: gatewayv1.RouteStatus{Parents: []gatewayv1.RouteParentStatus{{
		ParentRef: gatewayv1.ParentReference{Name: "steadystate", Namespace: &gatewayNamespace},
		Conditions: []metav1.Condition{
			{Type: string(gatewayv1.RouteConditionAccepted), Status: metav1.ConditionTrue},
			{Type: string(gatewayv1.RouteConditionResolvedRefs), Status: metav1.ConditionTrue},
		},
	}}}}}
	return deployment, route
}
