//go:build envtest

package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

var _ = Describe("Application CRD", func() {
	const namespace = "application-schema"

	BeforeEach(func(ctx SpecContext) {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
		err := k8sClient.Create(ctx, ns)
		Expect(err == nil || apierrors.IsAlreadyExists(err)).To(BeTrue())
	})

	It("applies API defaults", func(ctx SpecContext) {
		manifest := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "platform.steadystate.dev/v1alpha1",
			"kind":       "Application",
			"metadata": map[string]any{
				"name":      "defaults",
				"namespace": namespace,
			},
			"spec": map[string]any{
				"owner": "platform-team",
				"image": map[string]any{
					"repository": "ghcr.io/saadabdullaah/steadystate-demo-app",
					"tag":        "v0.1.0",
				},
				"resources": map[string]any{
					"requests": map[string]any{"cpu": "50m", "memory": "32Mi"},
					"limits":   map[string]any{"cpu": "200m", "memory": "128Mi"},
				},
				"reliability": map[string]any{
					"availabilityTarget": "99.9%",
					"maximumP95Latency":  "250ms",
					"maximumErrorRate":   "1%",
				},
			},
		}}
		manifest.SetGroupVersionKind(schema.GroupVersionKind{
			Group: "platform.steadystate.dev", Version: "v1alpha1", Kind: "Application",
		})
		Expect(k8sClient.Create(ctx, manifest)).To(Succeed())

		application := &platformv1alpha1.Application{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "defaults", Namespace: namespace}, application)).To(Succeed())
		Expect(application.Spec.Runtime.Port).To(Equal(int32(8080)))
		Expect(application.Spec.Runtime.Replicas.Min).To(Equal(int32(1)))
		Expect(application.Spec.Runtime.Replicas.Max).To(Equal(int32(3)))
		Expect(application.Spec.Deployment.Strategy).To(Equal(platformv1alpha1.DeploymentStrategyRolling))
		Expect(application.Spec.Deployment.AutomaticRollback).To(BeTrue())
		Expect(application.Spec.Security.RunAsNonRoot).To(BeTrue())
	})

	DescribeTable("rejects invalid specifications",
		func(mutate func(*platformv1alpha1.Application)) {
			application := validApplication("invalid", namespace)
			mutate(application)
			Expect(k8sClient.Create(context.Background(), application)).NotTo(Succeed())
		},
		Entry("digest repository", func(app *platformv1alpha1.Application) {
			app.Spec.Image.Repository = "example.test/demo@sha256:deadbeef"
		}),
		Entry("latest tag regardless of case", func(app *platformv1alpha1.Application) {
			app.Spec.Image.Tag = "LaTeSt"
		}),
		Entry("minimum replicas above maximum", func(app *platformv1alpha1.Application) {
			app.Spec.Runtime.Replicas.Min = 4
			app.Spec.Runtime.Replicas.Max = 3
		}),
		Entry("CPU request above limit", func(app *platformv1alpha1.Application) {
			app.Spec.Resources.Requests.CPU = resource.MustParse("2")
		}),
		Entry("zero availability", func(app *platformv1alpha1.Application) {
			app.Spec.Reliability.AvailabilityTarget = "0.00%"
		}),
		Entry("non-positive latency", func(app *platformv1alpha1.Application) {
			app.Spec.Reliability.MaximumP95Latency = metav1.Duration{Duration: 0}
		}),
		Entry("rolling strategy with canary steps", func(app *platformv1alpha1.Application) {
			app.Spec.Deployment.Steps = []platformv1alpha1.CanaryStep{{Weight: 10, Pause: metav1.Duration{Duration: time.Second}}}
		}),
		Entry("duplicate canary weights", func(app *platformv1alpha1.Application) {
			app.Spec.Deployment.Strategy = platformv1alpha1.DeploymentStrategyCanary
			app.Spec.Observability.Metrics = true
			app.Spec.Deployment.Steps = []platformv1alpha1.CanaryStep{
				{Weight: 10, Pause: metav1.Duration{Duration: time.Second}},
				{Weight: 10, Pause: metav1.Duration{Duration: time.Second}},
			}
		}),
		Entry("decreasing canary weights", func(app *platformv1alpha1.Application) {
			app.Spec.Deployment.Strategy = platformv1alpha1.DeploymentStrategyCanary
			app.Spec.Observability.Metrics = true
			app.Spec.Deployment.Steps = []platformv1alpha1.CanaryStep{
				{Weight: 50, Pause: metav1.Duration{Duration: time.Second}},
				{Weight: 25, Pause: metav1.Duration{Duration: time.Second}},
			}
		}),
		Entry("canary without metrics", func(app *platformv1alpha1.Application) {
			app.Spec.Deployment.Strategy = platformv1alpha1.DeploymentStrategyCanary
			app.Spec.Deployment.Steps = []platformv1alpha1.CanaryStep{{Weight: 10, Pause: metav1.Duration{Duration: time.Second}}}
		}),
	)
})

func validApplication(name, namespace string) *platformv1alpha1.Application {
	return &platformv1alpha1.Application{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: platformv1alpha1.ApplicationSpec{
			Owner: "platform-team",
			Image: platformv1alpha1.ApplicationImage{
				Repository: "ghcr.io/saadabdullaah/steadystate-demo-app",
				Tag:        "v0.1.0",
			},
			Runtime: platformv1alpha1.ApplicationRuntime{
				Port: 8080,
				Replicas: platformv1alpha1.ReplicaBounds{
					Min: 1,
					Max: 3,
				},
			},
			Resources: platformv1alpha1.ApplicationResources{
				Requests: platformv1alpha1.ResourceValues{CPU: resource.MustParse("50m"), Memory: resource.MustParse("32Mi")},
				Limits:   platformv1alpha1.ResourceValues{CPU: resource.MustParse("200m"), Memory: resource.MustParse("128Mi")},
			},
			Deployment: platformv1alpha1.ApplicationDeployment{
				Strategy:          platformv1alpha1.DeploymentStrategyRolling,
				AutomaticRollback: true,
			},
			Reliability: platformv1alpha1.ReliabilityTargets{
				AvailabilityTarget: "99.9%",
				MaximumP95Latency:  metav1.Duration{Duration: 250 * time.Millisecond},
				MaximumErrorRate:   "1%",
			},
			Security: platformv1alpha1.SecurityOptions{RunAsNonRoot: true},
		},
	}
}
