package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TeamOwner is a Kubernetes username granted access to the managed Team namespace.
// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=253
// +kubebuilder:validation:Pattern=`^[^[:space:]]+$`
type TeamOwner string

// RepositoryPattern is a case-sensitive, path-style glob matched against an image repository.
// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=512
// +kubebuilder:validation:Pattern=`^[^@[:space:]]+$`
type RepositoryPattern string

// TeamQuota defines aggregate CPU and memory ceilings for requests and limits.
// +kubebuilder:validation:XValidation:rule="quantity(self.cpu).compareTo(quantity('500m')) >= 0",message="CPU quota must be at least 500m"
// +kubebuilder:validation:XValidation:rule="quantity(self.memory).compareTo(quantity('512Mi')) >= 0",message="memory quota must be at least 512Mi"
type TeamQuota struct {
	CPU    resource.Quantity `json:"cpu"`
	Memory resource.Quantity `json:"memory"`
}

// TeamSpec defines the desired tenant boundary.
type TeamSpec struct {
	// Owners are Kubernetes usernames bound to the generated Team role.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=32
	// +listType=set
	Owners []TeamOwner `json:"owners"`

	Quota TeamQuota `json:"quota"`

	// AllowedRepositories contains anchored, case-sensitive Go path.Match globs.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=32
	// +listType=set
	AllowedRepositories []RepositoryPattern `json:"allowedRepositories"`
}

// TeamStatus describes the observed state of a Team's generated boundary.
type TeamStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Namespace is the deterministic managed namespace name.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=team
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.status.namespace`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) <= 58",message="Team names must contain at most 58 characters"
// +kubebuilder:validation:XValidation:rule="self.metadata.name.matches('^[a-z0-9]([-a-z0-9]*[a-z0-9])?$')",message="Team names must be DNS labels"

// Team is the Schema for the cluster-scoped teams API.
type Team struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TeamSpec `json:"spec"`

	// +optional
	Status TeamStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// TeamList contains a list of Team.
type TeamList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Team `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Team{}, &TeamList{})
}
