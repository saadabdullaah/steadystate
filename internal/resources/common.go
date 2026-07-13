// Package resources contains deterministic builders for Application-owned objects.
package resources

import (
	"fmt"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

const (
	ManagedBy = "steadystate"
)

// Labels returns the stable identity labels shared by every generated object.
func Labels(application *platformv1alpha1.Application) map[string]string {
	return map[string]string{
		"app.kubernetes.io/instance":   application.Name,
		"app.kubernetes.io/managed-by": ManagedBy,
		"app.kubernetes.io/name":       application.Name,
		"app.kubernetes.io/part-of":    "steadystate",
	}
}

// SelectorLabels returns labels safe to use in immutable workload selectors.
func SelectorLabels(application *platformv1alpha1.Application) map[string]string {
	return map[string]string{
		"app.kubernetes.io/instance":   application.Name,
		"app.kubernetes.io/managed-by": ManagedBy,
	}
}

// Hostname returns the stable Gateway API hostname for an Application.
func Hostname(application *platformv1alpha1.Application) string {
	return fmt.Sprintf("%s.%s.steadystate.localtest.me", application.Name, application.Namespace)
}

// ConfigMapName returns the generated ConfigMap name.
func ConfigMapName(application *platformv1alpha1.Application) string {
	return application.Name + "-config"
}
