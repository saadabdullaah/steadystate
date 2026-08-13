package platformctl

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newConfigCommand(options *Options) *cobra.Command {
	command := &cobra.Command{Use: "config", Short: "Manage platformctl configuration"}
	var name, checkout, repository, branch, profile, cluster, kubeContext string
	var httpPort, httpsPort int
	var force bool
	initCommand := &cobra.Command{
		Use:   "init",
		Short: "Create the initial non-secret configuration",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			path, err := options.configPath()
			if err != nil {
				return err
			}
			if _, err := os.Stat(path); err == nil && !force {
				return exitError(ExitConflict, "configuration already exists at %s; use --force to replace it", path)
			}
			if checkout == "" {
				checkout, err = os.Getwd()
				if err != nil {
					return err
				}
			}
			checkout, err = filepath.Abs(checkout)
			if err != nil {
				return err
			}
			config := NewConfig(name, checkout)
			selected := config.Contexts[config.CurrentContext]
			if repository == "" {
				repository, branch, err = inferGitHubOrigin(checkout, branch)
				if err != nil {
					return err
				}
			}
			selected.Repository, selected.DefaultBranch = repository, branch
			selected.Profile, selected.ClusterName, selected.KubeContext = profile, cluster, kubeContext
			selected.HTTPPort, selected.HTTPSPort = httpPort, httpsPort
			config.Contexts[config.CurrentContext] = selected
			if err := SaveConfig(path, config); err != nil {
				return err
			}
			return options.printer().Table([]string{"CONFIG", "CONTEXT"}, [][]string{{path, config.CurrentContext}}, config)
		},
	}
	initCommand.Flags().StringVar(&name, "name", "local", "initial context name")
	initCommand.Flags().StringVar(&checkout, "checkout", "", "SteadyState checkout path")
	initCommand.Flags().StringVar(&repository, "repository", "", "GitHub repository as OWNER/REPO (inferred from origin when omitted)")
	initCommand.Flags().StringVar(&branch, "branch", "main", "default Git branch")
	initCommand.Flags().StringVar(&profile, "profile", "standard", "minimal, standard, or full")
	initCommand.Flags().StringVar(&cluster, "cluster", "steadystate", "kind cluster name")
	initCommand.Flags().StringVar(&kubeContext, "kube-context", "", "Kubernetes context")
	initCommand.Flags().IntVar(&httpPort, "http-port", 8080, "Gateway HTTP host port")
	initCommand.Flags().IntVar(&httpsPort, "https-port", 8443, "Gateway HTTPS host port")
	initCommand.Flags().BoolVar(&force, "force", false, "replace an existing configuration after creating a backup")
	viewCommand := &cobra.Command{
		Use:   "view",
		Short: "Show the redacted configuration",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			path, err := options.configPath()
			if err != nil {
				return err
			}
			config, err := LoadConfig(path)
			if err != nil {
				return err
			}
			rows := make([][]string, 0, len(config.Contexts))
			for _, contextName := range config.ContextNames() {
				context := config.Contexts[contextName]
				current := ""
				if contextName == config.CurrentContext {
					current = "*"
				}
				rows = append(rows, []string{current, contextName, context.Repository, context.Profile, context.KubeContext})
			}
			return options.printer().Table([]string{"CURRENT", "NAME", "REPOSITORY", "PROFILE", "KUBE CONTEXT"}, rows, config)
		},
	}
	command.AddCommand(initCommand, viewCommand)
	return command
}

var githubRemotePattern = regexp.MustCompile(`^(?:https://github\.com/|git@github\.com:)([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?$`)

func inferGitHubOrigin(checkout, branch string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	remote, err := runExternal(ctx, checkout, "git", "remote", "get-url", "origin")
	if err != nil {
		return "", "", exitError(ExitUsage, "repository was not provided and GitHub origin could not be read: %v", err)
	}
	match := githubRemotePattern.FindStringSubmatch(strings.TrimSpace(remote))
	if len(match) != 3 {
		return "", "", exitError(ExitUsage, "origin is not an unambiguous GitHub repository; pass --repository OWNER/REPO")
	}
	if branch == "" {
		branch = "main"
	}
	return match[1] + "/" + match[2], branch, nil
}

func newContextCommand(options *Options) *cobra.Command {
	command := &cobra.Command{Use: "context", Short: "Manage named platform contexts"}
	command.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List contexts",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			path, err := options.configPath()
			if err != nil {
				return err
			}
			config, err := LoadConfig(path)
			if err != nil {
				return err
			}
			rows := [][]string{}
			for _, name := range config.ContextNames() {
				value := config.Contexts[name]
				current := ""
				if name == config.CurrentContext {
					current = "*"
				}
				rows = append(rows, []string{current, name, value.Repository, value.Profile, value.ClusterName})
			}
			return options.printer().Table([]string{"CURRENT", "NAME", "REPOSITORY", "PROFILE", "CLUSTER"}, rows, config.Contexts)
		},
	})

	var value Context
	setCommand := &cobra.Command{
		Use:   "set NAME",
		Short: "Create or replace a context",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			path, err := options.configPath()
			if err != nil {
				return err
			}
			config, err := LoadConfig(path)
			if err != nil {
				return err
			}
			if value.CheckoutPath == "" {
				value.CheckoutPath, err = os.Getwd()
				if err != nil {
					return err
				}
			}
			value.CheckoutPath, err = filepath.Abs(value.CheckoutPath)
			if err != nil {
				return err
			}
			config.Contexts[args[0]] = value
			if err := SaveConfig(path, config); err != nil {
				return exitError(ExitUsage, "save context: %v", err)
			}
			return options.printer().Table([]string{"NAME", "REPOSITORY", "PROFILE"}, [][]string{{args[0], value.Repository, value.Profile}}, value)
		},
	}
	setCommand.Flags().StringVar(&value.Repository, "repository", "saadabdullaah/steadystate", "GitHub repository as OWNER/REPO")
	setCommand.Flags().StringVar(&value.DefaultBranch, "branch", "main", "default Git branch")
	setCommand.Flags().StringVar(&value.CheckoutPath, "checkout", "", "SteadyState checkout path")
	setCommand.Flags().StringVar(&value.Kubeconfig, "kubeconfig", "", "kubeconfig path")
	setCommand.Flags().StringVar(&value.KubeContext, "kube-context", "", "Kubernetes context")
	setCommand.Flags().StringVar(&value.ClusterName, "cluster", "steadystate", "kind cluster name")
	setCommand.Flags().StringVar(&value.Profile, "profile", "standard", "minimal, standard, or full")
	setCommand.Flags().IntVar(&value.HTTPPort, "http-port", 8080, "Gateway HTTP host port")
	setCommand.Flags().IntVar(&value.HTTPSPort, "https-port", 8443, "Gateway HTTPS host port")
	command.AddCommand(setCommand)

	command.AddCommand(&cobra.Command{
		Use:   "use NAME",
		Short: "Select the current context",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			path, err := options.configPath()
			if err != nil {
				return err
			}
			config, err := LoadConfig(path)
			if err != nil {
				return err
			}
			if _, exists := config.Contexts[args[0]]; !exists {
				return exitError(ExitNotFound, "context %q does not exist", args[0])
			}
			config.CurrentContext = args[0]
			if err := SaveConfig(path, config); err != nil {
				return err
			}
			return options.printer().Table([]string{"CURRENT CONTEXT"}, [][]string{{args[0]}}, map[string]string{"currentContext": args[0]})
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "delete NAME",
		Short: "Delete a non-current context",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			path, err := options.configPath()
			if err != nil {
				return err
			}
			config, err := LoadConfig(path)
			if err != nil {
				return err
			}
			if args[0] == config.CurrentContext {
				return exitError(ExitConflict, "cannot delete current context %q", args[0])
			}
			if _, exists := config.Contexts[args[0]]; !exists {
				return exitError(ExitNotFound, "context %q does not exist", args[0])
			}
			delete(config.Contexts, args[0])
			if err := SaveConfig(path, config); err != nil {
				return err
			}
			return options.printer().Table([]string{"DELETED"}, [][]string{{args[0]}}, map[string]string{"deleted": args[0]})
		},
	})
	return command
}

func newProfileCommand(options *Options) *cobra.Command {
	command := &cobra.Command{Use: "profile", Short: "Inspect supported local cluster profiles"}
	command.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List supported local cluster profiles",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			profiles := []map[string]string{
				{"name": "minimal", "purpose": "foundation and routing smoke"},
				{"name": "standard", "purpose": "GitOps, delivery, observability, and security"},
				{"name": "full", "purpose": "standard profile plus PostgreSQL and recovery"},
			}
			rows := [][]string{}
			for _, profile := range profiles {
				rows = append(rows, []string{profile["name"], profile["purpose"]})
			}
			return options.printer().Table([]string{"NAME", "PURPOSE"}, rows, profiles)
		},
	})
	return command
}

func newVersionCommand(options *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print platformctl build information",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			build := options.Build
			if build.Version == "" {
				build.Version = "development"
			}
			if build.Go == "" {
				build.Go = runtime.Version()
			}
			if build.PortalVersion == "" {
				build.PortalVersion = portalVersion
			}
			if build.PortalAssetsDigest == "" {
				build.PortalAssetsDigest = portalAssetsDigest()
			}
			return options.printer().Table([]string{"VERSION", "COMMIT", "DATE", "GO"}, [][]string{{build.Version, build.Commit, build.Date, build.Go}}, build)
		},
	}
}
