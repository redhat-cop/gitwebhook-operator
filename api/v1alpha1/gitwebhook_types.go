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
	"context"
	"errors"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// +kubebuilder:object:generate=false
type WebHook interface {
	Reconcile(ctx context.Context) error
	Delete(ctx context.Context) error
}

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// GitWebhookSpec defines the desired state of GitWebhook
type GitWebhookSpec struct {

	// GitLab the configuration to connect to the gitlab server. only one of gitlab or github is allowed
	GitLab *GitServerConfig `json:"gitLab,omitempty"`

	// GitHub the configuration to connect to the gitlab server
	GitHub *GitServerConfig `json:"gitHub,omitempty"`

	// RepositoryOwner The owner of the repository, can be either an organization or a user
	// +kubebuilder:validation:Required
	RepositoryOwner string `json:"RepositoryOwner,omitempty"`

	// RepositoryName The name of the repository
	RepositoryName string `json:"repositoryName,omitempty"`

	// WebhookURL The URL of the webhook to be called
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^https?:\/\/(?:www\.)?[-a-zA-Z0-9@:%._\+~#=]{1,256}\.[a-zA-Z0-9()]{1,6}\b(?:[-a-zA-Z0-9()@:%_\+.~#?&\/=]*)$`
	WebhookURL string `json:"webhookURL,omitempty"`

	// InsecureSSL whether to not verify the certificate of the server serving the webhook
	// +kubebuilder:default="true"
	InsecureSSL bool `json:"insecureSSL,omitempty"`

	// WebhookSecret The secret to be used in the webhook callbacks. The key "secret" will be used to retrieve the secret/token
	WebhookSecret corev1.LocalObjectReference `json:"webhookSecret,omitempty"`

	// Events The list of events that this webbook should be notified for
	// +listType=set
	Events []string `json:"events,omitempty"`

	// ContentType the content type of the webhook playload (github only, will be ignored for gitlab)
	// +kubebuilder:default="json"
	ContentType string `json:"content,omitempty"`

	// Active whether this webhook should be actibe (github only, will be ignored for gitlab)
	// +kubebuilder:default="true"
	Active bool `json:"active,omitempty"`

	// PushEventBranchFilter filter for push event on branches (gitlab only, will be ignored for github)
	PushEventBranchFilter string `json:"pushEventBranchFilter,omitempty"`
}

type GitServerConfig struct {
	// GitAPIServerURL the url of the git server api
	// +kubebuilder:validation:Pattern=`^https?:\/\/(?:www\.)?[-a-zA-Z0-9@:%._\+~#=]{1,256}\.[a-zA-Z0-9()]{1,6}\b(?:[-a-zA-Z0-9()@:%_\+.~#?&\/=]*)$`
	GitAPIServerURL string `json:"gitAPIServerURL,omitempty"`
	// GitServerCredentials credentials to use when authenticating to the git server, must contain a "token" key
	GitServerCredentials corev1.LocalObjectReference `json:"gitServerCredentials,omitempty"`
}

// GitWebhookStatus defines the observed state of GitWebhook
type GitWebhookStatus struct {
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// GitWebhook is the Schema for the gitwebhooks API
type GitWebhook struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GitWebhookSpec   `json:"spec,omitempty"`
	Status GitWebhookStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GitWebhookList contains a list of GitWebhook
type GitWebhookList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GitWebhook `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GitWebhook{}, &GitWebhookList{})
}

func (m *GitWebhook) GetWebhookSecret(ctx context.Context) (string, error) {
	if m.Spec.WebhookSecret.Name == "" {
		return "", nil
	}
	log := log.FromContext(ctx)
	kubeClient := ctx.Value("kubeClient").(client.Client)
	secret := &corev1.Secret{}
	err := kubeClient.Get(ctx, types.NamespacedName{
		Name:      m.Spec.WebhookSecret.Name,
		Namespace: m.GetNamespace(),
	}, secret, &client.GetOptions{})
	if err != nil {
		log.Error(err, "unable to find secret: "+m.Spec.WebhookSecret.Name)
		return "", err
	}
	if data, found := secret.Data["secret"]; !found {
		return "", errors.New("\"secret\" key not found in secret " + m.Spec.WebhookSecret.Name)
	} else {
		return string(data), nil
	}
}

func (m *GitWebhook) GetGitCredential(ctx context.Context, gitServerConfig *GitServerConfig) (string, error) {
	if gitServerConfig.GitServerCredentials.Name == "" {
		return "", nil
	}
	log := log.FromContext(ctx)
	kubeClient := ctx.Value("kubeClient").(client.Client)
	secret := &corev1.Secret{}
	err := kubeClient.Get(ctx, types.NamespacedName{
		Name:      gitServerConfig.GitServerCredentials.Name,
		Namespace: m.GetNamespace(),
	}, secret, &client.GetOptions{})
	if err != nil {
		log.Error(err, "unable to find secret: "+gitServerConfig.GitServerCredentials.Name)
		return "", err
	}
	if data, found := secret.Data["token"]; !found {
		return "", errors.New("\"token\" key not found in secret " + m.Spec.WebhookSecret.Name)
	} else {
		return string(data), nil
	}
}
