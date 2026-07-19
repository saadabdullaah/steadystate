// Package resources contains deterministic builders for SteadyState-managed objects.
package resources

import (
	"crypto/sha256"
	"fmt"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

const (
	ManagedBy                 = "steadystate"
	TeamLabelKey              = "steadystate.dev/team"
	TeamUIDAnnotationKey      = "steadystate.dev/team-uid"
	GatewayAccessLabelKey     = "steadystate.dev/gateway-access"
	TeamNamespacePrefix       = "team-"
	TeamQuotaName             = "steadystate-quota"
	TeamLimitRangeName        = "steadystate-defaults"
	TeamOwnerName             = "steadystate-team-owner"
	DefaultDenyPolicyName     = "steadystate-default-deny"
	AllowDNSPolicyName        = "steadystate-allow-dns"
	AllowEnvoyPolicyName      = "steadystate-allow-envoy-gateway"
	AllowMonitoringPolicyName = "steadystate-allow-monitoring"
	VersionLabelKey           = "steadystate.dev/version"
	LogsLabelKey              = "steadystate.dev/logs"
	TracesLabelKey            = "steadystate.dev/traces"
	ServiceRoleLabelKey       = "steadystate.dev/service-role"
	GatewayPluginName         = "argoproj-labs/gatewayAPI"
	PrometheusAddress         = "http://monitoring-kube-prometheus-prometheus.monitoring.svc:9090"
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
	return suffixedName(application.Name, "-config")
}

// StableServiceName returns the Rollouts-managed stable Service name.
func StableServiceName(application *platformv1alpha1.Application) string {
	return suffixedName(application.Name, "-stable")
}

// CanaryServiceName returns the Rollouts-managed canary Service name.
func CanaryServiceName(application *platformv1alpha1.Application) string {
	return suffixedName(application.Name, "-canary")
}

// AnalysisTemplateName returns the per-Application AnalysisTemplate name.
func AnalysisTemplateName(application *platformv1alpha1.Application) string {
	return suffixedName(application.Name, "-analysis")
}

// ServiceMonitorName returns the per-Application ServiceMonitor name.
func ServiceMonitorName(application *platformv1alpha1.Application) string {
	return suffixedName(application.Name, "-monitor")
}

// PrometheusRuleName returns the per-Application PrometheusRule name.
func PrometheusRuleName(application *platformv1alpha1.Application) string {
	return suffixedName(application.Name, "-alerts")
}

// OTelEgressPolicyName returns the trace-export NetworkPolicy name.
func OTelEgressPolicyName(application *platformv1alpha1.Application) string {
	return suffixedName(application.Name, "-otel-egress")
}

func suffixedName(name, suffix string) string {
	candidate := name + suffix
	if len(candidate) <= 63 {
		return candidate
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(candidate)))[:8]
	prefixLength := 63 - len(suffix) - len(digest) - 1
	return name[:prefixLength] + "-" + digest + suffix
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
