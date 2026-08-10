package platformctl

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

const docsDetailsAnnotation = "steadystate.dev/docs-details"

func newDocsCommand(root *cobra.Command) *cobra.Command {
	var outputDirectory string
	command := &cobra.Command{
		Use: "docs", Hidden: true, Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if strings.TrimSpace(outputDirectory) == "" {
				return exitError(ExitUsage, "--directory is required")
			}
			return generateCommandDocs(root, outputDirectory)
		},
	}
	command.Flags().StringVar(&outputDirectory, "directory", "", "output directory for reference, man pages, and completions")
	_ = command.MarkFlagRequired("directory")
	return command
}

func generateCommandDocs(root *cobra.Command, directory string) error {
	completionDirectory := filepath.Join(directory, "completions")
	manDirectory := filepath.Join(directory, "man")
	if err := os.MkdirAll(completionDirectory, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(manDirectory, 0o755); err != nil {
		return err
	}
	completionFiles := []struct {
		name  string
		write func(string) error
	}{
		{"platformctl.bash", root.GenBashCompletionFile},
		{"_platformctl", root.GenZshCompletionFile},
		{"platformctl.fish", func(path string) error { return root.GenFishCompletionFile(path, true) }},
		{"platformctl.ps1", root.GenPowerShellCompletionFileWithDesc},
	}
	for _, item := range completionFiles {
		if err := item.write(filepath.Join(completionDirectory, item.name)); err != nil {
			return err
		}
	}
	commands := documentedCommands(root)
	var markdown strings.Builder
	markdown.WriteString("# platformctl command reference\n\nGenerated from the `v0.8` command tree. Do not edit manually.\n\n")
	for _, command := range commands {
		name := strings.ReplaceAll(command.CommandPath(), " ", "-")
		summary := strings.TrimSpace(command.Short)
		if summary == "" {
			summary = "Operate this platformctl command."
		}
		markdown.WriteString("## `" + command.CommandPath() + "`\n\n")
		markdown.WriteString(summary + "\n\n")
		markdown.WriteString("```text\n" + command.UseLine() + "\n")
		if flags := strings.TrimSpace(command.NonInheritedFlags().FlagUsagesWrapped(100)); flags != "" {
			markdown.WriteString("\n" + flags + "\n")
		}
		markdown.WriteString("```\n\n")
		if details := strings.TrimSpace(command.Annotations[docsDetailsAnnotation]); details != "" {
			markdown.WriteString(details + "\n\n")
		}
		man := fmt.Sprintf(".TH %s 1\n.SH NAME\n%s \\- %s\n.SH SYNOPSIS\n.B %s\n.SH DESCRIPTION\n%s\n",
			strings.ToUpper(name), name, roffEscape(summary), roffEscape(command.UseLine()), roffEscape(summary))
		if err := os.WriteFile(filepath.Join(manDirectory, name+".1"), []byte(man), 0o644); err != nil {
			return err
		}
	}
	reference := strings.TrimRight(markdown.String(), "\n") + "\n"
	return os.WriteFile(filepath.Join(directory, "platformctl.md"), []byte(reference), 0o644)
}

func documentedCommands(root *cobra.Command) []*cobra.Command {
	commands := []*cobra.Command{}
	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		if !command.Hidden {
			commands = append(commands, command)
		}
		children := append([]*cobra.Command(nil), command.Commands()...)
		sort.Slice(children, func(i, j int) bool { return children[i].CommandPath() < children[j].CommandPath() })
		for _, child := range children {
			if !child.Hidden {
				walk(child)
			}
		}
	}
	walk(root)
	return commands
}

func roffEscape(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\e")
	return strings.ReplaceAll(value, "-", "\\-")
}
