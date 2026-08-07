package controller

import (
	"context"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
	"github.com/saadabdullaah/steadystate/internal/resources"
)

func TestDatabaseStatusRequiresRealBackupEvidence(t *testing.T) {
	database := databaseStatusFixture()
	database.Status.Conditions = []metav1.Condition{{
		Type: conditionApplicationDatabaseReady, Status: metav1.ConditionTrue,
		ObservedGeneration: database.Generation, Reason: "LegacyDatabaseReady",
	}}
	cluster := readyDatabaseCluster()

	status := databaseStatusFromCluster(database, cluster, databaseBackupState{})
	if status.Phase != platformv1alpha1.DatabasePhaseBackingUp {
		t.Fatalf("phase = %s, want BackingUp before first successful backup", status.Phase)
	}
	if condition := conditionByType(status.Conditions, conditionManagedDatabaseReady); condition == nil || condition.Status != metav1.ConditionFalse {
		t.Fatalf("Ready condition = %#v, want False before first backup", condition)
	}
	if condition := conditionByType(status.Conditions, conditionApplicationDatabaseReady); condition != nil {
		t.Fatalf("Database retained obsolete Application-only DatabaseReady condition: %#v", condition)
	}

	completedAt := metav1.NewTime(time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC))
	status = databaseStatusFromCluster(database, cluster, databaseBackupState{lastSuccessful: &completedAt, latestPhase: "completed"})
	if status.Phase != platformv1alpha1.DatabasePhaseHealthy {
		t.Fatalf("phase = %s, want Healthy after successful backup", status.Phase)
	}
	if status.LastSuccessfulBackup == nil || !status.LastSuccessfulBackup.Equal(&completedAt) {
		t.Fatalf("lastSuccessfulBackup = %#v, want %s", status.LastSuccessfulBackup, completedAt)
	}
	if condition := conditionByType(status.Conditions, conditionManagedDatabaseReady); condition == nil || condition.Status != metav1.ConditionTrue {
		t.Fatalf("Ready condition = %#v, want True after successful backup", condition)
	}
}

func TestDatabaseStatusDegradesOnLatestBackupFailure(t *testing.T) {
	database := databaseStatusFixture()
	completedAt := metav1.NewTime(time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC))
	status := databaseStatusFromCluster(database, readyDatabaseCluster(), databaseBackupState{
		lastSuccessful: &completedAt,
		latestPhase:    "failed",
	})
	if status.Phase != platformv1alpha1.DatabasePhaseDegraded {
		t.Fatalf("phase = %s, want Degraded", status.Phase)
	}
	if condition := conditionByType(status.Conditions, conditionDatabaseBackupHealthy); condition == nil || condition.Status != metav1.ConditionFalse {
		t.Fatalf("BackupHealthy condition = %#v, want False", condition)
	}
}

func TestInvalidDatabaseRevisionPreservesLastResolvedRevision(t *testing.T) {
	database := databaseStatusFixture()
	database.Status.ResolvedGitRevision = "fedcba9876543210fedcba9876543210fedcba98"
	database.Annotations[platformv1alpha1.SourceRevisionAnnotationKey] = "main"
	status := databaseFailureStatus(database, "InvalidConfiguration", "invalid source revision")
	if status.ResolvedGitRevision != database.Status.ResolvedGitRevision {
		t.Fatalf("invalid revision replaced last resolved revision: %q", status.ResolvedGitRevision)
	}
}

func TestDatabaseDeletionReportsFailedFinalBackupAsFalse(t *testing.T) {
	status := databaseDeletingStatus(databaseStatusFixture(), false, "backup failed")
	if condition := conditionByType(status.Conditions, conditionDatabaseBackupHealthy); condition == nil || condition.Status != metav1.ConditionFalse {
		t.Fatalf("BackupHealthy condition = %#v, want False", condition)
	}
}

func TestBackupWatchMapsManualBackupToDatabase(t *testing.T) {
	database := databaseStatusFixture()
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	reconciler := &DatabaseReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(database).Build()}
	backup := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "postgresql.cnpg.io/v1",
		"kind":       "Backup",
		"metadata":   map[string]any{"name": "manual", "namespace": database.Namespace},
		"spec":       map[string]any{"cluster": map[string]any{"name": resources.DatabaseClusterName(database)}},
	}}
	requests := reconciler.databaseForBackup(context.Background(), backup)
	if len(requests) != 1 || requests[0].Name != database.Name || requests[0].Namespace != database.Namespace {
		t.Fatalf("unexpected Backup watch mapping: %#v", requests)
	}
}

func TestForceDeleteSkipsFinalBackup(t *testing.T) {
	database := databaseStatusFixture()
	now := metav1.Now()
	database.DeletionTimestamp = &now
	database.Finalizers = []string{platformv1alpha1.DatabaseFinalizer}
	database.Annotations[platformv1alpha1.ForceDeleteAnnotationKey] = "true"
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	reconciler := &DatabaseReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(database).Build(),
		Scheme: scheme,
	}
	if _, err := reconciler.reconcileDatabaseDeletion(context.Background(), database); err != nil {
		t.Fatalf("force deletion failed: %v", err)
	}
	current := &platformv1alpha1.Database{}
	err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(database), current)
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatal(err)
	}
	if err == nil && len(current.Finalizers) != 0 {
		t.Fatalf("force deletion retained finalizers: %#v", current.Finalizers)
	}
	finalBackup := resources.DatabaseFinalBackup(database)
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(finalBackup), finalBackup); !apierrors.IsNotFound(err) {
		t.Fatalf("force deletion created a final Backup: %v", err)
	}
}

func TestDatabaseUnstructuredReconcileCreatesAbsentChildWithDesiredIdentity(t *testing.T) {
	database := databaseStatusFixture()
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	scheme.AddKnownTypeWithName(resources.BarmanObjectStoreGVK, &unstructured.Unstructured{})

	reconciler := &DatabaseReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(database).Build(),
		Scheme: scheme,
	}
	desired := resources.DatabaseObjectStore(database, resources.DefaultBackupStoreEndpoint)
	changed, err := reconciler.reconcileDatabaseUnstructured(context.Background(), database, desired)
	if err != nil {
		t.Fatalf("create-on-not-found reconciliation failed: %v", err)
	}
	if !changed {
		t.Fatal("create-on-not-found reconciliation reported no write")
	}

	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(resources.BarmanObjectStoreGVK)
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(desired), current); err != nil {
		t.Fatalf("created ObjectStore is unavailable: %v", err)
	}
	if current.GetName() != desired.GetName() || current.GetNamespace() != desired.GetNamespace() {
		t.Fatalf("created ObjectStore identity = %s/%s, want %s/%s",
			current.GetNamespace(), current.GetName(), desired.GetNamespace(), desired.GetName())
	}
	owner := metav1.GetControllerOf(current)
	if owner == nil || owner.UID != database.UID || owner.Kind != "Database" {
		t.Fatalf("created ObjectStore owner = %#v, want Database %s", owner, database.UID)
	}
}

func TestLegacyDatabaseMonitoringCleanupRequiresDatabaseOwnership(t *testing.T) {
	database := databaseStatusFixture()
	for _, test := range []struct {
		name      string
		owner     metav1.OwnerReference
		wantFound bool
	}{
		{name: "database-owned", owner: resources.DatabaseOwnerReference(database), wantFound: false},
		{name: "application-owned", owner: metav1.OwnerReference{
			APIVersion: platformv1alpha1.GroupVersion.String(), Kind: "Application", Name: database.Name,
			UID: types.UID("application-owner"), Controller: ptr.To(true), BlockOwnerDeletion: ptr.To(true),
		}, wantFound: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := platformv1alpha1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			scheme.AddKnownTypeWithName(resources.DatabaseServiceMonitorGVK, &unstructured.Unstructured{})
			legacy := resources.LegacyDatabaseMonitoringResources(database)[0]
			legacy.SetOwnerReferences([]metav1.OwnerReference{test.owner})
			reconciler := &DatabaseReconciler{
				Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(database, legacy).Build(),
				Scheme: scheme,
			}
			changed, err := reconciler.deleteDatabaseObjectIfControlledBy(context.Background(), database, legacy)
			if err != nil {
				t.Fatal(err)
			}
			if changed == test.wantFound {
				t.Fatalf("changed = %t, want %t", changed, !test.wantFound)
			}
			current := &unstructured.Unstructured{}
			current.SetGroupVersionKind(resources.DatabaseServiceMonitorGVK)
			err = reconciler.Get(context.Background(), client.ObjectKeyFromObject(legacy), current)
			if test.wantFound && err != nil {
				t.Fatalf("Application-owned monitoring object was removed: %v", err)
			}
			if !test.wantFound && !apierrors.IsNotFound(err) {
				t.Fatalf("Database-owned legacy monitoring object still exists: %v", err)
			}
		})
	}
}

func TestDatabaseFinalBackupReconcileCreatesAbsentBackupWithDesiredIdentity(t *testing.T) {
	database := databaseStatusFixture()
	scheme := runtime.NewScheme()
	if err := platformv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	scheme.AddKnownTypeWithName(resources.CNPGBackupGVK, &unstructured.Unstructured{})

	reconciler := &DatabaseReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(database).Build(),
		Scheme: scheme,
	}
	desired := resources.DatabaseFinalBackup(database)
	changed, err := reconciler.reconcileDatabaseFinalBackup(context.Background(), desired)
	if err != nil {
		t.Fatalf("final Backup create-on-not-found reconciliation failed: %v", err)
	}
	if !changed {
		t.Fatal("final Backup create-on-not-found reconciliation reported no write")
	}

	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(resources.CNPGBackupGVK)
	if err := reconciler.Get(context.Background(), client.ObjectKeyFromObject(desired), current); err != nil {
		t.Fatalf("created final Backup is unavailable: %v", err)
	}
	if current.GetName() != desired.GetName() || current.GetNamespace() != desired.GetNamespace() {
		t.Fatalf("created final Backup identity = %s/%s, want %s/%s",
			current.GetNamespace(), current.GetName(), desired.GetNamespace(), desired.GetName())
	}
	if len(current.GetOwnerReferences()) != 0 {
		t.Fatalf("created final Backup has owner references: %#v", current.GetOwnerReferences())
	}
}

func databaseStatusFixture() *platformv1alpha1.Database {
	return &platformv1alpha1.Database{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "orders",
			Namespace:  "team-payments",
			UID:        types.UID("01234567-89ab-cdef-0123-456789abcdef"),
			Generation: 2,
			Annotations: map[string]string{
				platformv1alpha1.SourceRevisionAnnotationKey: strings.Repeat("a", 40),
			},
		},
		Spec: platformv1alpha1.DatabaseSpec{
			Engine:    "postgres",
			Instances: 1,
			Storage:   platformv1alpha1.DatabaseStorage{Size: "1Gi"},
			Backups:   platformv1alpha1.DatabaseBackups{Enabled: true, Schedule: "0 0 2 * * *", Retention: "7d"},
		},
	}
}

func readyDatabaseCluster() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"status": map[string]any{
			"conditions": []any{map[string]any{"type": "Ready", "status": "True"}},
		},
	}}
}

func conditionByType(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}
