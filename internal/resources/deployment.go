package resources

import (
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

// Deployment builds the hardened rolling Deployment owned by an Application.
func Deployment(application *platformv1alpha1.Application) *appsv1.Deployment {
	labels := Labels(application)
	selectors := SelectorLabels(application)
	templateLabels := Labels(application)
	templateLabels[VersionLabelKey] = application.Spec.Image.Tag
	templateLabels[LogsLabelKey] = strconv.FormatBool(application.Spec.Observability.Logs)
	templateLabels[TracesLabelKey] = strconv.FormatBool(application.Spec.Observability.Traces)
	templateLabels[WorkloadKindLabelKey] = "application"
	templateLabels[RequireSignedImageLabelKey] = strconv.FormatBool(application.Spec.Security.RequireSignedImage)
	templateLabels[NetworkIsolationLabelKey] = strconv.FormatBool(application.Spec.Security.NetworkIsolation)
	environment := []corev1.EnvVar{}
	if application.Spec.Observability.Traces {
		environment = append(environment,
			corev1.EnvVar{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: "otel-collector.monitoring.svc.cluster.local:4317"},
			corev1.EnvVar{Name: "OTEL_SERVICE_NAME", Value: application.Name},
		)
	}
	replicas := application.Spec.Runtime.Replicas.Min
	if application.Spec.Deployment.Strategy == platformv1alpha1.DeploymentStrategyCanary {
		replicas = 0
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: application.Name, Namespace: application.Namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(replicas),
			Selector: &metav1.LabelSelector{MatchLabels: selectors},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxUnavailable: ptr.To(intstr.FromInt32(0)),
					MaxSurge:       ptr.To(intstr.FromInt32(1)),
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: templateLabels},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken:  ptr.To(false),
					TerminationGracePeriodSeconds: ptr.To(int64(30)),
					SecurityContext: &corev1.PodSecurityContext{
						SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
					Containers: []corev1.Container{{
						Name:  "application",
						Image: application.Spec.Image.Repository + ":" + application.Spec.Image.Tag,
						Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: application.Spec.Runtime.Port, Protocol: corev1.ProtocolTCP}},
						EnvFrom: []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: ConfigMapName(application)},
						}}},
						Env:            environment,
						LivenessProbe:  httpProbe("/healthz", application.Spec.Runtime.Port),
						ReadinessProbe: httpProbe("/readyz", application.Spec.Runtime.Port),
						Lifecycle: &corev1.Lifecycle{PreStop: &corev1.LifecycleHandler{
							Sleep: &corev1.SleepAction{Seconds: 15},
						}},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{corev1.ResourceCPU: application.Spec.Resources.Requests.CPU.DeepCopy(), corev1.ResourceMemory: application.Spec.Resources.Requests.Memory.DeepCopy()},
							Limits:   corev1.ResourceList{corev1.ResourceCPU: application.Spec.Resources.Limits.CPU.DeepCopy(), corev1.ResourceMemory: application.Spec.Resources.Limits.Memory.DeepCopy()},
						},
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: ptr.To(false),
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
							ReadOnlyRootFilesystem:   ptr.To(true),
							RunAsNonRoot:             ptr.To(application.Spec.Security.RunAsNonRoot),
						},
					}},
				},
			},
		},
	}
}

func httpProbe(path string, port int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler:     corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: path, Port: intstr.FromInt32(port), Scheme: corev1.URISchemeHTTP}},
		TimeoutSeconds:   1,
		PeriodSeconds:    10,
		SuccessThreshold: 1,
		FailureThreshold: 3,
	}
}
