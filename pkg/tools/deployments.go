package tools

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// DeploymentTools provides deployment-related operations.
type DeploymentTools struct {
	client kubernetes.Interface
}

// NewDeploymentTools creates a DeploymentTools instance.
func NewDeploymentTools(client kubernetes.Interface) *DeploymentTools {
	return &DeploymentTools{client: client}
}

// DeploymentSummary is a compact deployment representation.
type DeploymentSummary struct {
	Name       string            `json:"name"`
	Namespace  string            `json:"namespace"`
	Replicas   int32             `json:"replicas"`
	Ready      int32             `json:"ready"`
	Available  int32             `json:"available"`
	Labels     map[string]string `json:"labels,omitempty"`
	Images     []string          `json:"images"`
	Age        string            `json:"age"`
	Conditions []ConditionInfo   `json:"conditions,omitempty"`
}

// ConditionInfo summarises a condition.
type ConditionInfo struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// ListDeployments lists deployments in a namespace.
func (d *DeploymentTools) ListDeployments(ctx context.Context, namespace string) ([]DeploymentSummary, error) {
	deps, err := d.client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]DeploymentSummary, 0, len(deps.Items))
	for i := range deps.Items {
		out = append(out, deployToSummary(&deps.Items[i]))
	}
	return out, nil
}

// GetDeployment returns a single deployment.
func (d *DeploymentTools) GetDeployment(ctx context.Context, namespace, name string) (*DeploymentSummary, error) {
	dep, err := d.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	s := deployToSummary(dep)
	return &s, nil
}

// ScaleDeployment sets the replica count.
func (d *DeploymentTools) ScaleDeployment(ctx context.Context, namespace, name string, replicas int32) error {
	scale, err := d.client.AppsV1().Deployments(namespace).GetScale(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	scale.Spec.Replicas = replicas
	_, err = d.client.AppsV1().Deployments(namespace).UpdateScale(ctx, name, scale, metav1.UpdateOptions{})
	return err
}

// RestartDeployment triggers a rolling restart by patching an annotation.
func (d *DeploymentTools) RestartDeployment(ctx context.Context, namespace, name string) error {
	patch := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"%s"}}}}}`,
		time.Now().Format(time.RFC3339))
	_, err := d.client.AppsV1().Deployments(namespace).Patch(
		ctx, name, types.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{})
	return err
}

// DeleteDeployment deletes a deployment.
func (d *DeploymentTools) DeleteDeployment(ctx context.Context, namespace, name string) error {
	return d.client.AppsV1().Deployments(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// ListStatefulSets lists statefulsets in a namespace.
func (d *DeploymentTools) ListStatefulSets(ctx context.Context, namespace string) ([]map[string]interface{}, error) {
	sts, err := d.client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(sts.Items))
	for _, s := range sts.Items {
		out = append(out, map[string]interface{}{
			"name":      s.Name,
			"namespace": s.Namespace,
			"replicas":  s.Status.Replicas,
			"ready":     s.Status.ReadyReplicas,
		})
	}
	return out, nil
}

// ListDaemonSets lists daemonsets in a namespace.
func (d *DeploymentTools) ListDaemonSets(ctx context.Context, namespace string) ([]map[string]interface{}, error) {
	ds, err := d.client.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0, len(ds.Items))
	for _, s := range ds.Items {
		out = append(out, map[string]interface{}{
			"name":      s.Name,
			"namespace": s.Namespace,
			"desired":   s.Status.DesiredNumberScheduled,
			"ready":     s.Status.NumberReady,
			"available": s.Status.NumberAvailable,
		})
	}
	return out, nil
}

func derefInt32(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}

func deployToSummary(dep *appsv1.Deployment) DeploymentSummary {
	images := make([]string, 0)
	for _, c := range dep.Spec.Template.Spec.Containers {
		images = append(images, c.Image)
	}
	conds := make([]ConditionInfo, 0)
	for _, c := range dep.Status.Conditions {
		conds = append(conds, ConditionInfo{
			Type:    string(c.Type),
			Status:  string(c.Status),
			Message: c.Message,
		})
	}
	return DeploymentSummary{
		Name:       dep.Name,
		Namespace:  dep.Namespace,
		Replicas:   derefInt32(dep.Spec.Replicas),
		Ready:      dep.Status.ReadyReplicas,
		Available:  dep.Status.AvailableReplicas,
		Labels:     dep.Labels,
		Images:     images,
		Age:        dep.CreationTimestamp.Time.Format("2006-01-02T15:04:05Z"),
		Conditions: conds,
	}
}
