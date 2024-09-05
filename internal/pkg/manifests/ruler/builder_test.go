package ruler

import (
	"testing"

	"github.com/thanos-community/thanos-operator/internal/pkg/manifests"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/utils/ptr"
)

const (
	someCustomLabelValue string = "xyz"
	someOtherLabelValue  string = "abc"
)

func TestBuildRuler(t *testing.T) {
	opts := RulerOptions{
		Options: manifests.Options{
			Name:      "test",
			Namespace: "ns",
			Image:     ptr.To("some-custom-image"),
			Labels: map[string]string{
				"some-custom-label":      someCustomLabelValue,
				"some-other-label":       someOtherLabelValue,
				"app.kubernetes.io/name": "expect-to-be-discarded",
			},
		}.ApplyDefaults(),
		Endpoints: []Endpoint{
			{
				ServiceName: "test-query",
				Namespace:   "ns",
				Port:        19101,
			},
		},
		RuleFiles: []corev1.ConfigMapKeySelector{
			{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "test-rules",
				},
				Key: "rules.yaml",
			},
		},
		ObjStoreSecret: corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: "test-secret",
			},
			Key: "thanos.yaml",
		},
		Retention:       "15d",
		AlertmanagerURL: "http://test-alertmanager.com:9093",
		ExternalLabels: map[string]string{
			"rule_replica": "0",
		},
	}

	expectSA := manifests.BuildServiceAccount(opts.Options)
	expectService := NewRulerService(opts)
	expectStatefulset := NewRulerStatefulSet(opts)

	objs := BuildRuler(opts)
	if len(objs) != 3 {
		t.Fatalf("expected 3 objects, got %d", len(objs))
	}

	if !equality.Semantic.DeepEqual(objs[0], expectSA) {
		t.Errorf("expected first object to be a service account, wanted \n%v\n got \n%v\n", expectSA, objs[0])
	}

	if !equality.Semantic.DeepEqual(objs[1], expectStatefulset) {
		t.Errorf("expected second object to be a statefuleset, wanted \n%v\n got \n%v\n", expectStatefulset, objs[2])
	}

	if !equality.Semantic.DeepEqual(objs[2], expectService) {
		t.Errorf("expected third object to be a service, wanted \n%v\n got \n%v\n", expectService, objs[1])
	}

	wantLabels := labelsForRulers(opts)
	wantLabels["some-custom-label"] = someCustomLabelValue
	wantLabels["some-other-label"] = someOtherLabelValue

	for _, obj := range objs {
		if !equality.Semantic.DeepEqual(obj.GetLabels(), wantLabels) {
			t.Errorf("expected object to have labels %v, got %v", wantLabels, obj.GetLabels())
		}
	}
}

func TestNewRulerStatefulSet(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts RulerOptions
	}{
		{
			name: "test ruler statefulset correctness",
			opts: RulerOptions{
				Options: manifests.Options{
					Name:      "test",
					Namespace: "ns",
					Image:     ptr.To("some-custom-image"),
					Labels: map[string]string{
						"some-custom-label":      someCustomLabelValue,
						"some-other-label":       someOtherLabelValue,
						"app.kubernetes.io/name": "expect-to-be-discarded",
					},
				}.ApplyDefaults(),
				Endpoints: []Endpoint{
					{
						ServiceName: "test-query",
						Namespace:   "ns",
						Port:        19101,
					},
				},
				RuleFiles: []corev1.ConfigMapKeySelector{
					{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-rules",
						},
						Key: "rules.yaml",
					},
				},
				ObjStoreSecret: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "test-secret",
					},
					Key: "thanos.yaml",
				},
				Retention:       "15d",
				AlertmanagerURL: "http://test-alertmanager.com:9093",
				ExternalLabels: map[string]string{
					"rule_replica": "0",
				},
			},
		},
		{
			name: "test additional volumemount",
			opts: RulerOptions{
				Options: manifests.Options{
					Name:      "test",
					Namespace: "ns",
					Image:     ptr.To("some-custom-image"),
					Labels: map[string]string{
						"some-custom-label":      someCustomLabelValue,
						"some-other-label":       someOtherLabelValue,
						"app.kubernetes.io/name": "expect-to-be-discarded",
					},
				}.ApplyDefaults(),
				Endpoints: []Endpoint{
					{
						ServiceName: "test-query",
						Namespace:   "ns",
						Port:        19101,
					},
				},
				RuleFiles: []corev1.ConfigMapKeySelector{
					{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-rules",
						},
						Key: "rules.yaml",
					},
				},
				ObjStoreSecret: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "test-secret",
					},
					Key: "thanos.yaml",
				},
				Retention:       "15d",
				AlertmanagerURL: "http://test-alertmanager.com:9093",
				ExternalLabels: map[string]string{
					"rule_replica": "0",
				},
				Additional: manifests.Additional{
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "some-rule",
							MountPath: "/some-rule",
						},
					},
				},
			},
		},
		{
			name: "test additional container",
			opts: RulerOptions{
				Options: manifests.Options{
					Name:      "test",
					Namespace: "ns",
					Image:     ptr.To("some-custom-image"),
					Labels: map[string]string{
						"some-custom-label":      someCustomLabelValue,
						"some-other-label":       someOtherLabelValue,
						"app.kubernetes.io/name": "expect-to-be-discarded",
					},
				}.ApplyDefaults(),
				Endpoints: []Endpoint{
					{
						ServiceName: "test-query",
						Namespace:   "ns",
						Port:        19101,
					},
				},
				RuleFiles: []corev1.ConfigMapKeySelector{
					{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "test-rules",
						},
						Key: "rules.yaml",
					},
				},
				ObjStoreSecret: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "test-secret",
					},
					Key: "thanos.yaml",
				},
				Retention:       "15d",
				AlertmanagerURL: "http://test-alertmanager.com:9093",
				ExternalLabels: map[string]string{
					"rule_replica": "0",
				},
				Additional: manifests.Additional{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "test-image:latest",
							Args:  []string{"--test-arg"},
							Env: []corev1.EnvVar{{
								Name:  "TEST_ENV",
								Value: "test",
							}},
						},
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.opts.Options = tc.opts.ApplyDefaults()
			ruler := NewRulerStatefulSet(tc.opts)
			if ruler.GetName() != tc.opts.Name {
				t.Errorf("expected ruler statefuleset name to be %s, got %s", tc.opts.Name, ruler.GetName())
			}
			if ruler.GetNamespace() != tc.opts.Namespace {
				t.Errorf("expected ruler statefulset namespace to be %s, got %s", tc.opts.Namespace, ruler.GetNamespace())
			}
			// ensure we inherit the labels from the Options struct and that the strict labels cannot be overridden
			if len(ruler.GetLabels()) != 7 {
				t.Errorf("expected ruler stateful to have 7 labels, got %d", len(ruler.GetLabels()))
			}
			// ensure custom labels are set
			if ruler.GetLabels()["some-custom-label"] != someCustomLabelValue {
				t.Errorf("expected ruler statefulset to have label 'some-custom-label' with value 'xyz', got %s", ruler.GetLabels()["some-custom-label"])
			}
			if ruler.GetLabels()["some-other-label"] != someOtherLabelValue {
				t.Errorf("expected ruler statefulset to have label 'some-other-label' with value 'abc', got %s", ruler.GetLabels()["some-other-label"])
			}
			// ensure default labels are set
			expect := labelsForRulers(tc.opts)
			for k, v := range expect {
				if ruler.GetLabels()[k] != v {
					t.Errorf("expected query deployment to have label %s with value %s, got %s", k, v, ruler.GetLabels()[k])
				}
			}

			if tc.name == "test additional container" && len(ruler.Spec.Template.Spec.Containers) != 2 {
				t.Errorf("expected ruler statefulset to have 2 containers, got %d", len(ruler.Spec.Template.Spec.Containers))
			}

			expectArgs := rulerArgs(tc.opts)
			var found bool
			for _, c := range ruler.Spec.Template.Spec.Containers {
				if c.Name == Name {
					found = true
					if c.Image != tc.opts.GetContainerImage() {
						t.Errorf("expected ruler statefulset to have image %s, got %s", tc.opts.GetContainerImage(), c.Image)
					}
					if len(c.Args) != len(expectArgs) {
						t.Errorf("expected ruler statefulset to have %d args, got %d", len(expectArgs), len(c.Args))
					}
					for i, arg := range c.Args {
						if arg != expectArgs[i] {
							t.Errorf("expected ruler statfulset to have arg %s, got %s", expectArgs[i], arg)
						}
					}

					if tc.name == "test additional volumemount" {
						if len(c.VolumeMounts) != 3 {
							t.Errorf("expected ruler statfulset to have 3 volumemount, got %d", len(c.VolumeMounts))
						}

						if c.VolumeMounts[2].Name != "some-rule" {
							t.Errorf("expected ruler statfulset to have volumemount with name 'some-rule', got %s", c.VolumeMounts[1].Name)
						}

						if c.VolumeMounts[2].MountPath != "/some-rule" {
							t.Errorf("expected ruler statfulset to have volumemount with mount path '/some-rule', got %s", c.VolumeMounts[1].MountPath)
						}
					}
				}
			}
			if !found {
				t.Errorf("expected ruler statfulset to have container named %s", Name)
			}
		})
	}
}

func TestNewRulerService(t *testing.T) {
	opts := RulerOptions{
		Options: manifests.Options{
			Name:      "test",
			Namespace: "ns",
			Image:     ptr.To("some-custom-image"),
			Labels: map[string]string{
				"some-custom-label":      someCustomLabelValue,
				"some-other-label":       someOtherLabelValue,
				"app.kubernetes.io/name": "expect-to-be-discarded",
			},
		}.ApplyDefaults(),
		Endpoints: []Endpoint{
			{
				ServiceName: "test-query",
				Namespace:   "ns",
				Port:        19101,
			},
		},
		RuleFiles: []corev1.ConfigMapKeySelector{
			{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "test-rules",
				},
				Key: "rules.yaml",
			},
		},
		ObjStoreSecret: corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: "test-secret",
			},
			Key: "thanos.yaml",
		},
		Retention:       "15d",
		AlertmanagerURL: "http://test-alertmanager.com:9093",
		ExternalLabels: map[string]string{
			"rule_replica": "0",
		},
	}

	for _, tc := range []struct {
		name string
		opts RulerOptions
	}{
		{
			name: "test ruler service correctness",
			opts: opts,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.opts.Options = tc.opts.ApplyDefaults()
			ruler := NewRulerService(tc.opts)
			if ruler.GetName() != tc.opts.Name {
				t.Errorf("expected ruler service name to be %s, got %s", tc.opts.Name, ruler.GetName())
			}
			if ruler.GetNamespace() != tc.opts.Namespace {
				t.Errorf("expected ruler service namespace to be %s, got %s", tc.opts.Namespace, ruler.GetNamespace())
			}
			// ensure we inherit the labels from the Options struct and that the strict labels cannot be overridden
			if len(ruler.GetLabels()) != 7 {
				t.Errorf("expected ruler service to have 7 labels, got %d", len(ruler.GetLabels()))
			}
			// ensure custom labels are set
			if ruler.GetLabels()["some-custom-label"] != someCustomLabelValue {
				t.Errorf("expected ruler service to have label 'some-custom-label' with value 'xyz', got %s", ruler.GetLabels()["some-custom-label"])
			}
			if ruler.GetLabels()["some-other-label"] != someOtherLabelValue {
				t.Errorf("expected ruler service to have label 'some-other-label' with value 'abc', got %s", ruler.GetLabels()["some-other-label"])
			}
			// ensure default labels are set
			expect := labelsForRulers(tc.opts)
			for k, v := range expect {
				if ruler.GetLabels()[k] != v {
					t.Errorf("expected ruler service to have label %s with value %s, got %s", k, v, ruler.GetLabels()[k])
				}
			}

			if ruler.Spec.ClusterIP != "None" {
				t.Errorf("expected ruler service to have ClusterIP 'None', got %s", ruler.Spec.ClusterIP)
			}
		})
	}
}