package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// ApplyTools provides manifest apply, delete, describe, and exec operations.
type ApplyTools struct {
	client    kubernetes.Interface
	dynClient dynamic.Interface
	restCfg   *rest.Config
}

// NewApplyTools creates an ApplyTools instance.
func NewApplyTools(client kubernetes.Interface, dynClient dynamic.Interface, restCfg *rest.Config) *ApplyTools {
	return &ApplyTools{client: client, dynClient: dynClient, restCfg: restCfg}
}

// ApplyResult holds the result of a manifest apply.
type ApplyResult struct {
	Created []string `json:"created"`
	Updated []string `json:"updated"`
	Errors  []string `json:"errors,omitempty"`
}

// Apply applies one or more YAML documents using server-side apply.
func (a *ApplyTools) Apply(ctx context.Context, manifest string, namespace string) (*ApplyResult, error) {
	result := &ApplyResult{}
	decoder := yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader([]byte(manifest)), 4096)

	for {
		var obj unstructured.Unstructured
		err := decoder.Decode(&obj)
		if err != nil {
			if err == io.EOF {
				break
			}
			result.Errors = append(result.Errors, "decode error")
			continue
		}
		if obj.Object == nil {
			continue
		}

		gvk := obj.GroupVersionKind()
		gvr := resolveGVR(gvk)
		ns := obj.GetNamespace()
		if ns == "" {
			ns = namespace
		}

		var res dynamic.ResourceInterface
		if ns != "" {
			res = a.dynClient.Resource(gvr).Namespace(ns)
		} else {
			res = a.dynClient.Resource(gvr)
		}

		applied, err := res.Apply(ctx, obj.GetName(), &obj, metav1.ApplyOptions{
			FieldManager: "kube-agent",
			Force:        true,
		})
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("apply failed for %s/%s", gvk.Kind, obj.GetName()))
			continue
		}
		name := fmt.Sprintf("%s/%s", applied.GetKind(), applied.GetName())
		result.Created = append(result.Created, name)
	}
	return result, nil
}

// DeleteResource deletes a resource by kind, name, and namespace.
func (a *ApplyTools) DeleteResource(ctx context.Context, kind, name, namespace string) error {
	gvr := resolveGVRFromKind(kind)
	var res dynamic.ResourceInterface
	if namespace != "" {
		res = a.dynClient.Resource(gvr).Namespace(namespace)
	} else {
		res = a.dynClient.Resource(gvr)
	}
	return res.Delete(ctx, name, metav1.DeleteOptions{})
}

// ExecInPod runs a command inside a pod container.
func (a *ApplyTools) ExecInPod(ctx context.Context, namespace, pod, container string, command []string) (string, error) {
	req := a.client.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(namespace).
		Name(pod).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(a.restCfg, "POST", req.URL())
	if err != nil {
		return "", err
	}

	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return stderr.String(), err
	}
	return stdout.String(), nil
}

func resolveGVR(gvk schema.GroupVersionKind) schema.GroupVersionResource {
	resource := strings.ToLower(gvk.Kind) + "s"
	aliases := map[string]string{
		"ingress":       "ingresses",
		"networkpolicy": "networkpolicies",
		"endpoints":     "endpoints",
		"resourcequota": "resourcequotas",
	}
	if r, ok := aliases[strings.ToLower(gvk.Kind)]; ok {
		resource = r
	}
	return schema.GroupVersionResource{
		Group:    gvk.Group,
		Version:  gvk.Version,
		Resource: resource,
	}
}

func resolveGVRFromKind(kind string) schema.GroupVersionResource {
	kindMap := map[string]schema.GroupVersionResource{
		"pod":                   {Group: "", Version: "v1", Resource: "pods"},
		"service":               {Group: "", Version: "v1", Resource: "services"},
		"configmap":             {Group: "", Version: "v1", Resource: "configmaps"},
		"secret":                {Group: "", Version: "v1", Resource: "secrets"},
		"namespace":             {Group: "", Version: "v1", Resource: "namespaces"},
		"node":                  {Group: "", Version: "v1", Resource: "nodes"},
		"persistentvolumeclaim": {Group: "", Version: "v1", Resource: "persistentvolumeclaims"},
		"deployment":            {Group: "apps", Version: "v1", Resource: "deployments"},
		"statefulset":           {Group: "apps", Version: "v1", Resource: "statefulsets"},
		"daemonset":             {Group: "apps", Version: "v1", Resource: "daemonsets"},
		"ingress":               {Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
		"networkpolicy":         {Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"},
		"job":                   {Group: "batch", Version: "v1", Resource: "jobs"},
		"cronjob":               {Group: "batch", Version: "v1", Resource: "cronjobs"},
	}
	lower := strings.ToLower(kind)
	if gvr, ok := kindMap[lower]; ok {
		return gvr
	}
	return schema.GroupVersionResource{Version: "v1", Resource: strings.ToLower(kind) + "s"}
}
