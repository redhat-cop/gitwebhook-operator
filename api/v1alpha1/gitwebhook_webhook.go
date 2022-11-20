/*
Copyright 2022.

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
	"errors"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var gitwebhooklog = logf.Log.WithName("gitwebhook-resource")

func (r *GitWebhook) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

//+kubebuilder:webhook:path=/mutate-redhatcop-redhat-io-v1alpha1-gitwebhook,mutating=true,failurePolicy=fail,sideEffects=None,groups=redhatcop.redhat.io,resources=gitwebhooks,verbs=create,versions=v1alpha1,name=mgitwebhook.kb.io,admissionReviewVersions=v1

var _ webhook.Defaulter = &GitWebhook{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *GitWebhook) Default() {
	gitwebhooklog.Info("default", "name", r.Name)

	// TODO(user): fill in your defaulting logic.
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
//+kubebuilder:webhook:path=/validate-redhatcop-redhat-io-v1alpha1-gitwebhook,mutating=false,failurePolicy=fail,sideEffects=None,groups=redhatcop.redhat.io,resources=gitwebhooks,verbs=create;update,versions=v1alpha1,name=vgitwebhook.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &GitWebhook{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *GitWebhook) ValidateCreate() error {
	gitwebhooklog.Info("validate create", "name", r.Name)

	return r.validateOnlyOneGitServer()
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *GitWebhook) ValidateUpdate(old runtime.Object) error {
	gitwebhooklog.Info("validate update", "name", r.Name)
	err := r.validateOnlyOneGitServer()
	if err != nil {
		return err
	}
	oldGW := old.(*GitWebhook)
	//owner,owertype, repository, git server and url cannot be changed and the git configuration
	if r.Spec.GitHub != nil && oldGW.Spec.GitHub != nil && r.Spec.GitHub.GitAPIServerURL != oldGW.Spec.GitHub.GitAPIServerURL {
		return errors.New("github server cannot be changed")
	}
	if r.Spec.GitLab != nil && oldGW.Spec.GitLab != nil && r.Spec.GitLab.GitAPIServerURL != oldGW.Spec.GitLab.GitAPIServerURL {
		return errors.New("gitlab server cannot be changed")
	}
	if r.Spec.OwnerType != oldGW.Spec.OwnerType {
		return errors.New("ownerType server cannot be changed")
	}
	if r.Spec.RepositoryOwner != oldGW.Spec.RepositoryOwner {
		return errors.New("repositoryOwner server cannot be changed")
	}
	if r.Spec.RepositoryName != oldGW.Spec.RepositoryName {
		return errors.New("repositoryName server cannot be changed")
	}
	if r.Spec.WebhookURL != oldGW.Spec.WebhookURL {
		return errors.New("webhookURL server cannot be changed")
	}

	// TODO(user): fill in your validation logic upon object update.
	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *GitWebhook) ValidateDelete() error {
	gitwebhooklog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil
}

func (r *GitWebhook) validateOnlyOneGitServer() error {
	count := 0
	if r.Spec.GitHub != nil {
		count++
	}
	if r.Spec.GitLab != nil {
		count++
	}
	if count != 1 {
		return errors.New("exaclty one of gitlab and github must be initialized")
	}
	return nil
}
