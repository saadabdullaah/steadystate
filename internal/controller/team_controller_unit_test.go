package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
	"github.com/saadabdullaah/steadystate/internal/resources"
)

func TestTeamRequestsForObject(t *testing.T) {
	t.Parallel()

	object := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{resources.TeamLabelKey: "payments"}}}
	requests := teamRequestsForObject(context.Background(), object)
	if len(requests) != 1 || requests[0].NamespacedName != (types.NamespacedName{Name: "payments"}) {
		t.Fatalf("unexpected Team requests: %#v", requests)
	}

	object.Labels = nil
	if requests := teamRequestsForObject(context.Background(), object); requests != nil {
		t.Fatalf("unmanaged object enqueued Team requests: %#v", requests)
	}
}

func TestTeamOwnershipRequiresMatchingUID(t *testing.T) {
	t.Parallel()

	team := &platformv1alpha1.Team{ObjectMeta: metav1.ObjectMeta{Name: "payments", UID: types.UID("team-uid")}}
	object := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Labels:      map[string]string{resources.TeamLabelKey: team.Name},
		Annotations: map[string]string{resources.TeamUIDAnnotationKey: string(team.UID)},
	}}
	if !ownedByTeamUID(object, team) || !managedByTeam(object, team) {
		t.Fatal("matching Team ownership was not recognized")
	}

	delete(object.Labels, resources.TeamLabelKey)
	if !ownedByTeamUID(object, team) {
		t.Fatal("label drift must remain repairable when the Team UID matches")
	}
	if managedByTeam(object, team) {
		t.Fatal("finalization ownership must require both the Team label and UID")
	}
}
