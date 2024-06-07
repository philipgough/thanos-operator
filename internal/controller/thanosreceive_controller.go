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

	monitoringthanosiov1alpha1 "github.com/thanos-community/thanos-operator/api/v1alpha1"
	"github.com/thanos-community/thanos-operator/internal/pkg/k8s"
	"github.com/thanos-community/thanos-operator/internal/pkg/manifests"
	"github.com/thanos-community/thanos-operator/internal/pkg/manifests/receive"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/tools/record"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	receiveFinalizer = "monitoring.thanos.io/finalizer"
)

// ThanosReceiveReconciler reconciles a ThanosReceive object
type ThanosReceiveReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.3/pkg/reconcile
func (r *ThanosReceiveReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the ThanosReceive instance to validate it is applied on the cluster.
	receiver := &monitoringthanosiov1alpha1.ThanosReceive{}
	err := r.Get(ctx, req.NamespacedName, receiver)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("thanos receive resource not found. ignoring since object may be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get ThanosReceive")
		return ctrl.Result{}, err
	}

	err = r.syncResources(ctx, *receiver)
	if err != nil {
		r.Recorder.Event(receiver, corev1.EventTypeWarning, "ReconcileError", err.Error())
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// +kubebuilder:rbac:groups=monitoring.thanos.io,resources=thanosreceives,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.thanos.io,resources=thanosreceives/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=monitoring.thanos.io,resources=thanosreceives/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets;deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;configmaps;serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="discovery.k8s.io",resources=endpointslices,verbs=get;list;watch

// SetupWithManager sets up the controller with the Manager.
func (r *ThanosReceiveReconciler) SetupWithManager(mgr ctrl.Manager) error {
	bld := ctrl.NewControllerManagedBy(mgr)
	return r.buildController(*bld)
}

// buildController sets up the controller with the Manager.
func (r *ThanosReceiveReconciler) buildController(bld builder.Builder) error {
	// add a selector to watch for the endpointslices that are owned by the ThanosReceive ingest Service(s).
	endpointSliceLS := metav1.LabelSelector{
		MatchLabels: map[string]string{discoveryv1.LabelServiceName: receive.IngestComponentName},
	}
	endpointSlicePredicate, err := predicate.LabelSelectorPredicate(endpointSliceLS)
	if err != nil {
		return err
	}

	bld.
		For(&monitoringthanosiov1alpha1.ThanosReceive{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.Service{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.StatefulSet{}).
		Watches(
			&discoveryv1.EndpointSlice{},
			r.enqueueForHashringConfig(),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}, endpointSlicePredicate),
		)

	return bld.Complete(r)
}

func (r *ThanosReceiveReconciler) syncResources(ctx context.Context, receive monitoringthanosiov1alpha1.ThanosReceive) error {
	err := r.syncHashrings(ctx, receive)
	return err
}

func (r *ThanosReceiveReconciler) syncHashrings(ctx context.Context, receiver monitoringthanosiov1alpha1.ThanosReceive) error {
	if err := receiver.Spec.Ingestor.Validate(receiver.Spec.Router.ReplicationFactor); err != nil {
		return err
	}

	var opts []receive.IngesterOptions
	baseLabels := receiver.GetLabels()
	baseSecret := k8s.ToSecretKeySelector(receiver.Spec.Ingestor.ObjectStorageConfig)
	image := receiver.Spec.Image

	for name, hashring := range receiver.Spec.Ingestor.Hashrings {
		objStoreSecret := baseSecret
		if hashring.ObjectStorageConfig != nil {
			objStoreSecret = k8s.ToSecretKeySelector(hashring.ObjectStorageConfig)
		}

		metaOpts := manifests.Options{
			Name:      r.ingestorNameFromParent(receiver.GetName(), string(name)),
			Namespace: receiver.GetNamespace(),
			Replicas:  *hashring.Replicas,
			Labels:    manifests.MergeLabels(baseLabels, hashring.Labels),
			Image:     image,
		}.ApplyDefaults()

		opt := receive.IngesterOptions{
			Options:        metaOpts,
			Retention:      string(hashring.Retention),
			StorageSize:    resource.MustParse(*hashring.StorageSize),
			ObjStoreSecret: objStoreSecret,
		}
		opts = append(opts, opt)
	}

	var errCount int32
	objs := receive.BuildIngesters(opts)
	logger := log.FromContext(ctx)

	for _, obj := range objs {
		if err := ctrl.SetControllerReference(&receiver, obj, r.Scheme); err != nil {
			logger.Error(err, "failed to set controller owner reference to resource")
			errCount++
			continue
		}

		depAnnotations, err := k8s.GetDependentAnnotations(ctx, r.Client, obj)
		if err != nil {
			logger.Error(err, "failed to fetch dependent annotations")
			return err
		}

		desired := obj.DeepCopyObject().(client.Object)
		mutateFn := manifests.MutateFuncFor(obj, desired, depAnnotations)

		op, err := ctrl.CreateOrUpdate(ctx, r.Client, obj, mutateFn)
		if err != nil {
			logger.Error(
				err, "failed to create or update resource",
				"gvk", obj.GetObjectKind().GroupVersionKind().String(),
				"resource", obj.GetName(),
				"namespace", obj.GetNamespace(),
			)
			errCount++
			continue
		}

		logger.V(1).Info(
			"resource configured",
			"operation", op, "gvk", obj.GetObjectKind().GroupVersionKind().String(),
			"resource", obj.GetName(), "namespace", obj.GetNamespace(),
		)
	}

	if errCount > 0 {
		return fmt.Errorf("failed to create or update %d resources for the hashrings", errCount)
	}

	return nil
}

// enqueueForHashringConfig enqueues requests for ThanosReceiveHashring when an EndpointSlice is owned by
// a headless Service which is owned by a ThanosReceiveHashring is touched.
func (r *ThanosReceiveReconciler) enqueueForHashringConfig() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		if obj.GetOwnerReferences() == nil {
			return nil
		}

		if len(obj.GetOwnerReferences()) != 1 || obj.GetOwnerReferences()[0].Kind != "Service" {
			return nil
		}

		owner := obj.GetOwnerReferences()[0]
		svc := &corev1.Service{}
		if err := r.Client.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: owner.Name}, svc); err != nil {
			return nil
		}

		if len(svc.GetOwnerReferences()) != 1 || svc.GetOwnerReferences()[0].Kind != "ThanosReceiveHashring" {
			return nil
		}

		parent := svc.GetOwnerReferences()[0]

		return []reconcile.Request{
			{
				NamespacedName: types.NamespacedName{
					Namespace: obj.GetNamespace(),
					Name:      parent.Name,
				},
			},
		}
	})
}

// ingestorNameFromParent returns a name for the ingester based on the parent ThanosReceive and the ingester name.
// The name is a concatenation of the ThanosReceive name and the ingester name. If the resulting name is longer
// than allowed, the ingester name is used as a fallback.
func (r *ThanosReceiveReconciler) ingestorNameFromParent(receiveName, ingesterName string) string {
	name := fmt.Sprintf("%s-%s", receiveName, ingesterName)
	// check if the name is a valid DNS-1123 subdomain
	if len(validation.IsDNS1123Subdomain(name)) == 0 {
		return name
	}
	// fallback to ingester name which is guaranteed to be valid via kubebuilder validation markers
	return ingesterName
}
