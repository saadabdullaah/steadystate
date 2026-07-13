package controller

import (
	"context"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
	applicationlogic "github.com/saadabdullaah/steadystate/internal/application"
	"github.com/saadabdullaah/steadystate/internal/resources"
)

const ApplicationFinalizer = "steadystate.dev/finalizer"

const (
	conditionConfigurationReady  = "ConfigurationReady"
	conditionSecurityPolicyReady = "SecurityPolicyReady"
	conditionRolloutHealthy      = "RolloutHealthy"
	conditionReady               = "Ready"
)

// ApplicationReconciler reconciles an Application object.
type ApplicationReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
}

// +kubebuilder:rbac:groups=platform.steadystate.dev,resources=applications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=platform.steadystate.dev,resources=applications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.steadystate.dev,resources=applications/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch;update
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;patch;delete

// Reconcile converges all Application-owned objects without periodic polling.
func (r *ApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	app := &platformv1alpha1.Application{}
	if err := r.Get(ctx, req.NamespacedName, app); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !app.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(app, ApplicationFinalizer) {
			before := app.DeepCopy()
			controllerutil.RemoveFinalizer(app, ApplicationFinalizer)
			if err := r.Patch(ctx, app, client.MergeFrom(before)); err != nil {
				return ctrl.Result{}, err
			}
			r.event(app, corev1.EventTypeNormal, "FinalizerReleased", "Application has no external cleanup and is ready for garbage collection")
		}
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(app, ApplicationFinalizer) {
		before := app.DeepCopy()
		controllerutil.AddFinalizer(app, ApplicationFinalizer)
		if err := r.Patch(ctx, app, client.MergeFrom(before)); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := applicationlogic.Validate(app); err != nil {
		changed, statusErr := r.patchStatus(ctx, app, degradedStatus(app, "InvalidConfiguration", err.Error()))
		if changed {
			r.event(app, corev1.EventTypeWarning, "InvalidConfiguration", err.Error())
		}
		return ctrl.Result{}, statusErr
	}

	if unsupported := applicationlogic.UnsupportedFeatures(app); len(unsupported) > 0 {
		message := "unsupported Phase 1 capabilities requested: " + strings.Join(unsupported, ", ")
		changed, err := r.patchStatus(ctx, app, degradedStatus(app, "UnsupportedFeature", message))
		if changed {
			r.event(app, corev1.EventTypeWarning, "UnsupportedFeature", message)
		}
		return ctrl.Result{}, err
	}

	deployment, route, mutated, err := r.reconcileChildren(ctx, app)
	if err != nil {
		message := fmt.Sprintf("failed to reconcile owned resources: %v", err)
		_, statusErr := r.patchStatus(ctx, app, degradedStatus(app, "ReconciliationFailed", message))
		r.event(app, corev1.EventTypeWarning, "ReconciliationFailed", message)
		if statusErr != nil {
			return ctrl.Result{}, fmt.Errorf("reconcile children: %w; patch status: %v", err, statusErr)
		}
		return ctrl.Result{}, err
	}

	desiredStatus, routeRejected := workloadStatus(app, deployment, route)
	statusChanged, err := r.patchStatus(ctx, app, desiredStatus)
	if err != nil {
		return ctrl.Result{}, err
	}
	if routeRejected && statusChanged {
		r.event(app, corev1.EventTypeWarning, "RouteRejected", "HTTPRoute was rejected by the shared Gateway")
	} else if mutated || statusChanged {
		r.event(app, corev1.EventTypeNormal, "Reconciled", "Application resources and status were reconciled")
	}
	return ctrl.Result{}, nil
}

func (r *ApplicationReconciler) reconcileChildren(ctx context.Context, app *platformv1alpha1.Application) (*appsv1.Deployment, *gatewayv1.HTTPRoute, bool, error) {
	mutated := false

	configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: resources.ConfigMapName(app), Namespace: app.Namespace}}
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, configMap, func() error {
		desired := resources.ConfigMap(app)
		mergeLabels(&configMap.ObjectMeta, desired.Labels)
		configMap.Data = desired.Data
		return controllerutil.SetControllerReference(app, configMap, r.Scheme)
	})
	if err != nil {
		return nil, nil, mutated, fmt.Errorf("config map: %w", err)
	}
	mutated = mutated || op != controllerutil.OperationResultNone

	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: app.Name, Namespace: app.Namespace}}
	op, err = controllerutil.CreateOrUpdate(ctx, r.Client, deployment, func() error {
		desired := resources.Deployment(app)
		mergeLabels(&deployment.ObjectMeta, desired.Labels)
		if deployment.CreationTimestamp.IsZero() {
			deployment.Spec = desired.Spec
		} else if err := mutateDeployment(deployment, desired); err != nil {
			return err
		}
		return controllerutil.SetControllerReference(app, deployment, r.Scheme)
	})
	if err != nil {
		return nil, nil, mutated, fmt.Errorf("deployment: %w", err)
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
		return nil, nil, mutated, fmt.Errorf("service: %w", err)
	}
	mutated = mutated || op != controllerutil.OperationResultNone

	route := &gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: app.Name, Namespace: app.Namespace}}
	op, err = controllerutil.CreateOrUpdate(ctx, r.Client, route, func() error {
		desired := resources.HTTPRoute(app)
		mergeLabels(&route.ObjectMeta, desired.Labels)
		route.Spec = desired.Spec
		return controllerutil.SetControllerReference(app, route, r.Scheme)
	})
	if err != nil {
		return nil, nil, mutated, fmt.Errorf("HTTP route: %w", err)
	}
	mutated = mutated || op != controllerutil.OperationResultNone

	return deployment, route, mutated, nil
}

func mutateDeployment(current, desired *appsv1.Deployment) error {
	if current.Spec.Selector != nil && !apiequality.Semantic.DeepEqual(current.Spec.Selector, desired.Spec.Selector) {
		return fmt.Errorf("immutable selector does not match the Application identity")
	}
	current.Spec.Replicas = desired.Spec.Replicas
	current.Spec.Selector = desired.Spec.Selector
	current.Spec.Strategy = desired.Spec.Strategy
	mergeStringMap(&current.Spec.Template.Labels, desired.Spec.Template.Labels)
	current.Spec.Template.Spec.AutomountServiceAccountToken = desired.Spec.Template.Spec.AutomountServiceAccountToken
	current.Spec.Template.Spec.SecurityContext = desired.Spec.Template.Spec.SecurityContext

	desiredContainer := desired.Spec.Template.Spec.Containers[0]
	for i := range current.Spec.Template.Spec.Containers {
		if current.Spec.Template.Spec.Containers[i].Name == desiredContainer.Name {
			container := &current.Spec.Template.Spec.Containers[i]
			container.Image = desiredContainer.Image
			container.Ports = desiredContainer.Ports
			container.EnvFrom = desiredContainer.EnvFrom
			container.LivenessProbe = desiredContainer.LivenessProbe
			container.ReadinessProbe = desiredContainer.ReadinessProbe
			container.Resources = desiredContainer.Resources
			container.SecurityContext = desiredContainer.SecurityContext
			return nil
		}
	}
	current.Spec.Template.Spec.Containers = append(current.Spec.Template.Spec.Containers, desiredContainer)
	return nil
}

func mergeLabels(meta *metav1.ObjectMeta, desired map[string]string) {
	mergeStringMap(&meta.Labels, desired)
}

func mergeStringMap(current *map[string]string, desired map[string]string) {
	if *current == nil {
		*current = make(map[string]string, len(desired))
	}
	for key, value := range desired {
		(*current)[key] = value
	}
}

func workloadStatus(app *platformv1alpha1.Application, deployment *appsv1.Deployment, route *gatewayv1.HTTPRoute) (platformv1alpha1.ApplicationStatus, bool) {
	status := baseStatus(app)
	setCondition(&status, app.Generation, conditionConfigurationReady, metav1.ConditionTrue, "ResourcesReconciled", "All generated resources match the Application specification")
	setCondition(&status, app.Generation, conditionSecurityPolicyReady, metav1.ConditionTrue, "Hardened", "Phase 1 workload security settings are applied")

	deploymentReady, deploymentFailed := deploymentState(deployment, app.Spec.Runtime.Replicas.Min)
	routeReady, routeRejected := routeState(route)
	switch {
	case deploymentFailed:
		status.Phase = platformv1alpha1.ApplicationPhaseDegraded
		status.CandidateVersion = ""
		setCondition(&status, app.Generation, conditionRolloutHealthy, metav1.ConditionFalse, "DeploymentFailed", "Deployment reported a rollout failure")
		setCondition(&status, app.Generation, conditionReady, metav1.ConditionFalse, "DeploymentFailed", "Application rollout failed")
	case routeRejected:
		status.Phase = platformv1alpha1.ApplicationPhaseDegraded
		status.CandidateVersion = ""
		setCondition(&status, app.Generation, conditionRolloutHealthy, conditionFromBool(deploymentReady), "DeploymentObserved", "Deployment readiness has been observed")
		setCondition(&status, app.Generation, conditionReady, metav1.ConditionFalse, "RouteRejected", "HTTPRoute was rejected or has unresolved references")
	case deploymentReady && routeReady:
		status.Phase = platformv1alpha1.ApplicationPhaseHealthy
		status.ActiveVersion = app.Spec.Image.Tag
		status.CandidateVersion = ""
		setCondition(&status, app.Generation, conditionRolloutHealthy, metav1.ConditionTrue, "DeploymentAvailable", "Desired Deployment replicas are available")
		setCondition(&status, app.Generation, conditionReady, metav1.ConditionTrue, "ApplicationReady", "Deployment and HTTPRoute are ready")
	default:
		status.Phase = platformv1alpha1.ApplicationPhaseProgressing
		status.CandidateVersion = app.Spec.Image.Tag
		setCondition(&status, app.Generation, conditionRolloutHealthy, metav1.ConditionUnknown, "Progressing", "Waiting for Deployment availability and HTTPRoute acceptance")
		setCondition(&status, app.Generation, conditionReady, metav1.ConditionFalse, "Progressing", "Application is progressing")
	}
	return status, routeRejected
}

func degradedStatus(app *platformv1alpha1.Application, reason, message string) platformv1alpha1.ApplicationStatus {
	status := baseStatus(app)
	status.Phase = platformv1alpha1.ApplicationPhaseDegraded
	status.CandidateVersion = ""
	setCondition(&status, app.Generation, conditionConfigurationReady, metav1.ConditionFalse, reason, message)
	securityStatus := metav1.ConditionUnknown
	if reason == "UnsupportedFeature" && (app.Spec.Security.RequireSignedImage || app.Spec.Security.NetworkIsolation) {
		securityStatus = metav1.ConditionFalse
	}
	setCondition(&status, app.Generation, conditionSecurityPolicyReady, securityStatus, reason, message)
	setCondition(&status, app.Generation, conditionRolloutHealthy, metav1.ConditionUnknown, reason, "No child resources were mutated")
	setCondition(&status, app.Generation, conditionReady, metav1.ConditionFalse, reason, message)
	return status
}

func baseStatus(app *platformv1alpha1.Application) platformv1alpha1.ApplicationStatus {
	status := *app.Status.DeepCopy()
	status.ObservedGeneration = app.Generation
	return status
}

func setCondition(status *platformv1alpha1.ApplicationStatus, generation int64, conditionType string, conditionStatus metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{Type: conditionType, Status: conditionStatus, ObservedGeneration: generation, Reason: reason, Message: message})
}

func deploymentState(deployment *appsv1.Deployment, desired int32) (ready, failed bool) {
	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentProgressing && condition.Status == corev1.ConditionFalse && condition.Reason == "ProgressDeadlineExceeded" {
			return false, true
		}
		if condition.Type == appsv1.DeploymentAvailable && condition.Status == corev1.ConditionTrue && deployment.Status.ObservedGeneration >= deployment.Generation && deployment.Status.AvailableReplicas >= desired && deployment.Status.UpdatedReplicas >= desired {
			ready = true
		}
	}
	return ready, false
}

func routeState(route *gatewayv1.HTTPRoute) (ready, rejected bool) {
	for _, parent := range route.Status.Parents {
		if parent.ParentRef.Name != gatewayv1.ObjectName("steadystate") || parent.ParentRef.Namespace == nil || *parent.ParentRef.Namespace != gatewayv1.Namespace("steadystate-system") {
			continue
		}
		accepted := meta.FindStatusCondition(parent.Conditions, string(gatewayv1.RouteConditionAccepted))
		resolved := meta.FindStatusCondition(parent.Conditions, string(gatewayv1.RouteConditionResolvedRefs))
		if (accepted != nil && accepted.ObservedGeneration >= route.Generation && accepted.Status == metav1.ConditionFalse) || (resolved != nil && resolved.ObservedGeneration >= route.Generation && resolved.Status == metav1.ConditionFalse) {
			return false, true
		}
		if accepted != nil && accepted.ObservedGeneration >= route.Generation && accepted.Status == metav1.ConditionTrue && resolved != nil && resolved.ObservedGeneration >= route.Generation && resolved.Status == metav1.ConditionTrue {
			return true, false
		}
	}
	return false, false
}

func conditionFromBool(value bool) metav1.ConditionStatus {
	if value {
		return metav1.ConditionTrue
	}
	return metav1.ConditionUnknown
}

func (r *ApplicationReconciler) patchStatus(ctx context.Context, app *platformv1alpha1.Application, desired platformv1alpha1.ApplicationStatus) (bool, error) {
	if apiequality.Semantic.DeepEqual(app.Status, desired) {
		return false, nil
	}
	key := types.NamespacedName{Name: app.Name, Namespace: app.Namespace}
	changed := false
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &platformv1alpha1.Application{}
		if err := r.Get(ctx, key, current); err != nil {
			return err
		}
		if current.Generation != desired.ObservedGeneration {
			return nil
		}
		updated := desired.DeepCopy()
		// Preserve transition timestamps calculated against the latest status.
		for i := range updated.Conditions {
			if existing := meta.FindStatusCondition(current.Status.Conditions, updated.Conditions[i].Type); existing != nil && existing.Status == updated.Conditions[i].Status {
				updated.Conditions[i].LastTransitionTime = existing.LastTransitionTime
			}
		}
		if apiequality.Semantic.DeepEqual(current.Status, *updated) {
			return nil
		}
		before := current.DeepCopy()
		current.Status = *updated
		if err := r.Status().Patch(ctx, current, client.MergeFrom(before)); err != nil {
			return err
		}
		changed = true
		return nil
	})
	return changed, err
}

func (r *ApplicationReconciler) event(app *platformv1alpha1.Application, eventType, reason, message string) {
	if r.Recorder != nil {
		r.Recorder.Eventf(app, nil, eventType, reason, reason, "%s", message)
	}
}

// SetupWithManager registers owner watches for every generated resource.
func (r *ApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.Application{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&gatewayv1.HTTPRoute{}).
		Named("application").
		Complete(r)
}
