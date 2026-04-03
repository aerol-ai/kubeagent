package tools

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ClusterTools provides namespace, node, event, and cluster operations.
type ClusterTools struct {
	client kubernetes.Interface
}

// NewClusterTools creates a ClusterTools instance.
func NewClusterTools(client kubernetes.Interface) *ClusterTools {
	return &ClusterTools{client: client}
}

// NamespaceSummary is a compact namespace representation.
type NamespaceSummary struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Age    string `json:"age"`
}

// NodeSummary is a compact node representation.
type NodeSummary struct {
	Name         string       `json:"name"`
	Status       string       `json:"status"`
	Roles        []string     `json:"roles"`
	Version      string       `json:"version"`
	OS           string       `json:"os"`
	Architecture string       `json:"architecture"`
	Resources    ResourceInfo `json:"resources"`
}

// ResourceInfo holds node resource details.
type ResourceInfo struct {
	CPUCapacity    string `json:"cpu_capacity"`
	MemoryCapacity string `json:"memory_capacity"`
	Pods           string `json:"pods"`
}

// EventSummary is a compact event representation.
type EventSummary struct {
	Type      string `json:"type"`
	Reason    string `json:"reason"`
	Message   string `json:"message"`
	Object    string `json:"object"`
	Count     int32  `json:"count"`
	FirstSeen string `json:"first_seen"`
	LastSeen  string `json:"last_seen"`
}

// ListNamespaces lists all namespaces.
func (c *ClusterTools) ListNamespaces(ctx context.Context) ([]NamespaceSummary, error) {
	nsList, err := c.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]NamespaceSummary, 0, len(nsList.Items))
	for _, ns := range nsList.Items {
		out = append(out, NamespaceSummary{
			Name:   ns.Name,
			Status: string(ns.Status.Phase),
			Age:    ns.CreationTimestamp.Time.Format("2006-01-02T15:04:05Z"),
		})
	}
	return out, nil
}

// CreateNamespace creates a namespace.
func (c *ClusterTools) CreateNamespace(ctx context.Context, name string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
	_, err := c.client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	return err
}

// protectedNamespaces cannot be deleted via the agent.
var protectedNamespaces = map[string]bool{
	"kube-system":     true,
	"kube-public":     true,
	"kube-node-lease": true,
	"default":         true,
}

// DeleteNamespace deletes a namespace. Protected system namespaces are rejected.
func (c *ClusterTools) DeleteNamespace(ctx context.Context, name string) error {
	if protectedNamespaces[name] {
		return fmt.Errorf("cannot delete protected namespace %q", name)
	}
	return c.client.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{})
}

// ListNodes lists all nodes.
func (c *ClusterTools) ListNodes(ctx context.Context) ([]NodeSummary, error) {
	nodes, err := c.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]NodeSummary, 0, len(nodes.Items))
	for _, n := range nodes.Items {
		status := "NotReady"
		for _, cond := range n.Status.Conditions {
			if cond.Type == "Ready" && cond.Status == "True" {
				status = "Ready"
			}
		}
		roles := make([]string, 0)
		for k := range n.Labels {
			if strings.HasPrefix(k, "node-role.kubernetes.io/") {
				roles = append(roles, k[24:])
			}
		}
		out = append(out, NodeSummary{
			Name:         n.Name,
			Status:       status,
			Roles:        roles,
			Version:      n.Status.NodeInfo.KubeletVersion,
			OS:           n.Status.NodeInfo.OSImage,
			Architecture: n.Status.NodeInfo.Architecture,
			Resources: ResourceInfo{
				CPUCapacity:    n.Status.Capacity.Cpu().String(),
				MemoryCapacity: n.Status.Capacity.Memory().String(),
				Pods:           n.Status.Capacity.Pods().String(),
			},
		})
	}
	return out, nil
}

// GetNode returns a single node.
func (c *ClusterTools) GetNode(ctx context.Context, name string) (*NodeSummary, error) {
	n, err := c.client.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	status := "NotReady"
	for _, cond := range n.Status.Conditions {
		if cond.Type == "Ready" && cond.Status == "True" {
			status = "Ready"
		}
	}
	roles := make([]string, 0)
	for k := range n.Labels {
		if strings.HasPrefix(k, "node-role.kubernetes.io/") {
			roles = append(roles, k[24:])
		}
	}
	summary := NodeSummary{
		Name:         n.Name,
		Status:       status,
		Roles:        roles,
		Version:      n.Status.NodeInfo.KubeletVersion,
		OS:           n.Status.NodeInfo.OSImage,
		Architecture: n.Status.NodeInfo.Architecture,
		Resources: ResourceInfo{
			CPUCapacity:    n.Status.Capacity.Cpu().String(),
			MemoryCapacity: n.Status.Capacity.Memory().String(),
			Pods:           n.Status.Capacity.Pods().String(),
		},
	}
	return &summary, nil
}

// ListEvents lists recent events in a namespace.
func (c *ClusterTools) ListEvents(ctx context.Context, namespace string) ([]EventSummary, error) {
	events, err := c.client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]EventSummary, 0, len(events.Items))
	for _, e := range events.Items {
		out = append(out, EventSummary{
			Type:      e.Type,
			Reason:    e.Reason,
			Message:   e.Message,
			Object:    fmt.Sprintf("%s/%s", e.InvolvedObject.Kind, e.InvolvedObject.Name),
			Count:     e.Count,
			FirstSeen: e.FirstTimestamp.Time.Format("2006-01-02T15:04:05Z"),
			LastSeen:  e.LastTimestamp.Time.Format("2006-01-02T15:04:05Z"),
		})
	}
	return out, nil
}

// GetClusterInfo returns version and node count.
func (c *ClusterTools) GetClusterInfo(ctx context.Context) (map[string]interface{}, error) {
	sv, err := c.client.Discovery().ServerVersion()
	if err != nil {
		return nil, err
	}
	nodes, err := c.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	nodeCount := 0
	if err == nil {
		nodeCount = len(nodes.Items)
	}
	ns, err := c.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	nsCount := 0
	if err == nil {
		nsCount = len(ns.Items)
	}
	return map[string]interface{}{
		"version":    sv.GitVersion,
		"platform":   sv.Platform,
		"nodes":      nodeCount,
		"namespaces": nsCount,
	}, nil
}

// ListConfigMaps lists config maps in a namespace.
func (c *ClusterTools) ListConfigMaps(ctx context.Context, namespace string) ([]map[string]interface{}, error) {
	cms, err := c.client.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(cms.Items))
	for _, cm := range cms.Items {
		keys := make([]string, 0, len(cm.Data))
		for k := range cm.Data {
			keys = append(keys, k)
		}
		out = append(out, map[string]interface{}{
			"name":      cm.Name,
			"namespace": cm.Namespace,
			"keys":      keys,
		})
	}
	return out, nil
}

// ListSecrets lists secrets (names and types only).
func (c *ClusterTools) ListSecrets(ctx context.Context, namespace string) ([]map[string]interface{}, error) {
	secrets, err := c.client.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(secrets.Items))
	for _, s := range secrets.Items {
		out = append(out, map[string]interface{}{
			"name":      s.Name,
			"namespace": s.Namespace,
			"type":      string(s.Type),
		})
	}
	return out, nil
}

// ListPVCs lists persistent volume claims in a namespace.
func (c *ClusterTools) ListPVCs(ctx context.Context, namespace string) ([]map[string]interface{}, error) {
	pvcs, err := c.client.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(pvcs.Items))
	for _, pvc := range pvcs.Items {
		storage := ""
		if req, ok := pvc.Spec.Resources.Requests["storage"]; ok {
			storage = req.String()
		}
		out = append(out, map[string]interface{}{
			"name":      pvc.Name,
			"namespace": pvc.Namespace,
			"status":    string(pvc.Status.Phase),
			"storage_class": func() string {
				if pvc.Spec.StorageClassName != nil {
					return *pvc.Spec.StorageClassName
				}
				return ""
			}(),
			"capacity": storage,
		})
	}
	return out, nil
}
