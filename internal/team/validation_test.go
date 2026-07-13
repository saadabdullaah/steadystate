package team

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := map[string]func(*platformv1alpha1.Team){
		"name exceeds namespace-safe length": func(team *platformv1alpha1.Team) { team.Name = strings.Repeat("a", 59) },
		"name is not a DNS label":            func(team *platformv1alpha1.Team) { team.Name = "Payments" },
		"owners are empty":                   func(team *platformv1alpha1.Team) { team.Spec.Owners = nil },
		"owner contains whitespace":          func(team *platformv1alpha1.Team) { team.Spec.Owners = []platformv1alpha1.TeamOwner{"payments owner"} },
		"owner is duplicated":                func(team *platformv1alpha1.Team) { team.Spec.Owners = []platformv1alpha1.TeamOwner{"owner", "owner"} },
		"CPU quota is too small":             func(team *platformv1alpha1.Team) { team.Spec.Quota.CPU = resource.MustParse("499m") },
		"memory quota is too small":          func(team *platformv1alpha1.Team) { team.Spec.Quota.Memory = resource.MustParse("511Mi") },
		"repositories are empty":             func(team *platformv1alpha1.Team) { team.Spec.AllowedRepositories = nil },
		"repository contains a digest": func(team *platformv1alpha1.Team) {
			team.Spec.AllowedRepositories = []platformv1alpha1.RepositoryPattern{"example.test/app@sha256:deadbeef"}
		},
		"repository glob is malformed": func(team *platformv1alpha1.Team) {
			team.Spec.AllowedRepositories = []platformv1alpha1.RepositoryPattern{"example.test/[app"}
		},
		"repository is duplicated": func(team *platformv1alpha1.Team) {
			team.Spec.AllowedRepositories = []platformv1alpha1.RepositoryPattern{"example.test/app", "example.test/app"}
		},
	}

	if err := Validate(validTeam()); err != nil {
		t.Fatalf("valid Team rejected: %v", err)
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			team := validTeam()
			mutate(team)
			if err := Validate(team); err == nil {
				t.Fatal("invalid Team was accepted")
			}
		})
	}
}

func TestRepositoryAllowedUsesAnchoredSlashAwareGlobs(t *testing.T) {
	t.Parallel()
	team := validTeam()
	team.Spec.AllowedRepositories = []platformv1alpha1.RepositoryPattern{
		"ghcr.io/saadabdullaah/steadystate-demo-app",
		"ghcr.io/saadabdullaah/payments-*",
		"ghcr.io/single/*",
	}

	tests := map[string]bool{
		"ghcr.io/saadabdullaah/steadystate-demo-app": true,
		"ghcr.io/saadabdullaah/payments-api":         true,
		"ghcr.io/saadabdullaah/orders-api":           false,
		"ghcr.io/single/app":                         true,
		"ghcr.io/single/nested/app":                  false,
		"GHCR.IO/saadabdullaah/payments-api":         false,
		"prefix-ghcr.io/saadabdullaah/payments-api":  false,
	}
	for repository, want := range tests {
		if got := RepositoryAllowed(team, repository); got != want {
			t.Errorf("RepositoryAllowed(%q)=%t, want %t", repository, got, want)
		}
	}
}

func validTeam() *platformv1alpha1.Team {
	return &platformv1alpha1.Team{
		ObjectMeta: metav1.ObjectMeta{Name: "payments"},
		Spec: platformv1alpha1.TeamSpec{
			Owners: []platformv1alpha1.TeamOwner{"payments-owner"},
			Quota: platformv1alpha1.TeamQuota{
				CPU:    resource.MustParse("2"),
				Memory: resource.MustParse("2Gi"),
			},
			AllowedRepositories: []platformv1alpha1.RepositoryPattern{"ghcr.io/saadabdullaah/payments-*"},
		},
	}
}
