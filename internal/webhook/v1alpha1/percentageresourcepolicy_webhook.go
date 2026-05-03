/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	policyv1alpha1 "github.com/selimk92/percentage-resource-operator/api/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var percentageresourcepolicylog = logf.Log.WithName("percentageresourcepolicy-resource")

// SetupPercentageResourcePolicyWebhookWithManager registers the webhook for PercentageResourcePolicy in the manager.
func SetupPercentageResourcePolicyWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &policyv1alpha1.PercentageResourcePolicy{}).
		WithValidator(&PercentageResourcePolicyCustomValidator{}).
		WithDefaulter(&PercentageResourcePolicyCustomDefaulter{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-policy-percentagepolicy-io-v1alpha1-percentageresourcepolicy,mutating=true,failurePolicy=fail,sideEffects=None,groups=policy.percentagepolicy.io,resources=percentageresourcepolicies,verbs=create;update,versions=v1alpha1,name=mpercentageresourcepolicy-v1alpha1.kb.io,admissionReviewVersions=v1

// PercentageResourcePolicyCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind PercentageResourcePolicy when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type PercentageResourcePolicyCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind PercentageResourcePolicy.
func (d *PercentageResourcePolicyCustomDefaulter) Default(_ context.Context, obj *policyv1alpha1.PercentageResourcePolicy) error {
	percentageresourcepolicylog.Info("Defaulting for PercentageResourcePolicy", "name", obj.GetName())

	// TODO(user): fill in your defaulting logic.

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-policy-percentagepolicy-io-v1alpha1-percentageresourcepolicy,mutating=false,failurePolicy=fail,sideEffects=None,groups=policy.percentagepolicy.io,resources=percentageresourcepolicies,verbs=create;update,versions=v1alpha1,name=vpercentageresourcepolicy-v1alpha1.kb.io,admissionReviewVersions=v1

// PercentageResourcePolicyCustomValidator struct is responsible for validating the PercentageResourcePolicy resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type PercentageResourcePolicyCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type PercentageResourcePolicy.
func (v *PercentageResourcePolicyCustomValidator) ValidateCreate(_ context.Context, obj *policyv1alpha1.PercentageResourcePolicy) (admission.Warnings, error) {
	percentageresourcepolicylog.Info("Validation for PercentageResourcePolicy upon creation", "name", obj.GetName())

	// TODO(user): fill in your validation logic upon object creation.

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type PercentageResourcePolicy.
func (v *PercentageResourcePolicyCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj *policyv1alpha1.PercentageResourcePolicy) (admission.Warnings, error) {
	percentageresourcepolicylog.Info("Validation for PercentageResourcePolicy upon update", "name", newObj.GetName())

	// TODO(user): fill in your validation logic upon object update.

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type PercentageResourcePolicy.
func (v *PercentageResourcePolicyCustomValidator) ValidateDelete(_ context.Context, obj *policyv1alpha1.PercentageResourcePolicy) (admission.Warnings, error) {
	percentageresourcepolicylog.Info("Validation for PercentageResourcePolicy upon deletion", "name", obj.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
