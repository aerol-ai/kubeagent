package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aerol-ai/kubeagent/pkg/config"
	"github.com/aerol-ai/kubeagent/pkg/executor"
	"github.com/aerol-ai/kubeagent/pkg/health"
	"github.com/aerol-ai/kubeagent/pkg/k8s"
	"github.com/aerol-ai/kubeagent/pkg/upgrade"
	"github.com/aerol-ai/kubeagent/pkg/ws"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.SetOutput(os.Stdout)

	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	clients, err := k8s.NewClients(cfg)
	if err != nil {
		log.Fatalf("Failed to create K8s clients: %v", err)
	}

	exec := executor.NewExecutor(clients, cfg)
	log.Printf("Loaded %d tools", len(exec.ListTools()))

	ver, nodeCount, err := clients.GetClusterInfo()
	if err == nil {
		log.Printf("Connected to K8s cluster %s (%d nodes)", ver, nodeCount)
	}

	// Build agent status for platform registration
	agentStatus := &config.AgentStatus{
		AgentID:    cfg.AgentID,
		Version:    ws.Version,
		K8sVersion: ver,
		NodeCount:  nodeCount,
		Ready:      true,
	}
	// Fetch namespace list for status
	nsList, err := clients.Clientset.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
	if err == nil {
		for _, ns := range nsList.Items {
			agentStatus.Namespaces = append(agentStatus.Namespaces, ns.Name)
		}
	}

	// Start health server
	healthSrv := health.NewServer()
	go func() {
		addr := getEnv("HEALTH_ADDR", ":8081")
		log.Printf("Health server listening on %s", addr)
		if err := healthSrv.ListenAndServe(addr); err != nil {
			log.Printf("Health server error: %v", err)
		}
	}()

	if cfg.AutoUpgradeEnabled {
		go upgrade.StartAutoUpdater(context.Background(), cfg, ws.Version)
	}

	var wsClient *ws.Client
	wsClient = ws.NewClient(cfg, func(cmd config.Command) {
		result := exec.Execute(cmd)
		if err := wsClient.SendResult(result); err != nil {
			log.Println("Failed to send result")
		}
	})
	wsClient.SetAgentStatus(agentStatus)
	wsClient.SetConnectionCallback(func(connected bool) {
		healthSrv.SetConnected(connected)
	})

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Println("Shutting down...")
		wsClient.Close()
		os.Exit(0)
	}()

	log.Println("Starting kubeagent...")
	if err := wsClient.ConnectWithRetry(); err != nil {
		log.Fatalf("WebSocket connection failed: %v", err)
	}
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
