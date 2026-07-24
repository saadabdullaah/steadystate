package controller

import (
	"context"
	"fmt"
	"sort"
	"strings"

	rolloutsv1alpha1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrlreconcile "sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
	applicationlogic "github.com/saadabdullaah/steadystate/internal/application"
	"github.com/saadabdullaah/steadystate/internal/resources"
	teamlogic "github.com/saadabdullaah/steadystate/internal/team"
)

const ApplicationFinalizer = "steadystate.dev/finalizer"

const (
	conditionConfigurationReady  = "ConfigurationReady"
	conditionSecurityPolicyReady = "SecurityPolicyReady"
	conditionServiceHealth       = "ServiceHealth"
	conditionRolloutHealthy      = "RolloutHealthy"
	conditionReady               = "Ready"
)

// ApplicationReconciler reconciles an Application object.
type ApplicationReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder

	// nil keeps direct unit/envtest reconcilers backward compatible; manager setup
	// always records the discovered hosted capability before controllers start.
	progressiveDeliveryAvailable *bool
}

// +kubebuilder:rbac:groups=platform.steadystate.dev,resources=applications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=platform.steadystate.dev,resources=applications/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.steadystate.dev,resources=applications/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=replicasets,verbs=get;list;watch
// +kubebuilder:rbac:groups=argoproj.io,resources=rollouts;analysistemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=argoproj.io,resources=analysisruns,verbs=get;list;watch
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors;prometheusrules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=wgpolicyk8s.io,resources=policyreports,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=services;configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch;update
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch;update
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=platform.steadystate.dev,resources=teams,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

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

	sourceRevision, revisionErr := resolvedSourceRevision(app)
	if revisionErr != nil {
		message := revisionErr.Error()
		changed, statusErr := r.patchStatus(ctx, app, degradedStatus(app, "InvalidSourceRevision", message))
		if changed {
			r.event(app, corev1.EventTypeWarning, "InvalidSourceRevision", message)
		}
		return ctrl.Result{}, statusErr
	}
	policyFailure, err := r.applicationTenancy(ctx, app)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("verify Application tenancy: %w", err)
	}
	if policyFailure != nil {
		changed, statusErr := r.patchStatus(ctx, app, degradedStatus(app, policyFailure.reason, policyFailure.message))
		if changed {
			r.event(app, corev1.EventTypeWarning, policyFailure.reason, policyFailure.message)
		}
		return ctrl.Result{}, statusErr
	}

	if unsupported := applicationlogic.UnsupportedFeatures(app); len(unsupported) > 0 {
		message := "unsupported capabilities requested: " + strings.Join(unsupported, ", ")
		changed, err := r.patchStatus(ctx, app, degradedStatus(app, "UnsupportedFeature", message))
		if changed {
			r.event(app, corev1.EventTypeWarning, "UnsupportedFeature", message)
		}
		return ctrl.Result{}, err
	}
	if app.Spec.Deployment.Strategy == platformv1alpha1.DeploymentStrategyCanary && !r.hasProgressiveDelivery() {
		message := "progressive delivery requires the Argo Rollouts and Prometheus Operator CRDs"
		changed, err := r.patchStatus(ctx, app, degradedStatus(app, "ProgressiveDeliveryUnavailable", message))
		if changed {
			r.event(app, corev1.EventTypeWarning, "ProgressiveDeliveryUnavailable", message)
		}
		return ctrl.Result{}, err
	}

	runtimeState, err := r.reconcileRuntimeChildren(ctx, app)
	if err != nil {
		reason := "ReconciliationFailed"
		message := fmt.Sprintf("failed to reconcile owned resources: %v", err)
		if isSecurityAdmissionRejection(err) {
			reason = "SecurityPolicyRejected"
			message = sanitizedAdmissionMessage(err)
		}
		_, statusErr := r.patchStatus(ctx, app, degradedStatus(app, reason, message))
		r.event(app, corev1.EventTypeWarning, reason, message)
		if statusErr != nil {
			return ctrl.Result{}, fmt.Errorf("reconcile children: %w; patch status: %v", err, statusErr)
		}
		return ctrl.Result{}, err
	}
	securityFailure, err := r.securityAdmissionFailure(ctx, app)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("inspect workload admission state: %w", err)
	}
	if securityFailure != "" {
		status := degradedStatus(app, "SecurityPolicyRejected", securityFailure)
		changed, statusErr := r.patchStatus(ctx, app, status)
		if changed {
			r.event(app, corev1.EventTypeWarning, "SecurityPolicyRejected", securityFailure)
		}
		return ctrl.Result{}, statusErr
	}

	digestResolution, digestErr := r.resolveImageDigest(ctx, app)
	if digestErr != nil {
		message := fmt.Sprintf("failed to resolve runtime image digest: %v", digestErr)
		_, statusErr := r.patchStatus(ctx, app, degradedStatus(app, "ReconciliationFailed", message))
		r.event(app, corev1.EventTypeWarning, "ReconciliationFailed", message)
		if statusErr != nil {
			return ctrl.Result{}, fmt.Errorf("resolve image digest: %w; patch status: %v", digestErr, statusErr)
		}
		return ctrl.Result{}, digestErr
	}

	var desiredStatus platformv1alpha1.ApplicationStatus
	var routeRejected bool
	switch {
	case app.Spec.Deployment.Strategy == platformv1alpha1.DeploymentStrategyCanary:
		desiredStatus, routeRejected = canaryWorkloadStatus(app, runtimeState, digestResolution, sourceRevision)
	case runtimeState.migrating:
		desiredStatus = strategyMigrationStatus(app, runtimeState)
	default:
		desiredStatus, routeRejected = workloadStatus(app, runtimeState.deployment, runtimeState.route, digestResolution, sourceRevision)
	}
	statusChanged, err := r.patchStatus(ctx, app, desiredStatus)
	if err != nil {
		return ctrl.Result{}, err
	}
	if routeRejected && statusChanged {
		r.event(app, corev1.EventTypeWarning, "RouteRejected", "HTTPRoute was rejected by the shared Gateway")
	} else if runtimeState.mutated || statusChanged {
		r.event(app, corev1.EventTypeNormal, "Reconciled", "Application resources and status were reconciled")
	}
	return ctrl.Result{RequeueAfter: runtimeState.requeueAfter}, nil
}

func isSecurityAdmissionRejection(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "denied the request") ||
		strings.Contains(message, "imagevalidatingpolicy") ||
		strings.Contains(message, "validatingpolicy") ||
		strings.Contains(message, "kyverno")
}

func sanitizedAdmissionMessage(err error) string {
	message := strings.Join(strings.Fields(err.Error()), " ")
	if len(message) > 512 {
		message = message[:512]
	}
	return "workload admission was rejected by platform security policy: " + message
}

func (r *ApplicationReconciler) securityAdmissionFailure(ctx context.Context, app *platformv1alpha1.Application) (string, error) {
	replicaSets := &appsv1.ReplicaSetList{}
	if err := r.List(ctx, replicaSets, client.InNamespace(app.Namespace), client.MatchingLabels(resources.SelectorLabels(app))); err != nil {
		return "", err
	}
	for i := range replicaSets.Items {
		for _, condition := range replicaSets.Items[i].Status.Conditions {
			if condition.Type != appsv1.ReplicaSetReplicaFailure || condition.Status != corev1.ConditionTrue {
				continue
			}
			combined := strings.Join(strings.Fields(condition.Reason+" "+condition.Message), " ")
			lower := strings.ToLower(combined)
			if strings.Contains(lower, "denied") || strings.Contains(lower, "kyverno") ||
				strings.Contains(lower, "validatingpolicy") || strings.Contains(lower, "signature") ||
				strings.Contains(lower, "attestation") {
				if len(combined) > 512 {
					combined = combined[:512]
				}
				return "workload admission was rejected by platform security policy: " + combined, nil
			}
		}
	}
	return "", nil
}

type applicationTenancyFailure struct {
	reason  string
	message string
}

func (r *ApplicationReconciler) applicationTenancy(ctx context.Context, app *platformv1alpha1.Application) (*applicationTenancyFailure, error) {
	namespace := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: app.Namespace}, namespace); err != nil {
		if apierrors.IsNotFound(err) {
			return &applicationTenancyFailure{reason: "NamespaceNotManaged", message: fmt.Sprintf("Namespace %s is not managed by a SteadyState Team", app.Namespace)}, nil
		}
		return nil, err
	}

	teamName := namespace.Labels[resources.TeamLabelKey]
	if teamName == "" || namespace.Name != resources.TeamNamespacePrefix+teamName || namespace.Annotations[resources.TeamUIDAnnotationKey] == "" {
		return &applicationTenancyFailure{reason: "NamespaceNotManaged", message: fmt.Sprintf("Namespace %s is not a verified SteadyState Team namespace", app.Namespace)}, nil
	}

	team := &platformv1alpha1.Team{}
	if err := r.Get(ctx, types.NamespacedName{Name: teamName}, team); err != nil {
		if apierrors.IsNotFound(err) {
			return &applicationTenancyFailure{reason: "TeamNotFound", message: fmt.Sprintf("Team %s referenced by Namespace %s does not exist", teamName, app.Namespace)}, nil
		}
		return nil, err
	}
	return validateApplicationTenancy(app, namespace, team), nil
}

func validateApplicationTenancy(app *platformv1alpha1.Application, namespace *corev1.Namespace, team *platformv1alpha1.Team) *applicationTenancyFailure {
	if namespace.Labels[resources.TeamLabelKey] != team.Name || namespace.Name != resources.TeamNamespaceName(team) || namespace.Annotations[resources.TeamUIDAnnotationKey] != string(team.UID) {
		return &applicationTenancyFailure{reason: "NamespaceOwnershipMismatch", message: fmt.Sprintf("Namespace %s does not belong to the current Team %s incarnation", namespace.Name, team.Name)}
	}
	if !team.DeletionTimestamp.IsZero() {
		return &applicationTenancyFailure{reason: "TeamTerminating", message: fmt.Sprintf("Team %s is terminating", team.Name)}
	}
	if err := teamlogic.Validate(team); err != nil {
		return &applicationTenancyFailure{reason: "TeamInvalid", message: fmt.Sprintf("Team %s configuration is invalid: %v", team.Name, err)}
	}
	if !teamlogic.RepositoryAllowed(team, app.Spec.Image.Repository) {
		return &applicationTenancyFailure{reason: "RepositoryNotAllowed", message: fmt.Sprintf("repository %s is not allowed by Team %s", app.Spec.Image.Repository, team.Name)}
	}
	return nil
}

func mutateDeployment(current, desired *appsv1.Deployment, manageReplicas bool) error {
	if current.Spec.Selector != nil && !apiequality.Semantic.DeepEqual(current.Spec.Selector, desired.Spec.Selector) {
		return fmt.Errorf("immutable selector does not match the Application identity")
	}
	if manageReplicas {
		current.Spec.Replicas = desired.Spec.Replicas
	}
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
			container.Env = desiredContainer.Env
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

func workloadStatus(app *platformv1alpha1.Application, deployment *appsv1.Deployment, route *gatewayv1.HTTPRoute, digest imageDigestResolution, sourceRevision string) (platformv1alpha1.ApplicationStatus, bool) {
	status := baseStatus(app)
	setCondition(&status, app.Generation, conditionConfigurationReady, metav1.ConditionTrue, "ResourcesReconciled", "All generated resources match the Application specification")
	setSecurityPolicyReady(&status, app)

	deploymentReady, deploymentFailed := deploymentState(deployment, app.Spec.Runtime.Replicas.Min)
	routeReady, routeRejected := routeState(route)
	setServiceHealth(&status, app, deploymentReady && routeReady, routeRejected)
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
	case deploymentReady && routeReady && digest.state == imageDigestInvalid:
		status.Phase = platformv1alpha1.ApplicationPhaseDegraded
		status.CandidateVersion = ""
		setCondition(&status, app.Generation, conditionRolloutHealthy, metav1.ConditionFalse, "ImageDigestInvalid", digest.message)
		setCondition(&status, app.Generation, conditionReady, metav1.ConditionFalse, "ImageDigestInvalid", digest.message)
	case deploymentReady && routeReady && digest.state == imageDigestConflict:
		status.Phase = platformv1alpha1.ApplicationPhaseDegraded
		status.CandidateVersion = ""
		setCondition(&status, app.Generation, conditionRolloutHealthy, metav1.ConditionFalse, "ImageDigestConflict", digest.message)
		setCondition(&status, app.Generation, conditionReady, metav1.ConditionFalse, "ImageDigestConflict", digest.message)
	case deploymentReady && routeReady && digest.state != imageDigestResolved:
		status.Phase = platformv1alpha1.ApplicationPhaseProgressing
		status.CandidateVersion = app.Spec.Image.Tag
		setCondition(&status, app.Generation, conditionRolloutHealthy, metav1.ConditionUnknown, "ResolvingImageDigest", digest.message)
		setCondition(&status, app.Generation, conditionReady, metav1.ConditionFalse, "ResolvingImageDigest", digest.message)
	case deploymentReady && routeReady:
		status.Phase = platformv1alpha1.ApplicationPhaseHealthy
		status.ActiveVersion = app.Spec.Image.Tag
		status.CandidateVersion = ""
		status.ResolvedImageDigest = digest.digest
		status.ResolvedGitRevision = sourceRevision
		setCondition(&status, app.Generation, conditionRolloutHealthy, metav1.ConditionTrue, "DeploymentAvailable", "Desired Deployment replicas are available at one runtime image digest")
		setCondition(&status, app.Generation, conditionReady, metav1.ConditionTrue, "ApplicationReady", "Deployment, runtime image digest, and HTTPRoute are ready")
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
	if reason == "SecurityPolicyRejected" ||
		(reason == "UnsupportedFeature" && (app.Spec.Security.RequireSignedImage || app.Spec.Security.NetworkIsolation)) {
		securityStatus = metav1.ConditionFalse
	}
	setCondition(&status, app.Generation, conditionSecurityPolicyReady, securityStatus, reason, message)
	setCondition(&status, app.Generation, conditionRolloutHealthy, metav1.ConditionUnknown, reason, "No child resources were mutated")
	setCondition(&status, app.Generation, conditionReady, metav1.ConditionFalse, reason, message)
	return status
}

func setSecurityPolicyReady(status *platformv1alpha1.ApplicationStatus, app *platformv1alpha1.Application) {
	if app.Spec.Security.RequireSignedImage {
		setCondition(status, app.Generation, conditionSecurityPolicyReady, metav1.ConditionTrue, "SignatureVerificationRequested", "Kyverno signature and SPDX attestation enforcement is requested for the current workload")
		return
	}
	setCondition(status, app.Generation, conditionSecurityPolicyReady, metav1.ConditionTrue, "SignatureVerificationNotRequested", "The workload meets baseline policy; signature verification was not requested")
}

func setServiceHealth(status *platformv1alpha1.ApplicationStatus, app *platformv1alpha1.Application, available, routeRejected bool) {
	if available {
		setCondition(status, app.Generation, conditionServiceHealth, metav1.ConditionTrue, "ServiceAvailable", "The active workload is available and its HTTPRoute is accepted with resolved references")
		return
	}
	reason := "ServiceUnavailable"
	message := "Waiting for an available active workload and an accepted HTTPRoute"
	if routeRejected {
		reason = "RouteRejected"
		message = "HTTPRoute was rejected or has unresolved references"
	}
	setCondition(status, app.Generation, conditionServiceHealth, metav1.ConditionFalse, reason, message)
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
	if deployment == nil {
		return false, false
	}
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

func (r *ApplicationReconciler) applicationRequestsInNamespace(ctx context.Context, namespace string) []ctrlreconcile.Request {
	applications := &platformv1alpha1.ApplicationList{}
	if err := r.List(ctx, applications, client.InNamespace(namespace)); err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "unable to list Applications for tenancy change", "namespace", namespace)
		return nil
	}
	requests := make([]ctrlreconcile.Request, 0, len(applications.Items))
	for i := range applications.Items {
		requests = append(requests, ctrlreconcile.Request{NamespacedName: types.NamespacedName{Name: applications.Items[i].Name, Namespace: namespace}})
	}
	sort.Slice(requests, func(i, j int) bool { return requests[i].Name < requests[j].Name })
	return requests
}

func (r *ApplicationReconciler) applicationsForNamespace(ctx context.Context, object client.Object) []ctrlreconcile.Request {
	return r.applicationRequestsInNamespace(ctx, object.GetName())
}

func (r *ApplicationReconciler) applicationsForTeam(ctx context.Context, object client.Object) []ctrlreconcile.Request {
	return r.applicationRequestsInNamespace(ctx, resources.TeamNamespacePrefix+object.GetName())
}

func (r *ApplicationReconciler) applicationsForSecurityReport(ctx context.Context, object client.Object) []ctrlreconcile.Request {
	return r.applicationRequestsInNamespace(ctx, object.GetNamespace())
}

func optionalResourceAvailable(mapper meta.RESTMapper, gvk schema.GroupVersionKind) (bool, error) {
	_, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err == nil {
		return true, nil
	}
	if meta.IsNoMatchError(err) {
		return false, nil
	}
	return false, fmt.Errorf("discover optional resource %s: %w", gvk.String(), err)
}

func (r *ApplicationReconciler) hasProgressiveDelivery() bool {
	return r.progressiveDeliveryAvailable == nil || *r.progressiveDeliveryAvailable
}

// SetupWithManager registers core watches and capability-aware progressive-delivery watches.
func (r *ApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.Application{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Owns(&gatewayv1.HTTPRoute{}).
		Watches(&appsv1.ReplicaSet{}, handler.EnqueueRequestsFromMapFunc(applicationRequestForPod)).
		Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(applicationRequestForPod)).
		Watches(&corev1.Namespace{}, handler.EnqueueRequestsFromMapFunc(r.applicationsForNamespace)).
		Watches(&platformv1alpha1.Team{}, handler.EnqueueRequestsFromMapFunc(r.applicationsForTeam))

	optionalWatches := []struct {
		gvk         schema.GroupVersionKind
		progressive bool
		register    func()
	}{
		{schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "Rollout"}, true, func() { builder = builder.Owns(&rolloutsv1alpha1.Rollout{}) }},
		{schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "AnalysisTemplate"}, true, func() { builder = builder.Owns(&rolloutsv1alpha1.AnalysisTemplate{}) }},
		{schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "AnalysisRun"}, true, func() {
			builder = builder.Watches(&rolloutsv1alpha1.AnalysisRun{}, handler.EnqueueRequestsFromMapFunc(applicationRequestForPod))
		}},
		{schema.GroupVersionKind{Group: "monitoring.coreos.com", Version: "v1", Kind: "ServiceMonitor"}, true, func() { builder = builder.Owns(monitoringWatchObject("ServiceMonitor")) }},
		{schema.GroupVersionKind{Group: "monitoring.coreos.com", Version: "v1", Kind: "PrometheusRule"}, true, func() { builder = builder.Owns(monitoringWatchObject("PrometheusRule")) }},
		{schema.GroupVersionKind{Group: "wgpolicyk8s.io", Version: "v1alpha2", Kind: "PolicyReport"}, false, func() {
			builder = builder.Watches(securityReportWatchObject(), handler.EnqueueRequestsFromMapFunc(r.applicationsForSecurityReport))
		}},
	}
	allProgressiveResourcesAvailable := true
	for _, watch := range optionalWatches {
		available, err := optionalResourceAvailable(mgr.GetRESTMapper(), watch.gvk)
		if err != nil {
			return err
		}
		if watch.progressive {
			allProgressiveResourcesAvailable = allProgressiveResourcesAvailable && available
		}
		if available {
			watch.register()
		}
	}
	r.progressiveDeliveryAvailable = &allProgressiveResourcesAvailable

	return builder.Named("application").Complete(r)
}
