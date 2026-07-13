package resources

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

// ConfigMap builds the Application runtime configuration.
func ConfigMap(application *platformv1alpha1.Application) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ConfigMapName(application),
			Namespace: application.Namespace,
			Labels:    Labels(application),
		},
		Data: map[string]string{
			"PORT":                      strconv.FormatInt(int64(application.Spec.Runtime.Port), 10),
			"STEADYSTATE_APP_NAME":      application.Name,
			"STEADYSTATE_APP_NAMESPACE": application.Namespace,
			"STEADYSTATE_APP_OWNER":     application.Spec.Owner,
			"STEADYSTATE_APP_VERSION":   application.Spec.Image.Tag,
		},
	}
}
