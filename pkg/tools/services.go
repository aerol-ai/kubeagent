package tools

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ServiceTools provides service and ingress operations.
type ServiceTools struct {
	client kubernetes.Interface
}

// NewServiceTools creates a ServiceTools instance.
func NewServiceTools(client kubernetes.Interface) *ServiceTools {
	return &ServiceTools{client: client}
}

// ServiceSummary is a compact service representation.
type ServiceSummary struct {
	Name       string            `json:"name"`
	Namespace  string            `json:"namespace"`
	Type       string            `json:"type"`
	ClusterIP  string            `json:"cluster_ip"`
	ExternalIP []string          `json:"external_ip,omitempty"`
	Ports      []ServicePort     `json:"ports"`
	Selector   map[string]string `json:"selector,omitempty"`
}

// ServicePort describes a port exposed by a service.
type ServicePort struct {
	Name       string `json:"name"`
	Port       int32  `json:"port"`
	TargetPort string `json:"target_port"`
	Protocol   string `json:"protocol"`
	NodePort   int32  `json:"node_port,omitempty"`
}

// IngressSummary is a compact ingress representation.
type IngressSummary struct {
	Name      string        `json:"name"`
	Namespace string        `json:"namespace"`
	Class     string        `json:"class,omitempty"`
	Rules     []IngressRule `json:"rules"`
	TLS       []IngressTLS  `json:"tls,omitempty"`
}

// IngressRule models an ingress rule.
type IngressRule struct {
	Host  string        `json:"host"`
	Paths []IngressPath `json:"paths"`
}

// IngressPath models a path within an ingress rule.
type IngressPath struct {
	Path        string `json:"path"`
	PathType    string `json:"path_type"`
	ServiceName string `json:"service_name"`
	ServicePort string `json:"service_port"`
}

// IngressTLS models TLS config for an ingress.
type IngressTLS struct {
	Hosts      []string `json:"hosts"`
	SecretName string   `json:"secret_name"`
}

// ListServices lists services in a namespace.
func (s *ServiceTools) ListServices(ctx context.Context, namespace string) ([]ServiceSummary, error) {
	svcs, err := s.client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]ServiceSummary, 0, len(svcs.Items))
	for _, svc := range svcs.Items {
		ports := make([]ServicePort, 0, len(svc.Spec.Ports))
		for _, p := range svc.Spec.Ports {
			ports = append(ports, ServicePort{
				Name:       p.Name,
				Port:       p.Port,
				TargetPort: p.TargetPort.String(),
				Protocol:   string(p.Protocol),
				NodePort:   p.NodePort,
			})
		}
		extIPs := make([]string, 0)
		for _, ing := range svc.Status.LoadBalancer.Ingress {
			if ing.IP != "" {
				extIPs = append(extIPs, ing.IP)
			} else if ing.Hostname != "" {
				extIPs = append(extIPs, ing.Hostname)
			}
		}
		out = append(out, ServiceSummary{
			Name:       svc.Name,
			Namespace:  svc.Namespace,
			Type:       string(svc.Spec.Type),
			ClusterIP:  svc.Spec.ClusterIP,
			ExternalIP: extIPs,
			Ports:      ports,
			Selector:   svc.Spec.Selector,
		})
	}
	return out, nil
}

// GetService returns a single service.
func (s *ServiceTools) GetService(ctx context.Context, namespace, name string) (*ServiceSummary, error) {
	svc, err := s.client.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	ports := make([]ServicePort, 0, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		ports = append(ports, ServicePort{
			Name:       p.Name,
			Port:       p.Port,
			TargetPort: p.TargetPort.String(),
			Protocol:   string(p.Protocol),
			NodePort:   p.NodePort,
		})
	}
	return &ServiceSummary{
		Name:      svc.Name,
		Namespace: svc.Namespace,
		Type:      string(svc.Spec.Type),
		ClusterIP: svc.Spec.ClusterIP,
		Ports:     ports,
		Selector:  svc.Spec.Selector,
	}, nil
}

// DeleteService deletes a service.
func (s *ServiceTools) DeleteService(ctx context.Context, namespace, name string) error {
	return s.client.CoreV1().Services(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// ListIngresses lists ingresses in a namespace.
func (s *ServiceTools) ListIngresses(ctx context.Context, namespace string) ([]IngressSummary, error) {
	ings, err := s.client.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]IngressSummary, 0, len(ings.Items))
	for _, ing := range ings.Items {
		className := ""
		if ing.Spec.IngressClassName != nil {
			className = *ing.Spec.IngressClassName
		}
		rules := make([]IngressRule, 0)
		for _, r := range ing.Spec.Rules {
			paths := make([]IngressPath, 0)
			if r.HTTP != nil {
				for _, p := range r.HTTP.Paths {
					pt := "Prefix"
					if p.PathType != nil {
						pt = string(*p.PathType)
					}
					svcPort := ""
					if p.Backend.Service != nil {
						if p.Backend.Service.Port.Name != "" {
							svcPort = p.Backend.Service.Port.Name
						} else {
							svcPort = fmt.Sprintf("%d", p.Backend.Service.Port.Number)
						}
					}
					svcName := ""
					if p.Backend.Service != nil {
						svcName = p.Backend.Service.Name
					}
					paths = append(paths, IngressPath{
						Path:        p.Path,
						PathType:    pt,
						ServiceName: svcName,
						ServicePort: svcPort,
					})
				}
			}
			rules = append(rules, IngressRule{Host: r.Host, Paths: paths})
		}
		tls := make([]IngressTLS, 0)
		for _, t := range ing.Spec.TLS {
			tls = append(tls, IngressTLS{Hosts: t.Hosts, SecretName: t.SecretName})
		}
		out = append(out, IngressSummary{
			Name:      ing.Name,
			Namespace: ing.Namespace,
			Class:     className,
			Rules:     rules,
			TLS:       tls,
		})
	}
	return out, nil
}

// ListEndpoints lists endpoints in a namespace.
func (s *ServiceTools) ListEndpoints(ctx context.Context, namespace string) ([]map[string]interface{}, error) {
	eps, err := s.client.CoreV1().Endpoints(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(eps.Items))
	for _, ep := range eps.Items {
		addrs := make([]string, 0)
		for _, sub := range ep.Subsets {
			for _, addr := range sub.Addresses {
				addrs = append(addrs, addr.IP)
			}
		}
		out = append(out, map[string]interface{}{
			"name":      ep.Name,
			"namespace": ep.Namespace,
			"addresses": addrs,
		})
	}
	return out, nil
}
