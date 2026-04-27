package upgrade

import (
	"context"
	"log"
	"time"

	"github.com/aerol-ai/kubeagent/pkg/config"
	"github.com/aerol-ai/kubeagent/pkg/k8s"
)

const autoUpgradeCheckTimeout = 15 * time.Second

// StartAutoUpdater polls the remote image registry and patches the deployment when a newer release exists.
func StartAutoUpdater(ctx context.Context, cfg *config.Config, currentVersion string) {
	if !cfg.AutoUpgradeEnabled {
		return
	}
	if cfg.AgentImageRepo == "" || cfg.PodNamespace == "" || cfg.DeploymentName == "" {
		log.Println("Auto-upgrade watcher disabled: AGENT_IMAGE_REPO, POD_NAMESPACE, and DEPLOYMENT_NAME are required.")
		return
	}

	clients, err := k8s.NewClients(cfg)
	if err != nil {
		log.Printf("Auto-upgrade watcher disabled: failed to create K8s clients: %v", err)
		return
	}

	interval := cfg.AutoUpgradeInterval
	if interval <= 0 {
		interval = time.Minute
	}

	log.Printf("Auto-upgrade watcher enabled for %s (poll interval: %s)", cfg.AgentImageRepo, interval)
	checkAndUpgrade(ctx, cfg, clients, currentVersion)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkAndUpgrade(ctx, cfg, clients, currentVersion)
		}
	}
}

func checkAndUpgrade(ctx context.Context, cfg *config.Config, clients *k8s.Clients, currentVersion string) {
	checkCtx, cancel := context.WithTimeout(ctx, autoUpgradeCheckTimeout)
	defer cancel()

	deployedVersion, err := CurrentDeploymentVersion(checkCtx, clients, cfg)
	if err != nil {
		if currentVersion == "" {
			log.Printf("Auto-upgrade check failed to determine current deployment version: %v", err)
			return
		}
		deployedVersion = currentVersion
	}

	latestVersion, err := LatestRemoteVersion(checkCtx, cfg.AgentImageRepo)
	if err != nil {
		log.Printf("Auto-upgrade check failed to query remote version: %v", err)
		return
	}

	if !IsNewerVersion(latestVersion, deployedVersion) {
		return
	}

	log.Printf("Remote agent release %s is newer than deployed version %s. Triggering automatic upgrade.", latestVersion, deployedVersion)
	Perform(cfg, latestVersion)
}
