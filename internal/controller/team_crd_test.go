//go:build envtest

package controller

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

var _ = Describe("Team CRD", func() {
	It("stores a valid cluster-scoped Team", func(ctx SpecContext) {
		team := validSchemaTeam("payments")
		Expect(k8sClient.Create(ctx, team)).To(Succeed())

		stored := &platformv1alpha1.Team{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: team.Name}, stored)).To(Succeed())
		Expect(stored.Namespace).To(BeEmpty())
		Expect(stored.Spec.Owners).To(ConsistOf(platformv1alpha1.TeamOwner("payments-owner")))
		Expect(stored.Spec.Quota.CPU.Cmp(resource.MustParse("2"))).To(Equal(0))
	})

	DescribeTable("rejects invalid specifications",
		func(name string, mutate func(*platformv1alpha1.Team)) {
			team := validSchemaTeam(name)
			mutate(team)
			Expect(k8sClient.Create(context.Background(), team)).NotTo(Succeed())
		},
		Entry("namespace-unsafe name", "name-too-long", func(team *platformv1alpha1.Team) {
			team.Name = strings.Repeat("a", 59)
		}),
		Entry("uppercase name", "uppercase", func(team *platformv1alpha1.Team) {
			team.Name = "Payments"
		}),
		Entry("empty owners", "empty-owners", func(team *platformv1alpha1.Team) {
			team.Spec.Owners = nil
		}),
		Entry("whitespace owner", "whitespace-owner", func(team *platformv1alpha1.Team) {
			team.Spec.Owners = []platformv1alpha1.TeamOwner{"payments owner"}
		}),
		Entry("CPU below platform minimum", "low-cpu", func(team *platformv1alpha1.Team) {
			team.Spec.Quota.CPU = resource.MustParse("499m")
		}),
		Entry("memory below platform minimum", "low-memory", func(team *platformv1alpha1.Team) {
			team.Spec.Quota.Memory = resource.MustParse("511Mi")
		}),
		Entry("empty repository allowlist", "empty-repositories", func(team *platformv1alpha1.Team) {
			team.Spec.AllowedRepositories = nil
		}),
		Entry("repository digest", "repository-digest", func(team *platformv1alpha1.Team) {
			team.Spec.AllowedRepositories = []platformv1alpha1.RepositoryPattern{"example.test/app@sha256:deadbeef"}
		}),
	)
})

func validSchemaTeam(name string) *platformv1alpha1.Team {
	return &platformv1alpha1.Team{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: platformv1alpha1.TeamSpec{
			Owners: []platformv1alpha1.TeamOwner{platformv1alpha1.TeamOwner(name + "-owner")},
			Quota: platformv1alpha1.TeamQuota{
				CPU:    resource.MustParse("2"),
				Memory: resource.MustParse("2Gi"),
			},
			AllowedRepositories: []platformv1alpha1.RepositoryPattern{"ghcr.io/saadabdullaah/steadystate-demo-app"},
		},
	}
}
