package gateway

import (
	"fmt"
	"github.com/philipgough/prom-auth-proxy/pkg/lbac"
	"github.com/philipgough/prom-auth-proxy/pkg/token_review"

	"github.com/philipgough/prom-auth-proxy/pkg/envoy"
	"github.com/thanos-community/thanos-operator/internal/pkg/manifests"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Name is the name of the Thanos Compact component.
	Name = "thanos-gateway"

	// ComponentName is the name of the Thanos Compact component.
	ComponentName = "thanos-gateway"

	MetricsReadPort     = envoy.ReadListenerPort
	MetricsReadPortName = "metrics-read"

	MetricsWritePort     = envoy.WriteListenerPort
	MetricsWritePortName = "metrics-write"
)

type Options struct {
	manifests.Options
	// Owner is the name of the owner of the object. This relates to the CustomResource or entity that created the object.
	// This value will be used to populate the OwnerLabel after it has been run through ValidateAndSanitizeResourceName.
	// This should be set to the name of the CustomResource that is creating the object and is a required field.
	Owner string
	// Labels is the labels for the object
	// Labels will be merged with the default labels for the component.
	// The builders should ensure that the default labels are set on the object.
	// The builders will overwrite the default labels if they are set in the Labels.
	Labels map[string]string
	// Namespace is the namespace for the object
	Namespace string
	// Replicas is the number of replicas for the object.
	// Specific build functions may override this value.
	Replicas int32
	// Annotations is the annotations for the object
	Annotations map[string]string
	Embedded    envoy.Options
}

// GetContainerImage for the Options
func (opts Options) GetContainerImage() string {
	return "docker.io/envoyproxy/envoy:distroless-dev"
}

func (opts Options) Build() []client.Object {
	var objs []client.Object
	selectorLabels := opts.GetSelectorLabels()
	objectMetaLabels := manifests.MergeLabels(opts.Labels, selectorLabels)

	objs = append(objs, manifests.BuildServiceAccount(opts.GetGeneratedResourceName(), opts.Namespace, selectorLabels, opts.Annotations))
	objs = append(objs, newConfigMap(opts, selectorLabels, objectMetaLabels))
	objs = append(objs, newDeployment(opts, selectorLabels, objectMetaLabels))
	objs = append(objs, newService(opts, selectorLabels, objectMetaLabels))

	return objs
}

func newConfigMap(opts Options, selectorLabels, objectMetaLabels map[string]string) *corev1.ConfigMap {
	contents := opts.Embedded.BuildOrDie()
	configMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        opts.GetGeneratedResourceName(),
			Namespace:   opts.Namespace,
			Labels:      objectMetaLabels,
			Annotations: opts.Annotations,
		},
		Data: map[string]string{
			"envoy.yaml": contents,
		},
	}
	return configMap
}

func newDeployment(opts Options, selectorLabels, objectMetaLabels map[string]string) *appsv1.Deployment {
	name := opts.GetGeneratedResourceName()
	podAffinity := corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{{
				Weight: 100,
				PodAffinityTerm: corev1.PodAffinityTerm{
					LabelSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{{
							Key:      manifests.NameLabel,
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{name},
						}},
					},
					Namespaces:  []string{opts.Namespace},
					TopologyKey: "kubernetes.io/hostname",
				},
			}},
		},
	}

	envoyContainer := corev1.Container{
		Image:           opts.GetContainerImage(),
		Name:            Name,
		ImagePullPolicy: corev1.PullIfNotPresent,
		// Ensure restrictive context for the envoyContainer
		// More info: https://kubernetes.io/docs/concepts/security/pod-security-standards/#restricted
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             ptr.To(true),
			AllowPrivilegeEscalation: ptr.To(false),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{
					"ALL",
				},
			},
		},
		Ports: []corev1.ContainerPort{
			{
				Name:          MetricsReadPortName,
				ContainerPort: MetricsReadPort,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          MetricsWritePortName,
				ContainerPort: MetricsWritePort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
		Args:                     []string{"-c", "/etc/envoy/envoy.yaml"},
		VolumeMounts: []corev1.VolumeMount{{
			Name:      "envoy-config",
			MountPath: "/etc/envoy",
		}},
	}
	if opts.Embedded.WriteOptions.MTLSConfig != nil {
		envoyContainer.VolumeMounts = append(envoyContainer.VolumeMounts, corev1.VolumeMount{
			Name:      "envoy-proxy-remote-write",
			MountPath: "/etc/envoy-proxy-remote-write",
		})
	}

	tokenReviewContainer := corev1.Container{
		Image:           "quay.io/philipgough/token-review:latest",
		Name:            "token-review",
		ImagePullPolicy: corev1.PullIfNotPresent,
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             ptr.To(false),
			AllowPrivilegeEscalation: ptr.To(false),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{
					"ALL",
				},
			},
		},
		Ports: []corev1.ContainerPort{
			{
				Name:          "grpc",
				ContainerPort: token_review.ServerDefaultPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
	}

	lbacContainer := corev1.Container{
		Image:           "quay.io/philipgough/lbac:latest",
		Name:            "lbac",
		ImagePullPolicy: corev1.PullIfNotPresent,
		SecurityContext: &corev1.SecurityContext{
			RunAsNonRoot:             ptr.To(false),
			AllowPrivilegeEscalation: ptr.To(false),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{
					"ALL",
				},
			},
		},
		Ports: []corev1.ContainerPort{
			{
				Name:          "grpc",
				ContainerPort: lbac.ServerDefaultPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		TerminationMessagePath:   corev1.TerminationMessagePathDefault,
	}

	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: appsv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   opts.Namespace,
			Labels:      objectMetaLabels,
			Annotations: opts.Annotations,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &opts.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: objectMetaLabels,
				},
				Spec: corev1.PodSpec{
					Affinity:        &podAffinity,
					SecurityContext: &corev1.PodSecurityContext{},
					Containers:      []corev1.Container{envoyContainer, tokenReviewContainer, lbacContainer},
					Volumes: []corev1.Volume{
						{
							Name: "envoy-config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: opts.GetGeneratedResourceName(),
									},
								},
							},
						},
						{
							Name: "envoy-proxy-remote-write",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: "envoy-proxy-remote-write",
								},
							},
						},
					},
					ServiceAccountName: name,
				},
			},
		},
	}
	return deployment
}

func newService(opts Options, selectorLabels, objectMetaLabels map[string]string) *corev1.Service {
	servicePorts := []corev1.ServicePort{
		{
			Name:       MetricsReadPortName,
			Port:       MetricsReadPort,
			TargetPort: intstr.FromInt32(MetricsReadPort),
		},
		{
			Name:       MetricsWritePortName,
			Port:       MetricsWritePort,
			TargetPort: intstr.FromInt32(MetricsWritePort),
		},
		{
			Name:       "admin",
			Port:       9901,
			TargetPort: intstr.FromInt32(9901),
		},
	}

	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        opts.GetGeneratedResourceName(),
			Namespace:   opts.Namespace,
			Labels:      objectMetaLabels,
			Annotations: opts.Annotations,
		},
		Spec: corev1.ServiceSpec{
			Selector: selectorLabels,
			Ports:    servicePorts,
		},
	}
	return svc
}

// GetRequiredLabels returns a map of labels that can be used to look up ThanosCompact resources.
func GetRequiredLabels() map[string]string {
	return map[string]string{
		manifests.NameLabel:      Name,
		manifests.ComponentLabel: ComponentName,
		manifests.PartOfLabel:    manifests.DefaultPartOfLabel,
		manifests.ManagedByLabel: manifests.DefaultManagedByLabel,
	}
}

// GetSelectorLabels returns a map of labels that can be used to look up ThanosGateway resources.
func (opts Options) GetSelectorLabels() map[string]string {
	labels := GetRequiredLabels()
	labels[manifests.InstanceLabel] = manifests.ValidateAndSanitizeNameToValidLabelValue(opts.GetGeneratedResourceName())
	labels[manifests.OwnerLabel] = manifests.ValidateAndSanitizeNameToValidLabelValue(opts.getOwner())
	return labels
}

func (opts Options) GetGeneratedResourceName() string {
	name := fmt.Sprintf("%s-%s", Name, opts.getOwner())
	return manifests.ValidateAndSanitizeResourceName(name)
}

func (opts Options) getOwner() string {
	return "test"
}
