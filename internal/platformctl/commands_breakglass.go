package platformctl

import (
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const breakGlassAuditVersion = "audit.steadystate.dev/v1alpha1"

type BreakGlassAudit struct {
	APIVersion      string         `json:"apiVersion"`
	RequestID       string         `json:"requestId"`
	Timestamp       time.Time      `json:"timestamp"`
	Actor           string         `json:"actor"`
	CLI             BuildInfo      `json:"cli"`
	Context         string         `json:"context"`
	Action          string         `json:"action"`
	Reason          string         `json:"reason"`
	Namespace       string         `json:"namespace"`
	Application     string         `json:"application"`
	TargetUID       string         `json:"targetUid"`
	ResourceVersion string         `json:"resourceVersion"`
	Before          map[string]any `json:"before"`
	After           map[string]any `json:"after,omitempty"`
	Result          string         `json:"result"`
	Error           string         `json:"error,omitempty"`
}

type BreakGlassResult struct {
	RequestID string `json:"requestId" yaml:"requestId"`
	Action    string `json:"action" yaml:"action"`
	Namespace string `json:"namespace" yaml:"namespace"`
	Name      string `json:"name" yaml:"name"`
	Result    string `json:"result" yaml:"result"`
	AuditPath string `json:"auditPath" yaml:"auditPath"`
}

func newBreakGlassCommand(options *Options, action string) *cobra.Command {
	var namespace, reason, confirmation string
	command := &cobra.Command{
		Use: action + " NAME", Short: strings.ToUpper(action[:1]) + action[1:] + " a canary Rollout using confirmed break glass", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if strings.TrimSpace(reason) == "" {
				return exitError(ExitUsage, "--reason must be non-empty")
			}
			if confirmation != name {
				return exitError(ExitUsage, "--confirm must exactly match Application name %q", name)
			}
			config, _, err := options.loadContext()
			if err != nil {
				return err
			}
			contextName := options.ContextName
			if contextName == "" {
				contextName = config.CurrentContext
			}
			_, ns, client, ctx, cancel, err := commandClusterContext(cmd, options, namespace, "Application", name)
			if err != nil {
				return err
			}
			defer cancel()
			application, err := client.Get(ctx, applicationGVR, ns, name)
			if err != nil {
				return err
			}
			strategy, _, _ := unstructured.NestedString(application.Object, "spec", "deployment", "strategy")
			if strategy != "canary" {
				return exitError(ExitUnhealthy, "Application %s/%s does not use canary delivery", ns, name)
			}
			rollout, err := client.Get(ctx, rolloutGVR, ns, name)
			if err != nil {
				return err
			}
			operations, err := breakGlassPatch(action, rollout)
			if err != nil {
				return err
			}
			requestID := uuid.NewString()
			audit := BreakGlassAudit{
				APIVersion: breakGlassAuditVersion, RequestID: requestID, Timestamp: time.Now().UTC(), Actor: localActor(),
				CLI: options.Build, Context: contextName, Action: action, Reason: Redact(strings.TrimSpace(reason)), Namespace: ns,
				Application: name, TargetUID: string(rollout.GetUID()), ResourceVersion: rollout.GetResourceVersion(),
				Before: rolloutState(rollout), Result: "Attempted",
			}
			auditPath, err := writeBreakGlassAudit(options, audit)
			if err != nil {
				return exitError(ExitRemote, "write break-glass audit before mutation: %v", err)
			}
			note := fmt.Sprintf("%s requested for Application %s/%s: %s", action, ns, name, audit.Reason)
			if err := client.RecordRolloutEvent(ctx, rollout, requestID, "Attempted", "PlatformctlBreakGlassAttempted", note, corev1.EventTypeNormal); err != nil {
				audit.Result, audit.Error = "Failed", ErrorMessage(err)
				_, _ = writeBreakGlassAudit(options, audit)
				return err
			}
			updated, err := client.PatchStatus(ctx, rolloutGVR, ns, name, rollout.GetResourceVersion(), operations)
			if err != nil {
				audit.Result, audit.Error = "Failed", ErrorMessage(err)
				_, _ = writeBreakGlassAudit(options, audit)
				_ = client.RecordRolloutEvent(ctx, rollout, requestID, "Failed", "PlatformctlBreakGlassFailed", note+": "+audit.Error, corev1.EventTypeWarning)
				return err
			}
			audit.Result, audit.After = "Completed", rolloutState(updated)
			if _, err := writeBreakGlassAudit(options, audit); err != nil {
				return exitError(ExitRemote, "update completed break-glass audit: %v", err)
			}
			if err := client.RecordRolloutEvent(ctx, updated, requestID, "Completed", "PlatformctlBreakGlassCompleted", note, corev1.EventTypeNormal); err != nil {
				return err
			}
			result := BreakGlassResult{RequestID: requestID, Action: action, Namespace: ns, Name: name, Result: "Completed", AuditPath: auditPath}
			return options.printer().Table([]string{"REQUEST", "ACTION", "TARGET", "RESULT", "AUDIT"}, [][]string{{requestID, action, ns + "/" + name, result.Result, auditPath}}, result)
		},
	}
	command.Flags().StringVarP(&namespace, "namespace", "n", "", "Team namespace")
	command.Flags().StringVar(&reason, "reason", "", "human reason recorded in break-glass audit evidence")
	command.Flags().StringVar(&confirmation, "confirm", "", "exact Application name confirmation")
	_ = command.MarkFlagRequired("reason")
	_ = command.MarkFlagRequired("confirm")
	return command
}

func breakGlassPatch(action string, rollout *unstructured.Unstructured) ([]map[string]any, error) {
	if rollout.GetResourceVersion() == "" || rollout.GetUID() == "" {
		return nil, exitError(ExitConflict, "Rollout identity is incomplete; read it again")
	}
	canary, found, _ := unstructured.NestedMap(rollout.Object, "spec", "strategy", "canary")
	if !found || canary == nil {
		return nil, exitError(ExitUnhealthy, "Rollout %s/%s is not canary", rollout.GetNamespace(), rollout.GetName())
	}
	status, found, _ := unstructured.NestedMap(rollout.Object, "status")
	if !found {
		return nil, exitError(ExitUnhealthy, "Rollout %s/%s has no controller status", rollout.GetNamespace(), rollout.GetName())
	}
	switch action {
	case "abort":
		if aborted, _ := status["abort"].(bool); aborted {
			return nil, exitError(ExitUnhealthy, "Rollout %s/%s is already aborted", rollout.GetNamespace(), rollout.GetName())
		}
		current, _ := status["currentPodHash"].(string)
		stable, _ := status["stableRS"].(string)
		if current == "" || current == stable {
			return nil, exitError(ExitUnhealthy, "Rollout %s/%s has no active candidate to abort", rollout.GetNamespace(), rollout.GetName())
		}
		return []map[string]any{{"op": "add", "path": "/status/abort", "value": true}}, nil
	case "promote":
		if paused, _, _ := unstructured.NestedBool(rollout.Object, "spec", "paused"); paused {
			return nil, exitError(ExitUnhealthy, "Rollout %s/%s is spec-paused; use a reviewed Git change", rollout.GetNamespace(), rollout.GetName())
		}
		pauseConditions, _, _ := unstructured.NestedSlice(rollout.Object, "status", "pauseConditions")
		controllerPause, _, _ := unstructured.NestedBool(rollout.Object, "status", "controllerPause")
		step, hasStep, _ := unstructured.NestedInt64(rollout.Object, "status", "currentStepIndex")
		if len(pauseConditions) == 0 && !controllerPause && !hasStep {
			return nil, exitError(ExitUnhealthy, "Rollout %s/%s is not paused at a promotable canary step", rollout.GetNamespace(), rollout.GetName())
		}
		operations := []map[string]any{{"op": "add", "path": "/status/pauseConditions", "value": nil}}
		analysisPhase, _, _ := unstructured.NestedString(rollout.Object, "status", "canary", "currentStepAnalysisRunStatus", "status")
		if controllerPause {
			operations = append(operations, map[string]any{"op": "add", "path": "/status/controllerPause", "value": false})
		}
		if hasStep && ((controllerPause && analysisPhase == "Inconclusive") || len(pauseConditions) == 0) {
			operations = append(operations, map[string]any{"op": "add", "path": "/status/currentStepIndex", "value": step + 1})
		}
		return operations, nil
	default:
		return nil, exitError(ExitUsage, "unsupported break-glass action %q", action)
	}
}

func rolloutState(rollout *unstructured.Unstructured) map[string]any {
	status, _, _ := unstructured.NestedMap(rollout.Object, "status")
	return map[string]any{
		"resourceVersion": rollout.GetResourceVersion(), "generation": rollout.GetGeneration(), "phase": status["phase"],
		"currentStepIndex": status["currentStepIndex"], "currentPodHash": status["currentPodHash"], "stableRS": status["stableRS"],
		"abort": status["abort"], "pauseConditions": status["pauseConditions"],
	}
}

func writeBreakGlassAudit(options *Options, audit BreakGlassAudit) (string, error) {
	directory := options.AuditDir
	if directory == "" {
		configPath, err := options.configPath()
		if err != nil {
			return "", err
		}
		directory = filepath.Join(filepath.Dir(configPath), "audit")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(audit, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(directory, audit.RequestID+".json")
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func localActor() string {
	for _, key := range []string{"GITHUB_ACTOR", "USER", "USERNAME"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return Redact(value)
		}
	}
	if current, err := user.Current(); err == nil && current.Username != "" {
		return Redact(current.Username)
	}
	return "unknown"
}
