/*
Copyright 2024.

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

package controller

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"time"

	monitoringthanosiov1alpha1 "github.com/thanos-community/thanos-operator/api/v1alpha1"

	"github.com/go-logr/logr"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	DefaultReceiveRouterContainerName = "thanos-receive-router"

	DefaultReceiveRouterHTTPPortName = "http"
	DefaultReceiveRouterHTTPPort     = 10902

	ReceiveRouterNameLabelValue      = DefaultReceiveRouterContainerName
	ReceiveRouterComponentLabelValue = "router"
	ReceiveRouterPartOfLabelValue    = "thanos"
	ReceiveRouterManagedByLabelValue = "thanos-operator"
)

const (
	receiveRouterFinalizer = "monitoring.thanos.io/router-finalizer"
)

// ThanosReceiveRouterReconciler reconciles a ThanosReceiveRouter object
type ThanosReceiveRouterReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=monitoring.thanos.io,resources=thanosreceiverouters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=monitoring.thanos.io,resources=thanosreceiverouters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=monitoring.thanos.io,resources=thanosreceiverouters/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ThanosReceiveRouter object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.3/pkg/reconcile
func (r *ThanosReceiveRouterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the ThanosReceiveRouter instance to validate it is applied on the cluster.
	receiveRouter := &monitoringthanosiov1alpha1.ThanosReceiveRouter{}
	err := r.Get(ctx, req.NamespacedName, receiveRouter)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("receive router resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get ThanosReceiveRouter")
		return ctrl.Result{}, err
	}

	// Set the status as Unknown when no status is available
	if receiveRouter.Status.Conditions == nil || len(receiveRouter.Status.Conditions) == 0 {
		meta.SetStatusCondition(
			&receiveRouter.Status.Conditions,
			metav1.Condition{Type: typeAvailableReceiveHashring,
				Status:  metav1.ConditionUnknown,
				Reason:  "Reconciling",
				Message: "Starting reconciliation"},
		)

		if err = r.Status().Update(ctx, receiveRouter); err != nil {
			logger.Error(err, "failed to update ThanosReceiveRouter status")
			return ctrl.Result{}, err
		}

		// re-fetch post status update so that we have the latest state of the resource on the cluster avoid raising
		// the error "the object has been modified, please apply your changes to the latest version and try again"
		// which would re-trigger the reconciliation if we try to update it again in the following operations
		if err := r.Get(ctx, req.NamespacedName, receiveRouter); err != nil {
			logger.Error(err, "failed to re-fetch ThanosReceiveRouter")
			return ctrl.Result{}, err
		}
	}

	// add a finalizer (see https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers)
	if !controllerutil.ContainsFinalizer(receiveRouter, receiveRouterFinalizer) {
		logger.Info("adding Finalizer for ThanosReceiveRouter")
		if ok := controllerutil.AddFinalizer(receiveRouter, receiveRouterFinalizer); !ok {
			logger.Error(err, "failed to add finalizer into the custom resource")
			return ctrl.Result{Requeue: true}, nil
		}

		if err = r.Update(ctx, receiveRouter); err != nil {
			logger.Error(err, "failed to update custom resource to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Check if the ThanosReceiveHashring instance is marked for deletion, indicated by the deletion timestamp being set.
	isMarkedForDeletion := receiveRouter.GetDeletionTimestamp() != nil
	if isMarkedForDeletion {
		return r.handleDeletionTimestamp(ctx, logger, req, receiveRouter)
	}

	result, err := r.syncService(ctx, logger, req, receiveRouter)
	if err != nil {
		logger.Error(err, "failed to sync headless service")
		return result, err
	}

	return ctrl.Result{}, nil
}

func (r *ThanosReceiveRouterReconciler) handleDeletionTimestamp(
	ctx context.Context,
	log logr.Logger,
	req ctrl.Request,
	receiveRouter *monitoringthanosiov1alpha1.ThanosReceiveRouter,
) (ctrl.Result, error) {
	if controllerutil.ContainsFinalizer(receiveRouter, receiveRouterFinalizer) {
		log.Info("performing Finalizer Operations for ThanosReceiveRouter before delete CR")

		// Let's add here a status "Downgrade" to reflect that this resource began its process to be terminated.
		meta.SetStatusCondition(&receiveRouter.Status.Conditions,
			metav1.Condition{
				Type:   typeDegradedReceiveHashring,
				Status: metav1.ConditionUnknown, Reason: "Finalizing",
				Message: fmt.Sprintf("performing finalizer operations for the custom resource: %s ", receiveRouter.Name)})

		if err := r.Status().Update(ctx, receiveRouter); err != nil {
			log.Error(err, "failed to update ThanosReceiveRouter status")
			return ctrl.Result{}, err
		}

		// Perform all operations required before removing the finalizer and allow
		// the Kubernetes API to remove the custom resource.
		r.doFinalizerOperationsForReceiveRouter(receiveRouter)

		// re-fetch before updating the status so that we have the latest state of the resource on the cluster
		if err := r.Get(ctx, req.NamespacedName, receiveRouter); err != nil {
			log.Error(err, "failed to re-fetch receive hashring")
			return ctrl.Result{}, err
		}

		meta.SetStatusCondition(&receiveRouter.Status.Conditions,
			metav1.Condition{
				Type:   typeDegradedReceiveHashring,
				Status: metav1.ConditionTrue, Reason: "Finalizing",
				Message: fmt.Sprintf("finalizer operations for custom resource %s name were successfully accomplished", receiveRouter.Name)})

		err := r.Status().Update(ctx, receiveRouter)
		if err != nil {
			log.Error(err, "failed to update ThanosReceiveRouter status")
			return ctrl.Result{}, err
		}

		log.Info("removing Finalizer for ThanosReceiveRouter after successfully perform the operations")
		if ok := controllerutil.RemoveFinalizer(receiveRouter, receiveRouterFinalizer); !ok {
			log.Error(err, "Failed to remove finalizer for ThanosReceiveRouter")
			return ctrl.Result{Requeue: true}, nil
		}

		if err := r.Update(ctx, receiveRouter); err != nil {
			log.Error(err, "Failed to remove finalizer for ThanosReceiveRouter")
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *ThanosReceiveRouterReconciler) doFinalizerOperationsForReceiveRouter(cr *monitoringthanosiov1alpha1.ThanosReceiveRouter) {
	// The following implementation will raise an event
	r.Recorder.Event(cr, "Warning", "Deleting",
		fmt.Sprintf("Custom Resource %s is being deleted from the namespace %s",
			cr.Name,
			cr.Namespace))
}

// labelsForThanosReceiveRouter returns the labels for selecting the resources.
// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
func labelsForThanosReceiveRouter() map[string]string {
	return map[string]string{
		monitoringthanosiov1alpha1.NameLabel:      ReceiveRouterNameLabelValue,
		monitoringthanosiov1alpha1.ComponentLabel: ReceiveRouterComponentLabelValue,
		monitoringthanosiov1alpha1.PartOfLabel:    ReceiveRouterPartOfLabelValue,
		monitoringthanosiov1alpha1.ManagedByLabel: ReceiveRouterManagedByLabelValue,
	}
}

// labelsForThanosReceiveRouterWithInstance returns the labels for selecting the resources with the instance label.
// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
func labelsForThanosReceiveRouterWithInstance(r *monitoringthanosiov1alpha1.ThanosReceiveRouter) map[string]string {
	baseLabels := labelsForThanosReceiveHashring()
	baseLabels[monitoringthanosiov1alpha1.InstanceLabel] = ReceiveRouterNameLabelValue + "-" + r.GetName()
	return baseLabels
}

// SetupWithManager sets up the controller with the Manager.
func (r *ThanosReceiveRouterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&monitoringthanosiov1alpha1.ThanosReceiveRouter{}).
		Complete(r)
}

func (r *ThanosReceiveRouterReconciler) syncService(
	ctx context.Context,
	log logr.Logger,
	req ctrl.Request,
	receiveRouter *monitoringthanosiov1alpha1.ThanosReceiveRouter,
) (ctrl.Result, error) {

	// Check if the Service already exists, if not create a new one
	found := &corev1.Service{}
	svcName := receiveRouter.GetServiceName()
	err := r.Get(ctx, types.NamespacedName{Name: svcName, Namespace: receiveRouter.Namespace}, found)
	if err != nil && apierrors.IsNotFound(err) {
		// Define a new Service
		svc, err := r.serviceForReceiveRouter(receiveRouter)
		if err != nil {
			log.Error(err, "failed to define new headless Service resource for ThanosReceiveRouter")

			// The following implementation will update the status
			meta.SetStatusCondition(&receiveRouter.Status.Conditions,
				metav1.Condition{
					Type:   typeAvailableReceiveHashring,
					Status: metav1.ConditionFalse, Reason: "Reconciling",
					Message: fmt.Sprintf("failed to create a Service for the custom resource (%s): (%s)",
						receiveRouter.Name, err)})

			if err := r.Status().Update(ctx, receiveRouter); err != nil {
				log.Error(err, "failed to update ThanosReceiveRouter status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		log.Info("creating a new Service",
			"Service.Namespace", svc.Namespace, "Service.Name", svc.Name)
		if err = r.Create(ctx, svc); err != nil {
			log.Error(err, "failed to create new Service",
				"Service.Namespace", svc.Namespace, "Service.Name", svc.Name)
			return ctrl.Result{}, err
		}

		// Service created successfully
		// We will requeue the reconciliation so that we can ensure the state
		// and move forward for the next operations
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	} else if err != nil {
		log.Error(err, "failed to get Service")
		// return the error for the reconciliation triggered again
		return ctrl.Result{}, err
	}

	want, _ := r.serviceForReceiveRouter(receiveRouter)
	if equality.Semantic.DeepEqual(found.Spec, want.Spec) {
		return ctrl.Result{}, nil
	}

	found.Spec.ClusterIP = corev1.ClusterIPNone
	found.Spec.Ports = getReceiveIngestServicePorts()

	if err = r.Update(ctx, found); err != nil {
		log.Error(err, "failed to update headless Service",
			"Service.Namespace", found.Namespace, "Service.Name", found.Name)

		// Re-fetch so that we have the latest state of the resource on the cluster
		if err := r.Get(ctx, req.NamespacedName, receiveRouter); err != nil {
			log.Error(err, "failed to re-fetch ThanosReceiveHashring")
			return ctrl.Result{}, err
		}

		// The following implementation will update the status
		meta.SetStatusCondition(&receiveRouter.Status.Conditions,
			metav1.Condition{
				Type:   typeAvailableReceiveHashring,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("failed to update the headless Service for the custom resource (%s): (%s)",
					receiveRouter.Name, err),
			},
		)

		if err := r.Status().Update(ctx, receiveRouter); err != nil {
			log.Error(err, "failed to update ThanosReceiveHashring status")
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// ThanosReceiveRouterReconciler returns a Service object for the router
func (r *ThanosReceiveRouterReconciler) serviceForReceiveRouter(
	receiveRouter *monitoringthanosiov1alpha1.ThanosReceiveRouter) (*corev1.Service, error) {
	labels := labelsForThanosReceiveRouterWithInstance(receiveRouter)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      receiveRouter.GetServiceName(),
			Namespace: receiveRouter.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports:    getReceiveRouterServicePorts(),
		},
	}

	// Set the ownerRef (see https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/)
	if err := ctrl.SetControllerReference(receiveRouter, svc, r.Scheme); err != nil {
		return nil, err
	}
	return svc, nil
}

func getReceiveRouterServicePorts() []corev1.ServicePort {
	return []corev1.ServicePort{
		{
			Name:       DefaultReceiveRouterHTTPPortName,
			Port:       DefaultReceiveRouterHTTPPort,
			TargetPort: intstr.FromInt32(DefaultReceiveRouterHTTPPort),
			Protocol:   "TCP",
		},
	}
}
