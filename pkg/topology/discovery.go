package topology

import (
	"context"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// systemNamespaces is shared with traffic.go
var systemNamespaces = map[string]bool{
	"kube-system":     true,
	"kube-public":     true,
	"kube-node-lease": true,
}

// TopologyNode represents a K8s resource in the topology graph.
type TopologyNode struct {
	ID        string                 `json:"id"`
	Kind      string                 `json:"kind"`
	Name      string                 `json:"name"`
	Namespace string                 `json:"namespace"`
	Labels    map[string]string      `json:"labels,omitempty"`
	Status    string                 `json:"status,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// TopologyEdge represents a connection between resources.
type TopologyEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
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
		Nodes: make([]TopologyNode, 0, 256),
		Edges: make([]TopologyEdge, 0, 256),
	}

	// nodeIDs prevents duplicate node entries
	nodeIDs := make(map[string]bool)
	addNode := func(n TopologyNode) {
		if nodeIDs[n.ID] {
			return
		}
		nodeIDs[n.ID] = true
		graph.Nodes = append(graph.Nodes, n)
	}
	addEdge := func(e TopologyEdge) {
		graph.Edges = append(graph.Edges, e)
	}

	// Collect all namespaces
	namespaces := []string{namespace}
	if namespace == "" {
		nsList, err := b.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		namespaces = make([]string, 0, len(nsList.Items))
		for _, ns := range nsList.Items {
			namespaces = append(namespaces, ns.Name)
		}
	}

	// ---------------------------------------------------------------
	// Phase 1 — Collect all resources
	// ---------------------------------------------------------------
	for _, ns := range namespaces {
		b.addPods(ctx, ns, addNode, graph)
		b.addDeployments(ctx, ns, addNode, addEdge, graph)
		b.addReplicaSets(ctx, ns, addNode, addEdge, graph)
		b.addStatefulSets(ctx, ns, addNode, addEdge, graph)
		b.addDaemonSets(ctx, ns, addNode, addEdge, graph)
		b.addServices(ctx, ns, addNode, addEdge, graph)
		b.addIngresses(ctx, ns, addNode, addEdge, graph)
		b.addEndpoints(ctx, ns, addNode, graph)
		b.addConfigMaps(ctx, ns, addNode)
		b.addSecrets(ctx, ns, addNode)
		b.addPVCs(ctx, ns, addNode)
		b.addServiceAccounts(ctx, ns, addNode)
		b.addJobs(ctx, ns, addNode, addEdge, graph)
		b.addCronJobs(ctx, ns, addNode, addEdge, graph)
		b.addHPAs(ctx, ns, addNode, addEdge)
		b.addNetworkPolicies(ctx, ns, addNode)
	}

	// Cluster-scoped: Nodes, PVs
	b.addClusterNodes(ctx, addNode, addEdge, graph)
	b.addPVs(ctx, addNode)

	// ---------------------------------------------------------------
	// Phase 2 — Build cross-resource edges
	// ---------------------------------------------------------------
	b.buildWorkloadConfigEdges(ctx, namespaces, addEdge)
	b.buildServiceToWorkloadEdges(graph, addEdge)

	return graph, nil
}

// ===================================================================
// Resource collectors
// ===================================================================

func (b *Builder) addPods(ctx context.Context, namespace string, addNode func(TopologyNode), graph *TopologyGraph) {
	pods, err := b.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	graph.Stats.Pods += len(pods.Items)
	for _, pod := range pods.Items {
		// Container status details
		containerStatuses := make([]map[string]interface{}, 0, len(pod.Status.ContainerStatuses))
		totalRestarts := int32(0)
		readyContainers := 0
		for _, cs := range pod.Status.ContainerStatuses {
			totalRestarts += cs.RestartCount
			if cs.Ready {
				readyContainers++
			}
			state := "waiting"
			if cs.State.Running != nil {
				state = "running"
			} else if cs.State.Terminated != nil {
				state = "terminated"
			}
			containerStatuses = append(containerStatuses, map[string]interface{}{
				"name":         cs.Name,
				"ready":        cs.Ready,
				"restartCount": cs.RestartCount,
				"state":        state,
			})
		}

		// Collect images and container names
		images := make([]string, 0, len(pod.Spec.Containers)+len(pod.Spec.InitContainers))
		containers := make([]string, 0, len(pod.Spec.Containers))
		for _, c := range pod.Spec.Containers {
			images = append(images, c.Image)
			containers = append(containers, c.Name)
		}
		for _, c := range pod.Spec.InitContainers {
			images = append(images, c.Image)
		}

		saName := pod.Spec.ServiceAccountName
		if saName == "" {
			saName = "default"
		}

		addNode(TopologyNode{
			ID:        fmt.Sprintf("pod/%s/%s", pod.Namespace, pod.Name),
			Kind:      "Pod",
			Name:      pod.Name,
			Namespace: pod.Namespace,
			Labels:    pod.Labels,
			Status:    string(pod.Status.Phase),
			Details: map[string]interface{}{
				"phase":             string(pod.Status.Phase),
				"podIP":             pod.Status.PodIP,
				"hostIP":            pod.Status.HostIP,
				"nodeName":          pod.Spec.NodeName,
				"containerCount":    len(pod.Spec.Containers),
				"readyContainers":   readyContainers,
				"totalRestarts":     totalRestarts,
				"containerStatuses": containerStatuses,
				"images":            images,
				"containers":        containers,
				"restartPolicy":     string(pod.Spec.RestartPolicy),
				"serviceAccount":    saName,
			},
		})
	}
}

func (b *Builder) addDeployments(ctx context.Context, namespace string, addNode func(TopologyNode), addEdge func(TopologyEdge), graph *TopologyGraph) {
	deps, err := b.client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	graph.Stats.Deployments += len(deps.Items)
	for _, dep := range deps.Items {
		depID := fmt.Sprintf("deployment/%s/%s", dep.Namespace, dep.Name)

		images, containers := extractContainerInfo(dep.Spec.Template.Spec.Containers, dep.Spec.Template.Spec.InitContainers)
		conditions := extractDeploymentConditions(dep.Status.Conditions)

		details := map[string]interface{}{
			"replicas":           dep.Status.Replicas,
			"readyReplicas":      dep.Status.ReadyReplicas,
			"availableReplicas":  dep.Status.AvailableReplicas,
			"updatedReplicas":    dep.Status.UpdatedReplicas,
			"images":             images,
			"containers":         containers,
			"conditions":         conditions,
		}
		if dep.Spec.Strategy.Type != "" {
			details["strategy"] = string(dep.Spec.Strategy.Type)
		}
		if dep.Spec.RevisionHistoryLimit != nil {
			details["revisionHistoryLimit"] = *dep.Spec.RevisionHistoryLimit
		}
		if hr := helmRelease(dep.Annotations, dep.Labels); hr != "" {
			details["helmRelease"] = hr
		}

		addNode(TopologyNode{
			ID:        depID,
			Kind:      "Deployment",
			Name:      dep.Name,
			Namespace: dep.Namespace,
			Labels:    dep.Labels,
			Details:   details,
		})

		// Deployment → Pods
		for _, node := range graph.Nodes {
			if node.Kind == "Pod" && node.Namespace == dep.Namespace {
				if matchLabels(node.Labels, dep.Spec.Selector.MatchLabels) {
					addEdge(TopologyEdge{Source: depID, Target: node.ID, Type: "manages"})
				}
			}
		}

		// Deployment → ServiceAccount
		saName := dep.Spec.Template.Spec.ServiceAccountName
		if saName == "" {
			saName = "default"
		}
		addEdge(TopologyEdge{
			Source: depID,
			Target: fmt.Sprintf("serviceaccount/%s/%s", dep.Namespace, saName),
			Type:   "uses-sa",
		})
	}
}

func (b *Builder) addReplicaSets(ctx context.Context, namespace string, addNode func(TopologyNode), addEdge func(TopologyEdge), graph *TopologyGraph) {
	rsList, err := b.client.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	for _, rs := range rsList.Items {
		rsID := fmt.Sprintf("replicaset/%s/%s", rs.Namespace, rs.Name)
		details := map[string]interface{}{
			"replicas":      rs.Status.Replicas,
			"readyReplicas": rs.Status.ReadyReplicas,
		}
		if hr := helmRelease(rs.Annotations, rs.Labels); hr != "" {
			details["helmRelease"] = hr
		}

		addNode(TopologyNode{
			ID:        rsID,
			Kind:      "ReplicaSet",
			Name:      rs.Name,
			Namespace: rs.Namespace,
			Labels:    rs.Labels,
			Details:   details,
		})

		// Owner ref: ReplicaSet is owned by a Deployment
		for _, owner := range rs.OwnerReferences {
			if owner.Kind == "Deployment" {
				addEdge(TopologyEdge{
					Source: fmt.Sprintf("deployment/%s/%s", rs.Namespace, owner.Name),
					Target: rsID,
					Type:   "owns",
				})
			}
		}
	}
}

func (b *Builder) addStatefulSets(ctx context.Context, namespace string, addNode func(TopologyNode), addEdge func(TopologyEdge), graph *TopologyGraph) {
	sts, err := b.client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	graph.Stats.StatefulSets += len(sts.Items)
	for _, s := range sts.Items {
		sID := fmt.Sprintf("statefulset/%s/%s", s.Namespace, s.Name)

		images, containers := extractContainerInfo(s.Spec.Template.Spec.Containers, s.Spec.Template.Spec.InitContainers)

		details := map[string]interface{}{
			"replicas":            s.Status.Replicas,
			"readyReplicas":       s.Status.ReadyReplicas,
			"currentReplicas":     s.Status.CurrentReplicas,
			"images":              images,
			"containers":          containers,
			"serviceName":         s.Spec.ServiceName,
			"podManagementPolicy": string(s.Spec.PodManagementPolicy),
		}
		if s.Spec.UpdateStrategy.Type != "" {
			details["updateStrategy"] = string(s.Spec.UpdateStrategy.Type)
		}
		if hr := helmRelease(s.Annotations, s.Labels); hr != "" {
			details["helmRelease"] = hr
		}

		// Volume claim templates
		if len(s.Spec.VolumeClaimTemplates) > 0 {
			claims := make([]map[string]interface{}, 0, len(s.Spec.VolumeClaimTemplates))
			for _, vct := range s.Spec.VolumeClaimTemplates {
				claim := map[string]interface{}{"name": vct.Name}
				if storage, ok := vct.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
					claim["storage"] = storage.String()
				}
				if vct.Spec.StorageClassName != nil {
					claim["storageClass"] = *vct.Spec.StorageClassName
				}
				claims = append(claims, claim)
			}
			details["volumeClaims"] = claims
		}

		addNode(TopologyNode{
			ID:        sID,
			Kind:      "StatefulSet",
			Name:      s.Name,
			Namespace: s.Namespace,
			Labels:    s.Labels,
			Details:   details,
		})

		// StatefulSet → Pods
		for _, node := range graph.Nodes {
			if node.Kind == "Pod" && node.Namespace == s.Namespace {
				if matchLabels(node.Labels, s.Spec.Selector.MatchLabels) {
					addEdge(TopologyEdge{Source: sID, Target: node.ID, Type: "manages"})
				}
			}
		}

		// StatefulSet → ServiceAccount
		saName := s.Spec.Template.Spec.ServiceAccountName
		if saName == "" {
			saName = "default"
		}
		addEdge(TopologyEdge{
			Source: sID,
			Target: fmt.Sprintf("serviceaccount/%s/%s", s.Namespace, saName),
			Type:   "uses-sa",
		})
	}
}

func (b *Builder) addDaemonSets(ctx context.Context, namespace string, addNode func(TopologyNode), addEdge func(TopologyEdge), graph *TopologyGraph) {
	dsList, err := b.client.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	graph.Stats.DaemonSets += len(dsList.Items)
	for _, ds := range dsList.Items {
		dsID := fmt.Sprintf("daemonset/%s/%s", ds.Namespace, ds.Name)

		images, containers := extractContainerInfo(ds.Spec.Template.Spec.Containers, ds.Spec.Template.Spec.InitContainers)

		details := map[string]interface{}{
			"desiredNumberScheduled": ds.Status.DesiredNumberScheduled,
			"currentNumberScheduled": ds.Status.CurrentNumberScheduled,
			"numberReady":            ds.Status.NumberReady,
			"numberAvailable":        ds.Status.NumberAvailable,
			"numberMisscheduled":     ds.Status.NumberMisscheduled,
			"images":                 images,
			"containers":             containers,
		}
		if ds.Spec.Template.Spec.NodeSelector != nil {
			details["nodeSelector"] = ds.Spec.Template.Spec.NodeSelector
		}
		if hr := helmRelease(ds.Annotations, ds.Labels); hr != "" {
			details["helmRelease"] = hr
		}

		addNode(TopologyNode{
			ID:        dsID,
			Kind:      "DaemonSet",
			Name:      ds.Name,
			Namespace: ds.Namespace,
			Labels:    ds.Labels,
			Details:   details,
		})

		// DaemonSet → Pods
		for _, node := range graph.Nodes {
			if node.Kind == "Pod" && node.Namespace == ds.Namespace {
				if matchLabels(node.Labels, ds.Spec.Selector.MatchLabels) {
					addEdge(TopologyEdge{Source: dsID, Target: node.ID, Type: "manages"})
				}
			}
		}

		// DaemonSet → ServiceAccount
		saName := ds.Spec.Template.Spec.ServiceAccountName
		if saName == "" {
			saName = "default"
		}
		addEdge(TopologyEdge{
			Source: dsID,
			Target: fmt.Sprintf("serviceaccount/%s/%s", ds.Namespace, saName),
			Type:   "uses-sa",
		})
	}
}

func (b *Builder) addServices(ctx context.Context, namespace string, addNode func(TopologyNode), addEdge func(TopologyEdge), graph *TopologyGraph) {
	svcs, err := b.client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	graph.Stats.Services += len(svcs.Items)
	for _, svc := range svcs.Items {
		svcID := fmt.Sprintf("service/%s/%s", svc.Namespace, svc.Name)

		ports := make([]map[string]interface{}, 0, len(svc.Spec.Ports))
		nodePorts := make([]int32, 0)
		for _, p := range svc.Spec.Ports {
			port := map[string]interface{}{
				"port":       p.Port,
				"targetPort": p.TargetPort.String(),
				"protocol":   string(p.Protocol),
			}
			if p.Name != "" {
				port["name"] = p.Name
			}
			ports = append(ports, port)
			if p.NodePort != 0 {
				nodePorts = append(nodePorts, p.NodePort)
			}
		}

		details := map[string]interface{}{
			"type":            string(svc.Spec.Type),
			"clusterIP":       svc.Spec.ClusterIP,
			"ports":           ports,
			"sessionAffinity": string(svc.Spec.SessionAffinity),
		}
		if svc.Spec.Selector != nil {
			details["selector"] = svc.Spec.Selector
		}
		if len(nodePorts) > 0 {
			details["nodePorts"] = nodePorts
		}
		if hr := helmRelease(svc.Annotations, svc.Labels); hr != "" {
			details["helmRelease"] = hr
		}

		// External IPs for LoadBalancer
		if svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
			extIPs := make([]string, 0)
			for _, ing := range svc.Status.LoadBalancer.Ingress {
				if ing.IP != "" {
					extIPs = append(extIPs, ing.IP)
				} else if ing.Hostname != "" {
					extIPs = append(extIPs, ing.Hostname)
				}
			}
			if len(extIPs) > 0 {
				details["externalIPs"] = extIPs
			}
		}

		addNode(TopologyNode{
			ID:        svcID,
			Kind:      "Service",
			Name:      svc.Name,
			Namespace: svc.Namespace,
			Labels:    svc.Labels,
			Details:   details,
		})

		// Service → Pods (selector match)
		for _, node := range graph.Nodes {
			if node.Kind == "Pod" && node.Namespace == svc.Namespace {
				if matchLabels(node.Labels, svc.Spec.Selector) {
					addEdge(TopologyEdge{Source: svcID, Target: node.ID, Type: "selects"})
				}
			}
		}
	}
}

func (b *Builder) addEndpoints(ctx context.Context, namespace string, addNode func(TopologyNode), graph *TopologyGraph) {
	eps, err := b.client.CoreV1().Endpoints(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	// We don't add Endpoints as graph nodes, but we update the matching Service's hasTraffic status
	for _, ep := range eps.Items {
		hasTraffic := false
		for _, subset := range ep.Subsets {
			if len(subset.Addresses) > 0 {
				hasTraffic = true
				break
			}
		}
		// Find the matching service node and update its details
		svcID := fmt.Sprintf("service/%s/%s", ep.Namespace, ep.Name)
		for i := range graph.Nodes {
			if graph.Nodes[i].ID == svcID {
				if graph.Nodes[i].Details == nil {
					graph.Nodes[i].Details = make(map[string]interface{})
				}
				graph.Nodes[i].Details["hasTraffic"] = hasTraffic
				break
			}
		}
	}
}

func (b *Builder) addIngresses(ctx context.Context, namespace string, addNode func(TopologyNode), addEdge func(TopologyEdge), graph *TopologyGraph) {
	ings, err := b.client.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	graph.Stats.Ingresses += len(ings.Items)
	for _, ing := range ings.Items {
		ingID := fmt.Sprintf("ingress/%s/%s", ing.Namespace, ing.Name)

		hosts := make([]string, 0)
		for _, rule := range ing.Spec.Rules {
			if rule.Host != "" {
				hosts = append(hosts, rule.Host)
			}
		}

		tlsEnabled := len(ing.Spec.TLS) > 0
		tlsHosts := make([]string, 0)
		for _, tls := range ing.Spec.TLS {
			tlsHosts = append(tlsHosts, tls.Hosts...)
		}

		details := map[string]interface{}{
			"hosts":      hosts,
			"ruleCount":  len(ing.Spec.Rules),
			"tlsEnabled": tlsEnabled,
		}
		if tlsEnabled {
			details["tlsHosts"] = tlsHosts
		}

		addNode(TopologyNode{
			ID:        ingID,
			Kind:      "Ingress",
			Name:      ing.Name,
			Namespace: ing.Namespace,
			Labels:    ing.Labels,
			Details:   details,
		})

		// Ingress → Service edges
		for _, rule := range ing.Spec.Rules {
			if rule.HTTP == nil {
				continue
			}
			for _, path := range rule.HTTP.Paths {
				if path.Backend.Service != nil {
					svcID := fmt.Sprintf("service/%s/%s", ing.Namespace, path.Backend.Service.Name)
					addEdge(TopologyEdge{Source: ingID, Target: svcID, Type: "routes"})
				}
			}
		}
	}
}

func (b *Builder) addConfigMaps(ctx context.Context, namespace string, addNode func(TopologyNode)) {
	cms, err := b.client.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	for _, cm := range cms.Items {
		keys := make([]string, 0, len(cm.Data)+len(cm.BinaryData))
		for k := range cm.Data {
			keys = append(keys, k)
		}
		for k := range cm.BinaryData {
			keys = append(keys, k)
		}

		details := map[string]interface{}{
			"keyCount": len(cm.Data) + len(cm.BinaryData),
			"keys":     keys,
		}
		if hr := helmRelease(cm.Annotations, cm.Labels); hr != "" {
			details["helmRelease"] = hr
		}

		addNode(TopologyNode{
			ID:        fmt.Sprintf("configmap/%s/%s", cm.Namespace, cm.Name),
			Kind:      "ConfigMap",
			Name:      cm.Name,
			Namespace: cm.Namespace,
			Labels:    cm.Labels,
			Details:   details,
		})
	}
}

func (b *Builder) addSecrets(ctx context.Context, namespace string, addNode func(TopologyNode)) {
	secrets, err := b.client.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	for _, sec := range secrets.Items {
		// Only key names — never values
		keys := make([]string, 0, len(sec.Data))
		for k := range sec.Data {
			keys = append(keys, k)
		}

		details := map[string]interface{}{
			"type":     string(sec.Type),
			"keyCount": len(sec.Data),
			"keys":     keys,
		}
		if hr := helmRelease(sec.Annotations, sec.Labels); hr != "" {
			details["helmRelease"] = hr
		}

		addNode(TopologyNode{
			ID:        fmt.Sprintf("secret/%s/%s", sec.Namespace, sec.Name),
			Kind:      "Secret",
			Name:      sec.Name,
			Namespace: sec.Namespace,
			Labels:    sec.Labels,
			Details:   details,
		})
	}
}

func (b *Builder) addPVCs(ctx context.Context, namespace string, addNode func(TopologyNode)) {
	pvcs, err := b.client.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	for _, pvc := range pvcs.Items {
		details := map[string]interface{}{
			"phase":       string(pvc.Status.Phase),
			"accessModes": accessModesToStrings(pvc.Spec.AccessModes),
			"volumeMode":  string(*pvc.Spec.VolumeMode),
		}
		if storage, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
			details["storage"] = storage.String()
		}
		if pvc.Spec.StorageClassName != nil {
			details["storageClassName"] = *pvc.Spec.StorageClassName
		}
		if pvc.Spec.VolumeName != "" {
			details["volumeName"] = pvc.Spec.VolumeName
		}
		if actual, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok {
			details["actualStorage"] = actual.String()
		}
		if hr := helmRelease(pvc.Annotations, pvc.Labels); hr != "" {
			details["helmRelease"] = hr
		}

		addNode(TopologyNode{
			ID:        fmt.Sprintf("persistentvolumeclaim/%s/%s", pvc.Namespace, pvc.Name),
			Kind:      "PersistentVolumeClaim",
			Name:      pvc.Name,
			Namespace: pvc.Namespace,
			Labels:    pvc.Labels,
			Details:   details,
		})
	}
}

func (b *Builder) addPVs(ctx context.Context, addNode func(TopologyNode)) {
	pvs, err := b.client.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	for _, pv := range pvs.Items {
		details := map[string]interface{}{
			"phase":       string(pv.Status.Phase),
			"accessModes": accessModesToStrings(pv.Spec.AccessModes),
		}
		if storage, ok := pv.Spec.Capacity[corev1.ResourceStorage]; ok {
			details["storage"] = storage.String()
		}
		if pv.Spec.StorageClassName != "" {
			details["storageClassName"] = pv.Spec.StorageClassName
		}
		if pv.Spec.ClaimRef != nil {
			details["claimRef"] = fmt.Sprintf("%s/%s", pv.Spec.ClaimRef.Namespace, pv.Spec.ClaimRef.Name)
		}

		addNode(TopologyNode{
			ID:      fmt.Sprintf("persistentvolume/%s", pv.Name),
			Kind:    "PersistentVolume",
			Name:    pv.Name,
			Details: details,
		})
	}
}

func (b *Builder) addServiceAccounts(ctx context.Context, namespace string, addNode func(TopologyNode)) {
	sas, err := b.client.CoreV1().ServiceAccounts(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	for _, sa := range sas.Items {
		details := map[string]interface{}{}
		if hr := helmRelease(sa.Annotations, sa.Labels); hr != "" {
			details["helmRelease"] = hr
		}

		addNode(TopologyNode{
			ID:        fmt.Sprintf("serviceaccount/%s/%s", sa.Namespace, sa.Name),
			Kind:      "ServiceAccount",
			Name:      sa.Name,
			Namespace: sa.Namespace,
			Labels:    sa.Labels,
			Details:   details,
		})
	}
}

func (b *Builder) addJobs(ctx context.Context, namespace string, addNode func(TopologyNode), addEdge func(TopologyEdge), graph *TopologyGraph) {
	jobs, err := b.client.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	for _, job := range jobs.Items {
		jobID := fmt.Sprintf("job/%s/%s", job.Namespace, job.Name)

		details := map[string]interface{}{
			"activeCount":   job.Status.Active,
			"succeededCount": job.Status.Succeeded,
			"failedCount":   job.Status.Failed,
		}
		if job.Spec.Completions != nil {
			details["completions"] = *job.Spec.Completions
		}
		if job.Spec.Parallelism != nil {
			details["parallelism"] = *job.Spec.Parallelism
		}
		if job.Spec.BackoffLimit != nil {
			details["backoffLimit"] = *job.Spec.BackoffLimit
		}
		if job.Status.StartTime != nil {
			details["startTime"] = job.Status.StartTime.Time.String()
		}
		if job.Status.CompletionTime != nil {
			details["completionTime"] = job.Status.CompletionTime.Time.String()
		}

		conditions := make([]map[string]interface{}, 0)
		for _, c := range job.Status.Conditions {
			conditions = append(conditions, map[string]interface{}{
				"type":   string(c.Type),
				"status": string(c.Status),
				"reason": c.Reason,
			})
		}
		if len(conditions) > 0 {
			details["conditions"] = conditions
		}

		images, containers := extractContainerInfo(job.Spec.Template.Spec.Containers, job.Spec.Template.Spec.InitContainers)
		details["images"] = images
		details["containers"] = containers

		addNode(TopologyNode{
			ID:        jobID,
			Kind:      "Job",
			Name:      job.Name,
			Namespace: job.Namespace,
			Labels:    job.Labels,
			Details:   details,
		})

		// Job → Pods
		for _, node := range graph.Nodes {
			if node.Kind == "Pod" && node.Namespace == job.Namespace {
				if matchLabels(node.Labels, job.Spec.Selector.MatchLabels) {
					addEdge(TopologyEdge{Source: jobID, Target: node.ID, Type: "manages"})
				}
			}
		}

		// Owner: CronJob → Job
		for _, owner := range job.OwnerReferences {
			if owner.Kind == "CronJob" {
				addEdge(TopologyEdge{
					Source: fmt.Sprintf("cronjob/%s/%s", job.Namespace, owner.Name),
					Target: jobID,
					Type:   "manages",
				})
			}
		}
	}
}

func (b *Builder) addCronJobs(ctx context.Context, namespace string, addNode func(TopologyNode), addEdge func(TopologyEdge), graph *TopologyGraph) {
	cjs, err := b.client.BatchV1().CronJobs(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	for _, cj := range cjs.Items {
		cjID := fmt.Sprintf("cronjob/%s/%s", cj.Namespace, cj.Name)

		details := map[string]interface{}{
			"schedule":          cj.Spec.Schedule,
			"concurrencyPolicy": string(cj.Spec.ConcurrencyPolicy),
		}
		if cj.Spec.Suspend != nil {
			details["suspend"] = *cj.Spec.Suspend
		}
		if cj.Status.LastScheduleTime != nil {
			details["lastScheduleTime"] = cj.Status.LastScheduleTime.Time.String()
		}
		if cj.Status.LastSuccessfulTime != nil {
			details["lastSuccessfulTime"] = cj.Status.LastSuccessfulTime.Time.String()
		}
		if cj.Spec.FailedJobsHistoryLimit != nil {
			details["failedJobsHistoryLimit"] = *cj.Spec.FailedJobsHistoryLimit
		}
		if cj.Spec.SuccessfulJobsHistoryLimit != nil {
			details["successfulJobsHistoryLimit"] = *cj.Spec.SuccessfulJobsHistoryLimit
		}

		images, containers := extractContainerInfo(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, cj.Spec.JobTemplate.Spec.Template.Spec.InitContainers)
		details["images"] = images
		details["containers"] = containers

		addNode(TopologyNode{
			ID:        cjID,
			Kind:      "CronJob",
			Name:      cj.Name,
			Namespace: cj.Namespace,
			Labels:    cj.Labels,
			Details:   details,
		})
	}
}

func (b *Builder) addHPAs(ctx context.Context, namespace string, addNode func(TopologyNode), addEdge func(TopologyEdge)) {
	hpas, err := b.client.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	for _, hpa := range hpas.Items {
		hpaID := fmt.Sprintf("horizontalpodautoscaler/%s/%s", hpa.Namespace, hpa.Name)

		details := map[string]interface{}{
			"targetKind":      hpa.Spec.ScaleTargetRef.Kind,
			"targetName":      hpa.Spec.ScaleTargetRef.Name,
			"maxReplicas":     hpa.Spec.MaxReplicas,
			"currentReplicas": hpa.Status.CurrentReplicas,
			"desiredReplicas": hpa.Status.DesiredReplicas,
		}
		if hpa.Spec.MinReplicas != nil {
			details["minReplicas"] = *hpa.Spec.MinReplicas
		}

		metrics := make([]map[string]interface{}, 0, len(hpa.Spec.Metrics))
		for _, m := range hpa.Spec.Metrics {
			metric := map[string]interface{}{"type": string(m.Type)}
			if m.Resource != nil {
				metric["resource"] = string(m.Resource.Name)
				if m.Resource.Target.AverageUtilization != nil {
					metric["targetUtilization"] = *m.Resource.Target.AverageUtilization
				}
			}
			metrics = append(metrics, metric)
		}
		if len(metrics) > 0 {
			details["metrics"] = metrics
		}

		addNode(TopologyNode{
			ID:        hpaID,
			Kind:      "HorizontalPodAutoscaler",
			Name:      hpa.Name,
			Namespace: hpa.Namespace,
			Labels:    hpa.Labels,
			Details:   details,
		})

		// HPA → target workload
		targetKind := strings.ToLower(hpa.Spec.ScaleTargetRef.Kind)
		targetID := fmt.Sprintf("%s/%s/%s", targetKind, hpa.Namespace, hpa.Spec.ScaleTargetRef.Name)
		addEdge(TopologyEdge{Source: hpaID, Target: targetID, Type: "scales"})
	}
}

func (b *Builder) addNetworkPolicies(ctx context.Context, namespace string, addNode func(TopologyNode)) {
	nps, err := b.client.NetworkingV1().NetworkPolicies(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	for _, np := range nps.Items {
		policyTypes := make([]string, 0, len(np.Spec.PolicyTypes))
		for _, pt := range np.Spec.PolicyTypes {
			policyTypes = append(policyTypes, string(pt))
		}

		details := map[string]interface{}{
			"policyTypes":      policyTypes,
			"ingressRuleCount": len(np.Spec.Ingress),
			"egressRuleCount":  len(np.Spec.Egress),
		}
		if np.Spec.PodSelector.MatchLabels != nil {
			details["podSelector"] = np.Spec.PodSelector.MatchLabels
		}

		addNode(TopologyNode{
			ID:        fmt.Sprintf("networkpolicy/%s/%s", np.Namespace, np.Name),
			Kind:      "NetworkPolicy",
			Name:      np.Name,
			Namespace: np.Namespace,
			Labels:    np.Labels,
			Details:   details,
		})
	}
}

func (b *Builder) addClusterNodes(ctx context.Context, addNode func(TopologyNode), addEdge func(TopologyEdge), graph *TopologyGraph) {
	nodes, err := b.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}
	graph.Stats.Nodes = len(nodes.Items)
	for _, n := range nodes.Items {
		addNode(TopologyNode{
			ID:     fmt.Sprintf("node/%s", n.Name),
			Kind:   "Node",
			Name:   n.Name,
			Labels: n.Labels,
		})
	}

	// Connect pods to their nodes
	for _, node := range graph.Nodes {
		if node.Kind == "Pod" && node.Details != nil {
			if nodeName, ok := node.Details["nodeName"].(string); ok && nodeName != "" {
				addEdge(TopologyEdge{
					Source: node.ID,
					Target: fmt.Sprintf("node/%s", nodeName),
					Type:   "runs-on",
				})
			}
		}
	}
}

// ===================================================================
// Phase 2 — Cross-resource edges
// ===================================================================

// buildWorkloadConfigEdges creates Workload→ConfigMap, Workload→Secret, Workload→PVC edges
// by inspecting the actual pod spec of each workload.
func (b *Builder) buildWorkloadConfigEdges(ctx context.Context, namespaces []string, addEdge func(TopologyEdge)) {
	for _, ns := range namespaces {
		// Deployments
		deps, _ := b.client.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
		if deps != nil {
			for _, dep := range deps.Items {
				depID := fmt.Sprintf("deployment/%s/%s", dep.Namespace, dep.Name)
				extractPodSpecEdges(depID, dep.Namespace, &dep.Spec.Template.Spec, addEdge)
			}
		}
		// StatefulSets
		stsList, _ := b.client.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{})
		if stsList != nil {
			for _, s := range stsList.Items {
				sID := fmt.Sprintf("statefulset/%s/%s", s.Namespace, s.Name)
				extractPodSpecEdges(sID, s.Namespace, &s.Spec.Template.Spec, addEdge)
			}
		}
		// DaemonSets
		dsList, _ := b.client.AppsV1().DaemonSets(ns).List(ctx, metav1.ListOptions{})
		if dsList != nil {
			for _, ds := range dsList.Items {
				dsID := fmt.Sprintf("daemonset/%s/%s", ds.Namespace, ds.Name)
				extractPodSpecEdges(dsID, ds.Namespace, &ds.Spec.Template.Spec, addEdge)
			}
		}
		// Jobs
		jobs, _ := b.client.BatchV1().Jobs(ns).List(ctx, metav1.ListOptions{})
		if jobs != nil {
			for _, job := range jobs.Items {
				jobID := fmt.Sprintf("job/%s/%s", job.Namespace, job.Name)
				extractPodSpecEdges(jobID, job.Namespace, &job.Spec.Template.Spec, addEdge)
			}
		}
		// CronJobs
		cjs, _ := b.client.BatchV1().CronJobs(ns).List(ctx, metav1.ListOptions{})
		if cjs != nil {
			for _, cj := range cjs.Items {
				cjID := fmt.Sprintf("cronjob/%s/%s", cj.Namespace, cj.Name)
				extractPodSpecEdges(cjID, cj.Namespace, &cj.Spec.JobTemplate.Spec.Template.Spec, addEdge)
			}
		}
	}
}

// buildServiceToWorkloadEdges connects Services to Deployments/StatefulSets/DaemonSets
// (in addition to the existing Service→Pod edges).
func (b *Builder) buildServiceToWorkloadEdges(graph *TopologyGraph, addEdge func(TopologyEdge)) {
	workloadKinds := map[string]bool{"Deployment": true, "StatefulSet": true, "DaemonSet": true}

	for _, svc := range graph.Nodes {
		if svc.Kind != "Service" {
			continue
		}
		selector, _ := svc.Details["selector"].(map[string]string)
		if selector == nil {
			continue
		}
		for _, wl := range graph.Nodes {
			if !workloadKinds[wl.Kind] || wl.Namespace != svc.Namespace {
				continue
			}
			// Check workload labels against service selector
			if matchLabels(wl.Labels, selector) {
				addEdge(TopologyEdge{
					Source: svc.ID,
					Target: wl.ID,
					Type:   "routes-to",
				})
			}
		}
	}
}

// ===================================================================
// Helpers
// ===================================================================

// extractPodSpecEdges creates ConfigMap, Secret, and PVC edges from a PodSpec.
func extractPodSpecEdges(workloadID, namespace string, spec *corev1.PodSpec, addEdge func(TopologyEdge)) {
	configMaps := make(map[string]bool)
	secrets := make(map[string]bool)

	for _, c := range spec.Containers {
		collectEnvRefs(c.EnvFrom, c.Env, configMaps, secrets)
	}
	for _, c := range spec.InitContainers {
		collectEnvRefs(c.EnvFrom, c.Env, configMaps, secrets)
	}

	// Volume mounts → PVC, ConfigMap, Secret
	for _, vol := range spec.Volumes {
		if vol.PersistentVolumeClaim != nil {
			pvcID := fmt.Sprintf("persistentvolumeclaim/%s/%s", namespace, vol.PersistentVolumeClaim.ClaimName)
			addEdge(TopologyEdge{Source: workloadID, Target: pvcID, Type: "mounts"})
		}
		if vol.ConfigMap != nil {
			configMaps[vol.ConfigMap.Name] = true
		}
		if vol.Secret != nil {
			secrets[vol.Secret.SecretName] = true
		}
	}

	for name := range configMaps {
		addEdge(TopologyEdge{
			Source: workloadID,
			Target: fmt.Sprintf("configmap/%s/%s", namespace, name),
			Type:   "refs-config",
		})
	}
	for name := range secrets {
		addEdge(TopologyEdge{
			Source: workloadID,
			Target: fmt.Sprintf("secret/%s/%s", namespace, name),
			Type:   "refs-secret",
		})
	}
}

func collectEnvRefs(envFrom []corev1.EnvFromSource, env []corev1.EnvVar, configMaps, secrets map[string]bool) {
	for _, ef := range envFrom {
		if ef.ConfigMapRef != nil {
			configMaps[ef.ConfigMapRef.Name] = true
		}
		if ef.SecretRef != nil {
			secrets[ef.SecretRef.Name] = true
		}
	}
	for _, e := range env {
		if e.ValueFrom != nil {
			if e.ValueFrom.ConfigMapKeyRef != nil {
				configMaps[e.ValueFrom.ConfigMapKeyRef.Name] = true
			}
			if e.ValueFrom.SecretKeyRef != nil {
				secrets[e.ValueFrom.SecretKeyRef.Name] = true
			}
		}
	}
}

func extractContainerInfo(containers []corev1.Container, initContainers []corev1.Container) ([]string, []string) {
	images := make([]string, 0, len(containers)+len(initContainers))
	names := make([]string, 0, len(containers))
	for _, c := range containers {
		images = append(images, c.Image)
		names = append(names, c.Name)
	}
	for _, c := range initContainers {
		images = append(images, c.Image)
	}
	return images, names
}

func extractDeploymentConditions(conditions []appsv1.DeploymentCondition) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(conditions))
	for _, c := range conditions {
		result = append(result, map[string]interface{}{
			"type":   string(c.Type),
			"status": string(c.Status),
			"reason": c.Reason,
		})
	}
	return result
}

func helmRelease(annotations, labels map[string]string) string {
	if v, ok := annotations["meta.helm.sh/release-name"]; ok {
		return v
	}
	// Some Helm charts use the managed-by label instead
	if v, ok := labels["app.kubernetes.io/managed-by"]; ok && v == "Helm" {
		if rel, ok := labels["app.kubernetes.io/instance"]; ok {
			return rel
		}
	}
	return ""
}

func accessModesToStrings(modes []corev1.PersistentVolumeAccessMode) []string {
	result := make([]string, 0, len(modes))
	for _, m := range modes {
		result = append(result, string(m))
	}
	return result
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
