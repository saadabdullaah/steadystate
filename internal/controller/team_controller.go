package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrlreconcile "sigs.k8s.io/controller-runtime/pkg/reconcile"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
	"github.com/saadabdullaah/steadystate/internal/resources"
	teamlogic "github.com/saadabdullaah/steadystate/internal/team"
)

const TeamFinalizer = "steadystate.dev/team-finalizer"

const (
	conditionTeamNamespaceReady      = "NamespaceReady"
	conditionTeamResourcePolicyReady = "ResourcePolicyReady"
	conditionTeamRBACReady           = "RBACReady"
	conditionTeamNetworkPolicyReady  = "NetworkPolicyReady"
	conditionTeamReady               = "Ready"
)

type teamReconcileStage int

const (
	teamStageNamespace teamReconcileStage = iota
	teamStageResourcePolicy
	teamStageRBAC
	teamStageNetworkPolicy
)

// TeamReconciler reconciles a cluster-scoped Team into one protected Namespace boundary.
type TeamReconciler struct {
	client.Client
	Recorder events.EventRecorder
}

// +kubebuilder:rbac:groups=platform.steadystate.dev,resources=teams,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=platform.steadystate.dev,resources=teams/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.steadystate.dev,resources=teams/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces;resourcequotas;limitranges;serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;list;watch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,resourceNames=steadystate-team-owner,verbs=bind
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch;update
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch;update

// Reconcile converges the complete Team boundary without periodic steady-state polling.
func (r *TeamReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	team := &platformv1alpha1.Team{}
	if err := r.Get(ctx, req.NamespacedName, team); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !team.DeletionTimestamp.IsZero() {
		return r.reconcileDeletion(ctx, team)
	}

	mutated := false
	if !controllerutil.ContainsFinalizer(team, TeamFinalizer) {
		before := team.DeepCopy()
		controllerutil.AddFinalizer(team, TeamFinalizer)
		if err := r.Patch(ctx, team, client.MergeFrom(before)); err != nil {
			return ctrl.Result{}, err
		}
		mutated = true
	}

	if err := teamlogic.Validate(team); err != nil {
		changed, statusErr := r.patchTeamStatus(ctx, team, invalidTeamStatus(team, err.Error()))
		if changed {
			r.event(team, corev1.EventTypeWarning, "InvalidConfiguration", err.Error())
		}
		return ctrl.Result{}, statusErr
	}

	changed, err := r.reconcileNamespace(ctx, team)
	mutated = mutated || changed
	if err != nil {
		return r.handleReconcileFailure(ctx, team, teamStageNamespace, err)
	}
	changed, err = r.reconcileResourcePolicy(ctx, team)
	mutated = mutated || changed
	if err != nil {
		return r.handleReconcileFailure(ctx, team, teamStageResourcePolicy, err)
	}
	changed, err = r.reconcileRBAC(ctx, team)
	mutated = mutated || changed
	if err != nil {
		return r.handleReconcileFailure(ctx, team, teamStageRBAC, err)
	}
	changed, err = r.reconcileNetworkPolicy(ctx, team)
	mutated = mutated || changed
	if err != nil {
		return r.handleReconcileFailure(ctx, team, teamStageNetworkPolicy, err)
	}

	statusChanged, err := r.patchTeamStatus(ctx, team, readyTeamStatus(team))
	if err != nil {
		return ctrl.Result{}, err
	}
	if mutated || statusChanged {
		r.event(team, corev1.EventTypeNormal, "Reconciled", "Team namespace, resource policy, RBAC, and network policy are reconciled")
	}
	return ctrl.Result{}, nil
}

func (r *TeamReconciler) reconcileNamespace(ctx context.Context, team *platformv1alpha1.Team) (bool, error) {
	desired := resources.TeamNamespace(team)
	current := &corev1.Namespace{}
	return r.reconcileManagedObject(ctx, team, "Namespace", current, desired, func() {
		// Namespace specification and system finalizers are owned by Kubernetes.
	})
}

func (r *TeamReconciler) reconcileResourcePolicy(ctx context.Context, team *platformv1alpha1.Team) (bool, error) {
	mutated := false
	desiredQuota := resources.TeamResourceQuota(team)
	currentQuota := &corev1.ResourceQuota{}
	changed, err := r.reconcileManagedObject(ctx, team, "ResourceQuota", currentQuota, desiredQuota, func() {
		currentQuota.Spec = desiredQuota.Spec
	})
	mutated = mutated || changed
	if err != nil {
		return mutated, err
	}

	desiredLimits := resources.TeamLimitRange(team)
	currentLimits := &corev1.LimitRange{}
	changed, err = r.reconcileManagedObject(ctx, team, "LimitRange", currentLimits, desiredLimits, func() {
		currentLimits.Spec = desiredLimits.Spec
	})
	return mutated || changed, err
}

func (r *TeamReconciler) reconcileRBAC(ctx context.Context, team *platformv1alpha1.Team) (bool, error) {
	installedRole := &rbacv1.ClusterRole{}
	if err := r.Get(ctx, types.NamespacedName{Name: resources.TeamOwnerName}, installedRole); err != nil {
		return false, fmt.Errorf("installed Team owner ClusterRole: %w", err)
	}
	if !apiequality.Semantic.DeepEqual(installedRole.Rules, resources.TeamOwnerRules()) {
		return false, fmt.Errorf("installed Team owner ClusterRole rules do not match the protected platform contract")
	}

	mutated := false
	desiredAccount := resources.TeamServiceAccount(team)
	currentAccount := &corev1.ServiceAccount{}
	changed, err := r.reconcileManagedObject(ctx, team, "ServiceAccount", currentAccount, desiredAccount, func() {
		currentAccount.AutomountServiceAccountToken = desiredAccount.AutomountServiceAccountToken
	})
	mutated = mutated || changed
	if err != nil {
		return mutated, err
	}

	desiredBinding := resources.TeamRoleBinding(team)
	currentBinding := &rbacv1.RoleBinding{}
	changed, err = r.reconcileManagedObject(ctx, team, "RoleBinding", currentBinding, desiredBinding, func() {
		currentBinding.Subjects = desiredBinding.Subjects
		currentBinding.RoleRef = desiredBinding.RoleRef
	})
	return mutated || changed, err
}

func (r *TeamReconciler) reconcileNetworkPolicy(ctx context.Context, team *platformv1alpha1.Team) (bool, error) {
	mutated := false
	for _, desired := range []*networkingv1.NetworkPolicy{
		resources.TeamDefaultDenyNetworkPolicy(team),
		resources.TeamAllowDNSNetworkPolicy(team),
		resources.TeamAllowEnvoyNetworkPolicy(team),
		resources.TeamAllowMonitoringNetworkPolicy(team),
	} {
		current := &networkingv1.NetworkPolicy{}
		changed, err := r.reconcileManagedObject(ctx, team, "NetworkPolicy", current, desired, func() {
			current.Spec = desired.Spec
		})
		mutated = mutated || changed
		if err != nil {
			return mutated, err
		}
	}
	return mutated, nil
}

func (r *TeamReconciler) reconcileManagedObject(ctx context.Context, team *platformv1alpha1.Team, kind string, current, desired client.Object, mutate func()) (bool, error) {
	current.SetName(desired.GetName())
	current.SetNamespace(desired.GetNamespace())
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), current)
	if err != nil && !apierrors.IsNotFound(err) {
		return false, err
	}
	if err == nil && !ownedByTeamUID(current, team) {
		return false, &ownershipConflictError{kind: kind, key: client.ObjectKeyFromObject(desired)}
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, current, func() error {
		mergeManagedMetadata(current, desired)
		mutate()
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("%s %s: %w", kind, client.ObjectKeyFromObject(desired), err)
	}
	return op != controllerutil.OperationResultNone, nil
}

func mergeManagedMetadata(current, desired client.Object) {
	labels := current.GetLabels()
	mergeStringMap(&labels, desired.GetLabels())
	current.SetLabels(labels)
	annotations := current.GetAnnotations()
	mergeStringMap(&annotations, desired.GetAnnotations())
	current.SetAnnotations(annotations)
}

func managedByTeam(object client.Object, team *platformv1alpha1.Team) bool {
	return object.GetLabels()[resources.TeamLabelKey] == team.Name && object.GetAnnotations()[resources.TeamUIDAnnotationKey] == string(team.UID)
}

func ownedByTeamUID(object client.Object, team *platformv1alpha1.Team) bool {
	return object.GetAnnotations()[resources.TeamUIDAnnotationKey] == string(team.UID)
}

type ownershipConflictError struct {
	kind string
	key  client.ObjectKey
}

func (e *ownershipConflictError) Error() string {
	return fmt.Sprintf("%s %s already exists without matching Team label and UID ownership", e.kind, e.key)
}

func (r *TeamReconciler) handleReconcileFailure(ctx context.Context, team *platformv1alpha1.Team, stage teamReconcileStage, reconcileErr error) (ctrl.Result, error) {
	reason := "ReconciliationFailed"
	eventType := corev1.EventTypeWarning
	var conflict *ownershipConflictError
	if errors.As(reconcileErr, &conflict) {
		reason = "OwnershipConflict"
	}
	changed, statusErr := r.patchTeamStatus(ctx, team, failedTeamStatus(team, stage, reason, reconcileErr.Error()))
	if changed {
		r.event(team, eventType, reason, reconcileErr.Error())
	}
	if statusErr != nil {
		return ctrl.Result{}, fmt.Errorf("reconcile Team: %w; patch status: %v", reconcileErr, statusErr)
	}
	if reason == "OwnershipConflict" {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, reconcileErr
}

func (r *TeamReconciler) reconcileDeletion(ctx context.Context, team *platformv1alpha1.Team) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(team, TeamFinalizer) {
		return ctrl.Result{}, nil
	}

	namespace := &corev1.Namespace{}
	err := r.Get(ctx, types.NamespacedName{Name: resources.TeamNamespaceName(team)}, namespace)
	if apierrors.IsNotFound(err) {
		before := team.DeepCopy()
		controllerutil.RemoveFinalizer(team, TeamFinalizer)
		if err := r.Patch(ctx, team, client.MergeFrom(before)); err != nil {
			return ctrl.Result{}, err
		}
		r.event(team, corev1.EventTypeNormal, "FinalizerReleased", "Managed Team namespace is absent and finalization is complete")
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	if !managedByTeam(namespace, team) {
		message := fmt.Sprintf("refusing to delete Namespace %s because its Team label or UID does not match", namespace.Name)
		changed, statusErr := r.patchTeamStatus(ctx, team, failedTeamStatus(team, teamStageNamespace, "OwnershipConflict", message))
		if changed {
			r.event(team, corev1.EventTypeWarning, "OwnershipConflict", message)
		}
		return ctrl.Result{}, statusErr
	}

	if _, err := r.patchTeamStatus(ctx, team, terminatingTeamStatus(team)); err != nil {
		return ctrl.Result{}, err
	}
	if namespace.DeletionTimestamp.IsZero() {
		if err := r.Delete(ctx, namespace); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		r.event(team, corev1.EventTypeNormal, "NamespaceDeletionRequested", fmt.Sprintf("Deletion requested for managed Namespace %s", namespace.Name))
	}
	return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
}

func readyTeamStatus(team *platformv1alpha1.Team) platformv1alpha1.TeamStatus {
	status := baseTeamStatus(team)
	setTeamCondition(&status, team.Generation, conditionTeamNamespaceReady, metav1.ConditionTrue, "NamespaceReconciled", "Managed Namespace exists and has verified ownership")
	setTeamCondition(&status, team.Generation, conditionTeamResourcePolicyReady, metav1.ConditionTrue, "ResourcePolicyReconciled", "ResourceQuota and LimitRange match the Team specification")
	setTeamCondition(&status, team.Generation, conditionTeamRBACReady, metav1.ConditionTrue, "RBACReconciled", "ServiceAccount and RoleBinding match the installed Team owner ClusterRole")
	setTeamCondition(&status, team.Generation, conditionTeamNetworkPolicyReady, metav1.ConditionTrue, "NetworkPolicyReconciled", "Default deny, DNS, Envoy Gateway, and Prometheus policies are enforced")
	setTeamCondition(&status, team.Generation, conditionTeamReady, metav1.ConditionTrue, "TeamReady", "All Team boundary resources are reconciled")
	return status
}

func invalidTeamStatus(team *platformv1alpha1.Team, message string) platformv1alpha1.TeamStatus {
	status := baseTeamStatus(team)
	for _, conditionType := range []string{conditionTeamNamespaceReady, conditionTeamResourcePolicyReady, conditionTeamRBACReady, conditionTeamNetworkPolicyReady} {
		setTeamCondition(&status, team.Generation, conditionType, metav1.ConditionFalse, "InvalidConfiguration", message)
	}
	setTeamCondition(&status, team.Generation, conditionTeamReady, metav1.ConditionFalse, "InvalidConfiguration", message)
	return status
}

func failedTeamStatus(team *platformv1alpha1.Team, failedStage teamReconcileStage, reason, message string) platformv1alpha1.TeamStatus {
	status := baseTeamStatus(team)
	conditions := []struct {
		conditionType string
		stage         teamReconcileStage
		successReason string
	}{
		{conditionTeamNamespaceReady, teamStageNamespace, "NamespaceReconciled"},
		{conditionTeamResourcePolicyReady, teamStageResourcePolicy, "ResourcePolicyReconciled"},
		{conditionTeamRBACReady, teamStageRBAC, "RBACReconciled"},
		{conditionTeamNetworkPolicyReady, teamStageNetworkPolicy, "NetworkPolicyReconciled"},
	}
	for _, condition := range conditions {
		switch {
		case condition.stage < failedStage:
			setTeamCondition(&status, team.Generation, condition.conditionType, metav1.ConditionTrue, condition.successReason, "Boundary layer is reconciled")
		case condition.stage == failedStage:
			setTeamCondition(&status, team.Generation, condition.conditionType, metav1.ConditionFalse, reason, message)
		default:
			setTeamCondition(&status, team.Generation, condition.conditionType, metav1.ConditionUnknown, "Blocked", "Waiting for an earlier Team boundary layer")
		}
	}
	setTeamCondition(&status, team.Generation, conditionTeamReady, metav1.ConditionFalse, reason, message)
	return status
}

func terminatingTeamStatus(team *platformv1alpha1.Team) platformv1alpha1.TeamStatus {
	status := baseTeamStatus(team)
	for _, conditionType := range []string{conditionTeamNamespaceReady, conditionTeamResourcePolicyReady, conditionTeamRBACReady, conditionTeamNetworkPolicyReady, conditionTeamReady} {
		setTeamCondition(&status, team.Generation, conditionType, metav1.ConditionFalse, "Terminating", "Managed Namespace deletion is in progress")
	}
	return status
}

func baseTeamStatus(team *platformv1alpha1.Team) platformv1alpha1.TeamStatus {
	status := *team.Status.DeepCopy()
	status.ObservedGeneration = team.Generation
	status.Namespace = resources.TeamNamespaceName(team)
	return status
}

func setTeamCondition(status *platformv1alpha1.TeamStatus, generation int64, conditionType string, conditionStatus metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{Type: conditionType, Status: conditionStatus, ObservedGeneration: generation, Reason: reason, Message: message})
}

func (r *TeamReconciler) patchTeamStatus(ctx context.Context, team *platformv1alpha1.Team, desired platformv1alpha1.TeamStatus) (bool, error) {
	if apiequality.Semantic.DeepEqual(team.Status, desired) {
		return false, nil
	}
	key := types.NamespacedName{Name: team.Name}
	changed := false
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &platformv1alpha1.Team{}
		if err := r.Get(ctx, key, current); err != nil {
			return err
		}
		if current.Generation != desired.ObservedGeneration {
			return nil
		}
		updated := desired.DeepCopy()
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

func (r *TeamReconciler) event(team *platformv1alpha1.Team, eventType, reason, message string) {
	if r.Recorder != nil {
		r.Recorder.Eventf(team, nil, eventType, reason, reason, "%s", message)
	}
}

func teamRequestsForObject(_ context.Context, object client.Object) []ctrlreconcile.Request {
	teamName := object.GetLabels()[resources.TeamLabelKey]
	if teamName == "" {
		return nil
	}
	return []ctrlreconcile.Request{{NamespacedName: types.NamespacedName{Name: teamName}}}
}

func (r *TeamReconciler) teamRequestsForOwnerClusterRole(ctx context.Context, object client.Object) []ctrlreconcile.Request {
	if object.GetName() != resources.TeamOwnerName {
		return nil
	}
	teams := &platformv1alpha1.TeamList{}
	if err := r.List(ctx, teams); err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "unable to list Teams for owner ClusterRole change")
		return nil
	}
	requests := make([]ctrlreconcile.Request, 0, len(teams.Items))
	for i := range teams.Items {
		requests = append(requests, ctrlreconcile.Request{NamespacedName: types.NamespacedName{Name: teams.Items[i].Name}})
	}
	return requests
}

// SetupWithManager registers label-based watches for every Team-generated kind.
func (r *TeamReconciler) SetupWithManager(mgr ctrl.Manager) error {
	mapper := handler.EnqueueRequestsFromMapFunc(teamRequestsForObject)
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.Team{}).
		Watches(&corev1.Namespace{}, mapper).
		Watches(&corev1.ResourceQuota{}, mapper).
		Watches(&corev1.LimitRange{}, mapper).
		Watches(&corev1.ServiceAccount{}, mapper).
		Watches(&rbacv1.RoleBinding{}, mapper).
		Watches(&rbacv1.ClusterRole{}, handler.EnqueueRequestsFromMapFunc(r.teamRequestsForOwnerClusterRole)).
		Watches(&networkingv1.NetworkPolicy{}, mapper).
		Named("team").
		Complete(r)
}
