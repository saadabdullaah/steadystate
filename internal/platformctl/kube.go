package platformctl

import (
	stdcontext "context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	teamGVR         = schema.GroupVersionResource{Group: "platform.steadystate.dev", Version: "v1alpha1", Resource: "teams"}
	applicationGVR  = schema.GroupVersionResource{Group: "platform.steadystate.dev", Version: "v1alpha1", Resource: "applications"}
	databaseGVR     = schema.GroupVersionResource{Group: "platform.steadystate.dev", Version: "v1alpha1", Resource: "databases"}
	rolloutGVR      = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "rollouts"}
	analysisRunGVR  = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "analysisruns"}
	policyReportGVR = schema.GroupVersionResource{Group: "wgpolicyk8s.io", Version: "v1alpha2", Resource: "policyreports"}
	backupGVR       = schema.GroupVersionResource{Group: "postgresql.cnpg.io", Version: "v1", Resource: "backups"}
	httpRouteGVR    = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}
)

type ClusterClient struct {
	dynamic dynamic.Interface
	core    kubernetes.Interface
	rest    *rest.Config
}

type ResourceSummary struct {
	Kind               string `json:"kind" yaml:"kind"`
	Namespace          string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name               string `json:"name" yaml:"name"`
	Phase              string `json:"phase,omitempty" yaml:"phase,omitempty"`
	Ready              string `json:"ready,omitempty" yaml:"ready,omitempty"`
	Reason             string `json:"reason,omitempty" yaml:"reason,omitempty"`
	Message            string `json:"message,omitempty" yaml:"message,omitempty"`
	Generation         int64  `json:"generation" yaml:"generation"`
	ObservedGeneration int64  `json:"observedGeneration,omitempty" yaml:"observedGeneration,omitempty"`
}

func NewClusterClient(context Context) (*ClusterClient, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if context.Kubeconfig != "" {
		rules.ExplicitPath = context.Kubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: context.KubeContext}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	if err != nil {
		return nil, exitError(ExitAuth, "load Kubernetes context: %v", err)
	}
	config.Timeout = 20 * time.Second
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, exitError(ExitAuth, "create Kubernetes client: %v", err)
	}
	coreClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, exitError(ExitAuth, "create Kubernetes core client: %v", err)
	}
	return &ClusterClient{dynamic: dynamicClient, core: coreClient, rest: config}, nil
}

func (c *ClusterClient) Get(ctx stdcontext.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error) {
	resource := c.dynamic.Resource(gvr)
	var result *unstructured.Unstructured
	var err error
	if namespace == "" {
		result, err = resource.Get(ctx, name, metav1.GetOptions{})
	} else {
		result, err = resource.Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	}
	if err != nil {
		return nil, kubernetesError(err, gvr.Resource, namespace, name)
	}
	return result, nil
}

func (c *ClusterClient) List(ctx stdcontext.Context, gvr schema.GroupVersionResource, namespace, selector string) ([]unstructured.Unstructured, error) {
	options := metav1.ListOptions{LabelSelector: selector}
	resource := c.dynamic.Resource(gvr)
	var list *unstructured.UnstructuredList
	var err error
	if namespace == "" {
		list, err = resource.List(ctx, options)
	} else {
		list, err = resource.Namespace(namespace).List(ctx, options)
	}
	if err != nil {
		return nil, kubernetesError(err, gvr.Resource, namespace, "")
	}
	sort.Slice(list.Items, func(i, j int) bool {
		if list.Items[i].GetNamespace() == list.Items[j].GetNamespace() {
			return list.Items[i].GetName() < list.Items[j].GetName()
		}
		return list.Items[i].GetNamespace() < list.Items[j].GetNamespace()
	})
	return list.Items, nil
}

func kubernetesError(err error, resource, namespace, name string) error {
	if errors.Is(err, stdcontext.DeadlineExceeded) || errors.Is(err, stdcontext.Canceled) {
		return exitError(ExitTimeout, "Kubernetes request for %s timed out", resource)
	}
	message := err.Error()
	if strings.Contains(strings.ToLower(message), "not found") {
		return exitError(ExitNotFound, "%s %s/%s was not found", resource, namespace, name)
	}
	if strings.Contains(strings.ToLower(message), "forbidden") || strings.Contains(strings.ToLower(message), "unauthorized") {
		return exitError(ExitAuth, "Kubernetes denied access to %s: %v", resource, err)
	}
	return exitError(ExitRemote, "read Kubernetes %s: %v", resource, err)
}

func Summarize(kind string, object *unstructured.Unstructured) ResourceSummary {
	phase, _, _ := unstructured.NestedString(object.Object, "status", "phase")
	observed, _, _ := unstructured.NestedInt64(object.Object, "status", "observedGeneration")
	summary := ResourceSummary{
		Kind:               kind,
		Namespace:          object.GetNamespace(),
		Name:               object.GetName(),
		Phase:              phase,
		Generation:         object.GetGeneration(),
		ObservedGeneration: observed,
	}
	conditions, _, _ := unstructured.NestedSlice(object.Object, "status", "conditions")
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if !ok || condition["type"] != "Ready" {
			continue
		}
		summary.Ready, _ = condition["status"].(string)
		summary.Reason, _ = condition["reason"].(string)
		summary.Message, _ = condition["message"].(string)
		break
	}
	return summary
}

func (c *ClusterClient) PodLogs(ctx stdcontext.Context, namespace, application string, follow bool, tail int64, since time.Duration) (io.ReadCloser, string, error) {
	selector := labels.Set{"app.kubernetes.io/instance": application, "app.kubernetes.io/managed-by": "steadystate"}.AsSelector().String()
	pods, err := c.core.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, "", kubernetesError(err, "pods", namespace, application)
	}
	if len(pods.Items) == 0 {
		return nil, "", exitError(ExitNotFound, "no Pods found for Application %s/%s", namespace, application)
	}
	sort.Slice(pods.Items, func(i, j int) bool { return pods.Items[i].Name < pods.Items[j].Name })
	options := &corev1.PodLogOptions{Container: "application", Follow: follow, TailLines: &tail}
	if since > 0 {
		seconds := int64(since.Seconds())
		options.SinceSeconds = &seconds
	}
	stream, err := c.core.CoreV1().Pods(namespace).GetLogs(pods.Items[0].Name, options).Stream(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", exitError(ExitTimeout, "Pod log request timed out")
		}
		return nil, "", exitError(ExitRemote, "stream Pod logs: %v", err)
	}
	return stream, pods.Items[0].Name, nil
}

func (c *ClusterClient) ServiceProxy(ctx stdcontext.Context, service string, port int, path string, query url.Values) (json.RawMessage, error) {
	transport, err := rest.TransportFor(c.rest)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(c.rest.Host, "/")
	proxyPath := "/api/v1/namespaces/monitoring/services/http:" + url.PathEscape(service) + ":" + strconv.Itoa(port) + "/proxy"
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	requestURL := base + proxyPath + path
	if encoded := query.Encode(); encoded != "" {
		requestURL += "?" + encoded
	}
	httpRequest, err := httpRequestWithContext(ctx, requestURL)
	if err != nil {
		return nil, err
	}
	response, err := transport.RoundTrip(httpRequest)
	if err != nil {
		if ctx.Err() != nil {
			return nil, exitError(ExitTimeout, "monitoring backend request timed out")
		}
		return nil, exitError(ExitRemote, "query monitoring backend: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, exitError(ExitRemote, "monitoring backend returned HTTP %d: %s", response.StatusCode, Redact(string(body)))
	}
	if !json.Valid(body) {
		return nil, exitError(ExitRemote, "monitoring backend returned invalid JSON")
	}
	return json.RawMessage(body), nil
}

func httpRequestWithContext(ctx stdcontext.Context, requestURL string) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
}

func conditionStatus(object *unstructured.Unstructured, conditionType string) (string, string, string) {
	conditions, _, _ := unstructured.NestedSlice(object.Object, "status", "conditions")
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if !ok || condition["type"] != conditionType {
			continue
		}
		status, _ := condition["status"].(string)
		reason, _ := condition["reason"].(string)
		message, _ := condition["message"].(string)
		return status, reason, message
	}
	return "Unknown", "ConditionMissing", fmt.Sprintf("%s condition is absent", conditionType)
}
