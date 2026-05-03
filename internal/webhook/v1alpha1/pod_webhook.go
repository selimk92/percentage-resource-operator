package v1alpha1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	policyv1alpha1 "github.com/selimk92/percentage-resource-operator/api/v1alpha1"
)

// +kubebuilder:webhook:path=/mutate-v1-pod,mutating=true,failurePolicy=ignore,sideEffects=None,groups="",resources=pods,verbs=create,versions=v1,name=mpod.percentagepolicy.io,admissionReviewVersions=v1

type PodResourceWebhook struct {
	client.Client
	decoder admission.Decoder
}

func SetupPodWebhookWithManager(mgr ctrl.Manager) error {
	mgr.GetWebhookServer().Register("/mutate-v1-pod", &webhook.Admission{
		Handler: &PodResourceWebhook{
			Client:  mgr.GetClient(),
			decoder: admission.NewDecoder(mgr.GetScheme()),
		},
	})
	return nil
}

func (w *PodResourceWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	pod := &corev1.Pod{}
	if err := w.decoder.Decode(req, pod); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	policy, err := w.findMatchingPolicy(ctx, pod)
	if err != nil || policy == nil {
		return admission.Allowed("no matching policy")
	}

	nodeCapacity, err := w.getNodeCapacity(ctx, pod)
	if err != nil {
		nodeCapacity = corev1.ResourceList{}
	}

	limits := corev1.ResourceList{}
	annotations := map[string]string{
		"percentagepolicy.io/policy-ref":          fmt.Sprintf("%s/%s", policy.Namespace, policy.Name),
		"percentagepolicy.io/update-strategy":     string(policy.Spec.UpdateStrategy.Type),
		"percentagepolicy.io/limit-last-updated":  time.Now().UTC().Format(time.RFC3339),
	}

	if spec := policy.Spec.Resources.Memory; spec != nil {
		qty, source := webhookCalculateLimit(nodeCapacity.Memory(), spec)
		limits[corev1.ResourceMemory] = qty
		annotations["percentagepolicy.io/memory-limit-calculated"] = qty.String()
		annotations["percentagepolicy.io/memory-limit-applied"] = qty.String()
		annotations["percentagepolicy.io/limit-source"] = string(source)
	}

	if spec := policy.Spec.Resources.CPU; spec != nil {
		qty, source := webhookCalculateLimit(nodeCapacity.Cpu(), spec)
		limits[corev1.ResourceCPU] = qty
		annotations["percentagepolicy.io/cpu-limit-calculated"] = qty.String()
		annotations["percentagepolicy.io/cpu-limit-applied"] = qty.String()
		if _, exists := annotations["percentagepolicy.io/limit-source"]; !exists {
			annotations["percentagepolicy.io/limit-source"] = string(source)
		}
	}

	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	for k, v := range annotations {
		pod.Annotations[k] = v
	}

	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Resources.Limits == nil {
			pod.Spec.Containers[i].Resources.Limits = corev1.ResourceList{}
		}
		for res, qty := range limits {
			pod.Spec.Containers[i].Resources.Limits[res] = qty
		}
	}

	marshaled, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaled)
}

func (w *PodResourceWebhook) findMatchingPolicy(ctx context.Context, pod *corev1.Pod) (*policyv1alpha1.PercentageResourcePolicy, error) {
	policyList := &policyv1alpha1.PercentageResourcePolicyList{}
	if err := w.List(ctx, policyList, client.InNamespace(pod.Namespace)); err != nil {
		return nil, err
	}
	for _, p := range policyList.Items {
		sel, err := metav1.LabelSelectorAsSelector(&p.Spec.PodSelector)
		if err != nil {
			continue
		}
		if sel.Matches(labels.Set(pod.Labels)) {
			copy := p
			return &copy, nil
		}
	}
	return nil, nil
}

func (w *PodResourceWebhook) getNodeCapacity(ctx context.Context, pod *corev1.Pod) (corev1.ResourceList, error) {
	if pod.Spec.NodeName != "" {
		node := &corev1.Node{}
		if err := w.Get(ctx, client.ObjectKey{Name: pod.Spec.NodeName}, node); err != nil {
			return nil, err
		}
		return node.Status.Allocatable, nil
	}

	nodeList := &corev1.NodeList{}
	listOpts := []client.ListOption{}
	if len(pod.Spec.NodeSelector) > 0 {
		listOpts = append(listOpts, client.MatchingLabels(pod.Spec.NodeSelector))
	}
	if err := w.List(ctx, nodeList, listOpts...); err != nil {
		return nil, err
	}
	return minNodeCapacity(nodeList.Items), nil
}

func minNodeCapacity(nodes []corev1.Node) corev1.ResourceList {
	var minMem, minCPU *resource.Quantity
	for _, node := range nodes {
		if node.Spec.Unschedulable {
			continue
		}
		mem := node.Status.Allocatable.Memory()
		cpu := node.Status.Allocatable.Cpu()
		if minMem == nil || mem.Cmp(*minMem) < 0 {
			minMem = mem
		}
		if minCPU == nil || cpu.Cmp(*minCPU) < 0 {
			minCPU = cpu
		}
	}
	result := corev1.ResourceList{}
	if minMem != nil {
		result[corev1.ResourceMemory] = *minMem
	}
	if minCPU != nil {
		result[corev1.ResourceCPU] = *minCPU
	}
	return result
}

func webhookCalculateLimit(nodeQty *resource.Quantity, spec *policyv1alpha1.ResourceLimitSpec) (resource.Quantity, policyv1alpha1.LimitSource) {
	if nodeQty == nil || nodeQty.IsZero() {
		return spec.Fallback, policyv1alpha1.LimitSourceFallback
	}
	milliValue := nodeQty.MilliValue()
	calculated := milliValue * int64(spec.Percentage) / 100
	result := resource.NewMilliQuantity(calculated, resource.BinarySI)
	if spec.Min != nil && result.Cmp(*spec.Min) < 0 {
		return *spec.Min, policyv1alpha1.LimitSourceClampedMin
	}
	if spec.Max != nil && result.Cmp(*spec.Max) > 0 {
		return *spec.Max, policyv1alpha1.LimitSourceClampedMax
	}
	return *result, policyv1alpha1.LimitSourcePercentage
}
