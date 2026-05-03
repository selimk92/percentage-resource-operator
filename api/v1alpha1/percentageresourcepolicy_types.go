package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Strategy",type="string",JSONPath=".spec.updateStrategy.type"

type PercentageResourcePolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PercentageResourcePolicySpec   `json:"spec,omitempty"`
	Status PercentageResourcePolicyStatus `json:"status,omitempty"`
}

type PercentageResourcePolicySpec struct {
	// +kubebuilder:validation:Required
	PodSelector metav1.LabelSelector `json:"podSelector"`

	// +kubebuilder:validation:Required
	Resources ResourcePercentageSpec `json:"resources"`

	// +kubebuilder:default={type: OnRestart, updateThreshold: "5%", debounce: "30s"}
	UpdateStrategy UpdateStrategy `json:"updateStrategy,omitempty"`
}

type ResourcePercentageSpec struct {
	// +optional
	Memory *ResourceLimitSpec `json:"memory,omitempty"`

	// +optional
	CPU *ResourceLimitSpec `json:"cpu,omitempty"`

	// +optional
	EphemeralStorage *ResourceLimitSpec `json:"ephemeralStorage,omitempty"`
}

type ResourceLimitSpec struct {
	// Node allocatable'ın yüzdesi (1-100)
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	Percentage int32 `json:"percentage"`

	// Node bilgisi alınamazsa kullanılacak statik değer
	// +kubebuilder:validation:Required
	Fallback resource.Quantity `json:"fallback"`

	// Hesaplanan değer bu değerin altına düşemez
	// +optional
	Min *resource.Quantity `json:"min,omitempty"`

	// Hesaplanan değer bu değeri geçemez
	// +optional
	Max *resource.Quantity `json:"max,omitempty"`
}

// +kubebuilder:validation:Enum=OnRestart;Evict;Disabled
type UpdateStrategyType string

const (
	UpdateStrategyOnRestart UpdateStrategyType = "OnRestart"
	UpdateStrategyEvict     UpdateStrategyType = "Evict"
	UpdateStrategyDisabled  UpdateStrategyType = "Disabled"
)

type UpdateStrategy struct {
	// +kubebuilder:default=OnRestart
	Type UpdateStrategyType `json:"type,omitempty"`

	// Hesaplanan değer bu oranda değişirse güncelleme tetiklenir (örn. "10%")
	// +kubebuilder:default="5%"
	UpdateThreshold string `json:"updateThreshold,omitempty"`

	// Node event'leri bu süre boyunca birleştirilir (örn. "30s")
	// +kubebuilder:default="30s"
	Debounce string `json:"debounce,omitempty"`
}

// +kubebuilder:validation:Enum=percentage;fallback;clamped-min;clamped-max
type LimitSource string

const (
	LimitSourcePercentage LimitSource = "percentage"
	LimitSourceFallback   LimitSource = "fallback"
	LimitSourceClampedMin LimitSource = "clamped-min"
	LimitSourceClampedMax LimitSource = "clamped-max"
)

type AppliedResourceStatus struct {
	Calculated  string      `json:"calculated,omitempty"`
	Applied     string      `json:"applied,omitempty"`
	Pending     string      `json:"pending,omitempty"`
	Source      LimitSource `json:"source,omitempty"`
	LastUpdated metav1.Time `json:"lastUpdated,omitempty"`
}

type PercentageResourcePolicyStatus struct {
	// Politikanın etkilediği pod sayısı
	AffectedPods int32 `json:"affectedPods,omitempty"`

	// Fallback kullanan pod sayısı
	FallbackPods int32 `json:"fallbackPods,omitempty"`

	// Güncelleme bekleyen pod sayısı (OnRestart modunda)
	PendingUpdatePods int32 `json:"pendingUpdatePods,omitempty"`

	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true

type PercentageResourcePolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PercentageResourcePolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PercentageResourcePolicy{}, &PercentageResourcePolicyList{})
}
