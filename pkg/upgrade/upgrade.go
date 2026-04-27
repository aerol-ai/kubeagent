package upgrade

import (
	"context"
	"fmt"
	"log"
	"strings"

	"golang.org/x/mod/semver"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/aerol-ai/kubeagent/pkg/config"
	"github.com/aerol-ai/kubeagent/pkg/k8s"
)

const agentContainerName = "agent"

// NormalizeVersion returns a semver-normalized version string or an empty string when invalid.
func NormalizeVersion(version string) string {
	trimmed := strings.TrimSpace(version)
	if trimmed == "" {
		return ""
	}
	if trimmed[0] != 'v' {
		trimmed = "v" + trimmed
	}
	if !semver.IsValid(trimmed) {
		return ""
	}
	return trimmed
}

// IsNewerVersion reports whether candidate is a newer semver than current.
func IsNewerVersion(candidate, current string) bool {
	normalizedCandidate := NormalizeVersion(candidate)
	if normalizedCandidate == "" {
		return false
	}

	normalizedCurrent := NormalizeVersion(current)
	if normalizedCurrent == "" {
		return true
	}

	return semver.Compare(normalizedCandidate, normalizedCurrent) > 0
}

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

	currentVersion, err := CurrentDeploymentVersion(context.Background(), clients, cfg)
	if err == nil && !IsNewerVersion(newVersion, currentVersion) {
		if NormalizeVersion(newVersion) == NormalizeVersion(currentVersion) {
			log.Printf("Agent deployment is already on version %s, skipping auto-upgrade.", currentVersion)
		} else {
			log.Printf("Skipping auto-upgrade because current version %s is not older than target %s.", currentVersion, newVersion)
		}
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

// CurrentDeploymentVersion returns the deployed agent image tag.
func CurrentDeploymentVersion(ctx context.Context, clients *k8s.Clients, cfg *config.Config) (string, error) {
	deployment, err := clients.Clientset.AppsV1().Deployments(cfg.PodNamespace).Get(ctx, cfg.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return imageTagFromDeployment(deployment)
}

func imageTagFromDeployment(deployment *appsv1.Deployment) (string, error) {
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name != agentContainerName {
			continue
		}
		tag := imageTag(container.Image)
		if tag == "" {
			return "", fmt.Errorf("agent container image %q does not include a tag", container.Image)
		}
		return tag, nil
	}
	return "", fmt.Errorf("container %q not found in deployment", agentContainerName)
}

func imageTag(image string) string {
	trimmed := strings.TrimSpace(image)
	if trimmed == "" {
		return ""
	}
	if at := strings.Index(trimmed, "@"); at >= 0 {
		trimmed = trimmed[:at]
	}
	lastSlash := strings.LastIndex(trimmed, "/")
	lastColon := strings.LastIndex(trimmed, ":")
	if lastColon <= lastSlash {
		return ""
	}
	return trimmed[lastColon+1:]
}
