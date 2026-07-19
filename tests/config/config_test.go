package config_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	kyaml "k8s.io/apimachinery/pkg/util/yaml"
)

func TestRepositoryYAMLParses(t *testing.T) {
	root := repositoryRoot(t)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == ".tools" || entry.Name() == ".artifacts" {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() == "m-plan.md" {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" && !strings.HasSuffix(path, ".yaml.tmpl") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".yaml.tmpl") {
			rendered := strings.NewReplacer(
				"__CLUSTER_NAME__", "steadystate-test",
				"__NODE_IMAGE__", "kindest/node:v1.35.5@sha256:ce977ae6d65918d0b58a5f8b5e940429c2ce42fa3a5619ec2bbc60b949c0ac95",
				"__HTTP_PORT__", "18080",
				"__HTTPS_PORT__", "18443",
			).Replace(string(content))
			content = []byte(rendered)
		}
		if err := decodeAll(content); err != nil {
			t.Errorf("%s: %v", path, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestKindProfilesUseKubernetes135KubeadmShape(t *testing.T) {
	root := repositoryRoot(t)
	for _, profile := range []string{"minimal", "standard", "full"} {
		path := filepath.Join(root, "hack", "kind-"+profile+".yaml.tmpl")
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(content)
		if !strings.Contains(text, "apiVersion: kubeadm.k8s.io/v1beta3") {
			t.Errorf("%s does not use the Kubernetes 1.35 kubeadm API", path)
		}
		if !regexp.MustCompile(`kubeletExtraArgs:\r?\n            node-labels: ingress-ready=true`).MatchString(text) {
			t.Errorf("%s does not use v1beta3 map-form kubelet arguments", path)
		}
		if strings.Contains(text, "kubeadm.k8s.io/v1beta4") || strings.Contains(text, "- name: node-labels") {
			t.Errorf("%s retains a Kubernetes 1.36 kubeadm shape", path)
		}
	}
}

func TestVersionLockContainsRequiredPins(t *testing.T) {
	root := repositoryRoot(t)
	content, err := os.ReadFile(filepath.Join(root, "scripts", "versions.env"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, key := range []string{
		"GO_VERSION", "KUBEBUILDER_VERSION", "KIND_VERSION", "KUBERNETES_VERSION",
		"KIND_NODE_IMAGE", "HELM_VERSION", "CALICO_VERSION", "ENVOY_GATEWAY_VERSION", "ARGO_CD_VERSION",
		"MIN_DOCKER_VERSION", "REQUIRED_CGROUP_VERSION",
		"CONTROLLER_TOOLS_VERSION", "CONTROLLER_RUNTIME_VERSION", "ENVTEST_K8S_VERSION",
		"SETUP_ENVTEST_VERSION", "KUSTOMIZE_VERSION", "GOLANGCI_LINT_VERSION",
		"VHS_VERSION", "TTYD_VERSION", "VHS_LINUX_X86_64_SHA256", "TTYD_LINUX_X86_64_SHA256",
		"GATEWAYCLASS_CRD_SHA256", "GATEWAY_CRD_SHA256", "HTTPROUTE_CRD_SHA256", "ARGO_CD_MANIFEST_SHA256",
		"ARGO_ROLLOUT_CRD_SHA256", "ARGO_ANALYSIS_TEMPLATE_CRD_SHA256", "ARGO_ANALYSIS_RUN_CRD_SHA256",
		"SERVICE_MONITOR_CRD_SHA256", "PROMETHEUS_RULE_CRD_SHA256",
		"GO_BUILDER_IMAGE", "OPERATOR_IMAGE", "DEMO_IMAGE", "ISOLATION_CLIENT_IMAGE",
	} {
		if !strings.Contains(text, key+"=") {
			t.Errorf("versions.env is missing %s", key)
		}
	}
	if !regexp.MustCompile(`(?m)^ARGO_CD_MANIFEST_SHA256=[0-9a-f]{64}$`).MatchString(text) {
		t.Error("ARGO_CD_MANIFEST_SHA256 must be a lowercase sha256 checksum")
	}
	if !strings.Contains(text, "KUBERNETES_VERSION=1.35.5\n") && !strings.Contains(text, "KUBERNETES_VERSION=1.35.5\r\n") {
		t.Error("KUBERNETES_VERSION must remain pinned to 1.35.5")
	}
	if !regexp.MustCompile(`(?m)^KIND_NODE_IMAGE=kindest/node:v1\.35\.5@sha256:ce977ae6d65918d0b58a5f8b5e940429c2ce42fa3a5619ec2bbc60b949c0ac95\r?$`).MatchString(text) {
		t.Error("KIND_NODE_IMAGE must remain pinned to the approved Kubernetes 1.35.5 digest")
	}
	for _, key := range []string{"ARGO_ROLLOUT_CRD_SHA256", "ARGO_ANALYSIS_TEMPLATE_CRD_SHA256", "ARGO_ANALYSIS_RUN_CRD_SHA256", "SERVICE_MONITOR_CRD_SHA256", "PROMETHEUS_RULE_CRD_SHA256"} {
		if !regexp.MustCompile(`(?m)^` + key + `=[0-9a-f]{64}$`).MatchString(text) {
			t.Errorf("%s must be a lowercase sha256 checksum", key)
		}
	}
	if !regexp.MustCompile(`(?m)^ISOLATION_CLIENT_IMAGE=[^@\s]+@sha256:[0-9a-f]{64}$`).MatchString(text) {
		t.Error("ISOLATION_CLIENT_IMAGE must be pinned by a sha256 digest")
	}
}

func decodeAll(content []byte) error {
	decoder := kyaml.NewYAMLOrJSONDecoder(bytes.NewReader(content), 4096)
	for {
		var document any
		if err := decoder.Decode(&document); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("could not find repository root")
		}
		directory = parent
	}
}
