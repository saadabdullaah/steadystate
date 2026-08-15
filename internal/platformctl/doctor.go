package platformctl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DoctorCheck struct {
	Name        string   `json:"name" yaml:"name"`
	Status      string   `json:"status" yaml:"status"`
	Details     string   `json:"details" yaml:"details"`
	Evidence    []string `json:"evidence,omitempty" yaml:"evidence,omitempty"`
	Remediation string   `json:"remediation,omitempty" yaml:"remediation,omitempty"`
}

func newDoctorCommand(options *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Validate local and remote platform prerequisites",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, selected, err := options.loadContext()
			if err != nil {
				return err
			}
			lifecycleTimeout := options.Timeout
			if lifecycleTimeout == 30*time.Second {
				lifecycleTimeout = 30 * time.Minute
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), lifecycleTimeout)
			defer cancel()
			checks := runDoctor(ctx, selected)
			rows := make([][]string, 0, len(checks))
			failed := false
			for _, check := range checks {
				rows = append(rows, []string{check.Status, check.Name, check.Details, check.Remediation})
				if check.Status == "Fail" {
					failed = true
				}
			}
			if err := options.printer().Table([]string{"STATUS", "CHECK", "DETAILS", "REMEDIATION"}, rows, checks); err != nil {
				return err
			}
			if failed {
				return exitError(ExitUnhealthy, "one or more prerequisite checks failed")
			}
			return nil
		},
	}
}

func runDoctor(ctx context.Context, selected Context) []DoctorCheck {
	checks := []DoctorCheck{}
	add := func(name, status, details, remediation string) {
		checks = append(checks, DoctorCheck{Name: name, Status: status, Details: Redact(details), Remediation: remediation})
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "linux" {
		add("operating-system", "Pass", runtime.GOOS+" supports the full local lifecycle", "")
	} else {
		add("operating-system", "Warning", runtime.GOOS+" supports CLI Git/read operations; kind lifecycle is best-effort", "Use Windows or Linux for the supported local lifecycle.")
	}
	if info, err := os.Stat(selected.CheckoutPath); err != nil || !info.IsDir() {
		add("checkout", "Fail", "configured checkout is unavailable", "Run platformctl context set with a valid --checkout path.")
	} else if _, err := os.Stat(filepath.Join(selected.CheckoutPath, "go.mod")); err != nil {
		add("checkout", "Fail", "configured directory is not a SteadyState checkout", "Point the context at the SteadyState repository root.")
	} else {
		add("checkout", "Pass", selected.CheckoutPath, "")
	}

	tools := []string{"git", "docker"}
	if selected.Profile == "full" {
		tools = append(tools, "sops", "age")
	}
	for _, tool := range tools {
		if path, err := exec.LookPath(tool); err != nil {
			status, details, remediation := missingToolCheck(tool, selected.Profile)
			add("tool-"+tool, status, details, remediation)
		} else {
			add("tool-"+tool, "Pass", path, "")
		}
	}
	githubCLI := githubCLIExecutable()
	if path, err := exec.LookPath(githubCLI); err != nil {
		add("tool-gh", "Fail", "gh was not found", "Install GitHub CLI and ensure it is on PATH or in ~/.local/bin.")
	} else {
		add("tool-gh", "Pass", path, "")
	}
	if path, err := powerShellExecutable(); err != nil {
		add("tool-pwsh", "Fail", ErrorMessage(err), "Install PowerShell 7, or use Windows PowerShell 5.1 on Windows, and ensure it is on PATH.")
	} else {
		add("tool-pwsh", "Pass", path, "")
	}
	if _, err := runExternal(ctx, selected.CheckoutPath, "git", "rev-parse", "--show-toplevel"); err != nil {
		add("git-repository", "Fail", err.Error(), "Restore or re-clone the configured checkout.")
	} else {
		add("git-repository", "Pass", "repository metadata is readable", "")
	}
	if status, err := runExternal(ctx, selected.CheckoutPath, "git", "status", "--porcelain"); err != nil {
		add("git-worktree", "Fail", err.Error(), "Repair the Git checkout.")
	} else if strings.TrimSpace(status) != "" {
		add("git-worktree", "Warning", "worktree contains local changes", "Commit, stash, or intentionally preserve local files before Git write operations.")
	} else {
		add("git-worktree", "Pass", "worktree is clean", "")
	}
	if _, err := runExternal(ctx, selected.CheckoutPath, "gh", "auth", "status", "--hostname", "github.com"); err != nil {
		add("github-auth", "Fail", "GitHub CLI authentication is unavailable", "Run gh auth login and select the intended account.")
	} else {
		add("github-auth", "Pass", "GitHub CLI is authenticated", "")
	}
	requiredGH := readVersionPin(filepath.Join(selected.CheckoutPath, "scripts", "versions.env"), "GITHUB_CLI_VERSION")
	if raw, err := runExternal(ctx, selected.CheckoutPath, "gh", "--version"); err != nil {
		add("github-cli-version", "Fail", ErrorMessage(err), "Install the checksum-verified GitHub CLI version documented by SteadyState.")
	} else if actual := githubCLIVersion(raw); actual == "" || !versionAtLeast(actual, requiredGH) {
		add("github-cli-version", "Fail", fmt.Sprintf("GitHub CLI %s or newer is required; detected %s", requiredGH, actual), "Upgrade GitHub CLI from its official signed release.")
	} else {
		add("github-cli-version", "Pass", fmt.Sprintf("GitHub CLI %s satisfies the security baseline %s", actual, requiredGH), "")
	}
	checkGitHubNames := func(check, kind string, required []string) {
		raw, err := runExternal(ctx, selected.CheckoutPath, "gh", kind, "list", "--repo", selected.Repository, "--json", "name")
		if err != nil {
			add(check, "Warning", "GitHub "+kind+" names could not be inspected with the active token", "Confirm repository access or inspect the documented names in repository settings.")
			return
		}
		names := decodeGitHubNames(raw)
		missing := []string{}
		for _, name := range required {
			if !names[name] {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			add(check, "Fail", "required names are missing: "+strings.Join(missing, ", "), "Configure the documented repository "+kind+" names; values are never read.")
			return
		}
		add(check, "Pass", "required "+kind+" names are present; values were not read", "")
	}
	checkGitHubNames("github-app-variable", "variable", []string{"STEADYSTATE_BOT_CLIENT_ID"})
	requiredSecrets := []string{"STEADYSTATE_BOT_PRIVATE_KEY"}
	if selected.Profile == "full" {
		requiredSecrets = append(requiredSecrets, "SOPS_AGE_KEY")
	}
	checkGitHubNames("github-secrets", "secret", requiredSecrets)
	if _, err := runExternal(ctx, selected.CheckoutPath, "docker", "info", "--format", "{{json .ServerVersion}}"); err != nil {
		add("docker", "Fail", "Docker Engine is not ready", "Start Docker Desktop or Docker Engine.")
	} else {
		add("docker", "Pass", "Docker Engine responded", "")
	}
	if raw, err := runExternal(ctx, selected.CheckoutPath, "docker", "info", "--format", "{{.MemTotal}}"); err == nil {
		if available, parseErr := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); parseErr == nil {
			check := resourceBudgetCheck(selected.Profile, available)
			add(check.Name, check.Status, check.Details, check.Remediation)
		}
	}
	for _, port := range []int{selected.HTTPPort, selected.HTTPSPort} {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			add(fmt.Sprintf("port-%d", port), "Warning", "port is in use", "Confirm the configured cluster owns it or choose another port.")
			continue
		}
		_ = listener.Close()
		add(fmt.Sprintf("port-%d", port), "Pass", "port is available", "")
	}
	if client, err := NewClusterClient(selected); err != nil {
		add("kubernetes", "Warning", err.Error(), "Create the cluster with platformctl cluster up when cluster reads are required.")
	} else if version, err := client.core.Discovery().ServerVersion(); err != nil {
		add("kubernetes", "Warning", err.Error(), "Check the selected kube context.")
	} else {
		expected := readVersionPin(filepath.Join(selected.CheckoutPath, "scripts", "versions.env"), "KUBERNETES_VERSION")
		if expected != "" && strings.TrimPrefix(version.GitVersion, "v") != expected {
			add("kubernetes", "Fail", fmt.Sprintf("cluster is %s; repository requires v%s", version.GitVersion, expected), "Recreate the configured cluster from the pinned profile.")
		} else {
			add("kubernetes", "Pass", version.GitVersion, "")
		}
	}
	if selected.Profile == "full" {
		for _, relative := range []string{".artifacts/secrets/steadystate.agekey", "gitops/secrets/backup-store.enc.yaml"} {
			if relative == ".artifacts/secrets/steadystate.agekey" && fullProfileAgeIdentityAvailable(filepath.Join(selected.CheckoutPath, filepath.FromSlash(relative))) {
				add("full-profile-"+filepath.Base(relative), "Pass", "age identity is available through the process environment; its value was not read", "")
				continue
			}
			if _, err := os.Stat(filepath.Join(selected.CheckoutPath, filepath.FromSlash(relative))); err != nil {
				add("full-profile-"+filepath.Base(relative), "Fail", "required ignored secret material is absent", "Restore the documented SOPS/age full-profile prerequisites.")
			} else {
				add("full-profile-"+filepath.Base(relative), "Pass", "required ignored file is present", "")
			}
		}
	}
	sort.SliceStable(checks, func(i, j int) bool { return checks[i].Name < checks[j].Name })
	return checks
}

func missingToolCheck(tool, profile string) (string, string, string) {
	if profile == "full" && (tool == "sops" || tool == "age") {
		return "Warning", tool + " is not installed yet", "platformctl platform up installs the pinned repository-local " + tool + " tool before it is required."
	}
	return "Fail", tool + " was not found", "Install " + tool + " and ensure it is on PATH."
}

func fullProfileAgeIdentityAvailable(identityPath string) bool {
	if strings.TrimSpace(os.Getenv("SOPS_AGE_KEY")) != "" {
		return true
	}
	info, err := os.Stat(identityPath)
	return err == nil && !info.IsDir()
}

func resourceBudgetCheck(profile string, available int64) DoctorCheck {
	minimumGiB := map[string]int64{"minimal": 4, "standard": 7, "full": 9}[profile]
	availableGiB := float64(available) / float64(1<<30)
	check := DoctorCheck{Name: "resource-budget"}
	if available < minimumGiB*(1<<30) {
		check.Status = "Fail"
		check.Details = fmt.Sprintf("Docker has %.1f GiB; the %s profile requires at least %d GiB", availableGiB, profile, minimumGiB)
		check.Remediation = fmt.Sprintf("Select a smaller profile or allocate at least %d GiB to Docker before retrying.", minimumGiB)
		return check
	}
	check.Status = "Pass"
	check.Details = fmt.Sprintf("Docker has %.1f GiB for the %s profile", availableGiB, profile)
	return check
}

func githubCLIVersion(output string) string {
	fields := strings.Fields(strings.Split(strings.TrimSpace(output), "\n")[0])
	if len(fields) >= 3 && fields[0] == "gh" && fields[1] == "version" {
		return strings.TrimPrefix(fields[2], "v")
	}
	return ""
}

func versionAtLeast(actual, required string) bool {
	parse := func(value string) ([3]int, bool) {
		parts := strings.Split(strings.TrimPrefix(value, "v"), ".")
		if len(parts) != 3 {
			return [3]int{}, false
		}
		var result [3]int
		for index, part := range parts {
			value, err := strconv.Atoi(part)
			if err != nil {
				return [3]int{}, false
			}
			result[index] = value
		}
		return result, true
	}
	a, validActual := parse(actual)
	r, validRequired := parse(required)
	if !validActual || !validRequired {
		return false
	}
	for index := range a {
		if a[index] != r[index] {
			return a[index] > r[index]
		}
	}
	return true
}

func decodeGitHubNames(raw string) map[string]bool {
	var values []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return map[string]bool{}
	}
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value.Name] = true
	}
	return result
}

func readVersionPin(path, key string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	prefix := key + "="
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), prefix))
		}
	}
	return ""
}

func newClusterCommand(options *Options) *cobra.Command {
	command := &cobra.Command{Use: "cluster", Short: "Manage the configured local cluster"}
	command.AddCommand(newClusterLifecycleCommand(options, "up", "bootstrap"))
	command.AddCommand(newClusterLifecycleCommand(options, "down", "destroy"))
	command.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show the configured cluster status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			version, err := client.core.Discovery().ServerVersion()
			if err != nil {
				return exitError(ExitRemote, "query cluster: %v", err)
			}
			nodes, err := client.core.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
			if err != nil {
				return exitError(ExitRemote, "list nodes: %v", err)
			}
			value := map[string]any{"cluster": selected.ClusterName, "profile": selected.Profile, "version": version.GitVersion, "nodes": len(nodes.Items)}
			return options.printer().Table([]string{"CLUSTER", "PROFILE", "VERSION", "NODES"}, [][]string{{selected.ClusterName, selected.Profile, version.GitVersion, fmt.Sprint(len(nodes.Items))}}, value)
		},
	})
	return command
}

func newClusterLifecycleCommand(options *Options, name, scriptCommand string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: map[string]string{"up": "Reconcile the configured local cluster", "down": "Delete the exact configured local cluster"}[name],
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
				return exitError(ExitUsage, "cluster lifecycle is supported on Windows and Linux in v0.8")
			}
			_, selected, err := options.loadContext()
			if err != nil {
				return err
			}
			ctx, cancel := options.commandContext(cmd.Context())
			defer cancel()
			arguments := []string{"-NoProfile", "-File", filepath.Join(selected.CheckoutPath, "scripts", "dev.ps1"), scriptCommand, "-Profile", selected.Profile, "-ClusterName", selected.ClusterName, "-HttpPort", fmt.Sprint(selected.HTTPPort), "-HttpsPort", fmt.Sprint(selected.HTTPSPort)}
			powerShell, shellErr := powerShellExecutable()
			if shellErr != nil {
				return shellErr
			}
			// #nosec G204 -- executable is resolved only from the fixed pwsh/Windows PowerShell allowlist.
			process := exec.Command(powerShell, powerShellArguments(powerShell, arguments)...)
			process.Dir = selected.CheckoutPath
			process.Stdout = options.Stdout
			process.Stderr = options.Stderr
			if err := runCommandInProcessTree(ctx, process); err != nil {
				if ctx.Err() != nil {
					return exitError(ExitTimeout, "cluster %s timed out", name)
				}
				return exitError(ExitRemote, "cluster %s failed: %v", name, err)
			}
			return nil
		},
	}
}

func runExternal(ctx context.Context, directory, executable string, arguments ...string) (string, error) {
	if base := strings.ToLower(filepath.Base(executable)); base == "gh" || base == "gh.exe" {
		executable = githubCLIExecutable()
	}
	switch strings.ToLower(filepath.Base(executable)) {
	case "git", "git.exe", "gh", "gh.exe", "docker", "docker.exe", "kubectl", "kubectl.exe", "pwsh", "pwsh.exe", "powershell", "powershell.exe":
	default:
		return "", fmt.Errorf("unsupported prerequisite executable %q", executable)
	}
	// #nosec G204 -- executable is constrained to the fixed allowlist above.
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Dir = directory
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return "", exitError(ExitTimeout, "%s command timed out", executable)
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("%s", Redact(message))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func githubCLIExecutable() string {
	home, _ := os.UserHomeDir()
	localAppData := os.Getenv("LOCALAPPDATA")
	return selectGitHubCLIExecutable(runtime.GOOS, home, localAppData, func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && !info.IsDir()
	}, exec.LookPath)
}

func selectGitHubCLIExecutable(goos, home, localAppData string, exists func(string) bool, lookPath func(string) (string, error)) string {
	name := "gh"
	candidates := []string{filepath.Join(home, ".local", "bin", name)}
	if goos == "windows" {
		name = "gh.exe"
		candidates = []string{
			filepath.Join(home, ".local", "bin", name),
			filepath.Join(localAppData, "Programs", "GitHub CLI", name),
		}
	}
	for _, candidate := range candidates {
		if candidate != "" && exists(candidate) {
			return candidate
		}
	}
	if executable, err := lookPath(name); err == nil {
		return executable
	}
	return name
}
