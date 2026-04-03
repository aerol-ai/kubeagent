package upgrade

import (
	"context"
	"fmt"
	"log"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/aerol-ai/kubeagent/pkg/config"
	"github.com/aerol-ai/kubeagent/pkg/k8s"
)

// Perform issues a StrategicMergePatch to the agent's Deployment to update its image tag.
func Perform(cfg *config.Config, newVersion string) {
	if cfg.PodNamespace == "" || cfg.DeploymentName == "" || cfg.AgentImageRepo == "" {
		log.Println("Missing required environment variables for auto-upgrade, aborting.")
		return
	}

	clients, err := k8s.NewClients(cfg)
	if err != nil {
		log.Printf("Failed to create K8s clients for upgrade: %v", err)
		return
	}
	
	newImage := fmt.Sprintf("%s:%s", cfg.AgentImageRepo, newVersion)
	patchData := fmt.Sprintf(`{"spec":{"template":{"spec":{"containers":[{"name":"agent","image":"%s"}]}}}}`, newImage)
	
	log.Printf("Upgrading agent deployment %s in namespace %s to image %s", cfg.DeploymentName, cfg.PodNamespace, newImage)
	
	_, err = clients.Clientset.AppsV1().Deployments(cfg.PodNamespace).Patch(
		context.Background(),
		cfg.DeploymentName,
		types.StrategicMergePatchType,
		[]byte(patchData),
		metav1.PatchOptions{},
	)
	
	if err != nil {
		log.Printf("Failed to patch deployment for auto-upgrade: %v", err)
	} else {
		log.Printf("Successfully patched deployment for auto-upgrade.")
	}
}
