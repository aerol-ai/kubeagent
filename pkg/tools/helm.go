package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aerol-ai/kubeagent/pkg/config"
	"k8s.io/client-go/rest"
	clientcmd "k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const helmBinaryPath = "/usr/local/bin/helm"

type HelmTools struct {
	restCfg *rest.Config
}

type HelmUpsertResult struct {
	ReleaseName string `json:"releaseName"`
	Namespace   string `json:"namespace"`
	Status      string `json:"status,omitempty"`
	Revision    string `json:"revision,omitempty"`
	Chart       string `json:"chart,omitempty"`
	AppVersion  string `json:"appVersion,omitempty"`
	Output      string `json:"output,omitempty"`
}

type helmStatusPayload struct {
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	Revision   string `json:"revision"`
	Chart      string `json:"chart"`
	AppVersion string `json:"app_version"`
	Info       struct {
		Status string `json:"status"`
	} `json:"info"`
}

func NewHelmTools(restCfg *rest.Config) *HelmTools {
	return &HelmTools{restCfg: restCfg}
}

func (h *HelmTools) UpsertRelease(ctx context.Context, input config.HelmUpsertInput) (*HelmUpsertResult, error) {
	env, workspace, cleanup, err := h.prepareHelmEnvironment(input.Namespace)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	chartRef := buildHelmChartRef(input)
	timeoutSeconds := input.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 300
	}

	args := []string{}
	if input.Upgrade {
		args = append(args, "upgrade", input.ReleaseName, chartRef, "--install")
	} else {
		args = append(args, "install", input.ReleaseName, chartRef)
	}

	args = append(args, "--namespace", input.Namespace)
	if input.CreateNamespace {
		args = append(args, "--create-namespace")
	}
	if input.Version != "" {
		args = append(args, "--version", input.Version)
	}
	if input.RepoURL != "" && !strings.HasPrefix(input.RepoURL, "oci://") {
		args = append(args, "--repo", input.RepoURL)
	}
	args = append(args, "--wait", "--timeout", fmt.Sprintf("%ds", timeoutSeconds))

	if strings.TrimSpace(input.ValuesYAML) != "" {
		valuesPath := filepath.Join(workspace, "values.yaml")
		if err := os.WriteFile(valuesPath, []byte(input.ValuesYAML), 0o600); err != nil {
			return nil, err
		}
		args = append(args, "-f", valuesPath)
	}

	output, err := runHelm(ctx, env, args...)
	if err != nil {
		return nil, err
	}

	result := &HelmUpsertResult{
		ReleaseName: input.ReleaseName,
		Namespace:   input.Namespace,
		Output:      output,
	}

	statusOutput, statusErr := runHelm(ctx, env, "status", input.ReleaseName, "--namespace", input.Namespace, "-o", "json")
	if statusErr == nil {
		var payload helmStatusPayload
		if json.Unmarshal([]byte(statusOutput), &payload) == nil {
			result.Status = payload.Info.Status
			result.Revision = payload.Revision
			result.Chart = payload.Chart
			result.AppVersion = payload.AppVersion
		}
	}

	return result, nil
}

func (h *HelmTools) ListReleases(ctx context.Context, namespace string) ([]map[string]any, error) {
	env, _, cleanup, err := h.prepareHelmEnvironment(namespace)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	args := []string{"list", "-o", "json"}
	if namespace != "" {
		args = append(args, "--namespace", namespace)
	} else {
		args = append(args, "--all-namespaces")
	}

	output, err := runHelm(ctx, env, args...)
	if err != nil {
		if strings.Contains(err.Error(), "no releases found") {
			return []map[string]any{}, nil
		}
		return nil, err
	}

	var releases []map[string]any
	if strings.TrimSpace(output) == "" {
		return []map[string]any{}, nil
	}
	if err := json.Unmarshal([]byte(output), &releases); err != nil {
		return nil, err
	}
	return releases, nil
}

func (h *HelmTools) prepareHelmEnvironment(namespace string) ([]string, string, func(), error) {
	workspace, err := os.MkdirTemp("/tmp", "kubeagent-helm-")
	if err != nil {
		return nil, "", func() {}, err
	}

	cleanup := func() {
		_ = os.RemoveAll(workspace)
	}

	cacheDir := filepath.Join(workspace, "cache")
	configDir := filepath.Join(workspace, "config")
	dataDir := filepath.Join(workspace, "data")
	for _, dir := range []string{cacheDir, configDir, dataDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			cleanup()
			return nil, "", func() {}, err
		}
	}

	kubeConfig, err := buildKubeConfig(h.restCfg, namespace)
	if err != nil {
		cleanup()
		return nil, "", func() {}, err
	}

	kubeConfigPath := filepath.Join(workspace, "kubeconfig")
	if err := os.WriteFile(kubeConfigPath, kubeConfig, 0o600); err != nil {
		cleanup()
		return nil, "", func() {}, err
	}

	env := append(os.Environ(),
		"KUBECONFIG="+kubeConfigPath,
		"HELM_CACHE_HOME="+cacheDir,
		"HELM_CONFIG_HOME="+configDir,
		"HELM_DATA_HOME="+dataDir,
		"XDG_CACHE_HOME="+cacheDir,
		"XDG_CONFIG_HOME="+configDir,
		"XDG_DATA_HOME="+dataDir,
	)

	return env, workspace, cleanup, nil
}

func buildKubeConfig(restCfg *rest.Config, namespace string) ([]byte, error) {
	cluster := &clientcmdapi.Cluster{
		Server:                restCfg.Host,
		InsecureSkipTLSVerify: restCfg.Insecure,
	}
	if restCfg.CAFile != "" {
		cluster.CertificateAuthority = restCfg.CAFile
	}
	if len(restCfg.CAData) > 0 {
		cluster.CertificateAuthorityData = restCfg.CAData
	}

	authInfo := &clientcmdapi.AuthInfo{}
	if restCfg.BearerToken != "" {
		authInfo.Token = restCfg.BearerToken
	}
	if restCfg.BearerTokenFile != "" {
		authInfo.TokenFile = restCfg.BearerTokenFile
	}
	if restCfg.CertFile != "" {
		authInfo.ClientCertificate = restCfg.CertFile
	}
	if len(restCfg.CertData) > 0 {
		authInfo.ClientCertificateData = restCfg.CertData
	}
	if restCfg.KeyFile != "" {
		authInfo.ClientKey = restCfg.KeyFile
	}
	if len(restCfg.KeyData) > 0 {
		authInfo.ClientKeyData = restCfg.KeyData
	}

	configData := clientcmdapi.Config{
		Kind:       "Config",
		APIVersion: "v1",
		Clusters:   map[string]*clientcmdapi.Cluster{"cluster": cluster},
		AuthInfos:  map[string]*clientcmdapi.AuthInfo{"agent": authInfo},
		Contexts: map[string]*clientcmdapi.Context{
			"agent": &clientcmdapi.Context{
				Cluster:   "cluster",
				AuthInfo:  "agent",
				Namespace: namespace,
			},
		},
		CurrentContext: "agent",
	}

	return clientcmd.Write(configData)
}

func buildHelmChartRef(input config.HelmUpsertInput) string {
	if strings.HasPrefix(input.Chart, "oci://") {
		return input.Chart
	}
	if strings.HasPrefix(input.RepoURL, "oci://") {
		trimmedChart := strings.TrimPrefix(input.Chart, "/")
		repoURL := strings.TrimSuffix(input.RepoURL, "/")
		repoWithoutScheme := strings.TrimPrefix(repoURL, "oci://")
		if strings.HasPrefix(trimmedChart, repoWithoutScheme+"/") {
			return "oci://" + trimmedChart
		}
		return repoURL + "/" + trimmedChart
	}
	if input.RepoURL != "" {
		return chartNameOnly(input.Chart)
	}
	if input.RepoName != "" && !strings.Contains(input.Chart, "/") {
		return input.RepoName + "/" + input.Chart
	}
	return input.Chart
}

func chartNameOnly(chart string) string {
	parts := strings.Split(chart, "/")
	if len(parts) == 0 {
		return chart
	}
	return parts[len(parts)-1]
}

func runHelm(ctx context.Context, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, helmBinaryPath, args...)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if text == "" {
			return "", err
		}
		return "", fmt.Errorf("helm failed: %s", text)
	}
	return text, nil
}

func ParseHelmTimeoutSeconds(input json.RawMessage, fallback int) int {
	if fallback <= 0 {
		fallback = 60
	}
	var payload map[string]any
	if err := json.Unmarshal(input, &payload); err != nil {
		return fallback
	}
	raw, ok := payload["timeoutSeconds"]
	if !ok {
		return fallback
	}
	switch value := raw.(type) {
	case float64:
		if value > 0 {
			return int(value)
		}
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}
