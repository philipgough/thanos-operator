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
	"encoding/json"
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
	"k8s.io/client-go/tools/record"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	receiveFinalizer = "monitoring.thanos.io/receive-finalizer"
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
		MatchLabels: map[string]string{manifests.ComponentLabel: receive.IngestComponentName},
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
			k8s.EnqueueForEndpointSlice(r.Client, "ThanosReceive"),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}, endpointSlicePredicate),
		)

	return bld.Complete(r)
}

// syncResources syncs the resources for the ThanosReceive resource.
// It creates or updates the resources for the hashrings and the router.
func (r *ThanosReceiveReconciler) syncResources(ctx context.Context, receive monitoringthanosiov1alpha1.ThanosReceive) error {
	var objs []client.Object
	logger := log.FromContext(ctx)

	objs = append(objs, r.buildHashrings(receive)...)
	hashringConf, err := r.buildHashringConfig(ctx, receive)
	if err != nil {
		return fmt.Errorf("failed to build hashring configuration: %w", err)

	}
	objs = append(objs, hashringConf)

	var errCount int32
	for _, obj := range objs {
		if err := ctrl.SetControllerReference(&receive, obj, r.Scheme); err != nil {
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

// build hashring builds out the ingesters for the ThanosReceive resource.
func (r *ThanosReceiveReconciler) buildHashrings(receiver monitoringthanosiov1alpha1.ThanosReceive) []client.Object {
	var opts []receive.IngesterOptions
	baseLabels := receiver.GetLabels()
	baseSecret := k8s.ToSecretKeySelector(receiver.Spec.Ingester.DefaultObjectStorageConfig)
	image := receiver.Spec.Image

	for _, hashring := range receiver.Spec.Ingester.Hashrings {
		objStoreSecret := baseSecret
		if hashring.ObjectStorageConfig != nil {
			objStoreSecret = k8s.ToSecretKeySelector(*hashring.ObjectStorageConfig)
		}

		metaOpts := manifests.Options{
			Name:      receive.IngesterNameFromParent(receiver.GetName(), hashring.Name),
			Namespace: receiver.GetNamespace(),
			Replicas:  hashring.Replicas,
			Labels:    manifests.MergeLabels(baseLabels, hashring.Labels),
			Image:     image,
		}.ApplyDefaults()

		opt := receive.IngesterOptions{
			Options:        metaOpts,
			Retention:      string(hashring.Retention),
			StorageSize:    resource.MustParse(hashring.StorageSize),
			ObjStoreSecret: objStoreSecret,
		}
		opts = append(opts, opt)
	}

	return receive.BuildIngesters(opts)
}

// buildHashringConfig builds the hashring configuration for the ThanosReceive resource.
func (r *ThanosReceiveReconciler) buildHashringConfig(ctx context.Context, receiver monitoringthanosiov1alpha1.ThanosReceive) (client.Object, error) {
	logger := log.FromContext(ctx)

	cm := &corev1.ConfigMap{}
	err := r.Client.Get(ctx, client.ObjectKey{Namespace: receiver.GetNamespace(), Name: receiver.GetName()}, cm)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("failed to get config map for resource %s: %w", receiver.GetName(), err)
		}
	}

	// we want to get the current state to allow the builder to make more informed decisions.
	var currentState []receive.HashringConfig
	if cm != nil && cm.Data != nil && cm.Data[receive.HashringConfigKey] != "" {
		if err = json.Unmarshal([]byte(cm.Data[receive.HashringConfigKey]), &currentState); err != nil {
			return nil, fmt.Errorf("failed to unmarshal current state: %w", err)
		}
	}

	opts := receive.HashringOptions{
		Options: manifests.Options{
			Name:      receiver.GetName(),
			Namespace: receiver.GetNamespace(),
			Labels:    receiver.GetLabels(),
		},
		HashringSettings: make(map[string]receive.HashringMeta, len(receiver.Spec.Ingester.Hashrings)),
	}

	for _, hashring := range receiver.Spec.Ingester.Hashrings {
		labelValue := receive.IngesterNameFromParent(receiver.GetName(), hashring.Name)
		selectorListOpt := client.MatchingLabels{discoveryv1.LabelServiceName: labelValue}

		eps := discoveryv1.EndpointSliceList{}
		if err = r.Client.List(ctx, &eps, selectorListOpt, client.InNamespace(receiver.GetNamespace())); err != nil {
			return nil, fmt.Errorf("failed to list endpoint slices for resource %s: %w", receiver.GetName(), err)
		}

		opts.HashringSettings[labelValue] = receive.HashringMeta{
			Replicas:                 hashring.Replicas,
			OriginalName:             hashring.Name,
			Tenants:                  hashring.Tenants,
			TenantMatcherType:        receive.TenantMatcher(hashring.TenantMatcherType),
			AssociatedEndpointSlices: eps,
		}
	}

	return receive.BuildHashrings(logger, currentState, opts)
}

// buildRouter builds the router for the ThanosReceive resource.
func (r *ThanosReceiveReconciler) buildRouter(ctx context.Context, receiver monitoringthanosiov1alpha1.ThanosReceive) error {
	for _, hr := range receiver.Spec.Ingester.Hashrings {
		labelValue := receive.IngesterNameFromParent(receiver.GetName(), hr.Name)
		selectorListOpt := client.MatchingLabels{discoveryv1.LabelServiceName: labelValue}

		eps := discoveryv1.EndpointSliceList{}
		err := r.Client.List(ctx, &eps, selectorListOpt, client.InNamespace(receiver.GetNamespace()))
		if err != nil {
			return fmt.Errorf("failed to list endpoint slices for resource %s: %w", receiver.GetName(), err)
		}
	}

	return nil
}
