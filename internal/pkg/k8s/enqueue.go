package k8s

import (
	"context"

	v1 "k8s.io/api/discovery/v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// EnqueueForEndpointSlice returns a handler to enqueue requests for EndpointSlice objects.
// And endpoint slice is owned by a Service. If the Service is owned by another controller
// this function will enqueue a request for that resource.
func EnqueueForEndpointSlice(c client.Client, matchAgainstKind string) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		if obj.GetOwnerReferences() == nil {
			return nil
		}

		if len(obj.GetOwnerReferences()) != 1 {
			return nil
		}

		owner := obj.GetOwnerReferences()[0]

		if owner.Kind != "Service" {
			return nil
		}

		if obj.GetLabels()[v1.LabelServiceName] != owner.Name {
			return nil
		}

		svc := &corev1.Service{}
		if err := c.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: owner.Name}, svc); err != nil {
			return nil
		}

		if len(svc.GetOwnerReferences()) != 1 || svc.GetOwnerReferences()[0].Kind != matchAgainstKind {
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
