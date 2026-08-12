package platformctl

import (
	"context"
	"fmt"
	"runtime"
	"strings"
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
			stages := platformUpStages(selected.Profile)
			if action == "down" {
				stages = platformDownStages(selected.Profile)
			}
			gitRevision := ""
			if action == "up" {
				gitRevision, err = checkoutHeadRevision(cmd.Context(), selected.CheckoutPath)
				if err != nil {
					return err
				}
			}
			result := platformLifecycleResult{Action: action, Profile: selected.Profile, GitRevision: gitRevision, Stages: stages, State: "Completed"}
			errors := []string{}
			for _, stage := range stages {
				_, _ = fmt.Fprintf(options.Stderr, "[%s] %s\n", time.Now().UTC().Format(time.RFC3339), stage.Name)
				stageCtx, cancel := context.WithTimeout(cmd.Context(), stage.Timeout)
				arguments := []string{"-NoProfile", "-File", selected.CheckoutPath + "/scripts/dev.ps1", stage.Command, "-Profile", selected.Profile, "-ClusterName", selected.ClusterName, "-HttpPort", fmt.Sprint(selected.HTTPPort), "-HttpsPort", fmt.Sprint(selected.HTTPSPort)}
				if gitRevision != "" {
					arguments = append(arguments, "-GitRevision", gitRevision)
				}
				_, stageErr := runExternal(stageCtx, selected.CheckoutPath, "pwsh", arguments...)
				cancel()
				if stageErr != nil {
					errors = append(errors, stage.Name+": "+ErrorMessage(stageErr))
					if action == "up" {
						result.State = "Failed"
						return exitError(ExitRemote, "platform up stopped at %s; the environment was preserved for diagnosis: %s", stage.Name, strings.Join(errors, "; "))
					}
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
	stages := []platformStage{{"Install pinned tools", "tools", 15 * time.Minute}, {"Verify pinned tools", "check-versions", 5 * time.Minute}, {"Bootstrap cluster", "bootstrap", 20 * time.Minute}}
	if profile == "full" {
		stages = append(stages, platformStage{"Start retained backup store", "start-backup-store", 5 * time.Minute})
	}
	stages = append(stages, platformStage{"Build platform images", "build-images", 15 * time.Minute}, platformStage{"Load platform images", "load-images", 10 * time.Minute}, platformStage{"Deploy GitOps", "deploy-gitops", 30 * time.Minute}, platformStage{"Verify platform readiness", "test-gitops", 20 * time.Minute})
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
			_, err = runExternal(ctx, selected.CheckoutPath, "pwsh", "-NoProfile", "-File", selected.CheckoutPath+"/scripts/dev.ps1", stage.Command, "-Profile", selected.Profile, "-ClusterName", selected.ClusterName, "-HttpPort", fmt.Sprint(selected.HTTPPort), "-HttpsPort", fmt.Sprint(selected.HTTPSPort), "-GitRevision", revision)
			cancel()
			if err != nil {
				return exitError(ExitUnhealthy, "%s failed: %v", stage.Name, err)
			}
		}
		return options.printer().Table([]string{"PROFILE", "STATE"}, [][]string{{selected.Profile, "Verified"}}, map[string]string{"profile": selected.Profile, "state": "Verified"})
	}}
}
