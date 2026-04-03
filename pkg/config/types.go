package config

import "encoding/json"

// Command represents an incoming command from the platform.
type Command struct {
	Type      string          `json:"type,omitempty"`
	Version   string          `json:"version,omitempty"`
	RequestID string          `json:"request_id,omitempty"`
	Tool      string          `json:"tool,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
}

// Result represents the result of a command execution.
type Result struct {
	RequestID string      `json:"request_id"`
	Success   bool        `json:"success"`
	Data      interface{} `json:"data,omitempty"`
	Error     string      `json:"error,omitempty"`
}

// AgentStatus is sent as a heartbeat to the platform.
type AgentStatus struct {
	AgentID    string   `json:"agent_id"`
	Version    string   `json:"version"`
	K8sVersion string   `json:"k8s_version"`
	NodeCount  int      `json:"node_count"`
	Namespaces []string `json:"namespaces"`
	Ready      bool     `json:"ready"`
}

// --- Tool input types ---

type NamespaceInput struct {
	Name string `json:"name"`
}

type PodInput struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type PodLogsInput struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Container string `json:"container"`
	TailLines int64  `json:"tail_lines"`
	Previous  bool   `json:"previous"`
}

type DeploymentInput struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type ScaleInput struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Replicas  int32  `json:"replicas"`
}

type ApplyInput struct {
	Manifest  string `json:"manifest"`
	Namespace string `json:"namespace"`
}

type DeleteInput struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type ExecInput struct {
	Namespace string   `json:"namespace"`
	Pod       string   `json:"pod"`
	Container string   `json:"container"`
	Command   []string `json:"command"`
}

type TopologyInput struct {
	Namespace string `json:"namespace"`
}

type TrafficInput struct {
	Namespace string `json:"namespace"`
}
