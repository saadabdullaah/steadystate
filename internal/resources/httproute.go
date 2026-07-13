package resources

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

// HTTPRoute builds the route from the shared SteadyState Gateway to an Application.
func HTTPRoute(application *platformv1alpha1.Application) *gatewayv1.HTTPRoute {
	group := gatewayv1.Group("gateway.networking.k8s.io")
	kind := gatewayv1.Kind("Gateway")
	namespace := gatewayv1.Namespace("steadystate-system")
	pathType := gatewayv1.PathMatchPathPrefix
	path := "/"
	backendGroup := gatewayv1.Group("")
	backendKind := gatewayv1.Kind("Service")
	port := gatewayv1.PortNumber(80)
	weight := int32(1)
	return &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: application.Name, Namespace: application.Namespace, Labels: Labels(application)},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{
				Group: &group, Kind: &kind, Namespace: &namespace, Name: gatewayv1.ObjectName("steadystate"),
			}}},
			Hostnames: []gatewayv1.Hostname{gatewayv1.Hostname(Hostname(application))},
			Rules: []gatewayv1.HTTPRouteRule{{
				Matches: []gatewayv1.HTTPRouteMatch{{Path: &gatewayv1.HTTPPathMatch{Type: &pathType, Value: &path}}},
				BackendRefs: []gatewayv1.HTTPBackendRef{{BackendRef: gatewayv1.BackendRef{BackendObjectReference: gatewayv1.BackendObjectReference{
					Group: &backendGroup, Kind: &backendKind, Name: gatewayv1.ObjectName(application.Name), Port: &port,
				}, Weight: &weight}}},
			}},
		},
	}
}
