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

package v1

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	k8sdinovaonev1 "github.com/karavy/k8s-operator-fortigate/api/v1"
)

// nolint:unused
// log is for logging in this package.
var fortigatebackuplog = logf.Log.WithName("fortigatebackup-resource")

// SetupFortigateBackupWebhookWithManager registers the webhook for FortigateBackup in the manager.
func SetupFortigateBackupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &k8sdinovaonev1.FortigateBackup{}).
		WithValidator(&FortigateBackupCustomValidator{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-k8s-dinova-one-v1-fortigatebackup,mutating=false,failurePolicy=fail,sideEffects=None,groups=k8s.dinova.one,resources=fortigatebackups,verbs=create;update,versions=v1,name=vfortigatebackup-v1.kb.io,admissionReviewVersions=v1

// FortigateBackupCustomValidator struct is responsible for validating the FortigateBackup resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type FortigateBackupCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type FortigateBackup.
func (v *FortigateBackupCustomValidator) ValidateCreate(_ context.Context, obj *k8sdinovaonev1.FortigateBackup) (admission.Warnings, error) {
	fortigatebackuplog.Info("Validation for FortigateBackup upon creation", "name", obj.GetName())
	
	return nil, validateFortigateBackup(obj)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type FortigateBackup.
func (v *FortigateBackupCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj *k8sdinovaonev1.FortigateBackup) (admission.Warnings, error) {
	fortigatebackuplog.Info("Validation for FortigateBackup upon update", "name", newObj.GetName())

	return nil, validateFortigateBackup(newObj)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type FortigateBackup.
func (v *FortigateBackupCustomValidator) ValidateDelete(_ context.Context, obj *k8sdinovaonev1.FortigateBackup) (admission.Warnings, error) {
	fortigatebackuplog.Info("Validation for FortigateBackup upon deletion", "name", obj.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}

// validateFortigateBackup chiama ValidateFortigateBackupSpec, che resta
// dentro api/v1 (validation.go) - qui la richiamiamo qualificata, dato che
// questo file vive in un package diverso.
func validateFortigateBackup(backup *k8sdinovaonev1.FortigateBackup) error {
	errs := ValidateFortigateBackupSpec(&backup.Spec, field.NewPath("spec"))
	if len(errs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(
		schema.GroupKind{Group: k8sdinovaonev1.GroupVersion.Group, Kind: "FortigateBackup"},
		backup.Name,
		errs,
	)
}