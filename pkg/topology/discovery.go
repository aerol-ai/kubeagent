package topology

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// TopologyNode represents a K8s resource in the topology graph.
type TopologyNode struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Labels    map[string]string `json:"labels,omitempty"`
	Status    string            `json:"status,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// TopologyEdge represents a connection between resources.
type TopologyEdge struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	Type     string `json:"type"`
}

// TopologyGraph is the full topology result.
type TopologyGraph struct {
	Nodes []TopologyNode `json:"nodes"`
	Edges []TopologyEdge `json:"edges"`
	Stats TopologyStats  `json:"stats"`
}

// TopologyStats provides a summary of the graph.
type TopologyStats struct {
	Pods         int `json:"pods"`
	Services     int `json:"services"`
	Deployments  int `json:"deployments"`
	Ingresses    int `json:"ingresses"`
	Nodes        int `json:"nodes"`
	StatefulSets int `json:"statefulsets"`
	DaemonSets   int `json:"daemonsets"`
}

var systemNamespaces = map[string]bool{
	"kube-system":     true,
	"kube-public":     true,
	"kube-node-lease": true,
}

// Builder builds cluster topology graphs.
type Builder struct {
	client kubernetes.Interface
}

// NewBuilder creates a topology Builder.
func NewBuilder(client kubernetes.Interface) *Builder {
	return &Builder{client: client}
}

// BuildTopology scans the cluster and builds a graph.
func (b *Builder) BuildTopology(ctx context.Context, namespace string) (*TopologyGraph, error) {
	graph := &TopologyGraph{
		Nodes: make([]TopologyNode, 0),
		Edges: make([]TopologyEdge, 0),
	}

	namespaces := []string{namespace}
	if namespace == "" {
		nsList, err := b.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		namespaces = make([]string, 0)
		for _, ns := range nsList.Items {
			if !systemNamespaces[ns.Name] {
				namespaces = append(namespaces, ns.Name)
			}
		}
	}

	for _, ns := range namespaces {
		b.addPods(ctx, ns, graph)
		b.addDeployments(ctx, ns, graph)
		b.addServices(ctx, ns, graph)
		b.addIngresses(ctx, ns, graph)
		b.addStatefulSets(ctx, ns, graph)
		b.addDaemonSets(ctx, ns, graph)
	}

	// Add cluster nodes
	nodes, err := b.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err == nil {
		graph.Stats.Nodes = len(nodes.Items)
		for _, n := range nodes.Items {
			graph.Nodes = append(graph.Nodes, TopologyNode{
				ID:   fmt.Sprintf("node/%s", n.Name),
				Kind: "Node",
				Name: n.Name,
			})
		}
		// Connect pods to their nodes
		for _, node := range graph.Nodes {
			if node.Kind == "Pod" && node.Details != nil {
				if nodeName, ok := node.Details["node"].(string); ok {
					graph.Edges = append(graph.Edges, TopologyEdge{
						Source: node.ID,
						Target: fmt.Sprintf("node/%s", nodeName),
						Type:   "runs-on",
					})
				}
			}
		}
	}

	return graph, nil
}

func (b *Builder) addPods(ctx context.Context, namespace string, graph *TopologyGraph) {
	pods, err := b.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	graph.Stats.Pods += len(pods.Items)
	for _, pod := range pods.Items {
		graph.Nodes = append(graph.Nodes, TopologyNode{
			ID:        fmt.Sprintf("pod/%s/%s", pod.Namespace, pod.Name),
			Kind:      "Pod",
			Name:      pod.Name,
			Namespace: pod.Namespace,
			Labels:    pod.Labels,
			Status:    string(pod.Status.Phase),
			Details: map[string]interface{}{
				"ip":   pod.Status.PodIP,
				"node": pod.Spec.NodeName,
			},
		})
	}
}

func (b *Builder) addDeployments(ctx context.Context, namespace string, graph *TopologyGraph) {
	deps, err := b.client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	graph.Stats.Deployments += len(deps.Items)
	for _, dep := range deps.Items {
		depID := fmt.Sprintf("deployment/%s/%s", dep.Namespace, dep.Name)
		graph.Nodes = append(graph.Nodes, TopologyNode{
			ID:        depID,
			Kind:      "Deployment",
			Name:      dep.Name,
			Namespace: dep.Namespace,
			Labels:    dep.Labels,
		})
		// Connect to pods via label selector
		for _, node := range graph.Nodes {
			if node.Kind == "Pod" && node.Namespace == dep.Namespace {
				if matchLabels(node.Labels, dep.Spec.Selector.MatchLabels) {
					graph.Edges = append(graph.Edges, TopologyEdge{
						Source: depID,
						Target: node.ID,
						Type:   "manages",
					})
				}
			}
		}
	}
}

func (b *Builder) addServices(ctx context.Context, namespace string, graph *TopologyGraph) {
	svcs, err := b.client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	graph.Stats.Services += len(svcs.Items)
	for _, svc := range svcs.Items {
		svcID := fmt.Sprintf("service/%s/%s", svc.Namespace, svc.Name)
		graph.Nodes = append(graph.Nodes, TopologyNode{
			ID:        svcID,
			Kind:      "Service",
			Name:      svc.Name,
			Namespace: svc.Namespace,
			Labels:    svc.Labels,
			Details: map[string]interface{}{
				"type":      string(svc.Spec.Type),
				"clusterIP": svc.Spec.ClusterIP,
			},
		})
		// Connect service to pods
		for _, node := range graph.Nodes {
			if node.Kind == "Pod" && node.Namespace == svc.Namespace {
				if matchLabels(node.Labels, svc.Spec.Selector) {
					graph.Edges = append(graph.Edges, TopologyEdge{
						Source: svcID,
						Target: node.ID,
						Type:   "selects",
					})
				}
			}
		}
	}
}

func (b *Builder) addIngresses(ctx context.Context, namespace string, graph *TopologyGraph) {
	ings, err := b.client.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	graph.Stats.Ingresses += len(ings.Items)
	for _, ing := range ings.Items {
		ingID := fmt.Sprintf("ingress/%s/%s", ing.Namespace, ing.Name)
		graph.Nodes = append(graph.Nodes, TopologyNode{
			ID:        ingID,
			Kind:      "Ingress",
			Name:      ing.Name,
			Namespace: ing.Namespace,
			Labels:    ing.Labels,
		})
		for _, rule := range ing.Spec.Rules {
			if rule.HTTP == nil {
				continue
			}
			for _, path := range rule.HTTP.Paths {
				if path.Backend.Service != nil {
					svcID := fmt.Sprintf("service/%s/%s", ing.Namespace, path.Backend.Service.Name)
					graph.Edges = append(graph.Edges, TopologyEdge{
						Source: ingID,
						Target: svcID,
						Type:   "routes",
					})
				}
			}
		}
	}
}

func (b *Builder) addStatefulSets(ctx context.Context, namespace string, graph *TopologyGraph) {
	sts, err := b.client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	graph.Stats.StatefulSets += len(sts.Items)
	for _, s := range sts.Items {
		sID := fmt.Sprintf("statefulset/%s/%s", s.Namespace, s.Name)
		graph.Nodes = append(graph.Nodes, TopologyNode{
			ID:        sID,
			Kind:      "StatefulSet",
			Name:      s.Name,
			Namespace: s.Namespace,
			Labels:    s.Labels,
		})
		for _, node := range graph.Nodes {
			if node.Kind == "Pod" && node.Namespace == s.Namespace {
				if matchLabels(node.Labels, s.Spec.Selector.MatchLabels) {
					graph.Edges = append(graph.Edges, TopologyEdge{
						Source: sID,
						Target: node.ID,
						Type:   "manages",
					})
				}
			}
		}
	}
}

func (b *Builder) addDaemonSets(ctx context.Context, namespace string, graph *TopologyGraph) {
	dsList, err := b.client.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	graph.Stats.DaemonSets += len(dsList.Items)
	for _, ds := range dsList.Items {
		dsID := fmt.Sprintf("daemonset/%s/%s", ds.Namespace, ds.Name)
		graph.Nodes = append(graph.Nodes, TopologyNode{
			ID:        dsID,
			Kind:      "DaemonSet",
			Name:      ds.Name,
			Namespace: ds.Namespace,
			Labels:    ds.Labels,
		})
		for _, node := range graph.Nodes {
			if node.Kind == "Pod" && node.Namespace == ds.Namespace {
				if matchLabels(node.Labels, ds.Spec.Selector.MatchLabels) {
					graph.Edges = append(graph.Edges, TopologyEdge{
						Source: dsID,
						Target: node.ID,
						Type:   "manages",
					})
				}
			}
		}
	}
}

func matchLabels(podLabels, selector map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for k, v := range selector {
		if podLabels[k] != v {
			return false
		}
	}
	return true
}
