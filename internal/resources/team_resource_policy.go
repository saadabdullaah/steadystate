package resources

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

// TeamResourceQuota builds aggregate request and limit ceilings for a Team.
func TeamResourceQuota(team *platformv1alpha1.Team) *corev1.ResourceQuota {
	return &corev1.ResourceQuota{
		ObjectMeta: teamObjectMeta(team, TeamQuotaName),
		Spec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
			corev1.ResourceRequestsCPU:    team.Spec.Quota.CPU.DeepCopy(),
			corev1.ResourceRequestsMemory: team.Spec.Quota.Memory.DeepCopy(),
			corev1.ResourceLimitsCPU:      team.Spec.Quota.CPU.DeepCopy(),
			corev1.ResourceLimitsMemory:   team.Spec.Quota.Memory.DeepCopy(),
		}},
	}
}

// TeamLimitRange supplies conservative defaults while leaving aggregate enforcement to ResourceQuota.
func TeamLimitRange(team *platformv1alpha1.Team) *corev1.LimitRange {
	return &corev1.LimitRange{
		ObjectMeta: teamObjectMeta(team, TeamLimitRangeName),
		Spec: corev1.LimitRangeSpec{Limits: []corev1.LimitRangeItem{{
			Type: corev1.LimitTypeContainer,
			Min: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("16Mi"),
			},
			DefaultRequest: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			},
			Default: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		}}},
	}
}

func teamObjectMeta(team *platformv1alpha1.Team, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:        name,
		Namespace:   TeamNamespaceName(team),
		Labels:      TeamLabels(team),
		Annotations: TeamAnnotations(team),
	}
}
