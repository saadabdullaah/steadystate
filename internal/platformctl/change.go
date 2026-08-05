package platformctl

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	platformv1alpha1 "github.com/saadabdullaah/steadystate/api/v1alpha1"
	applicationvalidation "github.com/saadabdullaah/steadystate/internal/application"
	databasevalidation "github.com/saadabdullaah/steadystate/internal/database"
	teamvalidation "github.com/saadabdullaah/steadystate/internal/team"
)

const (
	ChangeRequestAPIVersion = "cli.steadystate.dev/v1alpha1"
	ChangeRequestKind       = "ChangeRequest"
	ChangeRequestSchema     = "v1alpha1"
	RendererVersion         = "v0.8.0"
	MaxProposalBase64Bytes  = 48 * 1024
)

var gitObjectPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

type ChangeRequest struct {
	APIVersion      string           `json:"apiVersion" yaml:"apiVersion"`
	Kind            string           `json:"kind" yaml:"kind"`
	SchemaVersion   string           `json:"schemaVersion" yaml:"schemaVersion"`
	RequestID       string           `json:"requestID" yaml:"requestID"`
	BaseSHA         string           `json:"baseSHA" yaml:"baseSHA"`
	RendererVersion string           `json:"rendererVersion" yaml:"rendererVersion"`
	Operation       string           `json:"operation" yaml:"operation"`
	Parameters      ChangeParameters `json:"parameters" yaml:"parameters"`
}

type ChangeParameters struct {
	Team                string   `json:"team,omitempty" yaml:"team,omitempty"`
	Name                string   `json:"name,omitempty" yaml:"name,omitempty"`
	Owners              []string `json:"owners,omitempty" yaml:"owners,omitempty"`
	AllowedRepositories []string `json:"allowedRepositories,omitempty" yaml:"allowedRepositories,omitempty"`
	CPUQuota            string   `json:"cpuQuota,omitempty" yaml:"cpuQuota,omitempty"`
	MemoryQuota         string   `json:"memoryQuota,omitempty" yaml:"memoryQuota,omitempty"`
	Owner               string   `json:"owner,omitempty" yaml:"owner,omitempty"`
	ImageRepository     string   `json:"imageRepository,omitempty" yaml:"imageRepository,omitempty"`
	ImageTag            string   `json:"imageTag,omitempty" yaml:"imageTag,omitempty"`
	Port                int32    `json:"port,omitempty" yaml:"port,omitempty"`
	MinReplicas         int32    `json:"minReplicas,omitempty" yaml:"minReplicas,omitempty"`
	MaxReplicas         int32    `json:"maxReplicas,omitempty" yaml:"maxReplicas,omitempty"`
	DatabaseRef         string   `json:"databaseRef,omitempty" yaml:"databaseRef,omitempty"`
	Instances           int32    `json:"instances,omitempty" yaml:"instances,omitempty"`
	StorageSize         string   `json:"storageSize,omitempty" yaml:"storageSize,omitempty"`
	BackupSchedule      string   `json:"backupSchedule,omitempty" yaml:"backupSchedule,omitempty"`
	BackupRetention     string   `json:"backupRetention,omitempty" yaml:"backupRetention,omitempty"`
	SourceServerName    string   `json:"sourceServerName,omitempty" yaml:"sourceServerName,omitempty"`
	TargetTime          string   `json:"targetTime,omitempty" yaml:"targetTime,omitempty"`
	DeletionRequest     string   `json:"deletionRequest,omitempty" yaml:"deletionRequest,omitempty"`
	ApprovalRevision    string   `json:"approvalRevision,omitempty" yaml:"approvalRevision,omitempty"`
	Force               bool     `json:"force,omitempty" yaml:"force,omitempty"`
	AcknowledgeDataLoss bool     `json:"acknowledgeDataLoss,omitempty" yaml:"acknowledgeDataLoss,omitempty"`
}

func NewChangeRequest(operation, baseSHA string, parameters ChangeParameters) ChangeRequest {
	return ChangeRequest{
		APIVersion: ChangeRequestAPIVersion, Kind: ChangeRequestKind, SchemaVersion: ChangeRequestSchema,
		RequestID: uuid.NewString(), BaseSHA: baseSHA, RendererVersion: RendererVersion,
		Operation: operation, Parameters: parameters,
	}
}

func DecodeChangeRequest(encoded string) (ChangeRequest, error) {
	if len(encoded) == 0 || len(encoded) > MaxProposalBase64Bytes {
		return ChangeRequest{}, exitError(ExitUsage, "proposal must be between 1 and %d Base64 bytes", MaxProposalBase64Bytes)
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return ChangeRequest{}, exitError(ExitUsage, "proposal is not strict Base64: %v", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var request ChangeRequest
	if err := decoder.Decode(&request); err != nil {
		return ChangeRequest{}, exitError(ExitUsage, "decode proposal: %v", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ChangeRequest{}, exitError(ExitUsage, "proposal contains trailing JSON")
	}
	if err := request.Validate(); err != nil {
		return ChangeRequest{}, err
	}
	return request, nil
}

func (r ChangeRequest) Encode() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	if len(encoded) > MaxProposalBase64Bytes {
		return "", exitError(ExitUsage, "proposal exceeds the %d-byte Base64 limit", MaxProposalBase64Bytes)
	}
	return encoded, nil
}

func (r ChangeRequest) Digest() (string, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (r ChangeRequest) Validate() error {
	if r.APIVersion != ChangeRequestAPIVersion || r.Kind != ChangeRequestKind || r.SchemaVersion != ChangeRequestSchema {
		return exitError(ExitUsage, "unsupported ChangeRequest schema; upgrade platformctl to match %s", ChangeRequestSchema)
	}
	if _, err := uuid.Parse(r.RequestID); err != nil {
		return exitError(ExitUsage, "request ID must be a UUID")
	}
	if !gitObjectPattern.MatchString(r.BaseSHA) {
		return exitError(ExitUsage, "base SHA must be a full lowercase Git object ID")
	}
	if r.RendererVersion != RendererVersion {
		return exitError(ExitConflict, "renderer %q does not match %q", r.RendererVersion, RendererVersion)
	}
	allowed := map[string]bool{
		"team.create": true, "team.update": true, "team.delete": true, "team.finalize": true,
		"app.create": true, "app.update": true, "app.delete": true, "app.finalize": true,
		"database.create": true, "database.update": true, "database.restore": true,
		"database.delete": true, "database.finalize": true,
	}
	if !allowed[r.Operation] {
		return exitError(ExitUsage, "unsupported operation %q", r.Operation)
	}
	if !validName(r.Parameters.Name, 63) || (r.Parameters.Team != "" && !validName(r.Parameters.Team, 58)) {
		return exitError(ExitUsage, "resource and Team names must be valid DNS labels")
	}
	if strings.HasPrefix(r.Operation, "team.") && r.Parameters.Team != "" {
		return exitError(ExitUsage, "Team operations use name, not team")
	}
	if !strings.HasPrefix(r.Operation, "team.") && !validName(r.Parameters.Team, 58) {
		return exitError(ExitUsage, "non-Team operations require a valid team")
	}
	if strings.HasSuffix(r.Operation, ".finalize") {
		if _, err := uuid.Parse(r.Parameters.DeletionRequest); err != nil {
			return exitError(ExitUsage, "finalization requires the approval deletion-request UUID")
		}
		if !gitObjectPattern.MatchString(r.Parameters.ApprovalRevision) {
			return exitError(ExitUsage, "finalization requires the merged approval revision")
		}
	}
	if r.Parameters.Force != r.Parameters.AcknowledgeDataLoss {
		return exitError(ExitUsage, "force deletion requires both --force and --acknowledge-data-loss")
	}
	if strings.HasSuffix(r.Operation, ".create") || strings.HasSuffix(r.Operation, ".update") || r.Operation == "database.restore" {
		return r.validateDesiredState()
	}
	return nil
}

func (r ChangeRequest) validateDesiredState() error {
	p := r.Parameters
	switch {
	case strings.HasPrefix(r.Operation, "team."):
		if len(p.Owners) == 0 || len(p.AllowedRepositories) == 0 || p.CPUQuota == "" || p.MemoryQuota == "" {
			return exitError(ExitUsage, "Team desired state requires owners, allowed repositories, CPU quota, and memory quota")
		}
		cpu, cpuErr := resource.ParseQuantity(p.CPUQuota)
		memory, memoryErr := resource.ParseQuantity(p.MemoryQuota)
		if cpuErr != nil || memoryErr != nil {
			return exitError(ExitUsage, "Team quota values must be Kubernetes quantities")
		}
		owners := make([]platformv1alpha1.TeamOwner, 0, len(p.Owners))
		for _, value := range p.Owners {
			owners = append(owners, platformv1alpha1.TeamOwner(value))
		}
		repositories := make([]platformv1alpha1.RepositoryPattern, 0, len(p.AllowedRepositories))
		for _, value := range p.AllowedRepositories {
			repositories = append(repositories, platformv1alpha1.RepositoryPattern(value))
		}
		team := &platformv1alpha1.Team{ObjectMeta: metav1.ObjectMeta{Name: p.Name}, Spec: platformv1alpha1.TeamSpec{Owners: owners, Quota: platformv1alpha1.TeamQuota{CPU: cpu, Memory: memory}, AllowedRepositories: repositories}}
		if err := teamvalidation.Validate(team); err != nil {
			return exitError(ExitUsage, "invalid Team desired state: %v", err)
		}
	case strings.HasPrefix(r.Operation, "app."):
		if p.Owner == "" || p.ImageRepository == "" || strings.ContainsAny(p.ImageRepository, "@ \t\r\n") || p.ImageTag == "" || strings.EqualFold(p.ImageTag, "latest") {
			return exitError(ExitUsage, "Application desired state requires an owner and an explicit non-latest image")
		}
		application := &platformv1alpha1.Application{Spec: platformv1alpha1.ApplicationSpec{
			Owner: p.Owner, Image: platformv1alpha1.ApplicationImage{Repository: p.ImageRepository, Tag: p.ImageTag},
			Runtime:       platformv1alpha1.ApplicationRuntime{Port: p.Port, Replicas: platformv1alpha1.ReplicaBounds{Min: p.MinReplicas, Max: p.MaxReplicas}},
			Resources:     platformv1alpha1.ApplicationResources{Requests: platformv1alpha1.ResourceValues{CPU: resource.MustParse("50m"), Memory: resource.MustParse("32Mi")}, Limits: platformv1alpha1.ResourceValues{CPU: resource.MustParse("200m"), Memory: resource.MustParse("128Mi")}},
			Deployment:    platformv1alpha1.ApplicationDeployment{Strategy: platformv1alpha1.DeploymentStrategyCanary, AutomaticRollback: true, Steps: []platformv1alpha1.CanaryStep{{Weight: 10, Pause: metav1.Duration{Duration: 30 * time.Second}}, {Weight: 25, Pause: metav1.Duration{Duration: 30 * time.Second}}, {Weight: 50, Pause: metav1.Duration{Duration: 30 * time.Second}}, {Weight: 100, Pause: metav1.Duration{Duration: 30 * time.Second}}}},
			Reliability:   platformv1alpha1.ReliabilityTargets{AvailabilityTarget: "99.9%", MaximumP95Latency: metav1.Duration{Duration: 250 * time.Millisecond}, MaximumErrorRate: "1%"},
			Observability: platformv1alpha1.ObservabilityOptions{Metrics: true, Logs: true, Traces: true}, Security: platformv1alpha1.SecurityOptions{RequireSignedImage: true, RunAsNonRoot: true, NetworkIsolation: true},
		}}
		if err := applicationvalidation.Validate(application); err != nil {
			return exitError(ExitUsage, "invalid Application desired state: %v", err)
		}
	case strings.HasPrefix(r.Operation, "database."):
		if p.Instances < 1 || p.Instances > 3 || p.StorageSize == "" || p.BackupSchedule == "" || p.BackupRetention == "" {
			return exitError(ExitUsage, "Database desired state requires valid instances, storage, schedule, and retention")
		}
		database := &platformv1alpha1.Database{Spec: platformv1alpha1.DatabaseSpec{Engine: "postgres", Instances: p.Instances, Storage: platformv1alpha1.DatabaseStorage{Size: p.StorageSize}, Backups: platformv1alpha1.DatabaseBackups{Enabled: true, Schedule: p.BackupSchedule, Retention: p.BackupRetention}}}
		if r.Operation == "database.restore" {
			recovery := &platformv1alpha1.DatabaseRecovery{SourceServerName: p.SourceServerName}
			if p.TargetTime != "" {
				parsed, err := time.Parse(time.RFC3339, p.TargetTime)
				if err != nil || parsed.Location() != time.UTC {
					return exitError(ExitUsage, "recovery target time must be RFC3339 UTC")
				}
				value := metav1.NewTime(parsed)
				recovery.TargetTime = &value
			}
			database.Spec.Recovery = recovery
		}
		if err := databasevalidation.Validate(database); err != nil {
			return exitError(ExitUsage, "invalid Database desired state: %v", err)
		}
	}
	return nil
}

func normalizeStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}
