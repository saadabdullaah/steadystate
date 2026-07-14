//go:build envtest

package controller

import (
	"context"
	"errors"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
	"github.com/saadabdullaah/steadystate/internal/resources"
)

var _ = Describe("Application reconciler", Ordered, func() {
	const (
		teamName  = "application-reconciler"
		namespace = "team-application-reconciler"
	)

	BeforeAll(func(ctx SpecContext) {
		team := validSchemaTeam(teamName)
		err := k8sClient.Create(ctx, team)
		Expect(err == nil || apierrors.IsAlreadyExists(err)).To(BeTrue())
		reconcileTeam(ctx, k8sClient, teamName)
	})

	It("creates and updates all owned resources", func(ctx SpecContext) {
		app := validApplication("owned", namespace)
		Expect(k8sClient.Create(ctx, app)).To(Succeed())
		reconcile(ctx, k8sClient, app)

		deployment := &appsv1.Deployment{}
		service := &corev1.Service{}
		configMap := &corev1.ConfigMap{}
		route := &gatewayv1.HTTPRoute{}
		key := types.NamespacedName{Name: app.Name, Namespace: namespace}
		Expect(k8sClient.Get(ctx, key, deployment)).To(Succeed())
		Expect(k8sClient.Get(ctx, key, service)).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: app.Name + "-config", Namespace: namespace}, configMap)).To(Succeed())
		Expect(k8sClient.Get(ctx, key, route)).To(Succeed())
		for _, object := range []client.Object{deployment, service, configMap, route} {
			Expect(metav1.IsControlledBy(object, app)).To(BeTrue(), object.GetName())
		}

		Expect(k8sClient.Get(ctx, key, app)).To(Succeed())
		app.Spec.Image.Tag = "v0.1.1"
		app.Spec.Runtime.Port = 9090
		app.Spec.Runtime.Replicas.Min = 2
		app.Spec.Resources.Limits.Memory.Set(256 * 1024 * 1024)
		app.Spec.Owner = "new-owner"
		Expect(k8sClient.Update(ctx, app)).To(Succeed())
		reconcile(ctx, k8sClient, app)

		Expect(k8sClient.Get(ctx, key, deployment)).To(Succeed())
		Expect(*deployment.Spec.Replicas).To(Equal(int32(2)))
		Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(HaveSuffix(":v0.1.1"))
		Expect(deployment.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort).To(Equal(int32(9090)))
		Expect(k8sClient.Get(ctx, key, service)).To(Succeed())
		Expect(service.Spec.Ports[0].TargetPort.IntVal).To(Equal(int32(9090)))
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: app.Name + "-config", Namespace: namespace}, configMap)).To(Succeed())
		Expect(configMap.Data["STEADYSTATE_APP_OWNER"]).To(Equal("new-owner"))
		Expect(configMap.Data["STEADYSTATE_APP_VERSION"]).To(Equal("v0.1.1"))
	})

	It("retains known-good children when a future capability is requested", func(ctx SpecContext) {
		app := validApplication("unsupported", namespace)
		Expect(k8sClient.Create(ctx, app)).To(Succeed())
		reconcile(ctx, k8sClient, app)
		key := types.NamespacedName{Name: app.Name, Namespace: namespace}
		deployment := &appsv1.Deployment{}
		service := &corev1.Service{}
		configMap := &corev1.ConfigMap{}
		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, key, deployment)).To(Succeed())
		Expect(k8sClient.Get(ctx, key, service)).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: app.Name + "-config", Namespace: namespace}, configMap)).To(Succeed())
		Expect(k8sClient.Get(ctx, key, route)).To(Succeed())
		resourceVersions := []string{deployment.ResourceVersion, service.ResourceVersion, configMap.ResourceVersion, route.ResourceVersion}
		image := deployment.Spec.Template.Spec.Containers[0].Image

		Expect(k8sClient.Get(ctx, key, app)).To(Succeed())
		app.Spec.Image.Tag = "v9.9.9"
		app.Spec.Security.NetworkIsolation = true
		Expect(k8sClient.Update(ctx, app)).To(Succeed())
		reconcile(ctx, k8sClient, app)

		Expect(k8sClient.Get(ctx, key, deployment)).To(Succeed())
		Expect(k8sClient.Get(ctx, key, service)).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: app.Name + "-config", Namespace: namespace}, configMap)).To(Succeed())
		Expect(k8sClient.Get(ctx, key, route)).To(Succeed())
		Expect([]string{deployment.ResourceVersion, service.ResourceVersion, configMap.ResourceVersion, route.ResourceVersion}).To(Equal(resourceVersions))
		Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal(image))
		Expect(k8sClient.Get(ctx, key, app)).To(Succeed())
		Expect(app.Status.Phase).To(Equal(platformv1alpha1.ApplicationPhaseDegraded))
		Expect(meta.FindStatusCondition(app.Status.Conditions, conditionReady).Reason).To(Equal("UnsupportedFeature"))
	})

	It("rejects a disallowed repository and recovers after the Team allowlist changes", func(ctx SpecContext) {
		app := validApplication("repository-guard", namespace)
		app.Spec.Image.Repository = "ghcr.io/saadabdullaah/payments-api"
		Expect(k8sClient.Create(ctx, app)).To(Succeed())
		reconcile(ctx, k8sClient, app)

		key := types.NamespacedName{Name: app.Name, Namespace: namespace}
		Expect(k8sClient.Get(ctx, key, app)).To(Succeed())
		configuration := meta.FindStatusCondition(app.Status.Conditions, conditionConfigurationReady)
		Expect(configuration).NotTo(BeNil())
		Expect(configuration.Status).To(Equal(metav1.ConditionFalse))
		Expect(configuration.Reason).To(Equal("RepositoryNotAllowed"))
		err := k8sClient.Get(ctx, key, &appsv1.Deployment{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())

		team := &platformv1alpha1.Team{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: teamName}, team)).To(Succeed())
		team.Spec.AllowedRepositories = append(team.Spec.AllowedRepositories, "ghcr.io/saadabdullaah/payments-*")
		Expect(k8sClient.Update(ctx, team)).To(Succeed())
		reconcile(ctx, k8sClient, app)
		Expect(k8sClient.Get(ctx, key, &appsv1.Deployment{})).To(Succeed())
	})

	It("retains known-good children when a repository becomes unauthorized", func(ctx SpecContext) {
		app := validApplication("repository-drift", namespace)
		Expect(k8sClient.Create(ctx, app)).To(Succeed())
		reconcile(ctx, k8sClient, app)
		key := types.NamespacedName{Name: app.Name, Namespace: namespace}
		deployment := &appsv1.Deployment{}
		service := &corev1.Service{}
		configMap := &corev1.ConfigMap{}
		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, key, deployment)).To(Succeed())
		Expect(k8sClient.Get(ctx, key, service)).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: app.Name + "-config", Namespace: namespace}, configMap)).To(Succeed())
		Expect(k8sClient.Get(ctx, key, route)).To(Succeed())
		resourceVersions := []string{deployment.ResourceVersion, service.ResourceVersion, configMap.ResourceVersion, route.ResourceVersion}
		image := deployment.Spec.Template.Spec.Containers[0].Image

		Expect(k8sClient.Get(ctx, key, app)).To(Succeed())
		app.Spec.Image.Repository = "registry.example.test/not-authorized"
		app.Spec.Image.Tag = "v9.9.9"
		Expect(k8sClient.Update(ctx, app)).To(Succeed())
		reconcile(ctx, k8sClient, app)

		Expect(k8sClient.Get(ctx, key, deployment)).To(Succeed())
		Expect(k8sClient.Get(ctx, key, service)).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: app.Name + "-config", Namespace: namespace}, configMap)).To(Succeed())
		Expect(k8sClient.Get(ctx, key, route)).To(Succeed())
		Expect([]string{deployment.ResourceVersion, service.ResourceVersion, configMap.ResourceVersion, route.ResourceVersion}).To(Equal(resourceVersions))
		Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal(image))
		Expect(k8sClient.Get(ctx, key, app)).To(Succeed())
		Expect(meta.FindStatusCondition(app.Status.Conditions, conditionConfigurationReady).Reason).To(Equal("RepositoryNotAllowed"))
	})

	It("rejects Applications outside Team-managed namespaces", func(ctx SpecContext) {
		const unmanagedNamespace = "application-unmanaged"
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: unmanagedNamespace}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		app := validApplication("unmanaged", unmanagedNamespace)
		Expect(k8sClient.Create(ctx, app)).To(Succeed())
		reconcile(ctx, k8sClient, app)

		key := types.NamespacedName{Name: app.Name, Namespace: unmanagedNamespace}
		Expect(k8sClient.Get(ctx, key, app)).To(Succeed())
		Expect(meta.FindStatusCondition(app.Status.Conditions, conditionConfigurationReady).Reason).To(Equal("NamespaceNotManaged"))
		err := k8sClient.Get(ctx, key, &appsv1.Deployment{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("rejects an invalid source revision before mutating children", func(ctx SpecContext) {
		app := validApplication("invalid-revision", namespace)
		app.Annotations = map[string]string{platformv1alpha1.SourceRevisionAnnotationKey: "not-a-full-object-id"}
		Expect(k8sClient.Create(ctx, app)).To(Succeed())
		reconcile(ctx, k8sClient, app)

		key := types.NamespacedName{Name: app.Name, Namespace: namespace}
		Expect(k8sClient.Get(ctx, key, app)).To(Succeed())
		Expect(app.Status.Phase).To(Equal(platformv1alpha1.ApplicationPhaseDegraded))
		Expect(meta.FindStatusCondition(app.Status.Conditions, conditionReady).Reason).To(Equal("InvalidSourceRevision"))
		err := k8sClient.Get(ctx, key, &appsv1.Deployment{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())

		app.Annotations[platformv1alpha1.SourceRevisionAnnotationKey] = testGitRevision
		Expect(k8sClient.Update(ctx, app)).To(Succeed())
		reconcile(ctx, k8sClient, app)
		Expect(k8sClient.Get(ctx, key, &appsv1.Deployment{})).To(Succeed())
	})
	It("maps Team and Namespace changes to dependent Applications", func(ctx SpecContext) {
		reconciler := &ApplicationReconciler{Client: k8sClient}
		team := &platformv1alpha1.Team{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: teamName}, team)).To(Succeed())
		teamRequests := reconciler.applicationsForTeam(ctx, team)
		Expect(teamRequests).To(ContainElement(ctrl.Request{NamespacedName: types.NamespacedName{Name: "repository-guard", Namespace: namespace}}))

		ns := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespace}, ns)).To(Succeed())
		namespaceRequests := reconciler.applicationsForNamespace(ctx, ns)
		Expect(namespaceRequests).To(ContainElement(ctrl.Request{NamespacedName: types.NamespacedName{Name: "repository-drift", Namespace: namespace}}))
	})

	It("derives Healthy status from Deployment, runtime Pod digest, and HTTPRoute status", func(ctx SpecContext) {
		app := validApplication("healthy", namespace)
		Expect(k8sClient.Create(ctx, app)).To(Succeed())
		reconcile(ctx, k8sClient, app)
		key := types.NamespacedName{Name: app.Name, Namespace: namespace}

		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, key, deployment)).To(Succeed())
		deployment.Status.Replicas = 1
		deployment.Status.ReadyReplicas = 1
		deployment.Status.AvailableReplicas = 1
		deployment.Status.UpdatedReplicas = 1
		deployment.Status.ObservedGeneration = deployment.Generation
		deployment.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue, Reason: "MinimumReplicasAvailable", Message: "ready"}}
		Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())

		route := &gatewayv1.HTTPRoute{}
		Expect(k8sClient.Get(ctx, key, route)).To(Succeed())
		route.Status.Parents = []gatewayv1.RouteParentStatus{{
			ParentRef:      route.Spec.ParentRefs[0],
			ControllerName: gatewayv1.GatewayController("gateway.envoyproxy.io/gatewayclass-controller"),
			Conditions: []metav1.Condition{
				{Type: string(gatewayv1.RouteConditionAccepted), Status: metav1.ConditionTrue, ObservedGeneration: route.Generation, Reason: "Accepted", Message: "accepted", LastTransitionTime: metav1.Now()},
				{Type: string(gatewayv1.RouteConditionResolvedRefs), Status: metav1.ConditionTrue, ObservedGeneration: route.Generation, Reason: "ResolvedRefs", Message: "resolved", LastTransitionTime: metav1.Now()},
			},
		}}
		Expect(k8sClient.Status().Update(ctx, route)).To(Succeed())

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "healthy-runtime", Namespace: namespace, Labels: resources.SelectorLabels(app)},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "application", Image: deployment.Spec.Template.Spec.Containers[0].Image}}},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "application", Image: deployment.Spec.Template.Spec.Containers[0].Image, ImageID: "containerd://" + testImageDigest, Ready: true}}
		Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
		reconcile(ctx, k8sClient, app)

		Expect(k8sClient.Get(ctx, key, app)).To(Succeed())
		Expect(app.Status.Phase).To(Equal(platformv1alpha1.ApplicationPhaseHealthy))
		Expect(app.Status.ActiveVersion).To(Equal("v0.1.0"))
		Expect(app.Status.CandidateVersion).To(BeEmpty())
		Expect(app.Status.ResolvedImageDigest).To(Equal(testImageDigest))
		Expect(app.Status.ResolvedGitRevision).To(BeEmpty())
		Expect(meta.IsStatusConditionTrue(app.Status.Conditions, conditionReady)).To(BeTrue())

		deploymentResourceVersion := deployment.ResourceVersion
		app.Annotations = map[string]string{platformv1alpha1.SourceRevisionAnnotationKey: testGitRevision}
		Expect(k8sClient.Update(ctx, app)).To(Succeed())
		counting := &countingClient{Client: k8sClient}
		reconcile(ctx, counting, app)
		Expect(counting.mutations.Load()).To(Equal(int64(1)), "only the status patch may write for a revision-only update")
		Expect(k8sClient.Get(ctx, key, deployment)).To(Succeed())
		Expect(deployment.ResourceVersion).To(Equal(deploymentResourceVersion))
		Expect(k8sClient.Get(ctx, key, app)).To(Succeed())
		Expect(app.Status.ResolvedGitRevision).To(Equal(testGitRevision))
		Expect(app.Status.ResolvedImageDigest).To(Equal(testImageDigest))

		Expect(k8sClient.Get(ctx, key, route)).To(Succeed())
		route.Status.Parents[0].Conditions[0].Status = metav1.ConditionFalse
		route.Status.Parents[0].Conditions[0].Reason = "NotAllowedByListeners"
		route.Status.Parents[0].Conditions[0].Message = "rejected"
		Expect(k8sClient.Status().Update(ctx, route)).To(Succeed())
		reconcile(ctx, k8sClient, app)
		Expect(k8sClient.Get(ctx, key, app)).To(Succeed())
		Expect(app.Status.Phase).To(Equal(platformv1alpha1.ApplicationPhaseDegraded))
		Expect(meta.FindStatusCondition(app.Status.Conditions, conditionReady).Reason).To(Equal("RouteRejected"))
	})

	It("retries a conflicting status patch", func(ctx SpecContext) {
		app := validApplication("conflict", namespace)
		app.Spec.Security.RequireSignedImage = true
		Expect(k8sClient.Create(ctx, app)).To(Succeed())
		wrapped := &conflictStatusClient{Client: k8sClient}
		reconcile(ctx, wrapped, app)
		Expect(wrapped.attempts.Load()).To(BeNumerically(">=", 2))
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: app.Name, Namespace: namespace}, app)).To(Succeed())
		Expect(app.Status.Phase).To(Equal(platformv1alpha1.ApplicationPhaseDegraded))
	})

	It("performs zero writes after reconciliation settles", func(ctx SpecContext) {
		app := validApplication("settled", namespace)
		Expect(k8sClient.Create(ctx, app)).To(Succeed())
		reconcile(ctx, k8sClient, app)
		reconcile(ctx, k8sClient, app)

		counting := &countingClient{Client: k8sClient}
		reconcile(ctx, counting, app)
		Expect(counting.mutations.Load()).To(BeZero())
	})

	It("releases its finalizer and relies on garbage collection", func(ctx SpecContext) {
		app := validApplication("deleting", namespace)
		Expect(k8sClient.Create(ctx, app)).To(Succeed())
		reconcile(ctx, k8sClient, app)
		key := types.NamespacedName{Name: app.Name, Namespace: namespace}
		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, key, deployment)).To(Succeed())
		Expect(metav1.IsControlledBy(deployment, app)).To(BeTrue())

		Expect(k8sClient.Delete(ctx, app)).To(Succeed())
		reconcile(ctx, k8sClient, app)
		err := k8sClient.Get(ctx, key, &platformv1alpha1.Application{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
		// envtest deliberately has no garbage collector; ownership is the contract tested here.
		Expect(k8sClient.Get(ctx, key, deployment)).To(Succeed())
		Expect(deployment.OwnerReferences).NotTo(BeEmpty())
	})
})

func reconcile(ctx context.Context, c client.Client, app *platformv1alpha1.Application) {
	reconciler := &ApplicationReconciler{Client: c, Scheme: clientgoscheme.Scheme}
	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: app.Name, Namespace: app.Namespace}})
	Expect(err).NotTo(HaveOccurred())
}

type countingClient struct {
	client.Client
	mutations atomic.Int64
}

func (c *countingClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	c.mutations.Add(1)
	return c.Client.Create(ctx, obj, opts...)
}

func (c *countingClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	c.mutations.Add(1)
	return c.Client.Update(ctx, obj, opts...)
}

func (c *countingClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	c.mutations.Add(1)
	return c.Client.Patch(ctx, obj, patch, opts...)
}

func (c *countingClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	c.mutations.Add(1)
	return c.Client.Delete(ctx, obj, opts...)
}

func (c *countingClient) Status() client.SubResourceWriter {
	return &countingStatusWriter{SubResourceWriter: c.Client.Status(), mutations: &c.mutations}
}

type countingStatusWriter struct {
	client.SubResourceWriter
	mutations *atomic.Int64
}

func (w *countingStatusWriter) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	w.mutations.Add(1)
	return w.SubResourceWriter.Create(ctx, obj, subResource, opts...)
}

func (w *countingStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	w.mutations.Add(1)
	return w.SubResourceWriter.Update(ctx, obj, opts...)
}

func (w *countingStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	w.mutations.Add(1)
	return w.SubResourceWriter.Patch(ctx, obj, patch, opts...)
}

type conflictStatusClient struct {
	client.Client
	attempts atomic.Int64
}

func (c *conflictStatusClient) Status() client.SubResourceWriter {
	return &conflictStatusWriter{SubResourceWriter: c.Client.Status(), attempts: &c.attempts}
}

type conflictStatusWriter struct {
	client.SubResourceWriter
	attempts *atomic.Int64
}

func (w *conflictStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	if w.attempts.Add(1) == 1 {
		return apierrors.NewConflict(schema.GroupResource{Group: platformv1alpha1.GroupVersion.Group, Resource: "applications"}, obj.GetName(), errors.New("injected conflict"))
	}
	return w.SubResourceWriter.Patch(ctx, obj, patch, opts...)
}
