package resources

import (
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

// TeamDefaultDenyNetworkPolicy denies all ingress and egress for Team pods by default.
func TeamDefaultDenyNetworkPolicy(team *platformv1alpha1.Team) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: teamObjectMeta(team, DefaultDenyPolicyName),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
		},
	}
}

// TeamAllowDNSNetworkPolicy permits only CoreDNS lookups over UDP and TCP.
func TeamAllowDNSNetworkPolicy(team *platformv1alpha1.Team) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: teamObjectMeta(team, AllowDNSPolicyName),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress: []networkingv1.NetworkPolicyEgressRule{{
				To: []networkingv1.NetworkPolicyPeer{{
					NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "kube-system"}},
					PodSelector:       &metav1.LabelSelector{MatchLabels: map[string]string{"k8s-app": "kube-dns"}},
				}},
				Ports: []networkingv1.NetworkPolicyPort{
					{Protocol: ptr.To(corev1.ProtocolUDP), Port: ptr.To(intstr.FromInt32(53))},
					{Protocol: ptr.To(corev1.ProtocolTCP), Port: ptr.To(intstr.FromInt32(53))},
				},
			}},
		},
	}
}

// TeamAllowEnvoyNetworkPolicy permits ingress only from the shared Gateway's managed proxy pods.
func TeamAllowEnvoyNetworkPolicy(team *platformv1alpha1.Team) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: teamObjectMeta(team, AllowEnvoyPolicyName),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{From: []networkingv1.NetworkPolicyPeer{{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "envoy-gateway-system"}},
				PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
					"app.kubernetes.io/component":                    "proxy",
					"gateway.envoyproxy.io/owning-gateway-name":      "steadystate",
					"gateway.envoyproxy.io/owning-gateway-namespace": "steadystate-system",
				}},
			}}}},
		},
	}
}

// TeamAllowMonitoringNetworkPolicy permits only Prometheus to scrape the named application HTTP port.
func TeamAllowMonitoringNetworkPolicy(team *platformv1alpha1.Team) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: teamObjectMeta(team, AllowMonitoringPolicyName),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{{
					NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
						"kubernetes.io/metadata.name": "monitoring",
					}},
					PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
						"app.kubernetes.io/name": "prometheus",
					}},
				}},
				Ports: []networkingv1.NetworkPolicyPort{{
					Protocol: ptr.To(corev1.ProtocolTCP),
					Port:     ptr.To(intstr.FromString("http")),
				}},
			}},
		},
	}
}

// TeamAllowApplicationsNetworkPolicy permits only non-isolated SteadyState
// Applications to communicate with non-isolated Applications in the same Team.
func TeamAllowApplicationsNetworkPolicy(team *platformv1alpha1.Team) *networkingv1.NetworkPolicy {
	selector := metav1.LabelSelector{MatchLabels: map[string]string{
		WorkloadKindLabelKey:     "application",
		NetworkIsolationLabelKey: "false",
	}}
	return &networkingv1.NetworkPolicy{
		ObjectMeta: teamObjectMeta(team, AllowTeamApplicationsPolicyName),
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: selector,
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{{PodSelector: &selector}},
			}},
			Egress: []networkingv1.NetworkPolicyEgressRule{{
				To: []networkingv1.NetworkPolicyPeer{{PodSelector: &selector}},
			}},
		},
	}
}
