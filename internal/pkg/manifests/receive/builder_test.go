package receive

import (
	"testing"

	"github.com/go-logr/logr/testr"

	"github.com/thanos-community/thanos-operator/internal/pkg/manifests"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestIngesterNameFromParent(t *testing.T) {
	for _, tc := range []struct {
		name   string
		parent string
		child  string
		expect string
	}{
		{
			name:   "test inherit from parent when valid",
			parent: "some-allowed-value",
			child:  "my-resource",
			expect: "some-allowed-value-my-resource",
		},
		{
			name:   "test inherit from parent when invalid",
			parent: "some-disallowed-value-because-the value-is-just-way-too-long-to-be-supported-by-label-constraints-which-are-required-for-matching-ingesters-and-even-though-this-is-unlikely-to-happen-in-practice-we-should-still-handle-it-because-its-possible",
			child:  "my-resource",
			expect: "my-resource",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IngesterNameFromParent(tc.parent, tc.child); got != tc.expect {
				t.Errorf("expected ingester name to be %s, got %s", tc.expect, got)
			}
		})
	}

}

func TestNewIngestorStatefulSet(t *testing.T) {
	opts := IngesterOptions{
		Options: manifests.Options{
			Name:      "test",
			Namespace: "ns",
			Image:     ptr.To("some-custom-image"),
			Labels: map[string]string{
				"some-custom-label":      "xyz",
				"some-other-label":       "abc",
				"app.kubernetes.io/name": "expect-to-be-discarded",
			},
		},
	}

	for _, tc := range []struct {
		name string
		opts IngesterOptions
	}{
		{
			name: "test ingester statefulset correctness",
			opts: opts,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.opts.Options = tc.opts.ApplyDefaults()
			ingester := NewIngestorStatefulSet(tc.opts)
			if ingester.GetName() != tc.opts.Name {
				t.Errorf("expected ingester statefulset name to be %s, got %s", tc.opts.Name, ingester.GetName())
			}
			if ingester.GetNamespace() != tc.opts.Namespace {
				t.Errorf("expected ingester statefulset namespace to be %s, got %s", tc.opts.Namespace, ingester.GetNamespace())
			}
			// ensure we inherit the labels from the Options struct and that the strict labels cannot be overridden
			if len(ingester.GetLabels()) != 7 {
				t.Errorf("expected ingester statefulset to have 7 labels, got %d", len(ingester.GetLabels()))
			}
			// ensure custom labels are set
			if ingester.GetLabels()["some-custom-label"] != "xyz" {
				t.Errorf("expected ingester statefulset to have label 'some-custom-label' with value 'xyz', got %s", ingester.GetLabels()["some-custom-label"])
			}
			if ingester.GetLabels()["some-other-label"] != "abc" {
				t.Errorf("expected ingester statefulset to have label 'some-other-label' with value 'abc', got %s", ingester.GetLabels()["some-other-label"])
			}
			// ensure default labels are set
			expect := labelsForIngestor(tc.opts)
			for k, v := range expect {
				if ingester.GetLabels()[k] != v {
					t.Errorf("expected ingester statefulset to have label %s with value %s, got %s", k, v, ingester.GetLabels()[k])
				}
			}

			expectArgs := ingestorArgsFrom(opts)
			var found bool
			for _, c := range ingester.Spec.Template.Spec.Containers {
				if c.Name == IngestComponentName {
					found = true
					if c.Image != tc.opts.GetContainerImage() {
						t.Errorf("expected ingester statefulset to have image %s, got %s", tc.opts.GetContainerImage(), c.Image)
					}
					if len(c.Args) != len(expectArgs) {
						t.Errorf("expected ingester statefulset to have %d args, got %d", len(expectArgs), len(c.Args))
					}
					for i, arg := range c.Args {
						if arg != expectArgs[i] {
							t.Errorf("expected ingester statefulset to have arg %s, got %s", expectArgs[i], arg)
						}
					}
				}
			}
			if !found {
				t.Errorf("expected ingester statefulset to have container named %s", IngestComponentName)
			}
		})
	}
}

func TestBuildHashrings(t *testing.T) {
	logger := testr.New(t)
	baseOptions := manifests.Options{
		Name:      "test",
		Namespace: "test",
		Replicas:  3,
	}

	for _, tc := range []struct {
		name        string
		passedState []HashringConfig
		opts        func() HashringOptions
		expect      client.Object
	}{
		{
			name:        "test result when no state is passed",
			passedState: nil,
			opts: func() HashringOptions {
				return HashringOptions{
					Options: baseOptions,
				}
			},
			expect: &corev1.ConfigMap{
				TypeMeta: v1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: "v1",
				},
				ObjectMeta: v1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
					Labels:    labelsForRouter(baseOptions),
				},
				Data: map[string]string{
					HashringConfigKey: "{}",
				},
			},
		},
		{
			name:        "test result when no previous state is passed but we have new state with missing owner reference",
			passedState: nil,
			opts: func() HashringOptions {
				return HashringOptions{
					Options: baseOptions,
					HashringSettings: map[string]HashringMeta{
						"test": {
							Replicas: 2,
							AssociatedEndpointSlices: discoveryv1.EndpointSliceList{
								Items: []discoveryv1.EndpointSlice{
									{
										Endpoints: []discoveryv1.Endpoint{
											{
												Addresses: []string{"a"},
												Conditions: discoveryv1.EndpointConditions{
													Ready: ptr.To(false),
												},
											},
											{
												Addresses: []string{"b"},
												Conditions: discoveryv1.EndpointConditions{
													Ready: ptr.To(false),
												},
											},
										},
									},
								},
							},
						},
					},
				}
			},
			expect: &corev1.ConfigMap{
				TypeMeta: v1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: "v1",
				},
				ObjectMeta: v1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
					Labels:    labelsForRouter(baseOptions),
				},
				Data: map[string]string{
					HashringConfigKey: "{}",
				},
			},
		},
		{
			name:        "test result when no previous state is passed but we have new state with mismatched owner reference",
			passedState: nil,
			opts: func() HashringOptions {
				return HashringOptions{
					Options: baseOptions,
					HashringSettings: map[string]HashringMeta{
						"test": {
							Replicas: 2,
							AssociatedEndpointSlices: discoveryv1.EndpointSliceList{
								Items: []discoveryv1.EndpointSlice{
									{
										ObjectMeta: v1.ObjectMeta{
											OwnerReferences: []v1.OwnerReference{
												{
													Name: "some-other-resource",
													Kind: "Service",
												},
											},
										},
										Endpoints: []discoveryv1.Endpoint{
											{
												Addresses: []string{"a"},
												Hostname:  ptr.To("a"),
												Conditions: discoveryv1.EndpointConditions{
													Ready: ptr.To(false),
												},
											},
											{
												Addresses: []string{"b"},
												Hostname:  ptr.To("b"),
												Conditions: discoveryv1.EndpointConditions{
													Ready: ptr.To(false),
												},
											},
										},
									},
								},
							},
						},
					},
				}
			},
			expect: &corev1.ConfigMap{
				TypeMeta: v1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: "v1",
				},
				ObjectMeta: v1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
					Labels:    labelsForRouter(baseOptions),
				},
				Data: map[string]string{
					HashringConfigKey: "{}",
				},
			},
		},
		{
			name:        "test result when no previous state is passed but we have new state which is not ready",
			passedState: nil,
			opts: func() HashringOptions {
				return HashringOptions{
					Options: baseOptions,
					HashringSettings: map[string]HashringMeta{
						"test": {
							Replicas: 2,
							AssociatedEndpointSlices: discoveryv1.EndpointSliceList{
								Items: []discoveryv1.EndpointSlice{
									{
										ObjectMeta: v1.ObjectMeta{
											OwnerReferences: []v1.OwnerReference{
												{
													Name: "test",
													Kind: "Service",
												},
											},
										},
										Endpoints: []discoveryv1.Endpoint{
											{
												Addresses: []string{"a"},
												Hostname:  ptr.To("a"),
												Conditions: discoveryv1.EndpointConditions{
													Ready: ptr.To(false),
												},
											},
											{
												Addresses: []string{"b"},
												Hostname:  ptr.To("b"),
												Conditions: discoveryv1.EndpointConditions{
													Ready: ptr.To(false),
												},
											},
										},
									},
								},
							},
						},
					},
				}
			},
			expect: &corev1.ConfigMap{
				TypeMeta: v1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: "v1",
				},
				ObjectMeta: v1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
					Labels:    labelsForRouter(baseOptions),
				},
				Data: map[string]string{
					HashringConfigKey: "{}",
				},
			},
		},
		{
			name:        "test result when no previous state is passed but we have new state which is ready",
			passedState: nil,
			opts: func() HashringOptions {
				return HashringOptions{
					Options: baseOptions,
					HashringSettings: map[string]HashringMeta{
						"test": {
							Replicas: 2,
							AssociatedEndpointSlices: discoveryv1.EndpointSliceList{
								Items: []discoveryv1.EndpointSlice{
									{
										ObjectMeta: v1.ObjectMeta{
											OwnerReferences: []v1.OwnerReference{
												{
													Name: "test",
													Kind: "Service",
												},
											},
										},
										Endpoints: []discoveryv1.Endpoint{
											{
												Addresses: []string{"a"},
												Hostname:  ptr.To("a"),
												Conditions: discoveryv1.EndpointConditions{
													Ready: ptr.To(true),
												},
											},
											{
												Addresses: []string{"b"},
												Hostname:  ptr.To("b"),
												Conditions: discoveryv1.EndpointConditions{
													Ready: ptr.To(true),
												},
											},
										},
									},
								},
							},
						},
					},
				}
			},
			expect: &corev1.ConfigMap{
				TypeMeta: v1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: "v1",
				},
				ObjectMeta: v1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
					Labels:    labelsForRouter(baseOptions),
				},
				Data: map[string]string{
					HashringConfigKey: `[
    {
        "endpoints": [
            {
                "address": "a.test:19291",
                "az": ""
            },
            {
                "address": "b.test:19291",
                "az": ""
            }
        ]
    }
]`,
				},
			},
		},
		{
			name:        "test result when tenants are set",
			passedState: nil,
			opts: func() HashringOptions {
				return HashringOptions{
					Options: baseOptions,
					HashringSettings: map[string]HashringMeta{
						"test": {
							Replicas: 2,
							Tenants:  []string{"foobar"},
							AssociatedEndpointSlices: discoveryv1.EndpointSliceList{
								Items: []discoveryv1.EndpointSlice{
									{
										ObjectMeta: v1.ObjectMeta{
											OwnerReferences: []v1.OwnerReference{
												{
													Name: "test",
													Kind: "Service",
												},
											},
										},
										Endpoints: []discoveryv1.Endpoint{
											{
												Addresses: []string{"a"},
												Hostname:  ptr.To("a"),
												Conditions: discoveryv1.EndpointConditions{
													Ready: ptr.To(true),
												},
											},
											{
												Addresses: []string{"b"},
												Hostname:  ptr.To("b"),
												Conditions: discoveryv1.EndpointConditions{
													Ready: ptr.To(true),
												},
											},
										},
									},
								},
							},
						},
					},
				}
			},
			expect: &corev1.ConfigMap{
				TypeMeta: v1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: "v1",
				},
				ObjectMeta: v1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
					Labels:    labelsForRouter(baseOptions),
				},
				Data: map[string]string{
					HashringConfigKey: `[
    {
        "tenants": [
            "foobar"
        ],
        "endpoints": [
            {
                "address": "a.test:19291",
                "az": ""
            },
            {
                "address": "b.test:19291",
                "az": ""
            }
        ]
    }
]`,
				},
			},
		},
		{
			name:        "test result with multiple hashrings where tenants are set",
			passedState: nil,
			opts: func() HashringOptions {
				return HashringOptions{
					Options: baseOptions,
					HashringSettings: map[string]HashringMeta{
						"a": {
							Replicas: 2,
							Tenants:  []string{"foobar"},
							AssociatedEndpointSlices: discoveryv1.EndpointSliceList{
								Items: []discoveryv1.EndpointSlice{
									{
										ObjectMeta: v1.ObjectMeta{
											OwnerReferences: []v1.OwnerReference{
												{
													Name: "a",
													Kind: "Service",
												},
											},
										},
										Endpoints: []discoveryv1.Endpoint{
											{
												Addresses: []string{"a"},
												Hostname:  ptr.To("a"),
												Conditions: discoveryv1.EndpointConditions{
													Ready: ptr.To(true),
												},
											},
											{
												Addresses: []string{"a1"},
												Hostname:  ptr.To("a1"),
												Conditions: discoveryv1.EndpointConditions{
													Ready: ptr.To(true),
												},
											},
										},
									},
								},
							},
						},
						"b": {
							Replicas:          2,
							TenantMatcherType: TenantMatcherGlob,
							Tenants:           []string{"baz*"},
							AssociatedEndpointSlices: discoveryv1.EndpointSliceList{
								Items: []discoveryv1.EndpointSlice{
									{
										ObjectMeta: v1.ObjectMeta{
											OwnerReferences: []v1.OwnerReference{
												{
													Name: "b",
													Kind: "Service",
												},
											},
										},
										Endpoints: []discoveryv1.Endpoint{
											{
												Addresses: []string{"b"},
												Hostname:  ptr.To("b"),
												Conditions: discoveryv1.EndpointConditions{
													Ready: ptr.To(true),
												},
											},
											{
												Addresses: []string{"b1"},
												Hostname:  ptr.To("b1"),
												Conditions: discoveryv1.EndpointConditions{
													Ready: ptr.To(true),
												},
											},
										},
									},
								},
							},
						},
					},
				}
			},
			expect: &corev1.ConfigMap{
				TypeMeta: v1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: "v1",
				},
				ObjectMeta: v1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
					Labels:    labelsForRouter(baseOptions),
				},
				Data: map[string]string{
					HashringConfigKey: `[
    {
        "tenants": [
            "foobar"
        ],
        "endpoints": [
            {
                "address": "a.a:19291",
                "az": ""
            },
            {
                "address": "a1.a:19291",
                "az": ""
            }
        ]
    },
    {
        "tenants": [
            "baz*"
        ],
        "tenant_matcher_type": "glob",
        "endpoints": [
            {
                "address": "b.b:19291",
                "az": ""
            },
            {
                "address": "b1.b:19291",
                "az": ""
            }
        ]
    }
]`,
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.opts()
			got, _ := BuildHashrings(logger, tc.passedState, opts)
			if got == nil {
				t.Errorf("expected BuildHashrings to return a ConfigMap, got nil")
			}

			if !equality.Semantic.DeepEqual(got, tc.expect) {
				t.Errorf("expected BuildHashrings to return a ConfigMap with data \n%v, \ngot \n%v", tc.expect, got)
			}
		})
	}
}
