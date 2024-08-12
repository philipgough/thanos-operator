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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	monitoringthanosiov1alpha1 "github.com/thanos-community/thanos-operator/api/v1alpha1"
)

var _ = Describe("ThanosGateway Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		//BeforeEach(func() {
		//	By("creating the custom resource for the Kind ThanosGateway")
		//	err := k8sClient.Get(ctx, typeNamespacedName, thanosgateway)
		//	if err != nil && errors.IsNotFound(err) {
		//		resource := &monitoringthanosiov1alpha1.ThanosGateway{
		//			ObjectMeta: metav1.ObjectMeta{
		//				Name:      resourceName,
		//				Namespace: "default",
		//			},
		//			// TODO(user): Specify other spec details if needed.
		//		}
		//		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		//	}
		//})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &monitoringthanosiov1alpha1.ThanosGateway{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance ThanosGateway")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		//It("should successfully reconcile the resource", func() {
		//	By("Reconciling the created resource")
		//	controllerReconciler := &ThanosGatewayReconciler{
		//		Client: k8sClient,
		//		Scheme: k8sClient.Scheme(),
		//	}
		//
		//	_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
		//		NamespacedName: typeNamespacedName,
		//	})
		//	Expect(err).NotTo(HaveOccurred())
		//	// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
		//	// Example: If you expect a certain status condition after reconciliation, verify it here.
		//})

		It("should error when the spec is invalid due to CEL rules", func() {
			resource := &monitoringthanosiov1alpha1.ThanosGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: monitoringthanosiov1alpha1.ThanosGatewaySpec{
					AuthProvidersConfig: &monitoringthanosiov1alpha1.AuthProvidersConfig{
						JWTProviders: []monitoringthanosiov1alpha1.JWTProviderConfig{
							{
								Name: "test-jwt-1",
							},
							{
								Name: "test-jwt-2",
							},
						},
						MTLSConfigs: []monitoringthanosiov1alpha1.MTLSConfig{
							{
								Name: "test-mtls-1",
							},
							{
								Name: "test-mtls-2",
							},
						},
					},
					Tenants: []monitoringthanosiov1alpha1.Tenant{
						{
							Name: "test-tenant-1",
							MetricsReadConfig: &monitoringthanosiov1alpha1.AuthConfig{
								TokenAuthConfig: map[string][]monitoringthanosiov1alpha1.TokenRBAC{
									"test-token": {
										{
											Matcher: monitoringthanosiov1alpha1.Matcher{
												IsRegex: false,
												Value:   "&*",
											},
										},
									},
								},
							},
							MetricsWriteConfig: &monitoringthanosiov1alpha1.AuthConfig{
								CertAutConfig: map[string][]monitoringthanosiov1alpha1.CertRBAC{
									"test-cert": {
										{
											Matcher: monitoringthanosiov1alpha1.Matcher{
												IsRegex: false,
												Value:   "&*",
											},
										},
									},
								},
							},
						},
					},
				},
			}
			By("failing when the named jwt provider for metrics read does not exist", func() {
				err := k8sClient.Create(context.Background(), resource)
				Expect(err).To(Not(BeNil()))
				Expect(err.Error()).To(ContainSubstring("jwt provider must exist for read"))
			})

			By("failing when the named cert provider for metrics write does not exist", func() {
				resource.Spec.Tenants[0].MetricsReadConfig = &monitoringthanosiov1alpha1.AuthConfig{
					TokenAuthConfig: map[string][]monitoringthanosiov1alpha1.TokenRBAC{
						"test-token": {
							{
								Matcher: monitoringthanosiov1alpha1.Matcher{
									IsRegex: false,
									Value:   "&*", // TODO(user): Modify as needed
								}
							}
						}
					}
				}
				err := k8sClient.Create(context.Background(), resource)
				Expect(err).To(Not(BeNil()))
				Expect(err.Error()).To(ContainSubstring("cert provider must exist for write"))
			})

			//By("doing this", func() {
			//	resource.Spec.ResourcePermissions = map[monitoringthanosiov1alpha1.Resource]monitoringthanosiov1alpha1.Permissions{
			//		monitoringthanosiov1alpha1.MetricsResource: []monitoringthanosiov1alpha1.Permission{"read", "write"},
			//	}
			//	resource.Spec.PathMatchers = map[monitoringthanosiov1alpha1.Resource]map[monitoringthanosiov1alpha1.Permission]monitoringthanosiov1alpha1.PathMatchers{
			//		monitoringthanosiov1alpha1.MetricsResource: {
			//			"watcher": monitoringthanosiov1alpha1.PathMatchers{},
			//		},
			//	}
			//	err := k8sClient.Create(context.Background(), resource)
			//	Expect(err).To(Not(BeNil()))
			//	Expect(err.Error()).To(ContainSubstring("patchMatchers key must exist in resourcePermissions"))
			//})

			By("ensuring hashring name is a singleton across the list", func() {
				//resource.Spec.Ingester.Hashrings[0].Replicas = 3
				//Expect(k8sClient.Create(context.Background(), resource)).Should(Succeed())
				//resource := &monitoringthanosiov1alpha1.ThanosReceive{}
				//err := k8sClient.Get(ctx, typeNamespacedName, resource)
				//Expect(err).NotTo(HaveOccurred())
				//resource.Spec.Ingester.Hashrings = append(
				//	resource.Spec.Ingester.Hashrings,
				//	monitoringthanosiov1alpha1.IngestorHashringSpec{
				//		Name:        "test-hashring",
				//		Labels:      map[string]string{"test": "my-ingester-test"},
				//		StorageSize: "100Mi",
				//		Tenants:     []string{"test-tenant"},
				//		Replicas:    5,
				//	},
				//)
				//Expect(k8sClient.Update(context.Background(), resource)).ShouldNot(Succeed())
				//resource.Spec.Ingester.Hashrings[1].Name = "test-hashring-2"
				//Expect(k8sClient.Update(context.Background(), resource)).Should(Succeed())
			})
		})
	})
})
