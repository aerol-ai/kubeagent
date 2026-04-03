package topology

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// TrafficRoute represents a complete request path through the cluster.
type TrafficRoute struct {
	Name string       `json:"name"`
	Hops []TrafficHop `json:"hops"`
}

// TrafficHop is a single segment in a traffic route.
type TrafficHop struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Port      string `json:"port,omitempty"`
	Details   string `json:"details,omitempty"`
}

// PolicyEffect describes how a network policy affects traffic.
type PolicyEffect struct {
	Name         string   `json:"name"`
	Namespace    string   `json:"namespace"`
	Action       string   `json:"action"`
	AffectedPods []string `json:"affected_pods"`
}

// TrafficMap is the complete traffic analysis result.
type TrafficMap struct {
	Routes   []TrafficRoute  `json:"routes"`
	Policies []PolicyEffect  `json:"policies,omitempty"`
	External []ExternalEntry `json:"external,omitempty"`
}

// ExternalEntry describes an externally accessible service.
type ExternalEntry struct {
	Type      string `json:"type"`
	Address   string `json:"address"`
	Service   string `json:"service"`
	Namespace string `json:"namespace"`
}

// TrafficMapper maps traffic routes through the cluster.
type TrafficMapper struct {
	client kubernetes.Interface
}

// NewTrafficMapper creates a TrafficMapper.
func NewTrafficMapper(client kubernetes.Interface) *TrafficMapper {
	return &TrafficMapper{client: client}
}

// MapTrafficRoutes discovers all traffic routes in a namespace.
func (t *TrafficMapper) MapTrafficRoutes(ctx context.Context, namespace string) (*TrafficMap, error) {
	result := &TrafficMap{
		Routes:   make([]TrafficRoute, 0),
		Policies: make([]PolicyEffect, 0),
		External: make([]ExternalEntry, 0),
	}

	namespaces := []string{namespace}
	if namespace == "" {
		nsList, err := t.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
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
		t.discoverIngressRoutes(ctx, ns, result)
		t.discoverServiceRoutes(ctx, ns, result)
		t.discoverNetworkPolicies(ctx, ns, result)
	}

	return result, nil
}

func (t *TrafficMapper) discoverIngressRoutes(ctx context.Context, namespace string, result *TrafficMap) {
	ings, err := t.client.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}

	for _, ing := range ings.Items {
		for _, rule := range ing.Spec.Rules {
			if rule.HTTP == nil {
				continue
			}
			for _, path := range rule.HTTP.Paths {
				if path.Backend.Service == nil {
					continue
				}
				svcName := path.Backend.Service.Name
				svcPort := ""
				if path.Backend.Service.Port.Name != "" {
					svcPort = path.Backend.Service.Port.Name
				} else if path.Backend.Service.Port.Number > 0 {
					svcPort = fmt.Sprintf("%d", path.Backend.Service.Port.Number)
				}

				route := TrafficRoute{
					Name: fmt.Sprintf("%s%s -> %s", rule.Host, path.Path, svcName),
					Hops: []TrafficHop{
						{Kind: "Ingress", Name: ing.Name, Namespace: namespace, Details: rule.Host + path.Path},
						{Kind: "Service", Name: svcName, Namespace: namespace, Port: svcPort},
					},
				}

				// Find pods behind the service
				svc, err := t.client.CoreV1().Services(namespace).Get(ctx, svcName, metav1.GetOptions{})
				if err == nil && len(svc.Spec.Selector) > 0 {
					pods, err := t.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
					if err == nil {
						for _, pod := range pods.Items {
							if matchLabels(pod.Labels, svc.Spec.Selector) {
								route.Hops = append(route.Hops, TrafficHop{
									Kind:      "Pod",
									Name:      pod.Name,
									Namespace: namespace,
									Details:   pod.Status.PodIP,
								})
							}
						}
					}
				}
				result.Routes = append(result.Routes, route)
			}
		}
	}
}

func (t *TrafficMapper) discoverServiceRoutes(ctx context.Context, namespace string, result *TrafficMap) {
	svcs, err := t.client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}

	for _, svc := range svcs.Items {
		// Track external entries
		switch svc.Spec.Type {
		case "LoadBalancer":
			for _, ingLB := range svc.Status.LoadBalancer.Ingress {
				addr := ingLB.IP
				if addr == "" {
					addr = ingLB.Hostname
				}
				result.External = append(result.External, ExternalEntry{
					Type:      "LoadBalancer",
					Address:   addr,
					Service:   svc.Name,
					Namespace: namespace,
				})
			}
		case "NodePort":
			for _, port := range svc.Spec.Ports {
				result.External = append(result.External, ExternalEntry{
					Type:      "NodePort",
					Address:   fmt.Sprintf(":<node>:%d", port.NodePort),
					Service:   svc.Name,
					Namespace: namespace,
				})
			}
		}

		if len(svc.Spec.Selector) == 0 {
			continue
		}

		route := TrafficRoute{
			Name: fmt.Sprintf("svc/%s/%s", namespace, svc.Name),
			Hops: []TrafficHop{
				{Kind: "Service", Name: svc.Name, Namespace: namespace, Details: string(svc.Spec.Type)},
			},
		}

		pods, err := t.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err == nil {
			for _, pod := range pods.Items {
				if matchLabels(pod.Labels, svc.Spec.Selector) {
					route.Hops = append(route.Hops, TrafficHop{
						Kind:      "Pod",
						Name:      pod.Name,
						Namespace: namespace,
						Details:   pod.Status.PodIP,
					})
				}
			}
		}
		result.Routes = append(result.Routes, route)
	}
}

func (t *TrafficMapper) discoverNetworkPolicies(ctx context.Context, namespace string, result *TrafficMap) {
	policies, err := t.client.NetworkingV1().NetworkPolicies(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return
	}

	for _, pol := range policies.Items {
		affected := make([]string, 0)
		pods, err := t.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err == nil {
			for _, pod := range pods.Items {
				if matchLabels(pod.Labels, pol.Spec.PodSelector.MatchLabels) {
					affected = append(affected, pod.Name)
				}
			}
		}

		action := "restrict"
		if len(pol.Spec.Ingress) > 0 || len(pol.Spec.Egress) > 0 {
			action = "allow-selective"
		}

		result.Policies = append(result.Policies, PolicyEffect{
			Name:         pol.Name,
			Namespace:    namespace,
			Action:       action,
			AffectedPods: affected,
		})
	}
}
