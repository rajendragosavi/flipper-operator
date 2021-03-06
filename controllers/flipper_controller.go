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
	"time"

	flipperv1alpha1 "github.com/rajendragosavi/flipper-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// FlipperReconciler reconciles a Flipper object
type FlipperReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=flipper.dev.io,resources=flippers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=flipper.dev.io,resources=flippers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=flipper.dev.io,resources=flippers/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Flipper object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
func (r *FlipperReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Reconciling......")

	var flipper flipperv1alpha1.Flipper
	err := r.Client.Get(ctx, req.NamespacedName, &flipper)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			log.Info("Flipper resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get the flipper object.")
		return ctrl.Result{}, err
	}
	log.Info("Flipper object - ", "OBJECT ", flipper)

	flipper.Status.Status = "available"

	ns := flipper.Spec.Match.Namespace
	filterLable := flipper.Spec.Match.Labels
	interval, _ := time.ParseDuration(flipper.Spec.Interval)
	var deploymentList = &appsv1.DeploymentList{}

	listOpts := []client.ListOption{
		client.InNamespace(ns),
		client.MatchingLabels(filterLable),
	}
	err = r.Client.List(ctx, deploymentList, listOpts...)
	if err != nil {
		log.Error(err, "failed to list the deployments in ", "Namespace ", ns)
		return ctrl.Result{}, err
	}
	names := getDeploymentNames(deploymentList.Items)
	log.Info("", "deployment - ", names)

	for _, name := range names {
		err = r.RollOutDeployment(ctx, name, ns)
		if err != nil {
			if errors.IsNotFound(err) {
				log.Info("Deployment object not found.", "Deployment Name", names[0], "Namespace", ns)
			}
		} else {
			log.Info("SUCCESSFULLY ROLLED OUT THE DEPLOYMENT!!!!")
		}
	}

	var finalStatus = make([]flipperv1alpha1.Deployments, 0)

	err = r.Client.List(ctx, deploymentList, listOpts...)
	if err != nil {
		log.Error(err, "failed to list the deployments in ", "Namespace ", ns)
		return ctrl.Result{}, err
	}
	for _, dep := range deploymentList.Items {
		var depStatus flipperv1alpha1.Deployments
		depStatus.Name = dep.ObjectMeta.Name
		depStatus.LastRolloutTime = dep.Spec.Template.ObjectMeta.Annotations["restartedAt"]
		finalStatus = append(finalStatus, depStatus)
	}
	flipper.Status.Deployments = finalStatus

	err = r.Status().Update(ctx, &flipper)
	if err != nil {
		log.Error(err, "erron in updating status", "Name", flipper.ObjectMeta.Name)
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: interval.Round(interval)}, nil
}

func (r *FlipperReconciler) RollOutDeployment(ctx context.Context, deploymentName string, namespace string) error {
	log.Log.Info("ROLLING OUT DEPLOYMENT")
	var existingDeployment = appsv1.Deployment{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: deploymentName, Namespace: namespace}, &existingDeployment)
	if err != nil {
		log.Log.Error(err, "error getting deployment object.", "Name", deploymentName)
		return err
	}
	// A merge patch will preserve other fields modified at runtime.
	patch := client.MergeFrom(existingDeployment.DeepCopy())
	updatedMap := make(map[string]string)

	updatedMap["restartedAt"] = metav1.Now().UTC().String()
	existingDeployment.Spec.Template.ObjectMeta.Annotations = updatedMap
	err = r.Patch(ctx, &existingDeployment, patch)
	if err != nil {
		log.Log.Error(err, "error patching deployment", "name", deploymentName)
		return err
	}
	return nil
}

// getDeploymentNames returns the deployment names
func getDeploymentNames(deployments []appsv1.Deployment) []string {
	var deploymentNames []string
	for _, deployment := range deployments {
		deploymentNames = append(deploymentNames, deployment.Name)
	}
	return deploymentNames
}

// SetupWithManager sets up the controller with the Manager.
func (r *FlipperReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&flipperv1alpha1.Flipper{}).WithEventFilter(predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldGeneration := e.ObjectOld.GetGeneration()
			newGeneration := e.ObjectNew.GetGeneration()
			// Generation is only updated on spec changes (also on deletion),
			// not metadata or status
			// Filter out events where the generation hasn't changed to
			// avoid being triggered by status updates
			return oldGeneration != newGeneration
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// The reconciler adds a finalizer so we perform clean-up
			// when the delete timestamp is added
			// Suppress Delete events to avoid filtering them out in the Reconcile function
			return false
		},
	}).Complete(r)
}
