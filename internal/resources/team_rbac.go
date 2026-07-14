package resources

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/utils/ptr"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

// TeamServiceAccount builds the local identity used to prove namespace-scoped RBAC.
func TeamServiceAccount(team *platformv1alpha1.Team) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta:                   teamObjectMeta(team, TeamOwnerName),
		AutomountServiceAccountToken: ptr.To(false),
	}
}

// TeamOwnerRules defines the install-time ClusterRole delegated inside each Team namespace.
func TeamOwnerRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{APIGroups: []string{"platform.steadystate.dev"}, Resources: []string{"applications"}, Verbs: []string{"create", "delete", "get", "list", "patch", "update", "watch"}},
		{APIGroups: []string{"platform.steadystate.dev"}, Resources: []string{"applications/status"}, Verbs: []string{"get"}},
		{APIGroups: []string{""}, Resources: []string{"configmaps", "secrets"}, Verbs: []string{"create", "delete", "get", "list", "patch", "update", "watch"}},
		{APIGroups: []string{""}, Resources: []string{"events", "pods", "services"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{""}, Resources: []string{"pods/log"}, Verbs: []string{"get"}},
		{APIGroups: []string{""}, Resources: []string{"pods/exec"}, Verbs: []string{"create", "get"}},
		{APIGroups: []string{"apps"}, Resources: []string{"deployments", "replicasets"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"events.k8s.io"}, Resources: []string{"events"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"gateway.networking.k8s.io"}, Resources: []string{"httproutes"}, Verbs: []string{"get", "list", "watch"}},
	}
}

// TeamRoleBinding binds sorted Kubernetes users and the generated ServiceAccount to the installed Team owner ClusterRole.
func TeamRoleBinding(team *platformv1alpha1.Team) *rbacv1.RoleBinding {
	owners := make([]string, len(team.Spec.Owners))
	for i, owner := range team.Spec.Owners {
		owners[i] = string(owner)
	}
	sort.Strings(owners)
	subjects := []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: TeamOwnerName, Namespace: TeamNamespaceName(team)}}
	for _, owner := range owners {
		subjects = append(subjects, rbacv1.Subject{Kind: rbacv1.UserKind, APIGroup: rbacv1.GroupName, Name: owner})
	}
	return &rbacv1.RoleBinding{
		ObjectMeta: teamObjectMeta(team, TeamOwnerName),
		Subjects:   subjects,
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     TeamOwnerName,
		},
	}
}
