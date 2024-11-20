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

	"github.com/go-logr/logr"
	monitoringthanosiov1alpha1 "github.com/thanos-community/thanos-operator/api/v1alpha1"
	"github.com/thanos-community/thanos-operator/internal/pkg/handlers"
	"github.com/thanos-community/thanos-operator/internal/pkg/manifests"
	manifestcompact "github.com/thanos-community/thanos-operator/internal/pkg/manifests/compact"
	manifestgateway "github.com/thanos-community/thanos-operator/internal/pkg/manifests/gateway"
	controllermetrics "github.com/thanos-community/thanos-operator/internal/pkg/metrics"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ThanosGatewayReconciler reconciles a ThanosGateway object
type ThanosGatewayReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	logger   logr.Logger
	metrics  controllermetrics.ThanosGatewayMetrics
	recorder record.EventRecorder

	handler *handlers.Handler
}

// NewThanosGatewayReconciler returns a reconciler for ThanosGateway resources.
func NewThanosGatewayReconciler(conf Config, client client.Client, scheme *runtime.Scheme) *ThanosGatewayReconciler {
	handler := handlers.NewHandler(client, scheme, conf.InstrumentationConfig.Logger)
	featureGates := conf.FeatureGate.ToGVK()
	if len(featureGates) > 0 {
		handler.SetFeatureGates(featureGates)
	}

	return &ThanosGatewayReconciler{
		Client:   client,
		Scheme:   scheme,
		logger:   conf.InstrumentationConfig.Logger,
		metrics:  controllermetrics.NewThanosGatewayMetrics(conf.InstrumentationConfig.MetricsRegistry, conf.InstrumentationConfig.BaseMetrics),
		recorder: conf.InstrumentationConfig.EventRecorder,
		handler:  handler,
	}
}

//+kubebuilder:rbac:groups=monitoring.thanos.io,resources=thanosgateways,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=monitoring.thanos.io,resources=thanosgateways/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=monitoring.thanos.io,resources=thanosgateways/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ThanosGateway object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.3/pkg/reconcile
func (r *ThanosGatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	gateway := &monitoringthanosiov1alpha1.ThanosGateway{}
	err := r.Get(ctx, req.NamespacedName, gateway)
	if err != nil {
		if apierrors.IsNotFound(err) {
			r.logger.Info("thanos compact resource not found. ignoring since object may be deleted")
			return ctrl.Result{}, nil
		}
		r.logger.Error(err, "failed to get ThanosGateway")
		r.metrics.ReconciliationsFailedTotal.WithLabelValues(manifestgateway.Name).Inc()
		r.recorder.Event(gateway, corev1.EventTypeWarning, "GetFailed", "Failed to get ThanosGateway resource")
		return ctrl.Result{}, err
	}

	if gateway.Spec.Paused != nil && *gateway.Spec.Paused {
		r.logger.Info("reconciliation is paused for ThanosGateway resource")
		r.recorder.Event(gateway, corev1.EventTypeNormal, "Paused", "Reconciliation is paused for ThanosGateway resource")
		return ctrl.Result{}, nil
	}

	err = r.syncResources(ctx, *gateway)
	if err != nil {
		r.logger.Error(err, "failed to sync resources")
		r.metrics.ReconciliationsFailedTotal.WithLabelValues(manifestgateway.Name).Inc()
		r.recorder.Event(gateway, corev1.EventTypeWarning, "SyncFailed", fmt.Sprintf("Failed to sync resources: %v", err))
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ThanosGatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&monitoringthanosiov1alpha1.ThanosGateway{}).
		Complete(r)
}

func (r *ThanosGatewayReconciler) syncResources(ctx context.Context, gateway monitoringthanosiov1alpha1.ThanosGateway) error {
	var errCount int
	opt := r.specToOptions(gateway)
	errCount += r.handler.CreateOrUpdate(ctx, gateway.GetNamespace(), &gateway, opt.Build())

	if errCount > 0 {
		r.metrics.ClientErrorsTotal.WithLabelValues(manifestcompact.Name).Add(float64(errCount))
		return fmt.Errorf("failed to create or update %d resources for gateway", errCount)
	}

	return nil
}

func (r *ThanosGatewayReconciler) specToOptions(gateway monitoringthanosiov1alpha1.ThanosGateway) manifests.Buildable {
	return gatewayV1Alpha1ToOptions(gateway)
}
