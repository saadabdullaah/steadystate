package resources

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

// Service builds the stable ClusterIP Service owned by an Application.
func Service(application *platformv1alpha1.Application) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: application.Name, Namespace: application.Namespace, Labels: Labels(application)},
		Spec: corev1.ServiceSpec{
			Selector: SelectorLabels(application),
			Ports:    []corev1.ServicePort{{Name: "http", Protocol: corev1.ProtocolTCP, Port: 80, TargetPort: intstr.FromInt32(application.Spec.Runtime.Port)}},
		},
	}
}
