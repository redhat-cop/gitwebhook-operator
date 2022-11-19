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

package controllers

import (
	"context"
	err "errors"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/go-logr/logr"
	redhatcopv1alpha1 "github.com/redhat-cop/gitwebhook-operator/api/v1alpha1"
	"github.com/redhat-cop/gitwebhook-operator/api/v1alpha1/github"
	"github.com/redhat-cop/gitwebhook-operator/api/v1alpha1/gitlab"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GitWebhookReconciler reconciles a GitWebhook object
type GitWebhookReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

const controllerName = "gitwebhook"

//+kubebuilder:rbac:groups=redhatcop.redhat.io,resources=gitwebhooks,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=redhatcop.redhat.io,resources=gitwebhooks/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=redhatcop.redhat.io,resources=gitwebhooks/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="coordination.k8s.io",resources=leases,verbs=get;list;watch;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the GitWebhook object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *GitWebhookReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	ctx = context.WithValue(ctx, "kubeClient", r.Client)

	instance := &redhatcopv1alpha1.GitWebhook{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// examine DeletionTimestamp to determine if object is under deletion
	if instance.DeletionTimestamp.IsZero() {
		// The object is not being deleted, so if it does not have our finalizer,
		// then lets add the finalizer and update the object. This is equivalent
		// registering our finalizer.
		if !controllerutil.ContainsFinalizer(instance, controllerName) {
			controllerutil.AddFinalizer(instance, controllerName)
			if err := r.Update(ctx, instance); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// The object is being deleted
		if controllerutil.ContainsFinalizer(instance, controllerName) {
			// our finalizer is present, so lets handle any external dependency
			if err := r.deleteWebhook(ctx, instance); err != nil {
				// if fail to delete the external dependency here, return with error
				// so that it can be retried
				return ctrl.Result{}, err
			}

			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(instance, controllerName)
			if err := r.Update(ctx, instance); err != nil {
				return ctrl.Result{}, err
			}
		}
		// Stop reconciliation as the item is being deleted
	}
	err = r.reconcileWebHook(ctx, instance)
	if err != nil {
		return r.manageFailure(ctx, instance, err)
	}
	return r.manageSuccess(ctx, instance)
}

func (r *GitWebhookReconciler) deleteWebhook(ctx context.Context, instance *redhatcopv1alpha1.GitWebhook) error {
	if instance.Spec.GitHub != nil {
		return github.FromGitWebhook(instance).Delete(ctx)
	}
	if instance.Spec.GitLab != nil {
		return gitlab.FromGitWebhook(instance).Delete(ctx)
	}
	return err.New("unable to find gitserver definition")
}

func (r *GitWebhookReconciler) reconcileWebHook(ctx context.Context, instance *redhatcopv1alpha1.GitWebhook) error {
	if instance.Spec.GitHub != nil {
		return github.FromGitWebhook(instance).Reconcile(ctx)
	}
	if instance.Spec.GitLab != nil {
		return gitlab.FromGitWebhook(instance).Reconcile(ctx)
	}
	return err.New("unable to find gitserver definition")
}

// SetupWithManager sets up the controller with the Manager.
func (r *GitWebhookReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&redhatcopv1alpha1.GitWebhook{}).
		Watches(&source.Kind{Type: &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind: "Secret",
			}}}, &enqueForSelectedGitWebhook{
			log:    mgr.GetLogger().WithName("enqueForSelectedGitWebhook"),
			client: r.Client,
		}).
		Complete(r)
}

func (r *GitWebhookReconciler) manageSuccess(ctx context.Context, instance *redhatcopv1alpha1.GitWebhook) (reconcile.Result, error) {
	log := log.FromContext(ctx)
	condition := metav1.Condition{
		Type:               "Success",
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: instance.GetGeneration(),
		Reason:             "Webhook created or updated",
		Status:             metav1.ConditionTrue,
	}
	instance.Status.Conditions = (addOrReplaceCondition(condition, instance.Status.Conditions))
	err := r.Client.Status().Update(ctx, instance)
	if err != nil {
		log.Error(err, "unable to update status")
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func addOrReplaceCondition(c metav1.Condition, conditions []metav1.Condition) []metav1.Condition {
	for i, condition := range conditions {
		if c.Type == condition.Type {
			conditions[i] = c
			return conditions
		}
	}
	conditions = append(conditions, c)
	return conditions
}

func (r *GitWebhookReconciler) manageFailure(context context.Context, instance *redhatcopv1alpha1.GitWebhook, issue error) (reconcile.Result, error) {
	log := log.FromContext(context)
	r.Recorder.Event(instance, "Warning", "ProcessingError", issue.Error())

	condition := metav1.Condition{
		Type:               "Failure",
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: instance.GetGeneration(),
		Message:            issue.Error(),
		Reason:             "unable to reconcile",
		Status:             metav1.ConditionTrue,
	}
	instance.Status.Conditions = (addOrReplaceCondition(condition, instance.Status.Conditions))
	err := r.Client.Status().Update(context, instance)
	if err != nil {
		log.Error(err, "unable to update status")
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, issue
}

type enqueForSelectedGitWebhook struct {
	client client.Client
	log    logr.Logger
}

// return whether this EgressIPAM macthes this hostSubnet and with which CIDR
func (e *enqueForSelectedGitWebhook) matchesSecret(instance *redhatcopv1alpha1.GitWebhook, secret *corev1.Secret) bool {
	return instance.Spec.WebhookSecret.Name == secret.Name ||
		(instance.Spec.GitHub != nil && instance.Spec.GitHub.GitServerCredentials.Name == secret.Name) ||
		(instance.Spec.GitLab != nil && instance.Spec.GitLab.GitServerCredentials.Name == secret.Name)
}

func (e *enqueForSelectedGitWebhook) getAllGitWebhooks(namespace string) ([]redhatcopv1alpha1.GitWebhook, error) {
	gitWebhhookList := &redhatcopv1alpha1.GitWebhookList{}
	err := e.client.List(context.TODO(), gitWebhhookList, &client.ListOptions{
		Namespace: namespace,
	})
	if err != nil {
		e.log.Error(err, "unable to retrieve list of GitWebhook", "in namespace", namespace)
		return nil, err
	}
	return gitWebhhookList.Items, nil
}

func (e *enqueForSelectedGitWebhook) dispatchEvents(secret *corev1.Secret, q workqueue.RateLimitingInterface) {
	gitWebhooks, err := e.getAllGitWebhooks(secret.Namespace)
	if err != nil {
		e.log.Error(err, "unable to get all EgressIPAM resources")
		return
	}
	for _, gitWebhook := range gitWebhooks {
		if e.matchesSecret(&gitWebhook, secret) {
			q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
				Name:      gitWebhook.GetName(),
				Namespace: gitWebhook.GetNamespace(),
			}})
		}
	}
}

// trigger a egressIPAM reconcile event for those egressIPAM objects that reference this hostsubnet indireclty via the corresponding node.
func (e *enqueForSelectedGitWebhook) Create(evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	secret, ok := evt.Object.(*corev1.Secret)
	if !ok {
		e.log.Info("unable convert event object to secret,", "event", evt)
		return
	}
	e.dispatchEvents(secret, q)
}

// Update implements EventHandler
// trigger a router reconcile event for those routes that reference this secret
func (e *enqueForSelectedGitWebhook) Update(evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	secret, ok := evt.ObjectNew.(*corev1.Secret)
	if !ok {
		e.log.Info("unable convert event object to secret,", "event", evt)
		return
	}
	e.dispatchEvents(secret, q)
}

// Delete implements EventHandler
func (e *enqueForSelectedGitWebhook) Delete(evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
}
func (e *enqueForSelectedGitWebhook) Generic(evt event.GenericEvent, q workqueue.RateLimitingInterface) {
}
