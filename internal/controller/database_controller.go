package controller

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	controllermetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	ctrlreconcile "sigs.k8s.io/controller-runtime/pkg/reconcile"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
	databaselogic "github.com/saadabdullaah/steadystate/internal/database"
	"github.com/saadabdullaah/steadystate/internal/resources"
)

const (
	conditionDatabaseConfigurationReady = "ConfigurationReady"
	conditionDatabaseClusterReady       = "ClusterReady"
	conditionDatabaseBackupHealthy      = "BackupHealthy"
	conditionManagedDatabaseReady       = "Ready"
)

var (
	databaseBackupHealthyMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "steadystate_database_backup_healthy",
		Help: "Database backup health observed by the controller (1 healthy, 0 unhealthy, -1 unknown).",
	}, []string{"namespace", "database"})
	databaseLastSuccessfulBackupMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "steadystate_database_last_successful_backup_timestamp_seconds",
		Help: "Unix timestamp of the latest successful Database backup observed by the controller.",
	}, []string{"namespace", "database"})
)

func init() {
	controllermetrics.Registry.MustRegister(databaseBackupHealthyMetric, databaseLastSuccessfulBackupMetric)
}

// DatabaseReconciler manages CNPG and Barman resources without importing their Go modules.
type DatabaseReconciler struct {
	client.Client
	Scheme              *runtime.Scheme
	Recorder            events.EventRecorder
	BackupStoreEndpoint string
}

// +kubebuilder:rbac:groups=platform.steadystate.dev,resources=databases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=platform.steadystate.dev,resources=databases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.steadystate.dev,resources=databases/finalizers,verbs=update
// +kubebuilder:rbac:groups=platform.steadystate.dev,resources=applications,verbs=get;list;watch
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters;backups;scheduledbackups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=barmancloud.cnpg.io,resources=objectstores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors;prometheusrules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch;update
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch;update

func (r *DatabaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	database := &platformv1alpha1.Database{}
	if err := r.Get(ctx, req.NamespacedName, database); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !database.DeletionTimestamp.IsZero() {
		return r.reconcileDatabaseDeletion(ctx, database)
	}
	if !controllerutil.ContainsFinalizer(database, platformv1alpha1.DatabaseFinalizer) {
		before := database.DeepCopy()
		controllerutil.AddFinalizer(database, platformv1alpha1.DatabaseFinalizer)
		if err := r.Patch(ctx, database, client.MergeFrom(before)); err != nil {
			return ctrl.Result{}, err
		}
	}
	if err := databaselogic.Validate(database); err != nil {
		_, statusErr := r.patchDatabaseStatus(ctx, database, databaseFailureStatus(database, "InvalidConfiguration", err.Error()))
		return ctrl.Result{}, statusErr
	}
	if err := r.validateDatabaseTenancy(ctx, database); err != nil {
		_, statusErr := r.patchDatabaseStatus(ctx, database, databaseFailureStatus(database, "TenancyRejected", err.Error()))
		return ctrl.Result{}, statusErr
	}
	mutated := false
	var changed bool
	var err error
	if database.Spec.Backups.Enabled {
		source := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: resources.BackupStoreSourceNamespace, Name: resources.BackupStoreSourceSecretName}, source); err != nil {
			message := "platform backup credential is unavailable"
			_, statusErr := r.patchDatabaseStatus(ctx, database, databaseFailureStatus(database, "BackupCredentialUnavailable", message))
			if statusErr != nil {
				return ctrl.Result{}, statusErr
			}
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		if len(source.Data["ACCESS_KEY_ID"]) == 0 || len(source.Data["ACCESS_SECRET_KEY"]) == 0 {
			message := "platform backup credential is missing required keys"
			_, statusErr := r.patchDatabaseStatus(ctx, database, databaseFailureStatus(database, "BackupCredentialInvalid", message))
			return ctrl.Result{}, statusErr
		}
		credential := resources.DatabaseBackupCredential(database, source)
		changed, err = r.reconcileDatabaseObject(ctx, database, credential, func(current client.Object) {
			secret := current.(*corev1.Secret)
			secret.Type = credential.Type
			secret.Data = credential.Data
		})
		mutated = mutated || changed
		if err != nil {
			return r.databaseReconcileFailure(ctx, database, err)
		}
		changed, err = r.reconcileDatabaseUnstructured(ctx, database, resources.DatabaseObjectStore(database, r.BackupStoreEndpoint))
		mutated = mutated || changed
		if err != nil {
			return r.databaseReconcileFailure(ctx, database, err)
		}
		changed, err = r.reconcileDatabaseUnstructured(ctx, database, resources.DatabaseScheduledBackup(database))
		mutated = mutated || changed
		if err != nil {
			return r.databaseReconcileFailure(ctx, database, err)
		}
	} else {
		for _, obsolete := range []client.Object{
			resources.DatabaseBackupCredential(database, &corev1.Secret{}),
			resources.DatabaseObjectStore(database, r.BackupStoreEndpoint),
			resources.DatabaseScheduledBackup(database),
			resources.DatabaseBackupNetworkPolicy(database, endpointCIDR(r.BackupStoreEndpoint)),
		} {
			changed, err = r.deleteDatabaseObject(ctx, obsolete)
			mutated = mutated || changed
			if err != nil {
				return r.databaseReconcileFailure(ctx, database, err)
			}
		}
	}
	changed, err = r.reconcileDatabaseUnstructured(ctx, database, resources.DatabaseCluster(database))
	mutated = mutated || changed
	if err != nil {
		return r.databaseReconcileFailure(ctx, database, err)
	}
	for _, desired := range resources.DatabaseNetworkPolicies(database, endpointCIDR(r.BackupStoreEndpoint)) {
		networkPolicy := desired
		changed, err = r.reconcileDatabaseObject(ctx, database, networkPolicy, func(current client.Object) {
			current.(*networkingv1.NetworkPolicy).Spec = networkPolicy.Spec
		})
		mutated = mutated || changed
		if err != nil {
			return r.databaseReconcileFailure(ctx, database, err)
		}
	}
	for _, desired := range []*unstructured.Unstructured{resources.DatabaseServiceMonitor(database), resources.DatabasePrometheusRule(database)} {
		changed, err = r.reconcileDatabaseUnstructured(ctx, database, desired)
		mutated = mutated || changed
		if err != nil {
			return r.databaseReconcileFailure(ctx, database, err)
		}
	}

	cluster := resources.DatabaseCluster(database)
	if err := r.Get(ctx, client.ObjectKeyFromObject(cluster), cluster); err != nil {
		return r.databaseReconcileFailure(ctx, database, err)
	}
	backupState, err := r.observeDatabaseBackups(ctx, database)
	if err != nil {
		return r.databaseReconcileFailure(ctx, database, err)
	}
	status := databaseStatusFromCluster(database, cluster, backupState)
	updateDatabaseBackupMetrics(database, status)
	statusChanged, err := r.patchDatabaseStatus(ctx, database, status)
	if err != nil {
		return ctrl.Result{}, err
	}
	if mutated || statusChanged {
		r.eventDatabase(database, corev1.EventTypeNormal, "Reconciled", "Database, backup, monitoring, and network resources were reconciled")
	}
	return ctrl.Result{}, nil
}

func endpointCIDR(endpoint string) string {
	if endpoint == "" {
		return "172.30.240.10/32"
	}
	parsed, err := url.Parse(endpoint)
	if err == nil && parsed.Hostname() != "" && strings.Count(parsed.Hostname(), ".") == 3 {
		return parsed.Hostname() + "/32"
	}
	return "172.30.240.10/32"
}

func (r *DatabaseReconciler) validateDatabaseTenancy(ctx context.Context, database *platformv1alpha1.Database) error {
	namespace := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: database.Namespace}, namespace); err != nil {
		return fmt.Errorf("managed Team namespace is unavailable")
	}
	if namespace.Labels[resources.TeamLabelKey] == "" || namespace.Labels["app.kubernetes.io/managed-by"] != resources.ManagedBy {
		return fmt.Errorf("database must be created in a SteadyState Team namespace")
	}
	return nil
}

func (r *DatabaseReconciler) reconcileDatabaseObject(ctx context.Context, database *platformv1alpha1.Database, desired client.Object, mutate func(client.Object)) (bool, error) {
	current := desired.DeepCopyObject().(client.Object)
	current.SetResourceVersion("")
	current.SetUID("")
	current.SetOwnerReferences(nil)
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), current)
	if err != nil && !apierrors.IsNotFound(err) {
		return false, err
	}
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, current, func() error {
		current.SetLabels(desired.GetLabels())
		current.SetAnnotations(desired.GetAnnotations())
		if err := controllerutil.SetControllerReference(database, current, r.Scheme); err != nil {
			return err
		}
		mutate(current)
		return nil
	})
	return op != controllerutil.OperationResultNone, err
}

func (r *DatabaseReconciler) reconcileDatabaseUnstructured(ctx context.Context, database *platformv1alpha1.Database, desired *unstructured.Unstructured) (bool, error) {
	// Preserve the desired identity for the create-on-not-found path. Get
	// populates the rest of the object when the child already exists.
	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(desired.GroupVersionKind())
	current.SetName(desired.GetName())
	current.SetNamespace(desired.GetNamespace())
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), current)
	if err != nil && !apierrors.IsNotFound(err) {
		return false, err
	}
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, current, func() error {
		current.SetLabels(desired.GetLabels())
		current.SetAnnotations(desired.GetAnnotations())
		if err := controllerutil.SetControllerReference(database, current, r.Scheme); err != nil {
			return err
		}
		spec, found, err := unstructured.NestedMap(desired.Object, "spec")
		if err != nil || !found {
			return fmt.Errorf("desired %s spec: %w", desired.GetKind(), err)
		}
		return unstructured.SetNestedMap(current.Object, spec, "spec")
	})
	return op != controllerutil.OperationResultNone, err
}

func (r *DatabaseReconciler) deleteDatabaseObject(ctx context.Context, object client.Object) (bool, error) {
	current := object.DeepCopyObject().(client.Object)
	if unstructuredObject, ok := object.(*unstructured.Unstructured); ok {
		current.(*unstructured.Unstructured).SetGroupVersionKind(unstructuredObject.GroupVersionKind())
	}
	if err := r.Get(ctx, client.ObjectKeyFromObject(object), current); apierrors.IsNotFound(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, client.IgnoreNotFound(r.Delete(ctx, current))
}

func (r *DatabaseReconciler) databaseReconcileFailure(ctx context.Context, database *platformv1alpha1.Database, reconcileErr error) (ctrl.Result, error) {
	message := strings.Join(strings.Fields(reconcileErr.Error()), " ")
	if len(message) > 512 {
		message = message[:512]
	}
	_, statusErr := r.patchDatabaseStatus(ctx, database, databaseFailureStatus(database, "ReconciliationFailed", message))
	if statusErr != nil {
		return ctrl.Result{}, fmt.Errorf("reconcile Database: %w; status: %v", reconcileErr, statusErr)
	}
	return ctrl.Result{}, reconcileErr
}

func (r *DatabaseReconciler) reconcileDatabaseDeletion(ctx context.Context, database *platformv1alpha1.Database) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(database, platformv1alpha1.DatabaseFinalizer) {
		return ctrl.Result{}, nil
	}
	force := strings.EqualFold(database.Annotations[platformv1alpha1.ForceDeleteAnnotationKey], "true")
	if database.Spec.Backups.Enabled && !force {
		finalBackup := resources.DatabaseFinalBackup(database)
		if _, err := r.reconcileDatabaseFinalBackup(ctx, finalBackup); err != nil {
			_, _ = r.patchDatabaseStatus(ctx, database, databaseDeletingStatus(database, false, err.Error()))
			return ctrl.Result{}, err
		}
		if err := r.Get(ctx, client.ObjectKeyFromObject(finalBackup), finalBackup); err != nil {
			return ctrl.Result{}, err
		}
		phase, _, _ := unstructured.NestedString(finalBackup.Object, "status", "phase")
		if !strings.EqualFold(phase, "completed") {
			message := "waiting for the deterministic final Barman backup to complete"
			if strings.EqualFold(phase, "failed") {
				message = "final Barman backup failed; repair backup storage or set steadystate.dev/force-delete=true to accept data-loss risk"
			}
			_, _ = r.patchDatabaseStatus(ctx, database, databaseDeletingStatus(database, false, message))
			return ctrl.Result{}, nil
		}
		if err := r.Delete(ctx, finalBackup); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	} else if force {
		if err := r.Delete(ctx, resources.DatabaseFinalBackup(database)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}
	cluster := resources.DatabaseCluster(database)
	if err := r.Delete(ctx, cluster); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	before := database.DeepCopy()
	controllerutil.RemoveFinalizer(database, platformv1alpha1.DatabaseFinalizer)
	if err := r.Patch(ctx, database, client.MergeFrom(before)); err != nil {
		return ctrl.Result{}, err
	}
	databaseBackupHealthyMetric.DeleteLabelValues(database.Namespace, database.Name)
	databaseLastSuccessfulBackupMetric.DeleteLabelValues(database.Namespace, database.Name)
	message := "Database finalization completed; backups were disabled"
	eventType := corev1.EventTypeNormal
	if database.Spec.Backups.Enabled {
		message = "Final backup completed and external backup objects were retained"
	}
	if force {
		eventType = corev1.EventTypeWarning
		message = "Database finalizer was force-released with explicit data-loss acceptance"
	}
	r.eventDatabase(database, eventType, "FinalizerReleased", message)
	return ctrl.Result{}, nil
}

func (r *DatabaseReconciler) reconcileDatabaseFinalBackup(ctx context.Context, desired *unstructured.Unstructured) (bool, error) {
	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(desired.GroupVersionKind())
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), current)
	if err != nil && !apierrors.IsNotFound(err) {
		return false, err
	}
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, current, func() error {
		current.SetLabels(desired.GetLabels())
		current.SetAnnotations(desired.GetAnnotations())
		// A final Backup deliberately has no owner reference: creating a
		// dependent for an already-terminating Database is unsafe. It is deleted
		// explicitly after successful completion; external objects are retained.
		current.SetOwnerReferences(nil)
		spec, found, nestedErr := unstructured.NestedMap(desired.Object, "spec")
		if nestedErr != nil {
			return fmt.Errorf("desired final Backup spec: %w", nestedErr)
		}
		if !found {
			return fmt.Errorf("desired final Backup spec is missing")
		}
		return unstructured.SetNestedMap(current.Object, spec, "spec")
	})
	return op != controllerutil.OperationResultNone, err
}

type databaseBackupState struct {
	lastSuccessful *metav1.Time
	latestPhase    string
}

func (r *DatabaseReconciler) observeDatabaseBackups(ctx context.Context, database *platformv1alpha1.Database) (databaseBackupState, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{Group: resources.CNPGBackupGVK.Group, Version: resources.CNPGBackupGVK.Version, Kind: "BackupList"})
	if err := r.List(ctx, list, client.InNamespace(database.Namespace)); err != nil {
		if meta.IsNoMatchError(err) {
			return databaseBackupState{}, nil
		}
		return databaseBackupState{}, err
	}

	clusterName := resources.DatabaseClusterName(database)
	var state databaseBackupState
	var latestAttempt metav1.Time
	for i := range list.Items {
		backup := &list.Items[i]
		backupCluster, _, _ := unstructured.NestedString(backup.Object, "spec", "cluster", "name")
		if backupCluster != clusterName {
			continue
		}
		phase, _, _ := unstructured.NestedString(backup.Object, "status", "phase")
		attemptTime := backupTimestamp(backup)
		if latestAttempt.IsZero() || attemptTime.After(latestAttempt.Time) {
			latestAttempt = attemptTime
			state.latestPhase = strings.ToLower(phase)
		}
		if strings.EqualFold(phase, "completed") && (state.lastSuccessful == nil || attemptTime.After(state.lastSuccessful.Time)) {
			completed := attemptTime.DeepCopy()
			state.lastSuccessful = completed
		}
	}
	return state, nil
}

func backupTimestamp(backup *unstructured.Unstructured) metav1.Time {
	for _, field := range []string{"stoppedAt", "startedAt"} {
		value, found, _ := unstructured.NestedString(backup.Object, "status", field)
		if found {
			if parsed, err := time.Parse(time.RFC3339, value); err == nil {
				return metav1.NewTime(parsed)
			}
		}
	}
	return backup.GetCreationTimestamp()
}

func updateDatabaseBackupMetrics(database *platformv1alpha1.Database, status platformv1alpha1.DatabaseStatus) {
	healthy := -1.0
	if condition := meta.FindStatusCondition(status.Conditions, conditionDatabaseBackupHealthy); condition != nil {
		switch condition.Status {
		case metav1.ConditionTrue:
			healthy = 1
		case metav1.ConditionFalse:
			healthy = 0
		}
	}
	databaseBackupHealthyMetric.WithLabelValues(database.Namespace, database.Name).Set(healthy)
	if status.LastSuccessfulBackup != nil {
		databaseLastSuccessfulBackupMetric.WithLabelValues(database.Namespace, database.Name).Set(float64(status.LastSuccessfulBackup.Unix()))
	}
}

func databaseStatusFromCluster(database *platformv1alpha1.Database, cluster *unstructured.Unstructured, backupState databaseBackupState) platformv1alpha1.DatabaseStatus {
	status := databaseBaseStatus(database)
	status.LastSuccessfulBackup = backupState.lastSuccessful
	conditions, _, _ := unstructured.NestedSlice(cluster.Object, "status", "conditions")
	ready := false
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if ok && condition["type"] == "Ready" && condition["status"] == "True" {
			ready = true
		}
	}
	if ready {
		setDatabaseCondition(&status, database.Generation, conditionDatabaseClusterReady, metav1.ConditionTrue, "ClusterReady", "CloudNativePG reports the PostgreSQL cluster Ready")
		switch {
		case !database.Spec.Backups.Enabled:
			status.Phase = platformv1alpha1.DatabasePhaseHealthy
			setDatabaseCondition(&status, database.Generation, conditionDatabaseBackupHealthy, metav1.ConditionTrue, "BackupsDisabled", "Backups are explicitly disabled")
			setDatabaseCondition(&status, database.Generation, conditionManagedDatabaseReady, metav1.ConditionTrue, "DatabaseReady", "PostgreSQL is ready")
		case backupState.latestPhase == "failed":
			status.Phase = platformv1alpha1.DatabasePhaseDegraded
			setDatabaseCondition(&status, database.Generation, conditionDatabaseBackupHealthy, metav1.ConditionFalse, "BackupFailed", "The latest Barman backup failed")
			setDatabaseCondition(&status, database.Generation, conditionManagedDatabaseReady, metav1.ConditionFalse, "BackupFailed", "PostgreSQL is available but backup health is degraded")
		case backupState.lastSuccessful == nil:
			status.Phase = platformv1alpha1.DatabasePhaseBackingUp
			setDatabaseCondition(&status, database.Generation, conditionDatabaseBackupHealthy, metav1.ConditionUnknown, "AwaitingFirstBackup", "Waiting for the first successful Barman backup")
			setDatabaseCondition(&status, database.Generation, conditionManagedDatabaseReady, metav1.ConditionFalse, "AwaitingFirstBackup", "Database readiness requires a successful backup")
		case backupState.latestPhase == "pending" || backupState.latestPhase == "running":
			status.Phase = platformv1alpha1.DatabasePhaseBackingUp
			setDatabaseCondition(&status, database.Generation, conditionDatabaseBackupHealthy, metav1.ConditionTrue, "BackupInProgress", "A new backup is running after a successful backup")
			setDatabaseCondition(&status, database.Generation, conditionManagedDatabaseReady, metav1.ConditionTrue, "DatabaseReady", "PostgreSQL is ready and has a successful backup")
		default:
			status.Phase = platformv1alpha1.DatabasePhaseHealthy
			setDatabaseCondition(&status, database.Generation, conditionDatabaseBackupHealthy, metav1.ConditionTrue, "BackupSucceeded", "A successful Barman backup is available")
			setDatabaseCondition(&status, database.Generation, conditionManagedDatabaseReady, metav1.ConditionTrue, "DatabaseReady", "PostgreSQL and backup health are ready")
		}
	} else {
		if database.Spec.Recovery != nil {
			status.Phase = platformv1alpha1.DatabasePhaseRestoring
		} else {
			status.Phase = platformv1alpha1.DatabasePhaseProvisioning
		}
		setDatabaseCondition(&status, database.Generation, conditionDatabaseClusterReady, metav1.ConditionFalse, "ClusterProgressing", "Waiting for CloudNativePG readiness")
		setDatabaseCondition(&status, database.Generation, conditionDatabaseBackupHealthy, metav1.ConditionUnknown, "ClusterProgressing", "Backup health awaits PostgreSQL readiness")
		setDatabaseCondition(&status, database.Generation, conditionManagedDatabaseReady, metav1.ConditionFalse, "ClusterProgressing", "Database is progressing")
	}
	setDatabaseCondition(&status, database.Generation, conditionDatabaseConfigurationReady, metav1.ConditionTrue, "ResourcesReconciled", "Database configuration and generated resources are valid")
	return status
}

func databaseBaseStatus(database *platformv1alpha1.Database) platformv1alpha1.DatabaseStatus {
	status := *database.Status.DeepCopy()
	meta.RemoveStatusCondition(&status.Conditions, conditionApplicationDatabaseReady)
	status.ObservedGeneration = database.Generation
	status.ConnectionSecretName = resources.DatabaseConnectionSecretName(database)
	status.BackupServerName = resources.DatabaseBackupServerName(database)
	if revision, err := databaselogic.ResolvedSourceRevision(database); err == nil {
		status.ResolvedGitRevision = revision
	}
	if database.Spec.Recovery != nil {
		status.RecoverySourceServerName = database.Spec.Recovery.SourceServerName
	}
	return status
}

func databaseFailureStatus(database *platformv1alpha1.Database, reason, message string) platformv1alpha1.DatabaseStatus {
	status := databaseBaseStatus(database)
	status.Phase = platformv1alpha1.DatabasePhaseDegraded
	for _, conditionType := range []string{conditionDatabaseConfigurationReady, conditionDatabaseClusterReady, conditionDatabaseBackupHealthy, conditionManagedDatabaseReady} {
		setDatabaseCondition(&status, database.Generation, conditionType, metav1.ConditionFalse, reason, message)
	}
	return status
}

func databaseDeletingStatus(database *platformv1alpha1.Database, backupReady bool, message string) platformv1alpha1.DatabaseStatus {
	status := databaseBaseStatus(database)
	status.Phase = platformv1alpha1.DatabasePhaseDeleting
	backupStatus := metav1.ConditionFalse
	if backupReady {
		backupStatus = metav1.ConditionTrue
	}
	setDatabaseCondition(&status, database.Generation, conditionDatabaseBackupHealthy, backupStatus, "FinalBackup", message)
	setDatabaseCondition(&status, database.Generation, conditionManagedDatabaseReady, metav1.ConditionFalse, "Deleting", message)
	return status
}

func setDatabaseCondition(status *platformv1alpha1.DatabaseStatus, generation int64, conditionType string, conditionStatus metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{Type: conditionType, Status: conditionStatus, ObservedGeneration: generation, Reason: reason, Message: message})
}

func (r *DatabaseReconciler) patchDatabaseStatus(ctx context.Context, database *platformv1alpha1.Database, desired platformv1alpha1.DatabaseStatus) (bool, error) {
	if apiequality.Semantic.DeepEqual(database.Status, desired) {
		return false, nil
	}
	key := client.ObjectKeyFromObject(database)
	changed := false
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &platformv1alpha1.Database{}
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

func (r *DatabaseReconciler) eventDatabase(database *platformv1alpha1.Database, eventType, reason, message string) {
	if r.Recorder != nil {
		r.Recorder.Eventf(database, nil, eventType, reason, reason, "%s", message)
	}
}

func databaseWatchObject(gvk schema.GroupVersionKind) *unstructured.Unstructured {
	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(gvk)
	return object
}

func databaseRequestsForApplication(_ context.Context, object client.Object) []ctrlreconcile.Request {
	application, ok := object.(*platformv1alpha1.Application)
	if !ok || application.Spec.DatabaseRef == nil {
		return nil
	}
	return []ctrlreconcile.Request{{NamespacedName: types.NamespacedName{Name: application.Spec.DatabaseRef.Name, Namespace: application.Namespace}}}
}

func (r *DatabaseReconciler) databasesForBackupCredential(ctx context.Context, object client.Object) []ctrlreconcile.Request {
	if object.GetNamespace() != resources.BackupStoreSourceNamespace || object.GetName() != resources.BackupStoreSourceSecretName {
		return nil
	}
	databases := &platformv1alpha1.DatabaseList{}
	if err := r.List(ctx, databases); err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "unable to list Databases for backup credential rotation")
		return nil
	}
	requests := make([]ctrlreconcile.Request, 0, len(databases.Items))
	for i := range databases.Items {
		requests = append(requests, ctrlreconcile.Request{NamespacedName: client.ObjectKeyFromObject(&databases.Items[i])})
	}
	return requests
}

func (r *DatabaseReconciler) databaseForBackup(ctx context.Context, object client.Object) []ctrlreconcile.Request {
	backup, ok := object.(*unstructured.Unstructured)
	if !ok {
		return nil
	}
	clusterName, found, _ := unstructured.NestedString(backup.Object, "spec", "cluster", "name")
	if !found || clusterName == "" {
		return nil
	}
	databases := &platformv1alpha1.DatabaseList{}
	if err := r.List(ctx, databases, client.InNamespace(backup.GetNamespace())); err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "unable to list Databases for Backup watch")
		return nil
	}
	for i := range databases.Items {
		if resources.DatabaseClusterName(&databases.Items[i]) == clusterName {
			return []ctrlreconcile.Request{{NamespacedName: client.ObjectKeyFromObject(&databases.Items[i])}}
		}
	}
	return nil
}

// SetupWithManager uses unstructured watches only when exact external CRDs exist.
func (r *DatabaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.Database{}).
		Owns(&corev1.Secret{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.databasesForBackupCredential)).
		Watches(&platformv1alpha1.Application{}, handler.EnqueueRequestsFromMapFunc(databaseRequestsForApplication))
	for _, gvk := range []schema.GroupVersionKind{
		resources.CNPGClusterGVK,
		resources.CNPGBackupGVK,
		resources.CNPGScheduledGVK,
		resources.BarmanObjectStoreGVK,
		{Group: "monitoring.coreos.com", Version: "v1", Kind: "ServiceMonitor"},
		{Group: "monitoring.coreos.com", Version: "v1", Kind: "PrometheusRule"},
	} {
		available, err := optionalResourceAvailable(mgr.GetRESTMapper(), gvk)
		if err != nil {
			return err
		}
		if available {
			builder = builder.Owns(databaseWatchObject(gvk))
			if gvk == resources.CNPGBackupGVK {
				builder = builder.Watches(databaseWatchObject(gvk), handler.EnqueueRequestsFromMapFunc(r.databaseForBackup))
			}
		}
	}
	return builder.Named("database").Complete(r)
}
