// Package application contains Application validation and capability policy.
package application

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

var (
	repositoryPattern = regexp.MustCompile(`^[^@\s]+$`)
	tagPattern        = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)
)

// ParsePercentage parses a human-readable percentage and enforces 0-100 inclusive.
func ParsePercentage(value string) (float64, error) {
	if !strings.HasSuffix(value, "%") {
		return 0, fmt.Errorf("percentage %q must end in %%", value)
	}
	parsed, err := strconv.ParseFloat(strings.TrimSuffix(value, "%"), 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed < 0 || parsed > 100 {
		return 0, fmt.Errorf("percentage %q must be between 0%% and 100%%", value)
	}
	return parsed, nil
}

// Validate performs controller-side semantic validation for objects that bypass CRD admission.
func Validate(app *platformv1alpha1.Application) error {
	if strings.TrimSpace(app.Spec.Owner) == "" {
		return fmt.Errorf("owner must not be empty")
	}
	if !repositoryPattern.MatchString(app.Spec.Image.Repository) {
		return fmt.Errorf("image repository must be non-empty and must not contain a digest")
	}
	if !tagPattern.MatchString(app.Spec.Image.Tag) || strings.EqualFold(app.Spec.Image.Tag, "latest") {
		return fmt.Errorf("image tag must be explicit and must not be latest")
	}
	if app.Spec.Runtime.Port < 1 || app.Spec.Runtime.Port > 65535 {
		return fmt.Errorf("runtime port must be between 1 and 65535")
	}
	if app.Spec.Runtime.Replicas.Min < 1 || app.Spec.Runtime.Replicas.Min > 100 || app.Spec.Runtime.Replicas.Max < 1 || app.Spec.Runtime.Replicas.Max > 100 || app.Spec.Runtime.Replicas.Min > app.Spec.Runtime.Replicas.Max {
		return fmt.Errorf("replica bounds must be within 1-100 and min must not exceed max")
	}
	if app.Spec.Resources.Requests.CPU.Sign() <= 0 || app.Spec.Resources.Requests.Memory.Sign() <= 0 || app.Spec.Resources.Limits.CPU.Sign() <= 0 || app.Spec.Resources.Limits.Memory.Sign() <= 0 {
		return fmt.Errorf("CPU and memory requests and limits must be positive")
	}
	if app.Spec.Resources.Requests.CPU.Cmp(app.Spec.Resources.Limits.CPU) > 0 || app.Spec.Resources.Requests.Memory.Cmp(app.Spec.Resources.Limits.Memory) > 0 {
		return fmt.Errorf("resource requests must not exceed limits")
	}
	availability, err := ParsePercentage(string(app.Spec.Reliability.AvailabilityTarget))
	if err != nil || availability <= 0 {
		return fmt.Errorf("availability target must be greater than 0%% and at most 100%%")
	}
	if _, err := ParsePercentage(string(app.Spec.Reliability.MaximumErrorRate)); err != nil {
		return fmt.Errorf("invalid maximum error rate: %w", err)
	}
	if app.Spec.Reliability.MaximumP95Latency.Duration <= 0 {
		return fmt.Errorf("maximum P95 latency must be positive")
	}
	if err := validateDeployment(app); err != nil {
		return err
	}
	return nil
}

func validateDeployment(app *platformv1alpha1.Application) error {
	steps := app.Spec.Deployment.Steps
	switch app.Spec.Deployment.Strategy {
	case platformv1alpha1.DeploymentStrategyRolling:
		if len(steps) != 0 {
			return fmt.Errorf("rolling strategy cannot contain canary steps")
		}
	case platformv1alpha1.DeploymentStrategyCanary:
		if len(steps) == 0 {
			return fmt.Errorf("canary strategy requires steps")
		}
		if !app.Spec.Observability.Metrics {
			return fmt.Errorf("canary strategy requires observability.metrics=true")
		}
	default:
		return fmt.Errorf("unknown deployment strategy %q", app.Spec.Deployment.Strategy)
	}
	if len(steps) > 10 {
		return fmt.Errorf("canary strategy supports at most ten steps")
	}
	previous := int32(0)
	for _, step := range steps {
		if step.Weight < 1 || step.Weight > 100 || step.Weight <= previous {
			return fmt.Errorf("canary weights must be unique, increasing, and between 1 and 100")
		}
		if step.Pause.Duration <= 0 {
			return fmt.Errorf("canary pauses must be positive")
		}
		previous = step.Weight
	}
	return nil
}

// UnsupportedFeatures returns capabilities that are not active in the current platform phase.
func UnsupportedFeatures(app *platformv1alpha1.Application) []string {
	return nil
}

// ParseDuration rejects empty and non-positive human-readable durations.
func ParseDuration(value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("duration %q must be positive", value)
	}
	return duration, nil
}
