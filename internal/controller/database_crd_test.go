//go:build envtest

package controller

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

var _ = Describe("Database CRD", func() {
	const namespace = "database-crd"

	BeforeEach(func(ctx SpecContext) {
		current := &corev1.Namespace{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: namespace}, current)
		if err != nil {
			Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})).To(Succeed())
		}
	})

	It("stores a valid Database and defaults its contract", func(ctx SpecContext) {
		database := validSchemaDatabase("orders", namespace)
		Expect(k8sClient.Create(ctx, database)).To(Succeed())
		stored := &platformv1alpha1.Database{}
		Expect(k8sClient.Get(ctx, clientKey(database), stored)).To(Succeed())
		Expect(stored.Spec.Engine).To(Equal("postgres"))
		Expect(stored.Spec.Instances).To(Equal(int32(1)))
		Expect(stored.Spec.Backups.Retention).To(Equal("7d"))
	})

	DescribeTable("rejects invalid specifications",
		func(name string, mutate func(*platformv1alpha1.Database)) {
			database := validSchemaDatabase(name, namespace)
			mutate(database)
			Expect(k8sClient.Create(context.Background(), database)).NotTo(Succeed())
		},
		Entry("unsupported engine", "bad-engine", func(database *platformv1alpha1.Database) { database.Spec.Engine = "mysql" }),
		Entry("too many instances", "many-instances", func(database *platformv1alpha1.Database) { database.Spec.Instances = 4 }),
		Entry("small storage", "small-storage", func(database *platformv1alpha1.Database) { database.Spec.Storage.Size = "512Mi" }),
		Entry("invalid retention", "bad-retention", func(database *platformv1alpha1.Database) { database.Spec.Backups.Retention = "0d" }),
		Entry("recovery without backups", "bad-recovery", func(database *platformv1alpha1.Database) {
			database.Spec.Backups.Enabled = false
			database.Spec.Recovery = &platformv1alpha1.DatabaseRecovery{SourceServerName: "orders-old"}
		}),
		Entry("long name", "long-name", func(database *platformv1alpha1.Database) { database.Name = strings.Repeat("a", 64) }),
	)

	It("rejects storage shrink and recovery mutation", func(ctx SpecContext) {
		database := validSchemaDatabase("immutable", namespace)
		database.Spec.Storage.Size = "2Gi"
		database.Spec.Recovery = &platformv1alpha1.DatabaseRecovery{SourceServerName: "archive-one"}
		Expect(k8sClient.Create(ctx, database)).To(Succeed())
		database.Spec.Storage.Size = "1Gi"
		Expect(k8sClient.Update(ctx, database)).NotTo(Succeed())
		Expect(k8sClient.Get(ctx, clientKey(database), database)).To(Succeed())
		database.Spec.Recovery.SourceServerName = "archive-two"
		Expect(k8sClient.Update(ctx, database)).NotTo(Succeed())
	})

	It("rejects adding recovery or storage class after creation", func(ctx SpecContext) {
		database := validSchemaDatabase("immutable-absence", namespace)
		Expect(k8sClient.Create(ctx, database)).To(Succeed())
		database.Spec.Recovery = &platformv1alpha1.DatabaseRecovery{SourceServerName: "archive-one"}
		Expect(k8sClient.Update(ctx, database)).NotTo(Succeed())
		Expect(k8sClient.Get(ctx, clientKey(database), database)).To(Succeed())
		storageClass := "local-path"
		database.Spec.Storage.StorageClassName = &storageClass
		Expect(k8sClient.Update(ctx, database)).NotTo(Succeed())
	})
})

func validSchemaDatabase(name, namespace string) *platformv1alpha1.Database {
	return &platformv1alpha1.Database{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: platformv1alpha1.DatabaseSpec{
			Engine: "postgres", Instances: 1,
			Storage: platformv1alpha1.DatabaseStorage{Size: "1Gi"},
			Backups: platformv1alpha1.DatabaseBackups{Enabled: true, Schedule: "0 0 2 * * *", Retention: "7d"},
		},
	}
}

func clientKey(object metav1.Object) types.NamespacedName {
	return types.NamespacedName{Name: object.GetName(), Namespace: object.GetNamespace()}
}
