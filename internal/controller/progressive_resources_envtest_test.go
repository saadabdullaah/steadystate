//go:build envtest

package controller

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
	"github.com/saadabdullaah/steadystate/internal/resources"
)

var _ = Describe("Progressive-delivery CRD schemas", func() {
	const namespace = "progressive-schema"

	BeforeEach(func(ctx SpecContext) {
		err := k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})
		Expect(err == nil || apierrors.IsAlreadyExists(err)).To(BeTrue())
	})

	It("accepts every generated progressive-delivery resource", func(ctx SpecContext) {
		application := validApplication("progressive", namespace)
		application.Spec.Image.Tag = "v0.4.0"
		application.Spec.Deployment = platformv1alpha1.ApplicationDeployment{
			Strategy:          platformv1alpha1.DeploymentStrategyCanary,
			AutomaticRollback: true,
			Steps: []platformv1alpha1.CanaryStep{
				{Weight: 10, Pause: metav1.Duration{Duration: 30 * time.Second}},
				{Weight: 100, Pause: metav1.Duration{Duration: 30 * time.Second}},
			},
		}
		application.Spec.Observability.Metrics = true

		objects := []client.Object{
			resources.RolloutObject(application),
			resources.AnalysisTemplate(application),
			resources.ServiceMonitor(application),
			resources.PrometheusRule(application),
		}
		for _, object := range objects {
			Expect(k8sClient.Create(ctx, object)).To(Succeed(), "generated %s/%s was rejected", object.GetObjectKind().GroupVersionKind().Kind, object.GetName())
		}
	})

	It("rejects a ServiceMonitor missing its required endpoint list", func(ctx SpecContext) {
		invalid := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "monitoring.coreos.com/v1",
			"kind":       "ServiceMonitor",
			"metadata": map[string]any{
				"name": "invalid-monitor", "namespace": namespace,
			},
			"spec": map[string]any{"selector": map[string]any{"matchLabels": map[string]any{"app": "demo"}}},
		}}
		Expect(k8sClient.Create(ctx, invalid)).NotTo(Succeed())
	})
})
