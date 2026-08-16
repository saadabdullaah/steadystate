package platformctl

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type platformStage struct {
	Name    string        `json:"name" yaml:"name"`
	Command string        `json:"command" yaml:"command"`
	Timeout time.Duration `json:"timeout" yaml:"timeout"`
}

type platformLifecycleResult struct {
	Action      string          `json:"action" yaml:"action"`
	Profile     string          `json:"profile" yaml:"profile"`
	GitRevision string          `json:"gitRevision,omitempty" yaml:"gitRevision,omitempty"`
	Stages      []platformStage `json:"stages" yaml:"stages"`
	State       string          `json:"state" yaml:"state"`
}

const platformProgressInterval = 30 * time.Second

type stageLogWriter struct {
	mu          sync.Mutex
	outputMu    *sync.Mutex
	destination io.Writer
	prefix      string
	pending     string
	tail        []string
}

func newStageLogWriter(destination io.Writer, outputMu *sync.Mutex, prefix string) *stageLogWriter {
	return &stageLogWriter{destination: destination, outputMu: outputMu, prefix: prefix}
}

func (writer *stageLogWriter) Write(value []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.pending += strings.ReplaceAll(strings.ReplaceAll(string(value), "\r\n", "\n"), "\r", "\n")
	for {
		separator := strings.IndexByte(writer.pending, '\n')
		if separator < 0 {
			break
		}
		writer.emitLocked(writer.pending[:separator])
		writer.pending = writer.pending[separator+1:]
	}
	return len(value), nil
}

func (writer *stageLogWriter) Flush() {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.pending != "" {
		writer.emitLocked(writer.pending)
		writer.pending = ""
	}
}

func (writer *stageLogWriter) Tail() string {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return strings.Join(writer.tail, "\n")
}

func (writer *stageLogWriter) emitLocked(line string) {
	line = strings.TrimSpace(Redact(line))
	if line == "" {
		return
	}
	writer.outputMu.Lock()
	_, _ = fmt.Fprintf(writer.destination, "[%s] %s\n", writer.prefix, line)
	writer.outputMu.Unlock()
	writer.tail = append(writer.tail, line)
	if len(writer.tail) > 20 {
		writer.tail = writer.tail[len(writer.tail)-20:]
	}
}

func newPlatformCommand(options *Options) *cobra.Command {
	command := &cobra.Command{Use: "platform", Short: "Manage the complete local SteadyState platform"}
	command.AddCommand(newPlatformLifecycleCommand(options, "up"))
	command.AddCommand(newPlatformLifecycleCommand(options, "down"))
	command.AddCommand(newPlatformStatusCommand(options))
	command.AddCommand(newPlatformVerifyCommand(options))
	return command
}

func newPlatformLifecycleCommand(options *Options, action string) *cobra.Command {
	return &cobra.Command{
		Use: action, Short: map[string]string{"up": "Reconcile the complete configured platform", "down": "Stop the exact configured platform while retaining backups"}[action], Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
				return exitError(ExitUsage, "complete platform lifecycle is supported on Windows and Linux")
			}
			_, selected, err := options.loadContext()
			if err != nil {
				return err
			}
			stages := platformUpStagesForEnvironment(selected.Profile, hostedLinuxLifecycle())
			if action == "down" {
				stages = platformDownStages(selected.Profile)
			}
			gitRevision := ""
			if action == "up" {
				if err := runPlatformUpPreflight(cmd.Context(), options, selected); err != nil {
					return err
				}
				gitRevision, err = checkoutHeadRevision(cmd.Context(), selected.CheckoutPath)
				if err != nil {
					return err
				}
			}
			result := platformLifecycleResult{Action: action, Profile: selected.Profile, GitRevision: gitRevision, Stages: stages, State: "Completed"}
			errors := []string{}
			for _, stage := range stages {
				started := time.Now()
				_, _ = fmt.Fprintf(options.Stderr, "[%s] Starting %s (timeout %s)\n", started.UTC().Format(time.RFC3339), stage.Name, stage.Timeout)
				stageCtx, cancel := context.WithTimeout(cmd.Context(), stage.Timeout)
				arguments := []string{"-NoProfile", "-File", selected.CheckoutPath + "/scripts/dev.ps1", stage.Command, "-Profile", selected.Profile, "-ClusterName", selected.ClusterName, "-HttpPort", fmt.Sprint(selected.HTTPPort), "-HttpsPort", fmt.Sprint(selected.HTTPSPort)}
				if gitRevision != "" {
					arguments = append(arguments, "-GitRevision", gitRevision)
				}
				stageErr := runPlatformStage(stageCtx, options, selected.CheckoutPath, stage, arguments)
				cancel()
				if stageErr != nil {
					errors = append(errors, stage.Name+": "+ErrorMessage(stageErr))
					if action == "up" {
						result.State = "Failed"
						return exitError(ExitRemote, "platform up stopped at %s; the environment was preserved for diagnosis: %s", stage.Name, strings.Join(errors, "; "))
					}
				} else if !options.Quiet {
					_, _ = fmt.Fprintf(options.Stderr, "[%s] Completed %s in %s\n", time.Now().UTC().Format(time.RFC3339), stage.Name, time.Since(started).Round(time.Second))
				}
			}
			if len(errors) > 0 {
				result.State = "Failed"
				return exitError(ExitRemote, "platform down completed with errors: %s", strings.Join(errors, "; "))
			}
			if action == "up" && !options.Quiet {
				_, _ = fmt.Fprintln(options.Stderr, "Platform is ready. Run 'platformctl portal'.")
			}
			return options.printer().Table([]string{"ACTION", "PROFILE", "REVISION", "STATE"}, [][]string{{action, selected.Profile, gitRevision, result.State}}, result)
		},
	}
}

var platformUpBlockingChecks = map[string]bool{
	"checkout":            true,
	"docker":              true,
	"git-repository":      true,
	"github-app-variable": true,
	"github-auth":         true,
	"github-cli-version":  true,
	"github-secrets":      true,
	"operating-system":    true,
	"resource-budget":     true,
	"tool-docker":         true,
	"tool-gh":             true,
	"tool-git":            true,
	"tool-pwsh":           true,
}

func runPlatformUpPreflight(parent context.Context, options *Options, selected Context) error {
	_, _ = fmt.Fprintf(options.Stderr, "[%s] Validating local prerequisites for the %s profile\n", time.Now().UTC().Format(time.RFC3339), selected.Profile)
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	checks := runDoctor(ctx, selected)
	failures := []string{}
	warnings := 0
	for _, check := range checks {
		blocking := platformUpBlockingChecks[check.Name] || strings.HasPrefix(check.Name, "full-profile-")
		if check.Status == "Fail" && blocking {
			failures = append(failures, fmt.Sprintf("%s: %s (%s)", check.Name, check.Details, check.Remediation))
		}
		if check.Status == "Warning" {
			warnings++
		}
	}
	if len(failures) > 0 {
		return exitError(ExitUnhealthy, "platform preflight failed before changing the environment: %s", strings.Join(failures, "; "))
	}
	if !options.Quiet {
		_, _ = fmt.Fprintf(options.Stderr, "[%s] Preflight passed (%d non-blocking warnings); pinned tools will be installed next\n", time.Now().UTC().Format(time.RFC3339), warnings)
	}
	return nil
}

func runPlatformStage(ctx context.Context, options *Options, checkoutPath string, stage platformStage, arguments []string) error {
	powerShell, err := powerShellExecutable()
	if err != nil {
		return err
	}
	// #nosec G204 -- executable and arguments are fixed lifecycle contracts.
	command := exec.Command(powerShell, powerShellArguments(powerShell, arguments)...)
	command.Dir = checkoutPath
	destination := options.Stderr
	if options.Quiet {
		destination = io.Discard
	}
	outputMu := &sync.Mutex{}
	stdout := newStageLogWriter(destination, outputMu, stage.Command)
	stderr := newStageLogWriter(destination, outputMu, stage.Command)
	command.Stdout = stdout
	command.Stderr = stderr

	done := make(chan struct{})
	if !options.Quiet {
		started := time.Now()
		go func() {
			ticker := time.NewTicker(platformProgressInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					outputMu.Lock()
					_, _ = fmt.Fprintf(destination, "[%s] %s still running (%s elapsed; timeout %s)\n", time.Now().UTC().Format(time.RFC3339), stage.Name, time.Since(started).Round(time.Second), stage.Timeout)
					outputMu.Unlock()
				case <-done:
					return
				}
			}
		}()
	}

	err = runCommandInProcessTree(ctx, command)
	close(done)
	stdout.Flush()
	stderr.Flush()
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return exitError(ExitTimeout, "%s timed out after %s", stage.Name, stage.Timeout)
	}
	message := strings.TrimSpace(stderr.Tail())
	if message == "" {
		message = strings.TrimSpace(stdout.Tail())
	}
	if message == "" {
		message = err.Error()
	}
	return fmt.Errorf("%s", Redact(message))
}

func powerShellExecutable() (string, error) {
	return selectPowerShellExecutable(runtime.GOOS, exec.LookPath)
}

func selectPowerShellExecutable(goos string, lookPath func(string) (string, error)) (string, error) {
	if executable, err := lookPath("pwsh"); err == nil {
		return executable, nil
	}
	if goos == "windows" {
		if executable, err := lookPath("powershell.exe"); err == nil {
			return executable, nil
		}
		return "", exitError(ExitUnhealthy, "PowerShell 7 or Windows PowerShell 5.1 is required")
	}
	return "", exitError(ExitUnhealthy, "PowerShell 7 (pwsh) is required")
}

func powerShellArguments(executable string, arguments []string) []string {
	// filepath.Base follows the host OS. Normalize Windows separators first so
	// the Windows command contract remains testable on Linux CI runners.
	base := filepath.Base(strings.ReplaceAll(executable, `\`, "/"))
	if strings.EqualFold(base, "powershell.exe") {
		result := []string{"-NoProfile", "-ExecutionPolicy", "Bypass"}
		if len(arguments) > 0 && strings.EqualFold(arguments[0], "-NoProfile") {
			arguments = arguments[1:]
		}
		return append(result, arguments...)
	}
	return arguments
}

func checkoutHeadRevision(ctx context.Context, checkoutPath string) (string, error) {
	revisionCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	revision, err := runExternal(revisionCtx, checkoutPath, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", exitError(ExitRemote, "resolve exact checkout revision: %v", err)
	}
	if !gitObjectPattern.MatchString(revision) {
		return "", exitError(ExitRemote, "Git returned an invalid checkout revision")
	}
	return revision, nil
}

func platformUpStages(profile string) []platformStage {
	return platformUpStagesForEnvironment(profile, false)
}

func hostedLinuxLifecycle() bool {
	return os.Getenv("GITHUB_ACTIONS") == "true" && os.Getenv("RUNNER_OS") == "Linux"
}

func platformUpStagesForEnvironment(profile string, hostedLinux bool) []platformStage {
	// Build before starting kind so cold Go compilation gets the host's CPU and
	// memory instead of competing with four Kubernetes nodes. The build script
	// is content-addressed, so exact retries become a fast cache hit.
	stages := []platformStage{{"Install pinned tools", "tools", 15 * time.Minute}, {"Verify pinned tools", "check-versions", 5 * time.Minute}, {"Build platform images", "build-images", 25 * time.Minute}, {"Bootstrap cluster", "bootstrap", 20 * time.Minute}}
	if profile == "full" && hostedLinux {
		// The full hosted stack can starve kube-apiserver while its add-ons start.
		// Reuse Phase 8's proven relative scheduling contract before any GitOps
		// workloads are admitted; local lifecycle runs remain untouched.
		stages = append(stages, platformStage{"Stabilize hosted control plane", "stabilize-hosted-kind", 2 * time.Minute})
	}
	if profile == "full" {
		stages = append(stages, platformStage{"Start retained backup store", "start-backup-store", 5 * time.Minute})
	}
	stages = append(stages, platformStage{"Load platform images", "load-images", 10 * time.Minute}, platformStage{"Deploy GitOps", "deploy-gitops", 30 * time.Minute}, platformStage{"Verify platform readiness", "test-gitops", 20 * time.Minute})
	if profile == "full" {
		stages = append(stages, platformStage{"Verify data platform", "verify-data", 10 * time.Minute})
	}
	return stages
}

func platformDownStages(profile string) []platformStage {
	stages := []platformStage{{"Undeploy GitOps", "undeploy-gitops", 8 * time.Minute}, {"Destroy exact cluster", "destroy", 6 * time.Minute}}
	if profile == "full" {
		stages = append(stages, platformStage{"Stop backup store and retain data", "stop-backup-store", 3 * time.Minute})
	}
	return stages
}

func newPlatformStatusCommand(options *Options) *cobra.Command {
	return &cobra.Command{Use: "status", Short: "Show aggregate platform status", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		_, selected, err := options.loadContext()
		if err != nil {
			return err
		}
		ctx, cancel := options.commandContext(cmd.Context())
		defer cancel()
		client, err := NewClusterClient(selected)
		if err != nil {
			return err
		}
		nodes, err := client.core.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return exitError(ExitRemote, "query platform nodes: %v", err)
		}
		argo, argoErr := client.Get(ctx, argoAppGVR, "argocd", "steadystate-root")
		state := "Ready"
		if argoErr != nil || len(nodes.Items) == 0 {
			state = "Unavailable"
		}
		value := map[string]any{"cluster": selected.ClusterName, "profile": selected.Profile, "nodes": len(nodes.Items), "state": state, "argoAvailable": argo != nil && argoErr == nil}
		return options.printer().Table([]string{"CLUSTER", "PROFILE", "NODES", "STATE"}, [][]string{{selected.ClusterName, selected.Profile, fmt.Sprint(len(nodes.Items)), state}}, value)
	}}
}

func newPlatformVerifyCommand(options *Options) *cobra.Command {
	return &cobra.Command{Use: "verify", Short: "Verify the configured complete platform", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		_, selected, err := options.loadContext()
		if err != nil {
			return err
		}
		stages := []platformStage{{"Verify platform readiness", "test-gitops", 20 * time.Minute}}
		if selected.Profile == "full" {
			stages = append(stages, platformStage{"Verify data platform", "verify-data", 10 * time.Minute})
		}
		for _, stage := range stages {
			ctx, cancel := context.WithTimeout(cmd.Context(), stage.Timeout)
			revision, revisionErr := checkoutHeadRevision(ctx, selected.CheckoutPath)
			if revisionErr != nil {
				cancel()
				return revisionErr
			}
			arguments := []string{"-NoProfile", "-File", selected.CheckoutPath + "/scripts/dev.ps1", stage.Command, "-Profile", selected.Profile, "-ClusterName", selected.ClusterName, "-HttpPort", fmt.Sprint(selected.HTTPPort), "-HttpsPort", fmt.Sprint(selected.HTTPSPort), "-GitRevision", revision}
			err = runPlatformStage(ctx, options, selected.CheckoutPath, stage, arguments)
			cancel()
			if err != nil {
				return exitError(ExitUnhealthy, "%s failed: %v", stage.Name, err)
			}
		}
		return options.printer().Table([]string{"PROFILE", "STATE"}, [][]string{{selected.Profile, "Verified"}}, map[string]string{"profile": selected.Profile, "state": "Verified"})
	}}
}
