package controller

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	rolloutsv1alpha1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
	"github.com/saadabdullaah/steadystate/internal/resources"
)

const (
	gatewayPluginInProgressLabel = "rollouts.argoproj.io/gatewayapi-canary"
	gatewayPluginInProgressValue = "in-progress"
)

type applicationRuntimeState struct {
	deployment      *appsv1.Deployment
	route           *gatewayv1.HTTPRoute
	rollout         *rolloutsv1alpha1.Rollout
	mutated         bool
	migrating       bool
	migrationDetail string
	analysisFailure string
}

func (r *ApplicationReconciler) reconcileRuntimeChildren(ctx context.Context, app *platformv1alpha1.Application) (*applicationRuntimeState, error) {
	if app.Spec.Deployment.Strategy == platformv1alpha1.DeploymentStrategyCanary {
		return r.reconcileCanaryChildren(ctx, app)
	}
	return r.reconcileRollingChildren(ctx, app)
}

func (r *ApplicationReconciler) reconcileCanaryChildren(ctx context.Context, app *platformv1alpha1.Application) (*applicationRuntimeState, error) {
	state := &applicationRuntimeState{}
	rollout, rolloutExists, err := r.getRollout(ctx, app)
	if err != nil {
		return nil, err
	}
	currentRoute, routeExists, err := r.getRoute(ctx, app)
	if err != nil {
		return nil, err
	}

	anchorApplication := app
	canaryRouteConfigured := routeExists && routeUsesCanaryServices(currentRoute, app)
	currentRouteReady, currentRouteRejected := routeState(currentRoute)
	canaryRouteReady := canaryRouteConfigured && currentRouteReady && !currentRouteRejected
	holdServingDeployment := shouldHoldServingDeployment(app, rollout, rolloutExists, canaryRouteReady)
	if holdServingDeployment {
		anchorApplication = servingApplicationAtVersion(app, app.Status.ActiveVersion)
	}

	state.deployment, state.mutated, err = r.reconcileSharedChildren(ctx, app, anchorApplication, holdServingDeployment && !rolloutExists)
	if err != nil {
		return nil, err
	}
	progressiveMutated, err := r.reconcileProgressiveResources(ctx, app, holdServingDeployment)
	if err != nil {
		return nil, err
	}
	state.mutated = state.mutated || progressiveMutated
	rollout, _, err = r.getRollout(ctx, app)
	if err != nil {
		return nil, err
	}
	state.rollout = rollout

	switch {
	case !routeExists:
		state.route, progressiveMutated, err = r.reconcileRoute(ctx, app, true)
	case canaryRouteConfigured:
		state.route, progressiveMutated, err = r.reconcileRoute(ctx, app, true)
	case rolloutHealthy(rollout, app.Spec.Runtime.Replicas.Min):
		state.route, progressiveMutated, err = r.reconcileRoute(ctx, app, true)
	default:
		state.route = currentRoute
		state.migrating = true
		state.migrationDetail = "waiting for a healthy Rollout baseline before switching the HTTPRoute"
	}
	if err != nil {
		return nil, err
	}
	state.mutated = state.mutated || progressiveMutated
	if state.route != nil && !routeUsesCanaryServices(state.route, app) {
		state.migrating = true
	}
	if app.Status.ActiveVersion != "" {
		routeReady, routeRejected := routeState(state.route)
		if !routeReady || routeRejected {
			state.migrating = true
			state.migrationDetail = "waiting for the canary HTTPRoute cutover to be accepted"
		}
	}
	if state.migrating && state.migrationDetail == "" {
		state.migrationDetail = "waiting for the canary HTTPRoute cutover"
	}
	state.analysisFailure, err = r.analysisFailure(ctx, app, rollout)
	if err != nil {
		return nil, err
	}
	return state, nil
}

func shouldHoldServingDeployment(app *platformv1alpha1.Application, rollout *rolloutsv1alpha1.Rollout, rolloutExists, canaryRouteReady bool) bool {
	if app.Status.ActiveVersion == "" {
		return false
	}
	if !rolloutExists || rollout.Status.StableRS == "" {
		return true
	}
	// Route status briefly trails every plugin-owned weight update because each
	// update advances the HTTPRoute generation. Once traffic routing is active,
	// that expected lag must not put the Rollout back into its router-free
	// bootstrap contract and restart the canary from its first step.
	return !canaryRouteReady && !rolloutTrafficRoutingActive(rollout)
}

func rolloutTrafficRoutingActive(rollout *rolloutsv1alpha1.Rollout) bool {
	return rollout != nil &&
		rollout.Spec.Strategy.Canary != nil &&
		rollout.Spec.Strategy.Canary.TrafficRouting != nil &&
		len(rollout.Spec.Strategy.Canary.Steps) > 0
}

func (r *ApplicationReconciler) reconcileRollingChildren(ctx context.Context, app *platformv1alpha1.Application) (*applicationRuntimeState, error) {
	state := &applicationRuntimeState{}
	var changed bool
	rollout, rolloutExists, err := r.getRollout(ctx, app)
	if err != nil {
		return nil, err
	}
	if rolloutExists {
		changed, freezeErr := r.freezeRollout(ctx, app)
		if freezeErr != nil {
			return nil, fmt.Errorf("freeze Rollout for rolling migration: %w", freezeErr)
		}
		state.mutated = changed
		state.migrating = true
		state.migrationDetail = "scaling and verifying the rolling Deployment before switching the HTTPRoute"
	}

	state.deployment, changed, err = r.reconcileSharedChildren(ctx, app, app, true)
	if err != nil {
		return nil, err
	}
	state.mutated = state.mutated || changed
	currentRoute, routeExists, err := r.getRoute(ctx, app)
	if err != nil {
		return nil, err
	}
	deploymentReady, _ := deploymentState(state.deployment, app.Spec.Runtime.Replicas.Min)
	if !routeExists || !rolloutExists || deploymentReady {
		state.route, changed, err = r.reconcileRoute(ctx, app, false)
		if err != nil {
			return nil, err
		}
		state.mutated = state.mutated || changed
	} else {
		state.route = currentRoute
	}

	if rolloutExists && deploymentReady && routeUsesBaseService(state.route, app) {
		routeReady, routeRejected := routeState(state.route)
		if routeReady && !routeRejected {
			changed, err = r.deleteProgressiveResources(ctx, app)
			if err != nil {
				return nil, err
			}
			state.mutated = state.mutated || changed
			state.rollout = nil
			state.migrating = false
		}
	} else {
		state.rollout = rollout
	}
	return state, nil
}

func (r *ApplicationReconciler) reconcileSharedChildren(ctx context.Context, app, deploymentApplication *platformv1alpha1.Application, manageReplicas bool) (*appsv1.Deployment, bool, error) {
	mutated := false
	configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: resources.ConfigMapName(app), Namespace: app.Namespace}}
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, configMap, func() error {
		desired := resources.ConfigMap(app)
		mergeLabels(&configMap.ObjectMeta, desired.Labels)
		configMap.Data = desired.Data
		return controllerutil.SetControllerReference(app, configMap, r.Scheme)
	})
	if err != nil {
		return nil, mutated, fmt.Errorf("config map: %w", err)
	}
	mutated = op != controllerutil.OperationResultNone

	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: app.Name, Namespace: app.Namespace}}
	op, err = controllerutil.CreateOrUpdate(ctx, r.Client, deployment, func() error {
		desired := resources.Deployment(deploymentApplication)
		mergeLabels(&deployment.ObjectMeta, desired.Labels)
		if deployment.CreationTimestamp.IsZero() {
			deployment.Spec = desired.Spec
		} else if err := mutateDeployment(deployment, desired, manageReplicas); err != nil {
			return err
		}
		return controllerutil.SetControllerReference(app, deployment, r.Scheme)
	})
	if err != nil {
		return nil, mutated, fmt.Errorf("deployment: %w", err)
	}
	mutated = mutated || op != controllerutil.OperationResultNone

	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: app.Name, Namespace: app.Namespace}}
	op, err = controllerutil.CreateOrUpdate(ctx, r.Client, service, func() error {
		desired := resources.Service(app)
		mergeLabels(&service.ObjectMeta, desired.Labels)
		service.Spec.Selector = desired.Spec.Selector
		service.Spec.Ports = desired.Spec.Ports
		return controllerutil.SetControllerReference(app, service, r.Scheme)
	})
	if err != nil {
		return nil, mutated, fmt.Errorf("service: %w", err)
	}
	mutated = mutated || op != controllerutil.OperationResultNone
	return deployment, mutated, nil
}

func (r *ApplicationReconciler) reconcileProgressiveResources(ctx context.Context, app *platformv1alpha1.Application, holdServingDeployment bool) (bool, error) {
	mutated := false
	for _, desired := range []*corev1.Service{resources.StableService(app), resources.CanaryService(app)} {
		current := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
		op, err := controllerutil.CreateOrUpdate(ctx, r.Client, current, func() error {
			mergeLabels(&current.ObjectMeta, desired.Labels)
			if current.CreationTimestamp.IsZero() {
				current.Spec.Selector = desired.Spec.Selector
			}
			current.Spec.Ports = desired.Spec.Ports
			return controllerutil.SetControllerReference(app, current, r.Scheme)
		})
		if err != nil {
			return mutated, fmt.Errorf("rollout Service %s: %w", desired.Name, err)
		}
		mutated = mutated || op != controllerutil.OperationResultNone
	}

	analysis := &rolloutsv1alpha1.AnalysisTemplate{ObjectMeta: metav1.ObjectMeta{Name: resources.AnalysisTemplateName(app), Namespace: app.Namespace}}
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, analysis, func() error {
		desired := resources.AnalysisTemplate(app)
		mergeLabels(&analysis.ObjectMeta, desired.Labels)
		analysis.Spec = desired.Spec
		return controllerutil.SetControllerReference(app, analysis, r.Scheme)
	})
	if err != nil {
		return mutated, fmt.Errorf("AnalysisTemplate: %w", err)
	}
	mutated = mutated || op != controllerutil.OperationResultNone

	rolloutObject := resources.RolloutObject(app)
	if holdServingDeployment {
		if err := configureBootstrapRollout(rolloutObject); err != nil {
			return mutated, fmt.Errorf("hold serving Deployment during migration: %w", err)
		}
	}
	for _, desired := range []*unstructured.Unstructured{resources.ServiceMonitor(app), resources.PrometheusRule(app), rolloutObject} {
		changed, reconcileErr := r.reconcileUnstructured(ctx, app, desired)
		if reconcileErr != nil {
			return mutated, reconcileErr
		}
		mutated = mutated || changed
	}
	return mutated, nil
}

func configureBootstrapRollout(rolloutObject *unstructured.Unstructured) error {
	if err := unstructured.SetNestedField(rolloutObject.Object, rolloutsv1alpha1.ScaleDownNever, "spec", "workloadRef", "scaleDown"); err != nil {
		return err
	}
	// Bootstrap a healthy Rollout baseline without asking the Gateway plugin
	// to mutate a route that still targets the serving Deployment. Full canary
	// steps and every traffic-routing-only field are activated only after the
	// two-backend route has been accepted.
	for _, field := range []string{"steps", "trafficRouting", "scaleDownDelaySeconds", "abortScaleDownDelaySeconds", "minPodsPerReplicaSet"} {
		unstructured.RemoveNestedField(rolloutObject.Object, "spec", "strategy", "canary", field)
	}
	return nil
}

func (r *ApplicationReconciler) reconcileUnstructured(ctx context.Context, app *platformv1alpha1.Application, desired *unstructured.Unstructured) (bool, error) {
	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(desired.GroupVersionKind())
	current.SetName(desired.GetName())
	current.SetNamespace(desired.GetNamespace())
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, current, func() error {
		labels := current.GetLabels()
		mergeStringMap(&labels, desired.GetLabels())
		current.SetLabels(labels)
		spec, found, nestedErr := unstructured.NestedFieldCopy(desired.Object, "spec")
		if nestedErr != nil || !found {
			return fmt.Errorf("read desired %s spec: found=%t: %w", desired.GetKind(), found, nestedErr)
		}
		if nestedErr = unstructured.SetNestedField(current.Object, spec, "spec"); nestedErr != nil {
			return fmt.Errorf("set desired %s spec: %w", desired.GetKind(), nestedErr)
		}
		return controllerutil.SetControllerReference(app, current, r.Scheme)
	})
	if err != nil {
		return false, fmt.Errorf("%s %s: %w", desired.GetKind(), desired.GetName(), err)
	}
	return op != controllerutil.OperationResultNone, nil
}

func (r *ApplicationReconciler) reconcileRoute(ctx context.Context, app *platformv1alpha1.Application, canary bool) (*gatewayv1.HTTPRoute, bool, error) {
	route := &gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: app.Name, Namespace: app.Namespace}}
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, route, func() error {
		desired := resources.HTTPRoute(app)
		if canary {
			desired = resources.CanaryHTTPRoute(app)
			if route.Labels[gatewayPluginInProgressLabel] == gatewayPluginInProgressValue {
				preserveRouteWeights(desired, route)
			}
		}
		mergeLabels(&route.ObjectMeta, desired.Labels)
		route.Spec = desired.Spec
		return controllerutil.SetControllerReference(app, route, r.Scheme)
	})
	if err != nil {
		return nil, false, fmt.Errorf("HTTP route: %w", err)
	}
	return route, op != controllerutil.OperationResultNone, nil
}

func preserveRouteWeights(desired, current *gatewayv1.HTTPRoute) {
	weights := map[gatewayv1.ObjectName]*int32{}
	if len(current.Spec.Rules) > 0 {
		for _, backend := range current.Spec.Rules[0].BackendRefs {
			if backend.Weight != nil {
				weight := *backend.Weight
				weights[backend.Name] = &weight
			}
		}
	}
	for index := range desired.Spec.Rules[0].BackendRefs {
		if weight := weights[desired.Spec.Rules[0].BackendRefs[index].Name]; weight != nil {
			desired.Spec.Rules[0].BackendRefs[index].Weight = weight
		}
	}
}

func (r *ApplicationReconciler) freezeRollout(ctx context.Context, app *platformv1alpha1.Application) (bool, error) {
	rollout := resources.RolloutObject(app)
	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(rollout.GroupVersionKind())
	key := types.NamespacedName{Name: app.Name, Namespace: app.Namespace}
	if err := r.Get(ctx, key, current); err != nil {
		return false, client.IgnoreNotFound(err)
	}
	before := current.DeepCopy()
	if err := unstructured.SetNestedField(current.Object, true, "spec", "paused"); err != nil {
		return false, err
	}
	if err := unstructured.SetNestedField(current.Object, rolloutsv1alpha1.ScaleDownNever, "spec", "workloadRef", "scaleDown"); err != nil {
		return false, err
	}
	if apiequality.Semantic.DeepEqual(before.Object, current.Object) {
		return false, nil
	}
	return true, r.Patch(ctx, current, client.MergeFrom(before))
}

func (r *ApplicationReconciler) deleteProgressiveResources(ctx context.Context, app *platformv1alpha1.Application) (bool, error) {
	objects := []client.Object{
		resources.RolloutObject(app),
		&rolloutsv1alpha1.AnalysisTemplate{ObjectMeta: metav1.ObjectMeta{Name: resources.AnalysisTemplateName(app), Namespace: app.Namespace}},
		resources.ServiceMonitor(app),
		resources.PrometheusRule(app),
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: resources.StableServiceName(app), Namespace: app.Namespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: resources.CanaryServiceName(app), Namespace: app.Namespace}},
	}
	mutated := false
	for _, object := range objects {
		key := client.ObjectKeyFromObject(object)
		if err := r.Get(ctx, key, object); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return mutated, err
		}
		if err := r.Delete(ctx, object); err != nil && !apierrors.IsNotFound(err) {
			return mutated, err
		}
		mutated = true
	}
	return mutated, nil
}

func (r *ApplicationReconciler) getRollout(ctx context.Context, app *platformv1alpha1.Application) (*rolloutsv1alpha1.Rollout, bool, error) {
	rollout := &rolloutsv1alpha1.Rollout{}
	err := r.Get(ctx, types.NamespacedName{Name: app.Name, Namespace: app.Namespace}, rollout)
	if apierrors.IsNotFound(err) {
		return rollout, false, nil
	}
	return rollout, err == nil, err
}

func (r *ApplicationReconciler) getRoute(ctx context.Context, app *platformv1alpha1.Application) (*gatewayv1.HTTPRoute, bool, error) {
	route := &gatewayv1.HTTPRoute{}
	err := r.Get(ctx, types.NamespacedName{Name: app.Name, Namespace: app.Namespace}, route)
	if apierrors.IsNotFound(err) {
		return route, false, nil
	}
	return route, err == nil, err
}

func (r *ApplicationReconciler) analysisFailure(ctx context.Context, app *platformv1alpha1.Application, rollout *rolloutsv1alpha1.Rollout) (string, error) {
	if rollout == nil {
		return "", nil
	}
	if current := rollout.Status.Canary.CurrentStepAnalysisRunStatus; current != nil && current.Name != "" {
		run := &rolloutsv1alpha1.AnalysisRun{}
		if err := r.Get(ctx, types.NamespacedName{Name: current.Name, Namespace: app.Namespace}, run); err == nil {
			if message := failedMetricMessage(run); message != "" {
				return message, nil
			}
		} else if !apierrors.IsNotFound(err) {
			return "", err
		}
		if current.Message != "" {
			return current.Message, nil
		}
	}
	runs := &rolloutsv1alpha1.AnalysisRunList{}
	if err := r.List(ctx, runs, client.InNamespace(app.Namespace), client.MatchingLabels(resources.SelectorLabels(app))); err != nil {
		return "", err
	}
	sort.Slice(runs.Items, func(i, j int) bool {
		return runs.Items[i].CreationTimestamp.After(runs.Items[j].CreationTimestamp.Time)
	})
	for index := range runs.Items {
		if message := failedMetricMessage(&runs.Items[index]); message != "" {
			return message, nil
		}
	}
	return rollout.Status.Message, nil
}

func failedMetricMessage(run *rolloutsv1alpha1.AnalysisRun) string {
	for _, metric := range run.Status.MetricResults {
		switch metric.Phase {
		case rolloutsv1alpha1.AnalysisPhaseFailed, rolloutsv1alpha1.AnalysisPhaseError, rolloutsv1alpha1.AnalysisPhaseInconclusive:
			message := strings.TrimSpace(metric.Message)
			if message == "" {
				message = strings.TrimSpace(run.Status.Message)
			}
			return fmt.Sprintf("metric %s is %s: %s", metric.Name, metric.Phase, message)
		}
	}
	return ""
}

func canaryWorkloadStatus(app *platformv1alpha1.Application, state *applicationRuntimeState, digest imageDigestResolution, sourceRevision string) (platformv1alpha1.ApplicationStatus, bool) {
	status := baseStatus(app)
	setCondition(&status, app.Generation, conditionConfigurationReady, metav1.ConditionTrue, "ResourcesReconciled", "Progressive-delivery resources match the Application specification")
	setCondition(&status, app.Generation, conditionSecurityPolicyReady, metav1.ConditionTrue, "Hardened", "Workload security settings are applied")
	setCandidateVersion(&status, app)

	routeReady, routeRejected := routeState(state.route)
	if routeRejected {
		status.Phase = platformv1alpha1.ApplicationPhaseDegraded
		setCondition(&status, app.Generation, conditionRolloutHealthy, metav1.ConditionFalse, "RouteRejected", "HTTPRoute was rejected or has unresolved references")
		setCondition(&status, app.Generation, conditionReady, metav1.ConditionFalse, "RouteRejected", "HTTPRoute was rejected or has unresolved references")
		return status, true
	}
	if state.migrating {
		status.Phase = platformv1alpha1.ApplicationPhaseProgressing
		setCondition(&status, app.Generation, conditionRolloutHealthy, metav1.ConditionUnknown, "StrategyMigration", state.migrationDetail)
		setCondition(&status, app.Generation, conditionReady, metav1.ConditionFalse, "StrategyMigration", state.migrationDetail)
		return status, false
	}
	if state.rollout == nil {
		status.Phase = platformv1alpha1.ApplicationPhaseProgressing
		setCondition(&status, app.Generation, conditionRolloutHealthy, metav1.ConditionUnknown, "CreatingRollout", "Waiting for the Rollout controller")
		setCondition(&status, app.Generation, conditionReady, metav1.ConditionFalse, "CreatingRollout", "Waiting for the Rollout controller")
		return status, false
	}

	analysis := state.rollout.Status.Canary.CurrentStepAnalysisRunStatus
	if analysis != nil && analysis.Status == rolloutsv1alpha1.AnalysisPhaseInconclusive && !app.Spec.Deployment.AutomaticRollback && !state.rollout.Status.Abort {
		message := state.analysisFailure
		if message == "" {
			message = "canary analysis is Inconclusive and requires an explicit promote or abort"
		}
		status.Phase = platformv1alpha1.ApplicationPhaseProgressing
		setCondition(&status, app.Generation, conditionRolloutHealthy, metav1.ConditionUnknown, "CanaryAnalysisInconclusive", message)
		setCondition(&status, app.Generation, conditionReady, metav1.ConditionFalse, "CanaryAnalysisInconclusive", message)
		return status, false
	}
	if state.rollout.Status.Abort || state.rollout.Status.Phase == rolloutsv1alpha1.RolloutPhaseDegraded {
		message := state.analysisFailure
		if message == "" {
			message = state.rollout.Status.Message
		}
		if message == "" {
			message = "canary analysis failed"
		}
		if routeIsStableOnly(state.route, app) {
			status.Phase = platformv1alpha1.ApplicationPhaseDegraded
			setCondition(&status, app.Generation, conditionRolloutHealthy, metav1.ConditionFalse, "CanaryAnalysisFailed", message)
			setCondition(&status, app.Generation, conditionReady, metav1.ConditionFalse, "CanaryAnalysisFailed", message)
		} else {
			status.Phase = platformv1alpha1.ApplicationPhaseRollingBack
			setCondition(&status, app.Generation, conditionRolloutHealthy, metav1.ConditionFalse, "CanaryRollbackInProgress", message)
			setCondition(&status, app.Generation, conditionReady, metav1.ConditionFalse, "CanaryRollbackInProgress", "Returning all traffic to the last healthy release")
		}
		return status, false
	}

	ready := rolloutHealthy(state.rollout, app.Spec.Runtime.Replicas.Min)
	switch {
	case ready && routeReady && digest.state == imageDigestInvalid:
		status.Phase = platformv1alpha1.ApplicationPhaseDegraded
		setCondition(&status, app.Generation, conditionRolloutHealthy, metav1.ConditionFalse, "ImageDigestInvalid", digest.message)
		setCondition(&status, app.Generation, conditionReady, metav1.ConditionFalse, "ImageDigestInvalid", digest.message)
	case ready && routeReady && digest.state == imageDigestConflict:
		status.Phase = platformv1alpha1.ApplicationPhaseDegraded
		setCondition(&status, app.Generation, conditionRolloutHealthy, metav1.ConditionFalse, "ImageDigestConflict", digest.message)
		setCondition(&status, app.Generation, conditionReady, metav1.ConditionFalse, "ImageDigestConflict", digest.message)
	case ready && routeReady && digest.state != imageDigestResolved:
		status.Phase = platformv1alpha1.ApplicationPhaseProgressing
		setCondition(&status, app.Generation, conditionRolloutHealthy, metav1.ConditionUnknown, "ResolvingImageDigest", digest.message)
		setCondition(&status, app.Generation, conditionReady, metav1.ConditionFalse, "ResolvingImageDigest", digest.message)
	case ready && routeReady:
		status.Phase = platformv1alpha1.ApplicationPhaseHealthy
		status.ActiveVersion = app.Spec.Image.Tag
		status.CandidateVersion = ""
		status.ResolvedImageDigest = digest.digest
		status.ResolvedGitRevision = sourceRevision
		setCondition(&status, app.Generation, conditionRolloutHealthy, metav1.ConditionTrue, "CanaryPromoted", "Rollout is Healthy at one runtime image digest")
		setCondition(&status, app.Generation, conditionReady, metav1.ConditionTrue, "ApplicationReady", "Rollout, runtime image digest, and HTTPRoute are ready")
	default:
		status.Phase = platformv1alpha1.ApplicationPhaseProgressing
		message := "candidate is progressing through metric-gated traffic weights"
		setCondition(&status, app.Generation, conditionRolloutHealthy, metav1.ConditionUnknown, "CanaryProgressing", message)
		setCondition(&status, app.Generation, conditionReady, metav1.ConditionFalse, "CanaryProgressing", message)
	}
	return status, false
}

func strategyMigrationStatus(app *platformv1alpha1.Application, detail string) platformv1alpha1.ApplicationStatus {
	status := baseStatus(app)
	status.Phase = platformv1alpha1.ApplicationPhaseProgressing
	setCandidateVersion(&status, app)
	setCondition(&status, app.Generation, conditionConfigurationReady, metav1.ConditionTrue, "ResourcesReconciled", "Migration resources are reconciled")
	setCondition(&status, app.Generation, conditionSecurityPolicyReady, metav1.ConditionTrue, "Hardened", "Workload security settings are applied")
	setCondition(&status, app.Generation, conditionRolloutHealthy, metav1.ConditionUnknown, "StrategyMigration", detail)
	setCondition(&status, app.Generation, conditionReady, metav1.ConditionFalse, "StrategyMigration", detail)
	return status
}

func setCandidateVersion(status *platformv1alpha1.ApplicationStatus, app *platformv1alpha1.Application) {
	if status.ActiveVersion == "" || status.ActiveVersion != app.Spec.Image.Tag {
		status.CandidateVersion = app.Spec.Image.Tag
	} else {
		status.CandidateVersion = ""
	}
}

func rolloutHealthy(rollout *rolloutsv1alpha1.Rollout, desired int32) bool {
	if rollout == nil || rollout.Status.ObservedGeneration != strconv.FormatInt(rollout.Generation, 10) {
		return false
	}
	return rollout.Status.Phase == rolloutsv1alpha1.RolloutPhaseHealthy && rollout.Status.StableRS != "" && rollout.Status.AvailableReplicas >= desired
}

func routeUsesCanaryServices(route *gatewayv1.HTTPRoute, app *platformv1alpha1.Application) bool {
	if route == nil || len(route.Spec.Rules) != 1 || len(route.Spec.Rules[0].BackendRefs) != 2 {
		return false
	}
	names := map[string]bool{}
	for _, backend := range route.Spec.Rules[0].BackendRefs {
		names[string(backend.Name)] = true
	}
	return names[resources.StableServiceName(app)] && names[resources.CanaryServiceName(app)]
}

func routeUsesBaseService(route *gatewayv1.HTTPRoute, app *platformv1alpha1.Application) bool {
	return route != nil && len(route.Spec.Rules) == 1 && len(route.Spec.Rules[0].BackendRefs) == 1 && route.Spec.Rules[0].BackendRefs[0].Name == gatewayv1.ObjectName(app.Name)
}

func routeIsStableOnly(route *gatewayv1.HTTPRoute, app *platformv1alpha1.Application) bool {
	if !routeUsesCanaryServices(route, app) || route.Labels[gatewayPluginInProgressLabel] == gatewayPluginInProgressValue {
		return false
	}
	weights := map[string]int32{}
	for _, backend := range route.Spec.Rules[0].BackendRefs {
		if backend.Weight != nil {
			weights[string(backend.Name)] = *backend.Weight
		}
	}
	return weights[resources.StableServiceName(app)] == 100 && weights[resources.CanaryServiceName(app)] == 0
}

func servingApplicationAtVersion(app *platformv1alpha1.Application, version string) *platformv1alpha1.Application {
	copy := app.DeepCopy()
	copy.Spec.Image.Tag = version
	copy.Spec.Deployment.Strategy = platformv1alpha1.DeploymentStrategyRolling
	copy.Spec.Deployment.Steps = nil
	return copy
}

func monitoringWatchObject(kind string) *unstructured.Unstructured {
	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(schema.GroupVersionKind{Group: "monitoring.coreos.com", Version: "v1", Kind: kind})
	return object
}
