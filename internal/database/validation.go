// Package database contains Database semantic validation.
package database

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"k8s.io/apimachinery/pkg/api/resource"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

var (
	retentionPattern = regexp.MustCompile(`^[1-9][0-9]*[dwm]$`)
	gitObjectPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)
)

// Validate rejects invalid Database configuration before child mutation.
func Validate(database *platformv1alpha1.Database) error {
	if database.Spec.Engine != "postgres" {
		return fmt.Errorf("engine must be postgres")
	}
	if database.Spec.Instances < 1 || database.Spec.Instances > 3 {
		return fmt.Errorf("instances must be between 1 and 3")
	}
	size, err := resource.ParseQuantity(database.Spec.Storage.Size)
	if err != nil || size.Cmp(resource.MustParse("1Gi")) < 0 {
		return fmt.Errorf("storage size must be a quantity of at least 1Gi")
	}
	if err := validateCron(database.Spec.Backups.Schedule); err != nil {
		return err
	}
	if !retentionPattern.MatchString(database.Spec.Backups.Retention) {
		return fmt.Errorf("backup retention must match <positive integer><d|w|m>")
	}
	if database.Spec.Recovery != nil {
		if !database.Spec.Backups.Enabled {
			return fmt.Errorf("recovery requires backups to be enabled")
		}
		if strings.TrimSpace(database.Spec.Recovery.SourceServerName) == "" {
			return fmt.Errorf("recovery sourceServerName must not be empty")
		}
		if database.Spec.Recovery.TargetTime != nil {
			target := database.Spec.Recovery.TargetTime.Time
			if target.Location() != time.UTC || target.Format(time.RFC3339) != database.Spec.Recovery.TargetTime.Format(time.RFC3339) {
				return fmt.Errorf("recovery targetTime must be RFC3339 UTC")
			}
		}
	}
	if _, err := ResolvedSourceRevision(database); err != nil {
		return err
	}
	return nil
}

// ResolvedSourceRevision returns an optional validated full Git object ID.
func ResolvedSourceRevision(database *platformv1alpha1.Database) (string, error) {
	revision := database.Annotations[platformv1alpha1.SourceRevisionAnnotationKey]
	if revision != "" && !gitObjectPattern.MatchString(revision) {
		return "", fmt.Errorf("source revision must be a full lowercase SHA-1 or SHA-256 Git object ID")
	}
	return revision, nil
}

func validateCron(schedule string) error {
	if len(strings.Fields(schedule)) != 6 {
		return fmt.Errorf("backup schedule must contain exactly six cron fields including seconds")
	}
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(schedule); err != nil {
		return fmt.Errorf("backup schedule is invalid: %w", err)
	}
	return nil
}
