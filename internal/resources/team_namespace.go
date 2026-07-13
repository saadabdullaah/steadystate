package resources

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

// TeamNamespace builds the cluster-scoped namespace boundary for a Team.
func TeamNamespace(team *platformv1alpha1.Team) *corev1.Namespace {
	labels := TeamLabels(team)
	labels[GatewayAccessLabelKey] = "true"
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:        TeamNamespaceName(team),
		Labels:      labels,
		Annotations: TeamAnnotations(team),
	}}
}
