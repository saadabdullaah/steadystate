//go:build envtest

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
	"github.com/saadabdullaah/steadystate/internal/resources"
)

var _ = Describe("Team reconciler", Ordered, func() {
	It("creates the complete namespace boundary and reports truthful readiness", func(ctx SpecContext) {
		team := validSchemaTeam("team-runtime-create")
		Expect(k8sClient.Create(ctx, team)).To(Succeed())
		reconcileTeam(ctx, k8sClient, team.Name)

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: team.Name}, team)).To(Succeed())
		Expect(team.Finalizers).To(ContainElement(TeamFinalizer))
		Expect(team.Status.ObservedGeneration).To(Equal(team.Generation))
		Expect(team.Status.Namespace).To(Equal(resources.TeamNamespaceName(team)))
		Expect(meta.IsStatusConditionTrue(team.Status.Conditions, conditionTeamReady)).To(BeTrue())

		objects := []client.Object{
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: resources.TeamNamespaceName(team)}},
			&corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: resources.TeamQuotaName, Namespace: resources.TeamNamespaceName(team)}},
			&corev1.LimitRange{ObjectMeta: metav1.ObjectMeta{Name: resources.TeamLimitRangeName, Namespace: resources.TeamNamespaceName(team)}},
			&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: resources.TeamOwnerName, Namespace: resources.TeamNamespaceName(team)}},
			&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: resources.TeamOwnerName, Namespace: resources.TeamNamespaceName(team)}},
			&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: resources.TeamOwnerName, Namespace: resources.TeamNamespaceName(team)}},
			&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: resources.DefaultDenyPolicyName, Namespace: resources.TeamNamespaceName(team)}},
			&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: resources.AllowDNSPolicyName, Namespace: resources.TeamNamespaceName(team)}},
			&networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: resources.AllowEnvoyPolicyName, Namespace: resources.TeamNamespaceName(team)}},
		}
		for _, object := range objects {
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(object), object)).To(Succeed(), object.GetName())
			Expect(object.GetLabels()).To(HaveKeyWithValue(resources.TeamLabelKey, team.Name), object.GetName())
			Expect(object.GetAnnotations()).To(HaveKeyWithValue(resources.TeamUIDAnnotationKey, string(team.UID)), object.GetName())
			Expect(object.GetOwnerReferences()).To(BeEmpty(), object.GetName())
		}
	})

	It("updates policy and RBAC, repairs drift, and settles to zero writes", func(ctx SpecContext) {
		team := validSchemaTeam("team-runtime-drift")
		Expect(k8sClient.Create(ctx, team)).To(Succeed())
		reconcileTeam(ctx, k8sClient, team.Name)

		key := types.NamespacedName{Name: team.Name}
		Expect(k8sClient.Get(ctx, key, team)).To(Succeed())
		team.Spec.Quota.CPU = resource.MustParse("4")
		team.Spec.Owners = []platformv1alpha1.TeamOwner{"zoe", "alice"}
		Expect(k8sClient.Update(ctx, team)).To(Succeed())
		reconcileTeam(ctx, k8sClient, team.Name)

		namespace := resources.TeamNamespaceName(team)
		quota := &corev1.ResourceQuota{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.TeamQuotaName, Namespace: namespace}, quota)).To(Succeed())
		cpu := quota.Spec.Hard[corev1.ResourceRequestsCPU]
		Expect(cpu.Cmp(resource.MustParse("4"))).To(Equal(0))
		binding := &rbacv1.RoleBinding{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.TeamOwnerName, Namespace: namespace}, binding)).To(Succeed())
		Expect(binding.Subjects).To(HaveLen(3))
		Expect(binding.Subjects[1].Name).To(Equal("alice"))
		Expect(binding.Subjects[2].Name).To(Equal("zoe"))

		policyKey := types.NamespacedName{Name: resources.AllowDNSPolicyName, Namespace: namespace}
		policy := &networkingv1.NetworkPolicy{}
		Expect(k8sClient.Get(ctx, policyKey, policy)).To(Succeed())
		oldUID := policy.UID
		Expect(k8sClient.Delete(ctx, policy)).To(Succeed())
		reconcileTeam(ctx, k8sClient, team.Name)
		Expect(k8sClient.Get(ctx, policyKey, policy)).To(Succeed())
		Expect(policy.UID).NotTo(Equal(oldUID))

		managedNamespace := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespace}, managedNamespace)).To(Succeed())
		delete(managedNamespace.Labels, resources.TeamLabelKey)
		Expect(k8sClient.Update(ctx, managedNamespace)).To(Succeed())
		reconcileTeam(ctx, k8sClient, team.Name)
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespace}, managedNamespace)).To(Succeed())
		Expect(managedNamespace.Labels).To(HaveKeyWithValue(resources.TeamLabelKey, team.Name))

		reconcileTeam(ctx, k8sClient, team.Name)
		counting := &countingClient{Client: k8sClient}
		reconcileTeam(ctx, counting, team.Name)
		Expect(counting.mutations.Load()).To(BeZero())
	})

	It("reports invalid repository globs without creating a namespace", func(ctx SpecContext) {
		team := validSchemaTeam("team-runtime-invalid")
		team.Spec.AllowedRepositories = []platformv1alpha1.RepositoryPattern{"example.test/[app"}
		Expect(k8sClient.Create(ctx, team)).To(Succeed())
		reconcileTeam(ctx, k8sClient, team.Name)

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: team.Name}, team)).To(Succeed())
		ready := meta.FindStatusCondition(team.Status.Conditions, conditionTeamReady)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal("InvalidConfiguration"))
		err := k8sClient.Get(ctx, types.NamespacedName{Name: resources.TeamNamespaceName(team)}, &corev1.Namespace{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("refuses to adopt a pre-existing namespace", func(ctx SpecContext) {
		team := validSchemaTeam("team-runtime-conflict")
		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: resources.TeamNamespaceName(team)}}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		Expect(k8sClient.Create(ctx, team)).To(Succeed())
		reconcileTeam(ctx, k8sClient, team.Name)

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: team.Name}, team)).To(Succeed())
		ready := meta.FindStatusCondition(team.Status.Conditions, conditionTeamReady)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal("OwnershipConflict"))
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespace.Name}, namespace)).To(Succeed())
		Expect(namespace.Annotations).NotTo(HaveKey(resources.TeamUIDAnnotationKey))
		err := k8sClient.Get(ctx, types.NamespacedName{Name: resources.TeamQuotaName, Namespace: namespace.Name}, &corev1.ResourceQuota{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("keeps its finalizer when namespace ownership no longer matches", func(ctx SpecContext) {
		team := validSchemaTeam("team-runtime-delete")
		Expect(k8sClient.Create(ctx, team)).To(Succeed())
		reconcileTeam(ctx, k8sClient, team.Name)

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: team.Name}, team)).To(Succeed())
		namespace := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.TeamNamespaceName(team)}, namespace)).To(Succeed())
		namespace.Annotations[resources.TeamUIDAnnotationKey] = "different-team-uid"
		Expect(k8sClient.Update(ctx, namespace)).To(Succeed())
		Expect(k8sClient.Delete(ctx, team)).To(Succeed())
		reconcileTeam(ctx, k8sClient, team.Name)

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: team.Name}, team)).To(Succeed())
		Expect(team.Finalizers).To(ContainElement(TeamFinalizer))
		ready := meta.FindStatusCondition(team.Status.Conditions, conditionTeamReady)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Reason).To(Equal("OwnershipConflict"))
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespace.Name}, namespace)).To(Succeed())
	})
})

func reconcileTeam(ctx context.Context, c client.Client, name string) {
	reconciler := &TeamReconciler{Client: c}
	_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: name}})
	Expect(err).NotTo(HaveOccurred())
}
