package controller

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlreconcile "sigs.k8s.io/controller-runtime/pkg/reconcile"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
	"github.com/saadabdullaah/steadystate/internal/resources"
)

var (
	sourceRevisionPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
	imageDigestPattern    = regexp.MustCompile(`(^|[@/])(sha256:[0-9a-f]{64})$`)
)

type imageDigestState string

const (
	imageDigestPending  imageDigestState = "pending"
	imageDigestResolved imageDigestState = "resolved"
	imageDigestInvalid  imageDigestState = "invalid"
	imageDigestConflict imageDigestState = "conflict"
)

type imageDigestResolution struct {
	state   imageDigestState
	digest  string
	message string
}

func resolvedSourceRevision(app *platformv1alpha1.Application) (string, error) {
	revision, present := app.Annotations[platformv1alpha1.SourceRevisionAnnotationKey]
	if !present {
		return "", nil
	}
	if !sourceRevisionPattern.MatchString(revision) {
		return "", fmt.Errorf("annotation %s must be a full lowercase 40- or 64-character Git object ID", platformv1alpha1.SourceRevisionAnnotationKey)
	}
	return revision, nil
}

func (r *ApplicationReconciler) resolveImageDigest(ctx context.Context, app *platformv1alpha1.Application) (imageDigestResolution, error) {
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(app.Namespace), client.MatchingLabels(resources.SelectorLabels(app))); err != nil {
		return imageDigestResolution{}, fmt.Errorf("list Application Pods: %w", err)
	}
	return resolvePodImageDigest(pods.Items, app.Spec.Image.Repository+":"+app.Spec.Image.Tag), nil
}

func resolvePodImageDigest(pods []corev1.Pod, desiredImage string) imageDigestResolution {
	digests := map[string]struct{}{}
	readyDesiredPods := 0
	missingImageID := false
	for i := range pods {
		pod := &pods[i]
		if !pod.DeletionTimestamp.IsZero() || !podIsReady(pod) || !podImageMatchesDesired(podContainerImage(pod, "application"), desiredImage) {
			continue
		}
		readyDesiredPods++
		containerStatus := podContainerStatus(pod, "application")
		if containerStatus == nil || !containerStatus.Ready || containerStatus.ImageID == "" {
			missingImageID = true
			continue
		}
		digest, ok := normalizeImageDigest(containerStatus.ImageID)
		if !ok {
			return imageDigestResolution{
				state:   imageDigestInvalid,
				message: fmt.Sprintf("ready Pod %s reports malformed imageID %q", pod.Name, containerStatus.ImageID),
			}
		}
		digests[digest] = struct{}{}
	}

	if len(digests) > 1 {
		values := make([]string, 0, len(digests))
		for digest := range digests {
			values = append(values, digest)
		}
		sort.Strings(values)
		return imageDigestResolution{
			state:   imageDigestConflict,
			message: "ready Pods for the desired image report multiple digests: " + strings.Join(values, ", "),
		}
	}
	if readyDesiredPods == 0 || missingImageID || len(digests) == 0 {
		return imageDigestResolution{state: imageDigestPending, message: "waiting for every ready desired Pod to report its runtime image digest"}
	}
	for digest := range digests {
		return imageDigestResolution{state: imageDigestResolved, digest: digest}
	}
	return imageDigestResolution{state: imageDigestPending, message: "waiting for the runtime image digest"}
}

func podImageMatchesDesired(observed, desired string) bool {
	if observed == desired {
		return true
	}
	separator := strings.LastIndex(desired, ":")
	if separator < 0 {
		return false
	}
	return strings.HasPrefix(observed, desired[:separator]+"@sha256:") &&
		imageDigestPattern.MatchString(observed)
}
func podIsReady(pod *corev1.Pod) bool {
	for i := range pod.Status.Conditions {
		condition := pod.Status.Conditions[i]
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func podContainerImage(pod *corev1.Pod, name string) string {
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == name {
			return pod.Spec.Containers[i].Image
		}
	}
	return ""
}

func podContainerStatus(pod *corev1.Pod, name string) *corev1.ContainerStatus {
	for i := range pod.Status.ContainerStatuses {
		if pod.Status.ContainerStatuses[i].Name == name {
			return &pod.Status.ContainerStatuses[i]
		}
	}
	return nil
}

func normalizeImageDigest(imageID string) (string, bool) {
	match := imageDigestPattern.FindStringSubmatch(imageID)
	if len(match) != 3 {
		return "", false
	}
	return match[2], true
}

func applicationRequestForPod(_ context.Context, object client.Object) []ctrlreconcile.Request {
	labels := object.GetLabels()
	name := labels["app.kubernetes.io/instance"]
	if name == "" || labels["app.kubernetes.io/managed-by"] != resources.ManagedBy {
		return nil
	}
	return []ctrlreconcile.Request{{NamespacedName: types.NamespacedName{Name: name, Namespace: object.GetNamespace()}}}
}
