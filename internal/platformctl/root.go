package platformctl

import (
	"context"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type BuildInfo struct {
	Version string `json:"version" yaml:"version"`
	Commit  string `json:"commit" yaml:"commit"`
	Date    string `json:"date" yaml:"date"`
	Dirty   string `json:"dirty" yaml:"dirty"`
	Go      string `json:"goVersion" yaml:"goVersion"`
}

type Options struct {
	ConfigPath  string
	AuditDir    string
	ContextName string
	Format      string
	Timeout     time.Duration
	NoColor     bool
	Quiet       bool
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	Build       BuildInfo
}

func NewRootCommand(options Options) *cobra.Command {
	if options.Stdout == nil {
		options.Stdout = os.Stdout
	}
	if options.Stdin == nil {
		options.Stdin = os.Stdin
	}
	if options.Stderr == nil {
		options.Stderr = os.Stderr
	}
	if options.Format == "" {
		options.Format = "table"
	}
	if options.Timeout == 0 {
		options.Timeout = 30 * time.Second
	}
	root := &cobra.Command{
		Use:           "platformctl",
		Short:         "Operate the SteadyState developer platform",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			switch options.Format {
			case "table", "json", "yaml":
				return nil
			default:
				return exitError(ExitUsage, "unsupported output format %q", options.Format)
			}
		},
	}
	root.SetOut(options.Stdout)
	root.SetErr(options.Stderr)
	root.PersistentFlags().StringVar(&options.ContextName, "context", "", "configuration context")
	root.PersistentFlags().StringVarP(&options.Format, "output", "o", options.Format, "output format: table, json, or yaml")
	root.PersistentFlags().DurationVar(&options.Timeout, "timeout", options.Timeout, "command timeout")
	root.PersistentFlags().BoolVar(&options.NoColor, "no-color", false, "disable color output")
	root.PersistentFlags().BoolVarP(&options.Quiet, "quiet", "q", false, "suppress successful output")

	root.AddCommand(
		newConfigCommand(&options),
		newContextCommand(&options),
		newProfileCommand(&options),
		newVersionCommand(&options),
		newDoctorCommand(&options),
		newClusterCommand(&options),
		newInitCommand(&options),
		newDevCommand(&options),
		newServiceCommand(&options),
		newTeamCommand(&options),
		newApplicationCommand(&options),
		newDatabaseCommand(&options),
		newRequestCommand(&options),
		newBrokerCommand(&options),
		newCompletionCommand(root),
		newDocsCommand(root),
	)
	return root
}

func (o *Options) printer() Printer {
	return Printer{Format: o.Format, Quiet: o.Quiet, Writer: o.Stdout}
}

func (o *Options) configPath() (string, error) {
	if o.ConfigPath != "" {
		return o.ConfigPath, nil
	}
	return DefaultConfigPath()
}

func (o *Options) loadContext() (Config, Context, error) {
	path, err := o.configPath()
	if err != nil {
		return Config{}, Context{}, err
	}
	config, err := LoadConfig(path)
	if err != nil {
		return Config{}, Context{}, err
	}
	selected, err := config.Context(o.ContextName)
	return config, selected, err
}

func (o *Options) commandContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, o.Timeout)
}

func newCompletionCommand(root *cobra.Command) *cobra.Command {
	command := &cobra.Command{
		Use:       "completion [bash|zsh|fish|powershell]",
		Short:     "Generate shell completion",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch strings.ToLower(args[0]) {
			case "bash":
				return root.GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return root.GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(cmd.OutOrStdout(), true)
			case "powershell":
				return root.GenPowerShellCompletionWithDesc(cmd.OutOrStdout())
			default:
				return exitError(ExitUsage, "unsupported shell %q", args[0])
			}
		},
	}
	return command
}

// ErrorMessage returns a redacted message safe for terminal output.
func ErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return Redact(err.Error())
}
