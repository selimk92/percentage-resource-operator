package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	policyv1alpha1 "github.com/selimk92/percentage-resource-operator/api/v1alpha1"
)

const (
	annotationPolicyRef     = "percentagepolicy.io/policy-ref"
	annotationLimitSource   = "percentagepolicy.io/limit-source"
	annotationMemCalculated = "percentagepolicy.io/memory-limit-calculated"
	annotationMemApplied    = "percentagepolicy.io/memory-limit-applied"
	annotationMemPending    = "percentagepolicy.io/memory-limit-pending"
	annotationCPUCalculated = "percentagepolicy.io/cpu-limit-calculated"
	annotationCPUApplied    = "percentagepolicy.io/cpu-limit-applied"
	annotationCPUPending    = "percentagepolicy.io/cpu-limit-pending"
	annotationLastUpdated   = "percentagepolicy.io/limit-last-updated"
	annotationStrategy      = "percentagepolicy.io/update-strategy"
)

type applyResult struct {
	usedFallback  bool
	pendingUpdate bool
}

// PercentageResourcePolicyReconciler reconciles a PercentageResourcePolicy object
type PercentageResourcePolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=policy.percentagepolicy.io,resources=percentageresourcepolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy.percentagepolicy.io,resources=percentageresourcepolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=policy.percentagepolicy.io,resources=percentageresourcepolicies/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

func (r *PercentageResourcePolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	policy := &policyv1alpha1.PercentageResourcePolicy{}
	if err := r.Get(ctx, req.NamespacedName, policy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	selector, err := metav1.LabelSelectorAsSelector(&policy.Spec.PodSelector)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("invalid podSelector: %w", err)
	}

	podList := &corev1.PodList{}
	if err := r.List(ctx, podList,
		client.InNamespace(req.Namespace),
		client.MatchingLabelsSelector{Selector: selector},
	); err != nil {
		return ctrl.Result{}, err
	}

	var affectedPods, fallbackPods, pendingPods int32

	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.DeletionTimestamp != nil || pod.Spec.NodeName == "" {
			continue
		}

		result, err := r.applyPolicyToPod(ctx, policy, pod)
		if err != nil {
			log.Error(err, "failed to apply policy to pod", "pod", pod.Name)
			continue
		}

		affectedPods++
		if result.usedFallback {
			fallbackPods++
		}
		if result.pendingUpdate {
			pendingPods++
		}
	}

	policy.Status.AffectedPods = affectedPods
	policy.Status.FallbackPods = fallbackPods
	policy.Status.PendingUpdatePods = pendingPods

	if err := r.Status().Update(ctx, policy); err != nil {
		log.Error(err, "failed to update policy status")
	}

	return ctrl.Result{}, nil
}

func (r *PercentageResourcePolicyReconciler) applyPolicyToPod(
	ctx context.Context,
	policy *policyv1alpha1.PercentageResourcePolicy,
	pod *corev1.Pod,
) (applyResult, error) {
	log := logf.FromContext(ctx)
	result := applyResult{}

	node := &corev1.Node{}
	if err := r.Get(ctx, types.NamespacedName{Name: pod.Spec.NodeName}, node); err != nil {
		log.Info("node not found, using fallback", "node", pod.Spec.NodeName)
		result.usedFallback = true
		return result, r.applyFallbackLimits(ctx, policy, pod)
	}

	allocatable := node.Status.Allocatable
	newLimits := corev1.ResourceList{}
	annotations := map[string]string{
		annotationPolicyRef:   fmt.Sprintf("%s/%s", policy.Namespace, policy.Name),
		annotationStrategy:    string(policy.Spec.UpdateStrategy.Type),
		annotationLastUpdated: time.Now().UTC().Format(time.RFC3339),
	}

	isRunning := pod.Status.Phase == corev1.PodRunning
	strategy := policy.Spec.UpdateStrategy.Type

	if spec := policy.Spec.Resources.Memory; spec != nil {
		calculated, source := calculateLimit(allocatable.Memory(), spec)
		annotations[annotationMemCalculated] = calculated.String()
		annotations[annotationLimitSource] = string(source)

		if source == policyv1alpha1.LimitSourceFallback {
			result.usedFallback = true
		}

		current := getCurrentLimit(pod, corev1.ResourceMemory)
		if shouldUpdate(current, calculated, policy.Spec.UpdateStrategy.UpdateThreshold) {
			if isRunning && strategy == policyv1alpha1.UpdateStrategyOnRestart {
				annotations[annotationMemPending] = calculated.String()
				result.pendingUpdate = true
			} else {
				newLimits[corev1.ResourceMemory] = calculated
				annotations[annotationMemApplied] = calculated.String()
			}
		}
	}

	if spec := policy.Spec.Resources.CPU; spec != nil {
		calculated, source := calculateLimit(allocatable.Cpu(), spec)
		annotations[annotationCPUCalculated] = calculated.String()

		if source == policyv1alpha1.LimitSourceFallback {
			result.usedFallback = true
		}

		current := getCurrentLimit(pod, corev1.ResourceCPU)
		if shouldUpdate(current, calculated, policy.Spec.UpdateStrategy.UpdateThreshold) {
			if isRunning && strategy == policyv1alpha1.UpdateStrategyOnRestart {
				annotations[annotationCPUPending] = calculated.String()
				result.pendingUpdate = true
			} else {
				newLimits[corev1.ResourceCPU] = calculated
				annotations[annotationCPUApplied] = calculated.String()
			}
		}
	}

	// Evict stratejisinde çalışan pod'u sil; yeniden schedule'da doğru limitlerle başlar
	if isRunning && strategy == policyv1alpha1.UpdateStrategyEvict && len(newLimits) > 0 {
		log.Info("evicting pod for limit update", "pod", pod.Name)
		if err := r.Delete(ctx, pod); err != nil {
			return result, fmt.Errorf("evict failed: %w", err)
		}
		return result, nil
	}

	return result, r.patchPod(ctx, pod, newLimits, annotations)
}

func (r *PercentageResourcePolicyReconciler) applyFallbackLimits(
	ctx context.Context,
	policy *policyv1alpha1.PercentageResourcePolicy,
	pod *corev1.Pod,
) error {
	newLimits := corev1.ResourceList{}
	annotations := map[string]string{
		annotationPolicyRef:   fmt.Sprintf("%s/%s", policy.Namespace, policy.Name),
		annotationLimitSource: string(policyv1alpha1.LimitSourceFallback),
		annotationLastUpdated: time.Now().UTC().Format(time.RFC3339),
	}

	if spec := policy.Spec.Resources.Memory; spec != nil {
		newLimits[corev1.ResourceMemory] = spec.Fallback
		annotations[annotationMemApplied] = spec.Fallback.String()
	}
	if spec := policy.Spec.Resources.CPU; spec != nil {
		newLimits[corev1.ResourceCPU] = spec.Fallback
		annotations[annotationCPUApplied] = spec.Fallback.String()
	}

	return r.patchPod(ctx, pod, newLimits, annotations)
}

func (r *PercentageResourcePolicyReconciler) patchPod(
	ctx context.Context,
	pod *corev1.Pod,
	newLimits corev1.ResourceList,
	annotations map[string]string,
) error {
	log := logf.FromContext(ctx)

	// Önce sadece annotation patch'i dene (her zaman çalışır)
	annotationPatch := client.MergeFrom(pod.DeepCopy())
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	for k, v := range annotations {
		pod.Annotations[k] = v
	}
	if err := r.Patch(ctx, pod, annotationPatch); err != nil {
		return fmt.Errorf("annotation patch failed: %w", err)
	}

	if len(newLimits) == 0 {
		return nil
	}

	// Resource limit patch'ini ayrı dene
	limitPatch := client.MergeFrom(pod.DeepCopy())
	for i := range pod.Spec.Containers {
		for res, qty := range newLimits {
			if pod.Spec.Containers[i].Resources.Limits == nil {
				pod.Spec.Containers[i].Resources.Limits = corev1.ResourceList{}
			}
			pod.Spec.Containers[i].Resources.Limits[res] = qty
		}
	}
	if err := r.Patch(ctx, pod, limitPatch); err != nil {
		// InPlacePodVerticalScaling yoksa running pod'da limit değişmez
		// Değeri pending annotation'a yaz, bir sonraki restart'ta uygulanacak
		log.Info("resource limit patch not supported (InPlacePodVerticalScaling unavailable), storing as pending",
			"pod", pod.Name)
		pendingPatch := client.MergeFrom(pod.DeepCopy())
		for res, qty := range newLimits {
			switch res {
			case corev1.ResourceMemory:
				pod.Annotations[annotationMemPending] = qty.String()
			case corev1.ResourceCPU:
				pod.Annotations[annotationCPUPending] = qty.String()
			}
		}
		return r.Patch(ctx, pod, pendingPatch)
	}

	return nil
}

// calculateLimit computes actual quantity from a percentage of node capacity with min/max clamping.
func calculateLimit(nodeQty *resource.Quantity, spec *policyv1alpha1.ResourceLimitSpec) (resource.Quantity, policyv1alpha1.LimitSource) {
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

func getCurrentLimit(pod *corev1.Pod, res corev1.ResourceName) *resource.Quantity {
	if len(pod.Spec.Containers) == 0 {
		return nil
	}
	if limit, ok := pod.Spec.Containers[0].Resources.Limits[res]; ok {
		return &limit
	}
	return nil
}

// shouldUpdate returns true when the new value differs from current by more than threshold.
func shouldUpdate(current *resource.Quantity, newVal resource.Quantity, threshold string) bool {
	if current == nil {
		return true
	}
	if current.Cmp(newVal) == 0 {
		return false
	}
	pct, err := parsePercentage(threshold)
	if err != nil || pct == 0 {
		return true
	}
	currentMilli := current.MilliValue()
	if currentMilli == 0 {
		return true
	}
	diff := newVal.MilliValue() - currentMilli
	if diff < 0 {
		diff = -diff
	}
	return diff*100/currentMilli >= int64(pct)
}

func parsePercentage(s string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(s, "%")))
}

// policiesForPod maps a pod event to the policies that select it.
func (r *PercentageResourcePolicyReconciler) policiesForPod(ctx context.Context, obj client.Object) []reconcile.Request {
	pod, ok := obj.(*corev1.Pod)
	if !ok || pod.Spec.NodeName == "" {
		return nil
	}

	policyList := &policyv1alpha1.PercentageResourcePolicyList{}
	if err := r.List(ctx, policyList, client.InNamespace(pod.Namespace)); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, policy := range policyList.Items {
		sel, err := metav1.LabelSelectorAsSelector(&policy.Spec.PodSelector)
		if err != nil {
			continue
		}
		if sel.Matches(labels.Set(pod.Labels)) {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      policy.Name,
					Namespace: policy.Namespace,
				},
			})
		}
	}
	return requests
}

// policiesForNode maps a node capacity change to all policies in the cluster.
func (r *PercentageResourcePolicyReconciler) policiesForNode(ctx context.Context, _ client.Object) []reconcile.Request {
	policyList := &policyv1alpha1.PercentageResourcePolicyList{}
	if err := r.List(ctx, policyList); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, policy := range policyList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      policy.Name,
				Namespace: policy.Namespace,
			},
		})
	}
	return requests
}

func (r *PercentageResourcePolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Sadece pod'un nodeName'i boşken dolarsa tetikle
	podScheduled := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			old, ok1 := e.ObjectOld.(*corev1.Pod)
			new, ok2 := e.ObjectNew.(*corev1.Pod)
			return ok1 && ok2 && old.Spec.NodeName == "" && new.Spec.NodeName != ""
		},
		CreateFunc:  func(e event.CreateEvent) bool { return false },
		DeleteFunc:  func(e event.DeleteEvent) bool { return false },
		GenericFunc: func(e event.GenericEvent) bool { return false },
	}

	// Sadece node'un allocatable değeri değişirse tetikle
	nodeAllocatableChanged := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldNode, ok1 := e.ObjectOld.(*corev1.Node)
			newNode, ok2 := e.ObjectNew.(*corev1.Node)
			if !ok1 || !ok2 {
				return false
			}
			return oldNode.Status.Allocatable.Memory().Cmp(*newNode.Status.Allocatable.Memory()) != 0 ||
				oldNode.Status.Allocatable.Cpu().Cmp(*newNode.Status.Allocatable.Cpu()) != 0
		},
		CreateFunc:  func(e event.CreateEvent) bool { return false },
		DeleteFunc:  func(e event.DeleteEvent) bool { return false },
		GenericFunc: func(e event.GenericEvent) bool { return false },
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&policyv1alpha1.PercentageResourcePolicy{}).
		Watches(&corev1.Pod{},
			handler.EnqueueRequestsFromMapFunc(r.policiesForPod),
			builder.WithPredicates(podScheduled),
		).
		Watches(&corev1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.policiesForNode),
			builder.WithPredicates(nodeAllocatableChanged),
		).
		Named("percentageresourcepolicy").
		Complete(r)
}
