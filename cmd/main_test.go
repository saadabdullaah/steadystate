package main

import (
	"testing"

	rolloutsv1alpha1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestManagerPackage(t *testing.T) {
	t.Helper()
}

func TestManagerSchemeIncludesProgressiveDeliveryTypes(t *testing.T) {
	t.Parallel()
	for _, object := range []runtime.Object{&rolloutsv1alpha1.Rollout{}, &rolloutsv1alpha1.AnalysisTemplate{}, &rolloutsv1alpha1.AnalysisRun{}} {
		gvks, _, err := managerScheme.ObjectKinds(object)
		if err != nil {
			t.Fatal(err)
		}
		if len(gvks) != 1 || gvks[0].Group != "argoproj.io" || gvks[0].Version != "v1alpha1" {
			t.Fatalf("unexpected Rollouts GVKs for %T: %#v", object, gvks)
		}
	}
}
