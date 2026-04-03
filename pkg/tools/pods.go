package tools

import (
	"bytes"
	"context"
	"io"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// PodTools provides pod operations.
type PodTools struct {
	client kubernetes.Interface
}

// NewPodTools creates a PodTools instance.
func NewPodTools(client kubernetes.Interface) *PodTools {
	return &PodTools{client: client}
}

// PodSummary is a compact pod representation.
type PodSummary struct {
	Name       string            `json:"name"`
	Namespace  string            `json:"namespace"`
	Status     string            `json:"status"`
	IP         string            `json:"ip"`
	Node       string            `json:"node"`
	Containers []ContainerInfo   `json:"containers"`
	Labels     map[string]string `json:"labels,omitempty"`
	Age        string            `json:"age"`
}

// ContainerInfo summarises a container in a pod.
type ContainerInfo struct {
	Name    string `json:"name"`
	Image   string `json:"image"`
	Ready   bool   `json:"ready"`
	State   string `json:"state"`
}

// ListPods lists pods in a namespace (empty = all namespaces).
func (p *PodTools) ListPods(ctx context.Context, namespace string) ([]PodSummary, error) {
	pods, err := p.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]PodSummary, 0, len(pods.Items))
	for i := range pods.Items {
		out = append(out, podToSummary(&pods.Items[i]))
	}
	return out, nil
}

// GetPod returns details for a single pod.
func (p *PodTools) GetPod(ctx context.Context, namespace, name string) (*PodSummary, error) {
	pod, err := p.client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	s := podToSummary(pod)
	return &s, nil
}

// GetPodLogs returns the last tailLines lines from a container.
func (p *PodTools) GetPodLogs(ctx context.Context, namespace, name, container string, tailLines int64, previous bool) (string, error) {
	opts := &corev1.PodLogOptions{
		Container: container,
		Previous:  previous,
	}
	if tailLines > 0 {
		opts.TailLines = &tailLines
	}
	req := p.client.CoreV1().Pods(namespace).GetLogs(name, opts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer stream.Close()
	var buf bytes.Buffer
	limited := io.LimitReader(stream, 1<<20) // 1MB limit
	if _, err := io.Copy(&buf, limited); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// DeletePod deletes a pod.
func (p *PodTools) DeletePod(ctx context.Context, namespace, name string) error {
	return p.client.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

func podToSummary(pod *corev1.Pod) PodSummary {
	containers := make([]ContainerInfo, 0, len(pod.Spec.Containers))
	for _, c := range pod.Spec.Containers {
		ci := ContainerInfo{Name: c.Name, Image: c.Image}
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.Name == c.Name {
				ci.Ready = cs.Ready
				if cs.State.Running != nil {
					ci.State = "running"
				} else if cs.State.Waiting != nil {
					ci.State = cs.State.Waiting.Reason
				} else if cs.State.Terminated != nil {
					ci.State = cs.State.Terminated.Reason
				}
			}
		}
		containers = append(containers, ci)
	}
	return PodSummary{
		Name:       pod.Name,
		Namespace:  pod.Namespace,
		Status:     string(pod.Status.Phase),
		IP:         pod.Status.PodIP,
		Node:       pod.Spec.NodeName,
		Containers: containers,
		Labels:     pod.Labels,
		Age:        pod.CreationTimestamp.Time.Format("2006-01-02T15:04:05Z"),
	}
}
