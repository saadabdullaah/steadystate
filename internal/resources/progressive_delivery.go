package resources

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	rolloutsv1alpha1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

const (
	serviceMonitorAPIVersion = "monitoring.coreos.com/v1"
	prometheusRuleAPIVersion = "monitoring.coreos.com/v1"
)

// Rollout builds the typed Argo Rollout for a canary Application. Runtime
// reconciliation remains gated until the controller checkpoint.
func Rollout(application *platformv1alpha1.Application) *rolloutsv1alpha1.Rollout {
	pluginConfig, err := json.Marshal(map[string]string{
		"httpRoute": CanaryHTTPRouteName(application),
		"namespace": application.Namespace,
	})
	if err != nil {
		panic(fmt.Sprintf("marshal fixed Gateway plugin configuration: %v", err))
	}
	steps := make([]rolloutsv1alpha1.CanaryStep, 0, len(application.Spec.Deployment.Steps)*3)
	latest := rolloutsv1alpha1.Latest
	for _, desired := range application.Spec.Deployment.Steps {
		weight := desired.Weight
		pause := intstr.FromString(desired.Pause.Duration.String())
		steps = append(steps,
			rolloutsv1alpha1.CanaryStep{SetWeight: &weight},
			rolloutsv1alpha1.CanaryStep{Pause: &rolloutsv1alpha1.RolloutPause{Duration: &pause}},
			rolloutsv1alpha1.CanaryStep{Analysis: &rolloutsv1alpha1.RolloutAnalysis{
				Templates: []rolloutsv1alpha1.AnalysisTemplateRef{{TemplateName: AnalysisTemplateName(application)}},
				Args: []rolloutsv1alpha1.AnalysisRunArgument{
					{Name: "candidate-version", Value: application.Spec.Image.Tag},
					{Name: "candidate-hash", ValueFrom: &rolloutsv1alpha1.ArgumentValueFrom{PodTemplateHashValue: &latest}},
				},
			}},
		)
	}
	maxUnavailable := intstr.FromInt32(0)
	maxSurge := intstr.FromInt32(1)
	return &rolloutsv1alpha1.Rollout{
		TypeMeta: metav1.TypeMeta{APIVersion: "argoproj.io/v1alpha1", Kind: "Rollout"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      application.Name,
			Namespace: application.Namespace,
			Labels:    Labels(application),
		},
		Spec: rolloutsv1alpha1.RolloutSpec{
			Replicas: ptr.To(application.Spec.Runtime.Replicas.Min),
			WorkloadRef: &rolloutsv1alpha1.ObjectRef{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       application.Name,
				ScaleDown:  rolloutsv1alpha1.ScaleDownOnSuccess,
			},
			RevisionHistoryLimit:    ptr.To(int32(2)),
			ProgressDeadlineSeconds: ptr.To(int32(600)),
			ProgressDeadlineAbort:   application.Spec.Deployment.AutomaticRollback,
			Analysis: &rolloutsv1alpha1.AnalysisRunStrategy{
				SuccessfulRunHistoryLimit:   ptr.To(int32(2)),
				UnsuccessfulRunHistoryLimit: ptr.To(int32(2)),
			},
			Strategy: rolloutsv1alpha1.RolloutStrategy{Canary: &rolloutsv1alpha1.CanaryStrategy{
				StableService:              StableServiceName(application),
				CanaryService:              CanaryServiceName(application),
				Steps:                      steps,
				MaxUnavailable:             &maxUnavailable,
				MaxSurge:                   &maxSurge,
				ScaleDownDelaySeconds:      ptr.To(int32(30)),
				AbortScaleDownDelaySeconds: ptr.To(int32(30)),
				MinPodsPerReplicaSet:       ptr.To(int32(1)),
				TrafficRouting: &rolloutsv1alpha1.RolloutTrafficRouting{Plugins: map[string]json.RawMessage{
					GatewayPluginName: pluginConfig,
				}},
			}},
		},
	}
}

// RolloutObject converts the typed Rollout contract into the object sent to
// Kubernetes. RolloutSpec.Template is a non-pointer Go struct, so the typed
// JSON encoder emits an empty template even when workloadRef is selected. The
// upstream API requires that field to be absent for workloadRef Rollouts.
func RolloutObject(application *platformv1alpha1.Application) *unstructured.Unstructured {
	object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(Rollout(application))
	if err != nil {
		panic(fmt.Sprintf("convert typed Rollout to unstructured object: %v", err))
	}
	unstructured.RemoveNestedField(object, "spec", "template")
	return &unstructured.Unstructured{Object: object}
}

// StableService builds the Service whose selector is subsequently owned by Argo Rollouts.
func StableService(application *platformv1alpha1.Application) *corev1.Service {
	return rolloutService(application, StableServiceName(application), "stable")
}

// CanaryService builds the Service whose selector is subsequently owned by Argo Rollouts.
func CanaryService(application *platformv1alpha1.Application) *corev1.Service {
	return rolloutService(application, CanaryServiceName(application), "canary")
}

func rolloutService(application *platformv1alpha1.Application, name, role string) *corev1.Service {
	labels := Labels(application)
	labels[ServiceRoleLabelKey] = role
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: application.Namespace, Labels: labels},
		Spec: corev1.ServiceSpec{
			Selector: SelectorLabels(application),
			Ports: []corev1.ServicePort{{
				Name: "http", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.FromInt32(application.Spec.Runtime.Port),
			}},
		},
	}
}

// CanaryHTTPRouteName returns the route managed by the Gateway traffic-router plugin.
func CanaryHTTPRouteName(application *platformv1alpha1.Application) string {
	return application.Name
}

// CanaryHTTPRoute builds the two-backend route in its stable, idle state.
func CanaryHTTPRoute(application *platformv1alpha1.Application) *gatewayv1.HTTPRoute {
	route := HTTPRoute(application)
	stableWeight := int32(100)
	canaryWeight := int32(0)
	port := gatewayv1.PortNumber(80)
	group := gatewayv1.Group("")
	kind := gatewayv1.Kind("Service")
	route.Spec.Rules[0].BackendRefs = []gatewayv1.HTTPBackendRef{
		{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{
			Group: &group, Kind: &kind, Name: gatewayv1.ObjectName(StableServiceName(application)), Port: &port,
		}, Weight: &stableWeight}},
		{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{
			Group: &group, Kind: &kind, Name: gatewayv1.ObjectName(CanaryServiceName(application)), Port: &port,
		}, Weight: &canaryWeight}},
	}
	return route
}

// AnalysisTemplate builds the Prometheus metric gates used after every canary pause.
func AnalysisTemplate(application *platformv1alpha1.Application) *rolloutsv1alpha1.AnalysisTemplate {
	errorThreshold := 1 - percentageFraction(application.Spec.Reliability.MaximumErrorRate)
	latencyThreshold := application.Spec.Reliability.MaximumP95Latency.Seconds()
	return &rolloutsv1alpha1.AnalysisTemplate{
		TypeMeta: metav1.TypeMeta{APIVersion: "argoproj.io/v1alpha1", Kind: "AnalysisTemplate"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      AnalysisTemplateName(application),
			Namespace: application.Namespace,
			Labels:    Labels(application),
		},
		Spec: rolloutsv1alpha1.AnalysisTemplateSpec{
			Args: []rolloutsv1alpha1.Argument{{Name: "candidate-version"}, {Name: "candidate-hash"}},
			Metrics: []rolloutsv1alpha1.Metric{
				analysisMetric(application, "candidate-success-rate", successRateQuery(application), fmt.Sprintf("result[0] >= %s", formatNumber(errorThreshold))),
				analysisMetric(application, "candidate-p95-latency", p95Query(application), fmt.Sprintf("result[0] <= %s", formatNumber(latencyThreshold))),
				analysisMetric(application, "candidate-container-restarts", restartQuery(application), "result[0] <= 0"),
			},
		},
	}
}

func analysisMetric(application *platformv1alpha1.Application, name, query, successCondition string) rolloutsv1alpha1.Metric {
	count := intstr.FromInt32(3)
	failureLimit := intstr.FromInt32(1)
	// Rollouts marks Error when consecutive errors exceed this limit, so one
	// tolerated error makes the second consecutive provider error fail safe.
	errorLimit := intstr.FromInt32(1)
	successLimit := intstr.FromInt32(2)
	metric := rolloutsv1alpha1.Metric{
		Name:                    name,
		InitialDelay:            rolloutsv1alpha1.DurationString("30s"),
		Interval:                rolloutsv1alpha1.DurationString("30s"),
		Count:                   &count,
		SuccessCondition:        successCondition,
		FailureCondition:        "!(" + successCondition + ")",
		FailureLimit:            &failureLimit,
		ConsecutiveErrorLimit:   &errorLimit,
		ConsecutiveSuccessLimit: &successLimit,
		Provider: rolloutsv1alpha1.MetricProvider{Prometheus: &rolloutsv1alpha1.PrometheusMetric{
			Address: PrometheusAddress,
			Query:   query,
		}},
	}
	if !application.Spec.Deployment.AutomaticRollback {
		inconclusiveLimit := intstr.FromInt32(1)
		metric.FailureCondition = "false"
		metric.InconclusiveLimit = &inconclusiveLimit
	}
	return metric
}

func successRateQuery(application *platformv1alpha1.Application) string {
	selector := fmt.Sprintf(`application=%q,namespace=%q,version="{{args.candidate-version}}"`, application.Name, application.Namespace)
	return fmt.Sprintf(`(sum(rate(http_requests_total{%s,status=~"2.."}[1m])) / clamp_min(sum(rate(http_requests_total{%s}[1m])), 0.000001)) or vector(-1)`, selector, selector)
}

func p95Query(application *platformv1alpha1.Application) string {
	selector := fmt.Sprintf(`application=%q,namespace=%q,version="{{args.candidate-version}}"`, application.Name, application.Namespace)
	return fmt.Sprintf(`histogram_quantile(0.95, sum by (le) (rate(http_request_duration_seconds_bucket{%s}[1m]))) or vector(1000000000)`, selector)
}

func restartQuery(application *platformv1alpha1.Application) string {
	return fmt.Sprintf(`sum(clamp_min(increase(kube_pod_container_status_restarts_total{namespace=%q,container="application"}[1m]), 0) * on(namespace,pod) group_left() kube_pod_labels{namespace=%q,label_app_kubernetes_io_instance=%q,label_rollouts_pod_template_hash="{{args.candidate-hash}}"}) or vector(0)`, application.Namespace, application.Namespace, application.Name)
}

// ServiceMonitor builds the schema-tested unstructured monitoring target.
func ServiceMonitor(application *platformv1alpha1.Application) *unstructured.Unstructured {
	labels := Labels(application)
	labels[ServiceRoleLabelKey] = "base"
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": serviceMonitorAPIVersion,
		"kind":       "ServiceMonitor",
		"metadata": map[string]any{
			"name": ServiceMonitorName(application), "namespace": application.Namespace, "labels": stringMapAny(Labels(application)),
		},
		"spec": map[string]any{
			"selector":  map[string]any{"matchLabels": stringMapAny(labels)},
			"endpoints": []any{map[string]any{"port": "http", "path": "/metrics", "interval": "15s"}},
		},
	}}
}

// PrometheusRule builds basic candidate alerts from the same reliability thresholds as analysis.
func PrometheusRule(application *platformv1alpha1.Application) *unstructured.Unstructured {
	errorLimit := percentageFraction(application.Spec.Reliability.MaximumErrorRate)
	latencyLimit := application.Spec.Reliability.MaximumP95Latency.Seconds()
	labels := map[string]any{
		"application": application.Name, "namespace": application.Namespace, "version": application.Spec.Image.Tag, "severity": "warning",
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": prometheusRuleAPIVersion,
		"kind":       "PrometheusRule",
		"metadata": map[string]any{
			"name": PrometheusRuleName(application), "namespace": application.Namespace, "labels": stringMapAny(Labels(application)),
		},
		"spec": map[string]any{"groups": []any{map[string]any{
			"name": fmt.Sprintf("steadystate.%s.%s.candidate", application.Namespace, application.Name),
			"rules": []any{
				alertRule("SteadyStateCandidateHighErrorRate", candidateErrorRateAlertQuery(application, errorLimit), labels, "Candidate error rate exceeds the Application reliability target."),
				alertRule("SteadyStateCandidateHighP95Latency", candidateP95AlertQuery(application, latencyLimit), labels, "Candidate P95 latency exceeds the Application reliability target."),
				alertRule("SteadyStateCandidateRestarts", candidateRestartAlertQuery(application), labels, "Candidate containers restarted during the analysis window."),
			},
		}}},
	}}
}

func alertRule(name, expression string, labels map[string]any, summary string) map[string]any {
	return map[string]any{
		"alert": name, "expr": expression, "for": "30s", "labels": cloneAnyMap(labels), "annotations": map[string]any{"summary": summary},
	}
}

func candidateErrorRateAlertQuery(application *platformv1alpha1.Application, threshold float64) string {
	selector := fmt.Sprintf(`application=%q,namespace=%q,version=%q`, application.Name, application.Namespace, application.Spec.Image.Tag)
	return fmt.Sprintf(`(sum(rate(http_requests_total{%s,status!~"2.."}[1m])) / clamp_min(sum(rate(http_requests_total{%s}[1m])), 0.000001)) > %s`, selector, selector, formatNumber(threshold))
}

func candidateP95AlertQuery(application *platformv1alpha1.Application, threshold float64) string {
	selector := fmt.Sprintf(`application=%q,namespace=%q,version=%q`, application.Name, application.Namespace, application.Spec.Image.Tag)
	return fmt.Sprintf(`histogram_quantile(0.95, sum by (le) (rate(http_request_duration_seconds_bucket{%s}[1m]))) > %s`, selector, formatNumber(threshold))
}

func candidateRestartAlertQuery(application *platformv1alpha1.Application) string {
	return fmt.Sprintf(`sum(clamp_min(increase(kube_pod_container_status_restarts_total{namespace=%q,container="application"}[1m]), 0) * on(namespace,pod) group_left() kube_pod_labels{namespace=%q,label_app_kubernetes_io_instance=%q,label_steadystate_dev_version=%q}) > 0`, application.Namespace, application.Namespace, application.Name, application.Spec.Image.Tag)
}

func percentageFraction(value platformv1alpha1.Percentage) float64 {
	parsed, _ := strconv.ParseFloat(strings.TrimSuffix(string(value), "%"), 64)
	return parsed / 100
}

func formatNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func stringMapAny(values map[string]string) map[string]any {
	result := make(map[string]any, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func cloneAnyMap(values map[string]any) map[string]any {
	result := make(map[string]any, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
