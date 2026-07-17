package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ApplicationPhase describes the controller's current assessment of an Application.
// +kubebuilder:validation:Enum=Pending;Progressing;Healthy;Degraded;RollingBack
type ApplicationPhase string

const (
	ApplicationPhasePending     ApplicationPhase = "Pending"
	ApplicationPhaseProgressing ApplicationPhase = "Progressing"
	ApplicationPhaseHealthy     ApplicationPhase = "Healthy"
	ApplicationPhaseDegraded    ApplicationPhase = "Degraded"
	ApplicationPhaseRollingBack ApplicationPhase = "RollingBack"

	// SourceRevisionAnnotationKey carries the resolved Git commit that produced
	// an Application. GitOps systems set it without changing Application.spec.
	SourceRevisionAnnotationKey = "steadystate.dev/source-revision"
)

// DeploymentStrategy selects the requested delivery strategy.
// +kubebuilder:validation:Enum=rolling;canary
type DeploymentStrategy string

const (
	DeploymentStrategyRolling DeploymentStrategy = "rolling"
	DeploymentStrategyCanary  DeploymentStrategy = "canary"
)

// Percentage is a human-readable percentage in the inclusive range 0-100.
// +kubebuilder:validation:Pattern=`^(100(?:\.0+)?|[0-9]{1,2}(?:\.[0-9]+)?)%$`
type Percentage string

// AvailabilityPercentage is a human-readable percentage greater than 0 and at most 100.
// +kubebuilder:validation:Pattern=`^(100(?:\.0+)?|(?:[1-9][0-9]?)(?:\.[0-9]+)?|0\.(?:0*[1-9][0-9]*))%$`
type AvailabilityPercentage string

// ApplicationImage identifies an immutable-by-policy container image tag.
type ApplicationImage struct {
	// Repository is the image repository without a digest.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[^@[:space:]]+$`
	Repository string `json:"repository"`

	// Tag is an explicit non-latest OCI image tag.
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`
	// +kubebuilder:validation:XValidation:rule="self.lowerAscii() != 'latest'",message="latest is not an allowed image tag"
	Tag string `json:"tag"`
}

// ReplicaBounds define the minimum and maximum desired replica counts.
// +kubebuilder:validation:XValidation:rule="self.min <= self.max",message="minimum replicas cannot exceed maximum replicas"
type ReplicaBounds struct {
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	Min int32 `json:"min"`

	// +kubebuilder:default=3
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	Max int32 `json:"max"`
}

// ApplicationRuntime configures the workload listener and replica bounds.
type ApplicationRuntime struct {
	// +kubebuilder:default=8080
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`

	Replicas ReplicaBounds `json:"replicas"`
}

// ResourceValues contains the required CPU and memory quantities.
type ResourceValues struct {
	CPU    resource.Quantity `json:"cpu"`
	Memory resource.Quantity `json:"memory"`
}

// ApplicationResources defines required requests and limits.
// +kubebuilder:validation:XValidation:rule="quantity(self.requests.cpu).compareTo(quantity(self.limits.cpu)) <= 0",message="CPU request cannot exceed CPU limit"
// +kubebuilder:validation:XValidation:rule="quantity(self.requests.memory).compareTo(quantity(self.limits.memory)) <= 0",message="memory request cannot exceed memory limit"
type ApplicationResources struct {
	Requests ResourceValues `json:"requests"`
	Limits   ResourceValues `json:"limits"`
}

// CanaryStep describes one future canary traffic shift.
type CanaryStep struct {
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	Weight int32 `json:"weight"`

	// +kubebuilder:validation:XValidation:rule="duration(self) > duration('0s')",message="pause must be positive"
	Pause metav1.Duration `json:"pause"`
}

// ApplicationDeployment configures the requested rollout behavior.
// +kubebuilder:validation:XValidation:rule="self.strategy == 'rolling' ? !has(self.steps) || size(self.steps) == 0 : has(self.steps) && size(self.steps) > 0",message="rolling strategy cannot have canary steps and canary strategy requires steps"
// +kubebuilder:validation:XValidation:rule="!has(self.steps) || self.steps.map(s, s.weight).isSorted()",message="canary weights must be non-decreasing"
// +kubebuilder:validation:XValidation:rule="!has(self.steps) || self.steps.all(s, self.steps.filter(t, t.weight == s.weight).size() == 1)",message="canary weights must be unique"
type ApplicationDeployment struct {
	// +kubebuilder:default=rolling
	Strategy DeploymentStrategy `json:"strategy"`

	// +kubebuilder:validation:MaxItems=10
	// +optional
	Steps []CanaryStep `json:"steps,omitempty"`

	// +kubebuilder:default=true
	AutomaticRollback bool `json:"automaticRollback"`
}

// ReliabilityTargets records the application's service-level objectives.
type ReliabilityTargets struct {
	AvailabilityTarget AvailabilityPercentage `json:"availabilityTarget"`

	// +kubebuilder:validation:XValidation:rule="duration(self) > duration('0s')",message="maximum P95 latency must be positive"
	MaximumP95Latency metav1.Duration `json:"maximumP95Latency"`

	MaximumErrorRate Percentage `json:"maximumErrorRate"`
}

// ObservabilityOptions records future telemetry capabilities.
type ObservabilityOptions struct {
	// +kubebuilder:default=false
	Metrics bool `json:"metrics"`
	// +kubebuilder:default=false
	Logs bool `json:"logs"`
	// +kubebuilder:default=false
	Traces bool `json:"traces"`
}

// SecurityOptions controls workload hardening and future security capabilities.
type SecurityOptions struct {
	// +kubebuilder:default=false
	RequireSignedImage bool `json:"requireSignedImage"`
	// +kubebuilder:default=true
	RunAsNonRoot bool `json:"runAsNonRoot"`
	// +kubebuilder:default=false
	NetworkIsolation bool `json:"networkIsolation"`
}

// ApplicationSpec defines the desired state of Application.
// +kubebuilder:validation:XValidation:rule="self.deployment.strategy != 'canary' || self.observability.metrics",message="canary strategy requires observability.metrics=true"
type ApplicationSpec struct {
	// +kubebuilder:validation:MinLength=1
	Owner string `json:"owner"`

	Image ApplicationImage `json:"image"`

	// +kubebuilder:default={port: 8080, replicas: {min: 1, max: 3}}
	// +optional
	Runtime ApplicationRuntime `json:"runtime,omitempty"`

	Resources ApplicationResources `json:"resources"`

	// +kubebuilder:default={strategy: rolling, automaticRollback: true}
	// +optional
	Deployment ApplicationDeployment `json:"deployment,omitempty"`

	Reliability ReliabilityTargets `json:"reliability"`

	// +kubebuilder:default={metrics: false, logs: false, traces: false}
	// +optional
	Observability ObservabilityOptions `json:"observability,omitempty"`

	// +kubebuilder:default={requireSignedImage: false, runAsNonRoot: true, networkIsolation: false}
	// +optional
	Security SecurityOptions `json:"security,omitempty"`
}

// ApplicationStatus defines the observed state of Application.
type ApplicationStatus struct {
	// +optional
	Phase ApplicationPhase `json:"phase,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	ActiveVersion string `json:"activeVersion,omitempty"`

	// +optional
	CandidateVersion string `json:"candidateVersion,omitempty"`

	// ResolvedImageDigest is the canonical digest reported by every ready Pod
	// for the active version.
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	// +optional
	ResolvedImageDigest string `json:"resolvedImageDigest,omitempty"`

	// ResolvedGitRevision is the full Git object ID that produced the active
	// configuration. It is empty for Applications not delivered by GitOps.
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
// +kubebuilder:resource:scope=Namespaced,shortName=app
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="ActiveVersion",type=string,JSONPath=`.status.activeVersion`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Digest",type=string,JSONPath=`.status.resolvedImageDigest`,priority=1
// +kubebuilder:printcolumn:name="Revision",type=string,JSONPath=`.status.resolvedGitRevision`,priority=1
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) <= 63",message="Application names must contain at most 63 characters"

// Application is the Schema for the applications API.
type Application struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ApplicationSpec `json:"spec"`

	// +optional
	Status ApplicationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ApplicationList contains a list of Application.
type ApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Application `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Application{}, &ApplicationList{})
}
