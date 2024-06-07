package receive

import (
	"testing"

	"github.com/thanos-community/thanos-operator/internal/pkg/manifests"

	"k8s.io/utils/ptr"
)

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
