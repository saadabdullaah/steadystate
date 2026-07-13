// Package resources contains deterministic builders for SteadyState-managed objects.
package resources

import (
	"fmt"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

const (
	ManagedBy             = "steadystate"
	TeamLabelKey          = "steadystate.dev/team"
	TeamUIDAnnotationKey  = "steadystate.dev/team-uid"
	GatewayAccessLabelKey = "steadystate.dev/gateway-access"
	TeamNamespacePrefix   = "team-"
	TeamQuotaName         = "steadystate-quota"
	TeamLimitRangeName    = "steadystate-defaults"
	TeamOwnerName         = "steadystate-team-owner"
	DefaultDenyPolicyName = "steadystate-default-deny"
	AllowDNSPolicyName    = "steadystate-allow-dns"
	AllowEnvoyPolicyName  = "steadystate-allow-envoy-gateway"
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

// TeamNamespaceName returns the deterministic namespace managed for a Team.
func TeamNamespaceName(team *platformv1alpha1.Team) string {
	return TeamNamespacePrefix + team.Name
}

// TeamLabels returns the stable identity labels shared by Team-generated objects.
func TeamLabels(team *platformv1alpha1.Team) map[string]string {
	return map[string]string{
		"app.kubernetes.io/instance":   team.Name,
		"app.kubernetes.io/managed-by": ManagedBy,
		"app.kubernetes.io/name":       "steadystate-team",
		"app.kubernetes.io/part-of":    "steadystate",
		TeamLabelKey:                   team.Name,
	}
}

// TeamAnnotations identifies the exact Team incarnation that owns a reserved resource.
func TeamAnnotations(team *platformv1alpha1.Team) map[string]string {
	return map[string]string{TeamUIDAnnotationKey: string(team.UID)}
}
