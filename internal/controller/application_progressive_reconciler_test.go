//go:build envtest

package controller

import (
	"strconv"
	"time"

	rolloutsv1alpha1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
	"github.com/saadabdullaah/steadystate/internal/resources"
)

var _ = Describe("Progressive Application reconciliation", Ordered, func() {
	const (
		teamName  = "progressive-reconciler"
		namespace = "team-progressive-reconciler"
	)

	BeforeAll(func(ctx SpecContext) {
		team := validSchemaTeam(teamName)
		err := k8sClient.Create(ctx, team)
		Expect(err == nil || apierrors.IsAlreadyExists(err)).To(BeTrue())
		reconcileTeam(ctx, k8sClient, teamName)
	})

	It("stages migration, preserves plugin fields, and reconstructs the active release", func(ctx SpecContext) {
		app := validApplication("progressive", namespace)
		Expect(k8sClient.Create(ctx, app)).To(Succeed())
		reconcile(ctx, k8sClient, app)
		key := types.NamespacedName{Name: app.Name, Namespace: namespace}

		Expect(k8sClient.Get(ctx, key, app)).To(Succeed())
		app.Status.ActiveVersion = "v0.1.0"
		app.Status.ResolvedImageDigest = testImageDigest
		app.Status.ResolvedGitRevision = testGitRevision
		Expect(k8sClient.Status().Update(ctx, app)).To(Succeed())
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
		Expect(k8sClient.Update(ctx, app)).To(Succeed())
		reconcile(ctx, k8sClient, app)

		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, key, deployment)).To(Succeed())
		Expect(*deployment.Spec.Replicas).To(Equal(int32(1)), "the serving Deployment must not be scaled down by the operator")
		Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(HaveSuffix(":v0.1.0"), "migration must start from the active release")
		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, key, route)).To(Succeed())
		Expect(routeUsesBaseService(route, app)).To(BeTrue(), "the route must remain on the serving Deployment until the Rollout baseline is healthy")

		rollout := &rolloutsv1alpha1.Rollout{}
		Expect(k8sClient.Get(ctx, key, rollout)).To(Succeed())
		Expect(rollout.Spec.WorkloadRef.ScaleDown).To(Equal(rolloutsv1alpha1.ScaleDownNever))
		rollout.Status.ObservedGeneration = strconv.FormatInt(rollout.Generation, 10)
		rollout.Status.Phase = rolloutsv1alpha1.RolloutPhaseHealthy
		rollout.Status.StableRS = "baseline-hash"
		rollout.Status.AvailableReplicas = 1
		Expect(k8sClient.Status().Update(ctx, rollout)).To(Succeed())
		reconcile(ctx, k8sClient, app)

		Expect(k8sClient.Get(ctx, key, route)).To(Succeed())
		Expect(routeUsesCanaryServices(route, app)).To(BeTrue())
		setRouteReadyForEnvtest(route)
		Expect(k8sClient.Status().Update(ctx, route)).To(Succeed())
		reconcile(ctx, k8sClient, app)
		Expect(k8sClient.Get(ctx, key, deployment)).To(Succeed())
		Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(HaveSuffix(":v0.4.0"), "desired candidate must start only after the route cutover is observed")
		Expect(k8sClient.Get(ctx, key, rollout)).To(Succeed())
		Expect(rollout.Spec.WorkloadRef.ScaleDown).To(Equal(rolloutsv1alpha1.ScaleDownOnSuccess))

		Expect(k8sClient.Get(ctx, key, route)).To(Succeed())
		route.Labels[gatewayPluginInProgressLabel] = gatewayPluginInProgressValue
		*route.Spec.Rules[0].BackendRefs[0].Weight = 75
		*route.Spec.Rules[0].BackendRefs[1].Weight = 25
		Expect(k8sClient.Update(ctx, route)).To(Succeed())
		reconcile(ctx, k8sClient, app)
		Expect(k8sClient.Get(ctx, key, route)).To(Succeed())
		Expect(*route.Spec.Rules[0].BackendRefs[0].Weight).To(Equal(int32(75)))
		Expect(*route.Spec.Rules[0].BackendRefs[1].Weight).To(Equal(int32(25)))

		analysisKey := types.NamespacedName{Name: resources.AnalysisTemplateName(app), Namespace: namespace}
		analysis := &rolloutsv1alpha1.AnalysisTemplate{}
		Expect(k8sClient.Get(ctx, analysisKey, analysis)).To(Succeed())
		Expect(metav1.IsControlledBy(analysis, app)).To(BeTrue())
		monitor := resources.ServiceMonitor(app)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(monitor), monitor)).To(Succeed())
		Expect(metav1.IsControlledBy(monitor, app)).To(BeTrue())
		Expect(k8sClient.Delete(ctx, analysis)).To(Succeed())
		Expect(k8sClient.Delete(ctx, monitor)).To(Succeed())
		reconcile(ctx, k8sClient, app)
		Expect(k8sClient.Get(ctx, analysisKey, &rolloutsv1alpha1.AnalysisTemplate{})).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(resources.ServiceMonitor(app)), resources.ServiceMonitor(app))).To(Succeed())

		Expect(k8sClient.Get(ctx, key, rollout)).To(Succeed())
		Expect(k8sClient.Delete(ctx, rollout)).To(Succeed())
		reconcile(ctx, k8sClient, app)
		Expect(k8sClient.Get(ctx, key, rollout)).To(Succeed())
		Expect(k8sClient.Get(ctx, key, deployment)).To(Succeed())
		Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(HaveSuffix(":v0.1.0"), "a deleted Rollout must be reconstructed from the last active release")
	})

	It("performs zero writes after canary resources settle", func(ctx SpecContext) {
		app := validApplication("progressive-settled", namespace)
		app.Spec.Image.Tag = "v0.4.0"
		app.Spec.Observability.Metrics = true
		app.Spec.Deployment = platformv1alpha1.ApplicationDeployment{
			Strategy:          platformv1alpha1.DeploymentStrategyCanary,
			AutomaticRollback: true,
			Steps: []platformv1alpha1.CanaryStep{
				{Weight: 10, Pause: metav1.Duration{Duration: 30 * time.Second}},
				{Weight: 100, Pause: metav1.Duration{Duration: 30 * time.Second}},
			},
		}
		Expect(k8sClient.Create(ctx, app)).To(Succeed())
		reconcile(ctx, k8sClient, app)
		reconcile(ctx, k8sClient, app)
		// Give API-server defaults and the status subresource one full
		// reconciliation barrier before measuring the steady state.
		reconcile(ctx, k8sClient, app)

		counting := &countingClient{Client: k8sClient}
		reconcile(ctx, counting, app)
		Expect(counting.mutations.Load()).To(BeZero())
	})
})

func setRouteReadyForEnvtest(route *gatewayv1.HTTPRoute) {
	route.Status.Parents = []gatewayv1.RouteParentStatus{{
		ParentRef:      route.Spec.ParentRefs[0],
		ControllerName: gatewayv1.GatewayController("gateway.envoyproxy.io/gatewayclass-controller"),
		Conditions: []metav1.Condition{
			{Type: string(gatewayv1.RouteConditionAccepted), Status: metav1.ConditionTrue, ObservedGeneration: route.Generation, Reason: "Accepted", LastTransitionTime: metav1.Now()},
			{Type: string(gatewayv1.RouteConditionResolvedRefs), Status: metav1.ConditionTrue, ObservedGeneration: route.Generation, Reason: "ResolvedRefs", LastTransitionTime: metav1.Now()},
		},
	}}
}
