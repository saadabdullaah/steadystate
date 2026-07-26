package database

import (
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
)

func TestValidateDatabase(t *testing.T) {
	valid := validDatabase()
	if err := Validate(valid); err != nil {
		t.Fatalf("valid Database rejected: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*platformv1alpha1.Database)
	}{
		{"engine", func(value *platformv1alpha1.Database) { value.Spec.Engine = "mysql" }},
		{"instances", func(value *platformv1alpha1.Database) { value.Spec.Instances = 4 }},
		{"storage", func(value *platformv1alpha1.Database) { value.Spec.Storage.Size = "512Mi" }},
		{"cron", func(value *platformv1alpha1.Database) { value.Spec.Backups.Schedule = "0 2 * * *" }},
		{"malformed cron", func(value *platformv1alpha1.Database) { value.Spec.Backups.Schedule = "x y z a b c" }},
		{"retention", func(value *platformv1alpha1.Database) { value.Spec.Backups.Retention = "0d" }},
		{"recovery without backups", func(value *platformv1alpha1.Database) {
			value.Spec.Backups.Enabled = false
			value.Spec.Recovery = &platformv1alpha1.DatabaseRecovery{SourceServerName: "old"}
		}},
		{"invalid revision", func(value *platformv1alpha1.Database) {
			value.Annotations = map[string]string{platformv1alpha1.SourceRevisionAnnotationKey: "main"}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := validDatabase()
			test.mutate(value)
			if err := Validate(value); err == nil {
				t.Fatal("invalid Database was accepted")
			}
		})
	}
}

func TestValidateRecoveryUTC(t *testing.T) {
	value := validDatabase()
	target := metav1.NewTime(time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC))
	value.Spec.Recovery = &platformv1alpha1.DatabaseRecovery{SourceServerName: "orders-old", TargetTime: &target}
	if err := Validate(value); err != nil {
		t.Fatalf("valid recovery rejected: %v", err)
	}
}

func TestResolvedSourceRevision(t *testing.T) {
	value := validDatabase()
	if revision, err := ResolvedSourceRevision(value); err != nil || revision != "" {
		t.Fatalf("absent revision = %q, %v", revision, err)
	}
	value.Annotations = map[string]string{platformv1alpha1.SourceRevisionAnnotationKey: strings.Repeat("a", 40)}
	if revision, err := ResolvedSourceRevision(value); err != nil || revision != value.Annotations[platformv1alpha1.SourceRevisionAnnotationKey] {
		t.Fatalf("valid revision = %q, %v", revision, err)
	}
}

func validDatabase() *platformv1alpha1.Database {
	return &platformv1alpha1.Database{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "team-payments"},
		Spec: platformv1alpha1.DatabaseSpec{
			Engine:    "postgres",
			Instances: 1,
			Storage:   platformv1alpha1.DatabaseStorage{Size: "1Gi"},
			Backups: platformv1alpha1.DatabaseBackups{
				Enabled: true, Schedule: "0 0 2 * * *", Retention: "7d",
			},
		},
	}
}
