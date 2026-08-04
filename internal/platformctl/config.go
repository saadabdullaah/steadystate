package platformctl

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"
)

// Config is the non-secret platformctl configuration document.
type Config struct {
	APIVersion     string             `json:"apiVersion" yaml:"apiVersion"`
	Kind           string             `json:"kind" yaml:"kind"`
	CurrentContext string             `json:"currentContext,omitempty" yaml:"currentContext,omitempty"`
	Contexts       map[string]Context `json:"contexts" yaml:"contexts"`
}

// Context identifies one repository and local Kubernetes environment.
type Context struct {
	Repository    string `json:"repository" yaml:"repository"`
	DefaultBranch string `json:"defaultBranch" yaml:"defaultBranch"`
	CheckoutPath  string `json:"checkoutPath" yaml:"checkoutPath"`
	Kubeconfig    string `json:"kubeconfig,omitempty" yaml:"kubeconfig,omitempty"`
	KubeContext   string `json:"kubeContext,omitempty" yaml:"kubeContext,omitempty"`
	ClusterName   string `json:"clusterName" yaml:"clusterName"`
	Profile       string `json:"profile" yaml:"profile"`
	HTTPPort      int    `json:"httpPort" yaml:"httpPort"`
	HTTPSPort     int    `json:"httpsPort" yaml:"httpsPort"`
}

var gitBranchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

func defaultContext(checkout string) Context {
	return Context{
		Repository:    "saadabdullaah/steadystate",
		DefaultBranch: "main",
		CheckoutPath:  checkout,
		ClusterName:   "steadystate",
		Profile:       "standard",
		HTTPPort:      8080,
		HTTPSPort:     8443,
	}
}

// DefaultConfigPath returns the platform-native configuration location.
func DefaultConfigPath() (string, error) {
	if runtime.GOOS == "windows" {
		base := os.Getenv("APPDATA")
		if base == "" {
			var err error
			base, err = os.UserConfigDir()
			if err != nil {
				return "", err
			}
		}
		return filepath.Join(base, "SteadyState", "config.yaml"), nil
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "steadystate", "config.yaml"), nil
}

// NewConfig creates a valid initial configuration.
func NewConfig(name, checkout string) Config {
	if name == "" {
		name = "local"
	}
	return Config{
		APIVersion:     ConfigAPIVersion,
		Kind:           ConfigKind,
		CurrentContext: name,
		Contexts:       map[string]Context{name: defaultContext(checkout)},
	}
}

// LoadConfig reads and validates a configuration document.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, exitError(ExitUsage, "platformctl is not configured; run 'platformctl config init'")
		}
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var config Config
	if err := yaml.UnmarshalStrict(data, &config); err != nil {
		return Config{}, exitError(ExitUsage, "parse config: %v", err)
	}
	if err := config.Validate(); err != nil {
		return Config{}, exitError(ExitUsage, "invalid config: %v", err)
	}
	return config, nil
}

// SaveConfig writes a deterministic, owner-readable configuration and keeps a backup.
func SaveConfig(path string, config Config) error {
	if err := config.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if previous, readErr := os.ReadFile(path); readErr == nil {
		// #nosec G703 -- path is the platform-native config location or a
		// package-test override; it is not accepted from the CLI command line.
		if err := os.WriteFile(path+".bak", previous, 0o600); err != nil {
			return fmt.Errorf("back up config: %w", err)
		}
	} else if !os.IsNotExist(readErr) {
		return readErr
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// Validate verifies schema and context invariants without reading secrets.
func (c Config) Validate() error {
	if c.APIVersion != ConfigAPIVersion || c.Kind != ConfigKind {
		return fmt.Errorf("expected %s %s", ConfigAPIVersion, ConfigKind)
	}
	if len(c.Contexts) == 0 {
		return fmt.Errorf("at least one context is required")
	}
	if _, ok := c.Contexts[c.CurrentContext]; !ok {
		return fmt.Errorf("current context %q does not exist", c.CurrentContext)
	}
	for name, context := range c.Contexts {
		if strings.TrimSpace(name) == "" || strings.ContainsAny(name, "\r\n\t") {
			return fmt.Errorf("invalid context name %q", name)
		}
		if context.Repository == "" || strings.Count(context.Repository, "/") != 1 {
			return fmt.Errorf("context %q repository must be OWNER/REPO", name)
		}
		if !validGitBranch(context.DefaultBranch) || context.CheckoutPath == "" || context.ClusterName == "" {
			return fmt.Errorf("context %q is missing repository, checkout, branch, or cluster configuration", name)
		}
		if context.Profile != "minimal" && context.Profile != "standard" && context.Profile != "full" {
			return fmt.Errorf("context %q has unsupported profile %q", name, context.Profile)
		}
		if context.HTTPPort < 1 || context.HTTPPort > 65535 || context.HTTPSPort < 1 || context.HTTPSPort > 65535 || context.HTTPPort == context.HTTPSPort {
			return fmt.Errorf("context %q has invalid or conflicting ports", name)
		}
	}
	return nil
}

func validGitBranch(value string) bool {
	return gitBranchPattern.MatchString(value) && !strings.Contains(value, "..") && !strings.Contains(value, "//") && !strings.HasSuffix(value, "/") && !strings.HasSuffix(value, ".") && !strings.HasSuffix(value, ".lock")
}

func (c Config) Context(name string) (Context, error) {
	if name == "" {
		name = c.CurrentContext
	}
	context, ok := c.Contexts[name]
	if !ok {
		return Context{}, exitError(ExitNotFound, "context %q does not exist", name)
	}
	return context, nil
}

func (c Config) ContextNames() []string {
	names := make([]string, 0, len(c.Contexts))
	for name := range c.Contexts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
