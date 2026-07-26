package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// DatabasePhase is the operator's current assessment of a Database.
// +kubebuilder:validation:Enum=Pending;Provisioning;Restoring;Healthy;Degraded;BackingUp;Deleting
type DatabasePhase string

const (
	DatabasePhasePending      DatabasePhase = "Pending"
	DatabasePhaseProvisioning DatabasePhase = "Provisioning"
	DatabasePhaseRestoring    DatabasePhase = "Restoring"
	DatabasePhaseHealthy      DatabasePhase = "Healthy"
	DatabasePhaseDegraded     DatabasePhase = "Degraded"
	DatabasePhaseBackingUp    DatabasePhase = "BackingUp"
	DatabasePhaseDeleting     DatabasePhase = "Deleting"

	DatabaseFinalizer        = "steadystate.dev/database-finalizer"
	ForceDeleteAnnotationKey = "steadystate.dev/force-delete"
)

// DatabaseStorage configures persistent PostgreSQL storage.
// +kubebuilder:validation:XValidation:rule="has(self.storageClassName) == has(oldSelf.storageClassName) && (!has(self.storageClassName) || self.storageClassName == oldSelf.storageClassName)",message="storageClassName is immutable"
// +kubebuilder:validation:XValidation:rule="quantity(self.size).compareTo(quantity(oldSelf.size)) >= 0",message="storage size may only increase"
type DatabaseStorage struct {
	// +kubebuilder:default="1Gi"
	// +kubebuilder:validation:XValidation:rule="quantity(self).compareTo(quantity('1Gi')) >= 0",message="storage size must be at least 1Gi"
	Size string `json:"size"`

	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`
}

// DatabaseBackups configures continuous archiving and scheduled base backups.
type DatabaseBackups struct {
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`

	// Schedule is a six-field cron expression including seconds.
	// +kubebuilder:default="0 0 2 * * *"
	// +kubebuilder:validation:MinLength=11
	Schedule string `json:"schedule"`

	// +kubebuilder:default="7d"
	// +kubebuilder:validation:Pattern=`^[1-9][0-9]*[dwm]$`
	Retention string `json:"retention"`
}

// DatabaseRecovery defines immutable declarative recovery from an external archive.
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="recovery is immutable"
type DatabaseRecovery struct {
	// +kubebuilder:validation:MinLength=1
	SourceServerName string `json:"sourceServerName"`

	// TargetTime is an optional RFC3339 UTC recovery target.
	// +optional
	// +kubebuilder:validation:Format=date-time
	TargetTime *metav1.Time `json:"targetTime,omitempty"`
}

// DatabaseSpec defines a managed PostgreSQL database.
// +kubebuilder:validation:XValidation:rule="!has(self.recovery) || self.backups.enabled",message="recovery requires backups.enabled=true"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.recovery) ? !has(self.recovery) : self.recovery == oldSelf.recovery",message="recovery is immutable from object creation"
type DatabaseSpec struct {
	// +kubebuilder:validation:Enum=postgres
	// +kubebuilder:default=postgres
	Engine string `json:"engine"`

	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=3
	Instances int32 `json:"instances"`

	// +kubebuilder:default={size: "1Gi"}
	Storage DatabaseStorage `json:"storage"`

	// +kubebuilder:default={enabled: true, schedule: "0 0 2 * * *", retention: "7d"}
	Backups DatabaseBackups `json:"backups"`

	// +optional
	Recovery *DatabaseRecovery `json:"recovery,omitempty"`
}

// DatabaseStatus describes current database, backup, and recovery state.
type DatabaseStatus struct {
	// +optional
	Phase DatabasePhase `json:"phase,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +optional
	ConnectionSecretName string `json:"connectionSecretName,omitempty"`
	// +optional
	BackupServerName string `json:"backupServerName,omitempty"`
	// +optional
	RecoverySourceServerName string `json:"recoverySourceServerName,omitempty"`
	// +optional
	LastSuccessfulBackup *metav1.Time `json:"lastSuccessfulBackup,omitempty"`
	// +kubebuilder:validation:Pattern=`^([0-9a-f]{40}|[0-9a-f]{64})$`
	// +optional
	ResolvedGitRevision string `json:"resolvedGitRevision,omitempty"`
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=db
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Backup Server",type=string,JSONPath=`.status.backupServerName`,priority=1
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) <= 63",message="Database names must contain at most 63 characters"

// Database is the Schema for managed PostgreSQL databases.
type Database struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec DatabaseSpec `json:"spec"`
	// +optional
	Status DatabaseStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DatabaseList contains a list of Database.
type DatabaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Database `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Database{}, &DatabaseList{})
}
