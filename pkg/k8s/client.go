package k8s

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/aerol-ai/kubeagent/pkg/config"
)

// Clients bundles the Kubernetes API clients.
type Clients struct {
	Clientset     kubernetes.Interface
	DynamicClient dynamic.Interface
	RestConfig    *rest.Config
}

// NewClients creates Kubernetes clients from the agent config.
func NewClients(cfg *config.Config) (*Clients, error) {
	var restCfg *rest.Config
	var err error

	if cfg.InCluster {
		restCfg, err = rest.InClusterConfig()
	} else if cfg.KubeConfigPath != "" {
		restCfg, err = clientcmd.BuildConfigFromFlags("", cfg.KubeConfigPath)
	} else {
		return nil, fmt.Errorf("either InCluster or KubeConfigPath must be set")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to build k8s config: %w", err)
	}

	restCfg.QPS = 50
	restCfg.Burst = 100

	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}

	dynClient, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	return &Clients{
		Clientset:     clientset,
		DynamicClient: dynClient,
		RestConfig:    restCfg,
	}, nil
}

// GetClusterInfo returns K8s server version and node count.
func (c *Clients) GetClusterInfo() (string, int, error) {
	ctx := context.Background()
	sv, err := c.Clientset.Discovery().ServerVersion()
	if err != nil {
		return "", 0, err
	}
	nodes, err := c.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return sv.GitVersion, 0, nil
	}
	return sv.GitVersion, len(nodes.Items), nil
}
