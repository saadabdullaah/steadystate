// Package team contains Team validation and repository authorization policy.
package team

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"unicode"

	"k8s.io/apimachinery/pkg/api/resource"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

const (
	maxTeamNameLength = 58
	maxOwners         = 32
	maxRepositories   = 32
)

var (
	minimumCPUQuota    = resource.MustParse("500m")
	minimumMemoryQuota = resource.MustParse("512Mi")
	repositoryPattern  = regexp.MustCompile(`^[^@\s]+$`)
)

// Validate performs controller-side semantic validation for objects that bypass CRD admission.
func Validate(team *platformv1alpha1.Team) error {
	if len(team.Name) > maxTeamNameLength {
		return fmt.Errorf("team name must contain at most %d characters", maxTeamNameLength)
	}
	if errors := utilvalidation.IsDNS1123Label(team.Name); len(errors) > 0 {
		return fmt.Errorf("team name must be a DNS label: %s", strings.Join(errors, "; "))
	}
	if err := validateOwners(team.Spec.Owners); err != nil {
		return err
	}
	if team.Spec.Quota.CPU.Cmp(minimumCPUQuota) < 0 {
		return fmt.Errorf("CPU quota must be at least %s", minimumCPUQuota.String())
	}
	if team.Spec.Quota.Memory.Cmp(minimumMemoryQuota) < 0 {
		return fmt.Errorf("memory quota must be at least %s", minimumMemoryQuota.String())
	}
	return validateRepositories(team.Spec.AllowedRepositories)
}

func validateOwners(owners []platformv1alpha1.TeamOwner) error {
	if len(owners) == 0 || len(owners) > maxOwners {
		return fmt.Errorf("owners must contain between 1 and %d Kubernetes usernames", maxOwners)
	}
	seen := make(map[string]struct{}, len(owners))
	for _, owner := range owners {
		value := string(owner)
		if value == "" || len(value) > 253 || strings.IndexFunc(value, unicode.IsSpace) >= 0 {
			return fmt.Errorf("owner %q must be a non-empty Kubernetes username without whitespace", value)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("owner %q is duplicated", value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateRepositories(patterns []platformv1alpha1.RepositoryPattern) error {
	if len(patterns) == 0 || len(patterns) > maxRepositories {
		return fmt.Errorf("allowedRepositories must contain between 1 and %d patterns", maxRepositories)
	}
	seen := make(map[string]struct{}, len(patterns))
	for _, repository := range patterns {
		pattern := string(repository)
		if len(pattern) > 512 || !repositoryPattern.MatchString(pattern) {
			return fmt.Errorf("repository pattern %q must be non-empty and must not contain whitespace or a digest", pattern)
		}
		if _, err := path.Match(pattern, ""); err != nil {
			return fmt.Errorf("repository pattern %q is invalid: %w", pattern, err)
		}
		if _, exists := seen[pattern]; exists {
			return fmt.Errorf("repository pattern %q is duplicated", pattern)
		}
		seen[pattern] = struct{}{}
	}
	return nil
}

// RepositoryAllowed reports whether a repository matches any Team allowlist glob.
// Validate must succeed before this function is called.
func RepositoryAllowed(team *platformv1alpha1.Team, repository string) bool {
	for _, allowed := range team.Spec.AllowedRepositories {
		matched, err := path.Match(string(allowed), repository)
		if err == nil && matched {
			return true
		}
	}
	return false
}
