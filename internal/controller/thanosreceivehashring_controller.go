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
	"slices"
	"sort"
	"time"

	monitoringthanosiov1alpha1 "github.com/thanos-community/thanos-operator/api/v1alpha1"

	"github.com/go-logr/logr"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ThanosReceiveHashringReconciler reconciles a ThanosReceiveHashring object
type ThanosReceiveHashringReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=monitoring.thanos.io,resources=thanosreceivehashrings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=monitoring.thanos.io,resources=thanosreceivehashrings/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=monitoring.thanos.io,resources=thanosreceivehashrings/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=services;configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="discovery.k8s.io",resources=endpointslices,verbs=get;list;watch

// SetupWithManager sets up the controller with the Manager.
func (r *ThanosReceiveHashringReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// We want to index the ConfigMap name so that we can watch for changes to it and reconcile any of
	// the ThanosReceiveHashrings that reference it
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &monitoringthanosiov1alpha1.ThanosReceiveHashring{}, configMapField, func(rawObj client.Object) []string {
		// Extract the ConfigMap name from the ThanosReceiveHashring Spec, if one is provided
		receiveHashring := rawObj.(*monitoringthanosiov1alpha1.ThanosReceiveHashring)
		if receiveHashring.Spec.OutputHashringConfig == nil {
			return nil
		}
		return []string{receiveHashring.Spec.OutputHashringConfig.Name}
	}); err != nil {
		return err
	}

	endpointSliceLS := metav1.LabelSelector{
		MatchLabels: map[string]string{discoveryv1.LabelServiceName: ReceiveIngestNameLabelValue},
	}
	endpointSlicePredicate, err := predicate.LabelSelectorPredicate(endpointSliceLS)
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&monitoringthanosiov1alpha1.ThanosReceiveHashring{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.findObjectsForConfigMap),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Watches(
			&discoveryv1.EndpointSlice{},
			r.enqueueForHashringConfig(),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}, endpointSlicePredicate)).
		Complete(r)
}

const (
	DefaultReceiveIngestContainerName = "thanos-receive-ingest"

	DefaultReceiveIngestGRPCPortName        = "grpc"
	DefaultReceiveIngestGRPCPort            = 10901
	DefaultReceiveIngestHTTPPortName        = "http"
	DefaultReceiveIngestHTTPPort            = 10902
	DefaultReceiveIngestRemoteWritePortName = "remote-write"
	DefaultReceiveIngestRemoteWritePort     = 19291

	ReceiveIngestNameLabelValue      = "thanos-receive-hashring"
	ReceiveIngestComponentLabelValue = "ingestor"
	ReceiveIngestPartOfLabelValue    = "thanos"
	ReceiveIngestManagedByLabelValue = "thanos-operator"
)

const (
	receiveHashringFinalizer = "monitoring.thanos.io/hashring-finalizer"
	configMapField           = ".spec.outputHashringConfig.name"
)

// Definitions to manage status conditions
const (
	// typeAvailableReceiveHashring represents the status of the StatefulSet reconciliation
	typeAvailableReceiveHashring = "Available"
	// typeDegradedReceiveHashring represents the status used when the
	// custom resource is deleted and the finalizer operations are yet to occur.
	typeDegradedReceiveHashring = "Degraded"
)

const (
	objectStoreEnvVarName = "OBJSTORE_CONFIG"

	dataVolumeName      = "data"
	dataVolumeMountPath = "var/thanos/receive"
)

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.3/pkg/reconcile
func (r *ThanosReceiveHashringReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the ThanosReceiveHashring instance to validate it is applied on the cluster.
	receiveHashring := &monitoringthanosiov1alpha1.ThanosReceiveHashring{}
	err := r.Get(ctx, req.NamespacedName, receiveHashring)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("receive hashring resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get ThanosReceiveHashring")
		return ctrl.Result{}, err
	}

	// Set the status as Unknown when no status is available
	if receiveHashring.Status.Conditions == nil || len(receiveHashring.Status.Conditions) == 0 {
		meta.SetStatusCondition(
			&receiveHashring.Status.Conditions,
			metav1.Condition{Type: typeAvailableReceiveHashring,
				Status:  metav1.ConditionUnknown,
				Reason:  "Reconciling",
				Message: "Starting reconciliation"},
		)

		if err = r.Status().Update(ctx, receiveHashring); err != nil {
			logger.Error(err, "failed to update ThanosReceiveHashring status")
			return ctrl.Result{}, err
		}

		// re-fetch post status update so that we have the latest state of the resource on the cluster avoid raising
		// the error "the object has been modified, please apply your changes to the latest version and try again"
		// which would re-trigger the reconciliation if we try to update it again in the following operations
		if err := r.Get(ctx, req.NamespacedName, receiveHashring); err != nil {
			logger.Error(err, "failed to re-fetch ThanosReceiveHashring")
			return ctrl.Result{}, err
		}
	}

	// add a finalizer (see https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers)
	if !controllerutil.ContainsFinalizer(receiveHashring, receiveHashringFinalizer) {
		logger.Info("adding Finalizer for ThanosReceiveHashring")
		if ok := controllerutil.AddFinalizer(receiveHashring, receiveHashringFinalizer); !ok {
			logger.Error(err, "failed to add finalizer into the custom resource")
			return ctrl.Result{Requeue: true}, nil
		}

		if err = r.Update(ctx, receiveHashring); err != nil {
			logger.Error(err, "failed to update custom resource to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Check if the ThanosReceiveHashring instance is marked for deletion, indicated by the deletion timestamp being set.
	isMarkedForDeletion := receiveHashring.GetDeletionTimestamp() != nil
	if isMarkedForDeletion {
		return r.handleDeletionTimestamp(ctx, logger, req, receiveHashring)
	}

	result, err := r.syncHeadlessService(ctx, logger, req, receiveHashring)
	if err != nil {
		logger.Error(err, "failed to sync headless service")
		return result, err
	}

	result, err = r.syncStatefulSet(ctx, logger, req, receiveHashring)
	if err != nil {
		return result, err
	}

	// nothing else to do if we don't expect to manage the hashring configuration
	if receiveHashring.Spec.OutputHashringConfig == nil {
		return ctrl.Result{}, nil
	}

	result, err = r.syncHashringConfigMap(ctx, logger, req, receiveHashring)
	if err != nil {
		return result, err

	}
	return ctrl.Result{}, nil
}

func (r *ThanosReceiveHashringReconciler) syncHeadlessService(
	ctx context.Context,
	log logr.Logger,
	req ctrl.Request,
	receiveHashring *monitoringthanosiov1alpha1.ThanosReceiveHashring,
) (ctrl.Result, error) {

	// Check if the Service already exists, if not create a new one
	found := &corev1.Service{}
	svcName := receiveHashring.GetServiceName()
	err := r.Get(ctx, types.NamespacedName{Name: svcName, Namespace: receiveHashring.Namespace}, found)
	if err != nil && apierrors.IsNotFound(err) {
		// Define a new headless service
		svc, err := r.headlessServiceForReceiveIngest(receiveHashring)
		if err != nil {
			log.Error(err, "failed to define new headless Service resource for ThanosReceiveHashring")

			// The following implementation will update the status
			meta.SetStatusCondition(&receiveHashring.Status.Conditions,
				metav1.Condition{
					Type:   typeAvailableReceiveHashring,
					Status: metav1.ConditionFalse, Reason: "Reconciling",
					Message: fmt.Sprintf("failed to create a headless Service for the custom resource (%s): (%s)",
						receiveHashring.Name, err)})

			if err := r.Status().Update(ctx, receiveHashring); err != nil {
				log.Error(err, "failed to update ThanosReceiveHashring status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		log.Info("creating a new headless Service",
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

	want, _ := r.headlessServiceForReceiveIngest(receiveHashring)
	if equality.Semantic.DeepEqual(found.Spec, want.Spec) {
		log.Info("SOMETHING NOT EQUAL")
		return ctrl.Result{}, nil
	}

	found.Spec.ClusterIP = corev1.ClusterIPNone
	found.Spec.Ports = getServicePorts()

	if err = r.Update(ctx, found); err != nil {
		log.Error(err, "failed to update headless Service",
			"Service.Namespace", found.Namespace, "Service.Name", found.Name)

		// Re-fetch so that we have the latest state of the resource on the cluster
		if err := r.Get(ctx, req.NamespacedName, receiveHashring); err != nil {
			log.Error(err, "failed to re-fetch ThanosReceiveHashring")
			return ctrl.Result{}, err
		}

		// The following implementation will update the status
		meta.SetStatusCondition(&receiveHashring.Status.Conditions,
			metav1.Condition{
				Type:   typeAvailableReceiveHashring,
				Status: metav1.ConditionFalse, Reason: "Reconciling",
				Message: fmt.Sprintf("failed to update the headless Service for the custom resource (%s): (%s)",
					receiveHashring.Name, err),
			},
		)

		if err := r.Status().Update(ctx, receiveHashring); err != nil {
			log.Error(err, "failed to update ThanosReceiveHashring status")
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *ThanosReceiveHashringReconciler) syncStatefulSet(
	ctx context.Context,
	log logr.Logger,
	req ctrl.Request,
	receiveHashring *monitoringthanosiov1alpha1.ThanosReceiveHashring,
) (ctrl.Result, error) {
	// Check if the StatefulSet already exists, if not create a new one
	found := &appsv1.StatefulSet{}
	err := r.Get(ctx, types.NamespacedName{Name: receiveHashring.Name, Namespace: receiveHashring.Namespace}, found)
	if err != nil && apierrors.IsNotFound(err) {
		// Define a new sts
		sts, err := r.statefulSetForReceiveIngest(receiveHashring)
		if err != nil {
			log.Error(err, "failed to define new StatefulSet resource for ThanosReceiveHashring")

			// The following implementation will update the status
			meta.SetStatusCondition(&receiveHashring.Status.Conditions,
				metav1.Condition{
					Type:   typeAvailableReceiveHashring,
					Status: metav1.ConditionFalse, Reason: "Reconciling",
					Message: fmt.Sprintf("failed to create StatefulSet for the custom resource (%s): (%s)", receiveHashring.Name, err)})

			if err := r.Status().Update(ctx, receiveHashring); err != nil {
				log.Error(err, "failed to update ThanosReceiveHashring status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		log.Info("creating a new StatefulSet",
			"StatefulSet.Namespace", sts.Namespace, "StatefulSet.Name", sts.Name)
		if err = r.Create(ctx, sts); err != nil {
			log.Error(err, "failed to create new StatefulSet",
				"StatefulSet.Namespace", sts.Namespace, "StatefulSet.Name", sts.Name)
			return ctrl.Result{}, err
		}

		// StatefulSet created successfully
		// We will requeue the reconciliation so that we can ensure the state
		// and move forward for the next operations
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	} else if err != nil {
		log.Error(err, "failed to get StatefulSet")
		// return the error for the reconciliation triggered again
		return ctrl.Result{}, err
	}

	// The CRD API is defining that the ThanosReceiveHashring type, have a ThanosReceiveHashringSpec.Replicas field
	// to set the quantity of StatefulSet instances is the desired state on the cluster.
	// Therefore, the following code will ensure the StatefulSet size is the same as defined
	// via the Replica spec of the Custom Resource which we are reconciling.
	size := receiveHashring.Spec.Replicas
	if found.Spec.Replicas != size {
		found.Spec.Replicas = size
		if err = r.Update(ctx, found); err != nil {
			log.Error(err, "failed to update StatefulSet",
				"StatefulSet.Namespace", found.Namespace, "StatefulSet.Name", found.Name)

			// Re-fetch so that we have the latest state of the resource on the cluster
			if err := r.Get(ctx, req.NamespacedName, receiveHashring); err != nil {
				log.Error(err, "failed to re-fetch ThanosReceiveHashring")
				return ctrl.Result{}, err
			}

			// The following implementation will update the status
			meta.SetStatusCondition(&receiveHashring.Status.Conditions,
				metav1.Condition{
					Type:   typeAvailableReceiveHashring,
					Status: metav1.ConditionFalse, Reason: "Resizing",
					Message: fmt.Sprintf("failed to update the size for the custom resource (%s): (%s)", receiveHashring.Name, err)})

			if err := r.Status().Update(ctx, receiveHashring); err != nil {
				log.Error(err, "failed to update ThanosReceiveHashring status")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, err
		}

		// Now, that we update the size we want to requeue the reconciliation
		// so that we can ensure that we have the latest state of the resource before
		// update. Also, it will help ensure the desired state on the cluster
		return ctrl.Result{Requeue: true}, nil
	}

	// The following implementation will update the status
	meta.SetStatusCondition(&receiveHashring.Status.Conditions,
		metav1.Condition{
			Type:   typeAvailableReceiveHashring,
			Status: metav1.ConditionTrue, Reason: "Reconciling",
			Message: fmt.Sprintf("StatefulSet for custom resource (%s) with %d replicas created successfully",
				receiveHashring.Name, receiveHashring.Spec.Replicas)})

	if err := r.Status().Update(ctx, receiveHashring); err != nil {
		log.Error(err, "failed to update ThanosReceiveHashring status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ThanosReceiveHashringReconciler) syncHashringConfigMap(
	ctx context.Context,
	log logr.Logger,
	req ctrl.Request,
	receiveHashring *monitoringthanosiov1alpha1.ThanosReceiveHashring,
) (ctrl.Result, error) {

	// the ConfigMap can have multiple owners, so we need to set the OwnerReference
	// to track all the ThanosReceiveHashring that own a piece of the hashring configuration
	ownerRef := metav1.OwnerReference{
		APIVersion: "v1alpha1",
		Kind:       "ThanosReceiveHashring",
		Name:       receiveHashring.Name,
		UID:        receiveHashring.UID,
	}

	// Check if the ConfigMap already exists
	found := &corev1.ConfigMap{}
	name := receiveHashring.Spec.OutputHashringConfig.Name
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: receiveHashring.Namespace}, found)
	if err != nil && apierrors.IsNotFound(err) {
		// Define a new ConfigMap but don't set the data yet
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:            name,
				Labels:          labelsForThanosReceiveHashring(),
				Namespace:       receiveHashring.Namespace,
				OwnerReferences: []metav1.OwnerReference{ownerRef},
			},
		}

		if err = r.Create(ctx, cm); err != nil {
			log.Error(err, "failed to create new ConfigMap",
				"ConfigMap.Namespace", cm.Namespace, "ConfigMap.Name", cm.Name)

			meta.SetStatusCondition(&receiveHashring.Status.Conditions,
				metav1.Condition{
					Type:   typeAvailableReceiveHashring,
					Status: metav1.ConditionFalse, Reason: "Reconciling",
					Message: fmt.Sprintf("failed to create a hashring configuration for the custom resource (%s): (%s)",
						receiveHashring.Name, err)})

			if err := r.Status().Update(ctx, receiveHashring); err != nil {
				log.Error(err, "failed to update ThanosReceiveHashring status")
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, err
		}
		// Re-enqueue post creation
		return ctrl.Result{RequeueAfter: time.Second}, nil
	} else if err != nil {
		log.Error(err, "failed to get ConfigMap",
			"ConfigMap.Namespace", found.Namespace, "ConfigMap.Name", found.Name)
		// return the error for the reconciliation triggered again
		return ctrl.Result{}, err
	}

	filters := []client.ListOption{
		client.InNamespace(receiveHashring.Namespace),
		client.MatchingLabels(map[string]string{discoveryv1.LabelServiceName: receiveHashring.GetServiceName()}),
	}

	// lookup the EndPointSlice(s) associated with the hashring that matches the filters
	eps := &discoveryv1.EndpointSliceList{}
	if err := r.List(ctx, eps, filters...); err != nil {
		log.Error(err, "failed to list EndpointSlices", "hashring", receiveHashring.Name)
		return ctrl.Result{}, err
	}

	if len(eps.Items) == 0 {
		log.Info("no EndpointSlice found", "hashring", receiveHashring.Name)
		return ctrl.Result{}, nil
	}

	var readyRemoteWriteEndpoints []string
	for _, ep := range eps.Items {
		for _, ep := range ep.Endpoints {
			if ep.Hostname == nil {
				continue
			}
			if ep.Conditions.Ready != nil && !*ep.Conditions.Ready {
				continue
			}

			readyRemoteWriteEndpoints = append(readyRemoteWriteEndpoints,
				fmt.Sprintf("%s.%s:%d", *ep.Hostname, receiveHashring.GetServiceName(), DefaultReceiveIngestRemoteWritePort))
		}
	}
	// we want to sort and deduplicate the endpoints in case there are duplicates across multiple EndpointSlices
	slices.Sort(readyRemoteWriteEndpoints)
	deduplicatedRemoteWriteEndpoints := slices.Compact(readyRemoteWriteEndpoints)

	found = &corev1.ConfigMap{}
	// Get the ConfigMap again to update the data
	err = r.Get(ctx, types.NamespacedName{Name: name, Namespace: receiveHashring.Namespace}, found)
	if err != nil {
		log.Error(err, "failed to get ConfigMap",
			"ConfigMap.Namespace", found.Namespace, "ConfigMap.Name", found.Name)
		return ctrl.Result{}, err
	}

	// add sort and deduplicate the OwnerReferences prior to updating the ConfigMap
	found.OwnerReferences = append(found.OwnerReferences, ownerRef)
	sort.SliceStable(found.OwnerReferences, func(i, j int) bool {
		return found.OwnerReferences[i].Name < found.OwnerReferences[j].Name
	})
	_ = found.OwnerReferences
	found.OwnerReferences = slices.Compact(found.OwnerReferences)

	if found.Data == nil || found.Data[receiveHashring.Spec.OutputHashringConfig.Key] == "" {
		// this is the first time we have seen this data so we want to only write it if all members
		// of the hashring are ready
		found.Data = make(map[string]string)

		// if not ready we have nothing to do
		if int32(len(deduplicatedRemoteWriteEndpoints)) != *receiveHashring.Spec.Replicas {
			log.Info("not all members of the hashring are ready, skipping ConfigMap update")
			return ctrl.Result{}, nil
		}

		hashrings := []monitoringthanosiov1alpha1.HashringConfig{
			buildHashringConfigFrom(deduplicatedRemoteWriteEndpoints, receiveHashring),
		}
		b, err := json.MarshalIndent(hashrings, "", "    ")
		if err != nil {
			log.Error(err, "failed to marshal ConfigMap data", "ConfigMap.Namespace",
				found.Namespace, "ConfigMap.Name", found.Name, "key", receiveHashring.Spec.OutputHashringConfig.Key)
			return ctrl.Result{}, err
		}

		// if they are ready we can update the ConfigMap
		found.Data[receiveHashring.Spec.OutputHashringConfig.Key] = string(b)
		err = r.Update(ctx, found)
		if err != nil {
			log.Error(err, "failed to update ConfigMap",
				"ConfigMap.Namespace", found.Namespace, "ConfigMap.Name", found.Name)

			return ctrl.Result{}, err
		}
	}

	// we have the key already so we need to handle a potential merge
	var existingHashrings []monitoringthanosiov1alpha1.HashringConfig
	if err = json.Unmarshal([]byte(found.Data[receiveHashring.Spec.OutputHashringConfig.Key]), &existingHashrings); err != nil {
		log.Error(err, "failed to unmarshal ConfigMap data", "ConfigMap.Namespace",
			found.Namespace, "ConfigMap.Name", found.Name, "key", receiveHashring.Spec.OutputHashringConfig.Key)
		return ctrl.Result{}, err
	}
	// we have multiple hashrings in the ConfigMap data, we are only interested in the section that is
	// managed by this particular hashring
	var foundHashringConfig *monitoringthanosiov1alpha1.HashringConfig
	var needsUpdate bool
	for _, hashring := range existingHashrings {
		if hashring.Hashring == receiveHashring.Name {
			// we have found the hashring we are interested in
			// we can now update the ConfigMap data
			foundHashringConfig = &hashring
			break
		}
	}

	hashring := buildHashringConfigFrom(deduplicatedRemoteWriteEndpoints, receiveHashring)

	if foundHashringConfig == nil {
		// this is new so we can just set it
		needsUpdate = true
		existingHashrings = append(existingHashrings, hashring)
	} else {
		// we need to check if the endpoints have changed if they have we need to update the ConfigMap
		var endpoints []string
		for _, ep := range foundHashringConfig.Endpoints {
			endpoints = append(endpoints, ep.Address)
		}
		slices.Sort(endpoints)
		needsUpdate = !slices.Equal(endpoints, deduplicatedRemoteWriteEndpoints)

		if needsUpdate {
			foundHashringConfig = &hashring
		}
	}
	if !needsUpdate {
		return ctrl.Result{}, nil
	}

	// we swap out the old hashring with the new one
	foundHashringConfig = &hashring
	b, err := json.MarshalIndent(existingHashrings, "", "    ")
	if err != nil {
		log.Error(err, "failed to marshal ConfigMap data", "ConfigMap.Namespace",
			found.Namespace, "ConfigMap.Name", found.Name, "key", receiveHashring.Spec.OutputHashringConfig.Key)
		return ctrl.Result{}, err
	}

	found.Data[receiveHashring.Spec.OutputHashringConfig.Key] = string(b)
	err = r.Client.Update(ctx, found)
	if err != nil {
		log.Error(err, "failed to update ConfigMap",
			"ConfigMap.Namespace", found.Namespace, "ConfigMap.Name", found.Name)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// headlessServiceForReceiveIngest returns a Service object for the hashring
func (r *ThanosReceiveHashringReconciler) headlessServiceForReceiveIngest(
	receiveHashring *monitoringthanosiov1alpha1.ThanosReceiveHashring) (*corev1.Service, error) {
	labels := labelsForThanosReceiveHashringWithInstance(receiveHashring)

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      receiveHashring.GetServiceName(),
			Namespace: receiveHashring.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			Selector:  labels,
			Ports:     getServicePorts(),
		},
	}

	// Set the ownerRef (see https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/)
	if err := ctrl.SetControllerReference(receiveHashring, svc, r.Scheme); err != nil {
		return nil, err
	}
	return svc, nil
}

// statefulSetForReceiveIngest returns a StatefulSet object for the hashring
func (r *ThanosReceiveHashringReconciler) statefulSetForReceiveIngest(
	receiveHashring *monitoringthanosiov1alpha1.ThanosReceiveHashring) (*appsv1.StatefulSet, error) {
	labels := labelsForThanosReceiveHashringWithInstance(receiveHashring)
	replicas := receiveHashring.Spec.Replicas

	var vc []corev1.PersistentVolumeClaim
	if receiveHashring.Spec.StorageSize != nil {
		vc = []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dataVolumeName,
					Namespace: receiveHashring.Namespace,
					Labels:    labels,
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse(*receiveHashring.Spec.StorageSize),
						},
					},
				},
			},
		}
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      receiveHashring.Name,
			Namespace: receiveHashring.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: receiveHashring.GetServiceName(),
			Replicas:    replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			VolumeClaimTemplates: vc,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image:           DefaultThanosImage,
							Name:            DefaultReceiveIngestContainerName,
							ImagePullPolicy: corev1.PullAlways,
							// Ensure restrictive context for the container
							// More info: https://kubernetes.io/docs/concepts/security/pod-security-standards/#restricted
							SecurityContext: &corev1.SecurityContext{
								RunAsNonRoot:             ptr.To(true),
								AllowPrivilegeEscalation: ptr.To(false),
								RunAsUser:                ptr.To(int64(10001)),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{
										"ALL",
									},
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/-/ready",
										Port: intstr.FromInt32(DefaultReceiveIngestHTTPPort),
									},
								},
								InitialDelaySeconds: 60,
								TimeoutSeconds:      1,
								PeriodSeconds:       30,
								SuccessThreshold:    1,
								FailureThreshold:    8,
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/-/healthy",
										Port: intstr.FromInt32(DefaultReceiveIngestHTTPPort),
									},
								},
								InitialDelaySeconds: 60,
								TimeoutSeconds:      1,
								PeriodSeconds:       30,
								SuccessThreshold:    1,
								FailureThreshold:    8,
							},
							Env: []corev1.EnvVar{
								{
									Name: "NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.name",
										},
									},
								},
								{
									Name: "NAMESPACE",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.namespace",
										},
									},
								},
								{
									Name: objectStoreEnvVarName,
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: receiveHashring.Spec.ObjectStorageConfig.Name,
											},
											Key:      receiveHashring.Spec.ObjectStorageConfig.Key,
											Optional: ptr.To(false),
										},
									},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      dataVolumeName,
									MountPath: dataVolumeMountPath,
								},
							},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: DefaultReceiveIngestGRPCPort,
									Name:          DefaultReceiveIngestGRPCPortName,
								},
								{
									ContainerPort: DefaultReceiveIngestHTTPPort,
									Name:          DefaultReceiveIngestHTTPPortName,
								},
								{
									ContainerPort: DefaultReceiveIngestRemoteWritePort,
									Name:          DefaultReceiveIngestRemoteWritePortName,
								},
							},
							TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
							TerminationMessagePath:   corev1.TerminationMessagePathDefault,
							Args:                     argsForReceiveIngestSts(receiveHashring),
						},
					},
				},
			},
		},
	}

	// Set the ownerRef (see https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/)
	if err := ctrl.SetControllerReference(receiveHashring, sts, r.Scheme); err != nil {
		return nil, err
	}
	return sts, nil
}

func (r *ThanosReceiveHashringReconciler) handleDeletionTimestamp(
	ctx context.Context,
	log logr.Logger,
	req ctrl.Request,
	receiveHashring *monitoringthanosiov1alpha1.ThanosReceiveHashring,
) (ctrl.Result, error) {

	if controllerutil.ContainsFinalizer(receiveHashring, receiveHashringFinalizer) {
		log.Info("performing Finalizer Operations for ThanosReceiveHashring before delete CR")

		// Let's add here a status "Downgrade" to reflect that this resource began its process to be terminated.
		meta.SetStatusCondition(&receiveHashring.Status.Conditions,
			metav1.Condition{
				Type:   typeDegradedReceiveHashring,
				Status: metav1.ConditionUnknown, Reason: "Finalizing",
				Message: fmt.Sprintf("performing finalizer operations for the custom resource: %s ", receiveHashring.Name)})

		if err := r.Status().Update(ctx, receiveHashring); err != nil {
			log.Error(err, "failed to update ThanosReceiveHashring status")
			return ctrl.Result{}, err
		}

		// Perform all operations required before removing the finalizer and allow
		// the Kubernetes API to remove the custom resource.
		r.doFinalizerOperationsForReceiveHashring(receiveHashring)

		// re-fetch before updating the status so that we have the latest state of the resource on the cluster
		if err := r.Get(ctx, req.NamespacedName, receiveHashring); err != nil {
			log.Error(err, "failed to re-fetch receive hashring")
			return ctrl.Result{}, err
		}

		meta.SetStatusCondition(&receiveHashring.Status.Conditions,
			metav1.Condition{
				Type:   typeDegradedReceiveHashring,
				Status: metav1.ConditionTrue, Reason: "Finalizing",
				Message: fmt.Sprintf("finalizer operations for custom resource %s name were successfully accomplished", receiveHashring.Name)})

		err := r.Status().Update(ctx, receiveHashring)
		if err != nil {
			log.Error(err, "failed to update ThanosReceiveHashring status")
			return ctrl.Result{}, err
		}

		log.Info("removing Finalizer for ThanosReceiveHashring after successfully perform the operations")
		if ok := controllerutil.RemoveFinalizer(receiveHashring, receiveHashringFinalizer); !ok {
			log.Error(err, "Failed to remove finalizer for ThanosReceiveHashring")
			return ctrl.Result{Requeue: true}, nil
		}

		if err := r.Update(ctx, receiveHashring); err != nil {
			log.Error(err, "Failed to remove finalizer for ThanosReceiveHashring")
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *ThanosReceiveHashringReconciler) doFinalizerOperationsForReceiveHashring(cr *monitoringthanosiov1alpha1.ThanosReceiveHashring) {
	// The following implementation will raise an event
	r.Recorder.Event(cr, "Warning", "Deleting",
		fmt.Sprintf("Custom Resource %s is being deleted from the namespace %s",
			cr.Name,
			cr.Namespace))
}

// labelsForThanosReceiveHashring returns the labels for selecting the resources.
// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
func labelsForThanosReceiveHashring() map[string]string {
	return map[string]string{
		monitoringthanosiov1alpha1.NameLabel:      ReceiveIngestNameLabelValue,
		monitoringthanosiov1alpha1.ComponentLabel: ReceiveIngestComponentLabelValue,
		monitoringthanosiov1alpha1.PartOfLabel:    ReceiveIngestPartOfLabelValue,
		monitoringthanosiov1alpha1.ManagedByLabel: ReceiveIngestManagedByLabelValue,
	}
}

// labelsForThanosReceiveHashringWithInstance returns the labels for selecting the resources with the instance label.
// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
func labelsForThanosReceiveHashringWithInstance(r *monitoringthanosiov1alpha1.ThanosReceiveHashring) map[string]string {
	baseLabels := labelsForThanosReceiveHashring()
	baseLabels[monitoringthanosiov1alpha1.InstanceLabel] = ReceiveIngestNameLabelValue + "-" + r.GetName()
	return baseLabels
}

func argsForReceiveIngestSts(rh *monitoringthanosiov1alpha1.ThanosReceiveHashring) []string {
	return []string{
		"receive",
		"--log.level=info",
		"--log.format=logfmt",
		fmt.Sprintf("--grpc-address=0.0.0.0:%d", DefaultReceiveIngestGRPCPort),
		fmt.Sprintf("--http-address=0.0.0.0:%d", DefaultReceiveIngestHTTPPort),
		fmt.Sprintf("--remote-write.address=0.0.0.0:%d", DefaultReceiveIngestRemoteWritePort),
		fmt.Sprintf("--tsdb.path=%s", dataVolumeMountPath),
		fmt.Sprintf("--tsdb.retention=%s", rh.Spec.Retention),
		`--label=replica="$(NAME)"`,
		`--label=receive="true"`,
		fmt.Sprintf("--objstore.config=$(%s)", objectStoreEnvVarName),
		fmt.Sprintf("--receive.local-endpoint=$(NAME).%s.$(NAMESPACE).svc.cluster.local:%d",
			rh.GetServiceName(), DefaultReceiveIngestGRPCPort),
		"--receive.grpc-compression=none",
	}
}

func getServicePorts() []corev1.ServicePort {
	return []corev1.ServicePort{
		{
			Name:       DefaultReceiveIngestGRPCPortName,
			Port:       DefaultReceiveIngestGRPCPort,
			TargetPort: intstr.FromInt32(DefaultReceiveIngestGRPCPort),
			Protocol:   "TCP",
		},
		{
			Name:       DefaultReceiveIngestHTTPPortName,
			Port:       DefaultReceiveIngestHTTPPort,
			TargetPort: intstr.FromInt32(DefaultReceiveIngestHTTPPort),
			Protocol:   "TCP",
		},
		{
			Name:       DefaultReceiveIngestRemoteWritePortName,
			Port:       DefaultReceiveIngestRemoteWritePort,
			TargetPort: intstr.FromInt32(DefaultReceiveIngestRemoteWritePort),
			Protocol:   "TCP",
		},
	}
}

// enqueueForHashringConfig enqueues requests for ThanosReceiveHashring when an EndpointSlice is owned by
// a headless Service which is owned by a ThanosReceiveHashring is touched.
func (r *ThanosReceiveHashringReconciler) enqueueForHashringConfig() handler.EventHandler {
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

// findObjectsForConfigMap returns a list of reconcile.Request for ThanosReceiveHashring instances that are
// associated with a ConfigMap.
func (r *ThanosReceiveHashringReconciler) findObjectsForConfigMap(ctx context.Context, configMap client.Object) []reconcile.Request {
	attachedHashrings := &monitoringthanosiov1alpha1.ThanosReceiveHashringList{}
	listOps := &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(configMapField, configMap.GetName()),
		Namespace:     configMap.GetNamespace(),
	}
	err := r.List(ctx, attachedHashrings, listOps)
	if err != nil {
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, len(attachedHashrings.Items))
	for i, item := range attachedHashrings.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      item.GetName(),
				Namespace: item.GetNamespace(),
			},
		}
	}
	return requests
}

func buildHashringConfigFrom(endpoints []string, hashring *monitoringthanosiov1alpha1.ThanosReceiveHashring) monitoringthanosiov1alpha1.HashringConfig {
	var convertedEndpoints []monitoringthanosiov1alpha1.Endpoint
	for _, ep := range endpoints {
		convertedEndpoints = append(convertedEndpoints, monitoringthanosiov1alpha1.Endpoint{Address: ep})
	}

	return monitoringthanosiov1alpha1.HashringConfig{
		Hashring:          hashring.GetName(),
		Tenants:           hashring.Spec.Tenants,
		TenantMatcherType: hashring.Spec.TenantMatcherType,
		Endpoints:         convertedEndpoints,
		Algorithm:         monitoringthanosiov1alpha1.AlgorithmKetama,
		ExternalLabels:    nil,
	}
}
