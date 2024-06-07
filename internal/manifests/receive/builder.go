package receive

import (
	"fmt"
	"github.com/thanos-community/thanos-operator/internal/manifests"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

const (
	Name = "thanos-receive"

	RouterComponentName = "thanos-receive-router"
	RouterHTTPPortName  = "http"
	RouterHTTPPort      = 10902

	IngestComponentName       = "thanos-receive-ingestor"
	IngestGRPCPortName        = "grpc"
	IngestGRPCPort            = 10901
	IngestHTTPPortName        = "http"
	IngestHTTPPort            = 10902
	IngestRemoteWritePortName = "remote-write"
	IngestRemoteWritePort     = 19291
)

// IngesterOptions for Thanos Receive components
type IngesterOptions struct {
	manifests.Options
	RouterReplicas                int32
	IngestorReplicas              int32
	IngestorStorageSize           resource.Quantity
	IngestorObjStoreConfigMapName string
	IngestorObjStoreConfigMapKey  string
	IngestorRetention             string
}

// RouterOptions for Thanos Receive router
type RouterOptions struct {
	manifests.Options
	RouterReplicas                int32
	IngestorReplicas              int32
	IngestorStorageSize           resource.Quantity
	IngestorObjStoreConfigMapName string
	IngestorObjStoreConfigMapKey  string
	IngestorRetention             string
}

// HashringOptions for Thanos Receive hashring
type HashringOptions struct {
	manifests.Options
	Replicas              int32
	StorageSize           resource.Quantity
	ObjStoreConfigMapName string
	ObjStoreConfigMapKey  string
	Retention             string
}

// BuildIngesters  builds the ingesters for Thanos Receive
func BuildIngesters(opts []IngesterOptions) []metav1.Object {
	var objs []metav1.Object
	for _, opt := range opts {
		objs = append(objs, NewIngestorStatefulSet(opt))
	}
	return nil
}

func BuildIngester(opts IngesterOptions) []metav1.Object {
	var objs []metav1.Object
	objs = append(objs, manifests.BuildServiceAccount(opts.Options))
	objs = append(objs, NewIngestorStatefulSet(opts))
	objs = append(objs, NewIngestorService(opts))

}

func NewRouterDeployment(opts IngesterOptions) *appsv1.Deployment {
	defaultLabels := labelsForRouter(opts)
	aggregatedLabels := labels(opts, defaultLabels)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      opts.Name,
			Namespace: opts.Namespace,
			Labels:    aggregatedLabels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(opts.RouterReplicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: defaultLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:      opts.Name,
					Namespace: opts.Namespace,
					Labels:    aggregatedLabels,
				},
				Spec: corev1.PodSpec{
					Volumes: nil,
					Containers: []corev1.Container{
						{
							Image:           opts.Image,
							Name:            RouterComponentName,
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
										Port: intstr.FromInt32(RouterHTTPPort),
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
										Port: intstr.FromInt32(RouterHTTPPort),
									},
								},
								InitialDelaySeconds: 60,
								TimeoutSeconds:      1,
								PeriodSeconds:       30,
								SuccessThreshold:    1,
								FailureThreshold:    8,
							},
							Env: []corev1.EnvVar{},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      dataVolumeName,
									MountPath: dataVolumeMountPath,
								},
							},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: RouterHTTPPort,
									Name:          RouterHTTPPortName,
								},
							},
							TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
							TerminationMessagePath:   corev1.TerminationMessagePathDefault,
							Args:                     ingestorArgsFrom(opts),
						},
					},
					ServiceAccountName:           opts.Name,
					AutomountServiceAccountToken: ptr.To(true),
					SecurityContext:              nil,
					ImagePullSecrets:             nil,
					Affinity:                     nil,
					TopologySpreadConstraints:    nil,
				},
			},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxUnavailable: ptr.To(intstr.FromInt32(1)),
					MaxSurge:       ptr.To(intstr.FromInt32(0)),
				},
			},
			RevisionHistoryLimit: ptr.To(int32(10)),
			Paused:               false,
		},
	}
	return deployment
}

const (
	ingestObjectStoreEnvVarName = "OBJSTORE_CONFIG"

	dataVolumeName      = "data"
	dataVolumeMountPath = "var/thanos/receive"
)

func NewIngestorStatefulSet(opts IngesterOptions) *appsv1.StatefulSet {
	defaultLabels := labelsForIngestor(opts)
	aggregatedLabels := labels(opts, defaultLabels)

	vc := []corev1.PersistentVolumeClaim{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dataVolumeName,
				Namespace: opts.Namespace,
				Labels:    aggregatedLabels,
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: opts.IngestorStorageSize,
					},
				},
			},
		},
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      opts.Name,
			Namespace: opts.Namespace,
			Labels:    aggregatedLabels,
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: opts.Name,
			Replicas:    ptr.To(opts.IngestorReplicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: defaultLabels,
			},
			VolumeClaimTemplates: vc,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: aggregatedLabels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image:           opts.Image,
							Name:            IngestComponentName,
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
										Port: intstr.FromInt32(IngestHTTPPort),
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
										Port: intstr.FromInt32(IngestHTTPPort),
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
									Name: ingestObjectStoreEnvVarName,
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: opts.IngestorObjStoreConfigMapName,
											},
											Key:      opts.IngestorObjStoreConfigMapKey,
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
									ContainerPort: IngestGRPCPort,
									Name:          IngestGRPCPortName,
								},
								{
									ContainerPort: IngestHTTPPort,
									Name:          IngestHTTPPortName,
								},
								{
									ContainerPort: IngestRemoteWritePort,
									Name:          IngestRemoteWritePortName,
								},
							},
							TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
							TerminationMessagePath:   corev1.TerminationMessagePathDefault,
							Args:                     ingestorArgsFrom(opts),
						},
					},
				},
			},
		},
	}
	return sts
}

func NewIngestorService(opts IngesterOptions) *corev1.Service {
	servicePorts := []corev1.ServicePort{
		{
			Name:       IngestGRPCPortName,
			Port:       IngestGRPCPort,
			TargetPort: intstr.FromInt32(IngestGRPCPort),
			Protocol:   "TCP",
		},
		{
			Name:       IngestHTTPPortName,
			Port:       IngestHTTPPort,
			TargetPort: intstr.FromInt32(IngestHTTPPort),
			Protocol:   "TCP",
		},
		{
			Name:       IngestRemoteWritePortName,
			Port:       IngestRemoteWritePort,
			TargetPort: intstr.FromInt32(IngestRemoteWritePort),
			Protocol:   "TCP",
		},
	}

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      opts.Name,
			Namespace: opts.Namespace,
			Labels:    opts.Labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: opts.Labels,
			Ports:    servicePorts,
		},
	}
	return svc
}

func routerArgsFrom(opts IngesterOptions) []string {
	args := []string{
		"receive",
		"--log.level=info",
		"--log.format=logfmt",
		fmt.Sprintf("--http-address=0.0.0.0:%d", RouterHTTPPort),
	}
	return args
}

func ingestorArgsFrom(opts IngesterOptions) []string {
	args := []string{
		"receive",
		"--log.level=info",
		"--log.format=logfmt",
		fmt.Sprintf("--grpc-address=0.0.0.0:%d", IngestGRPCPort),
		fmt.Sprintf("--http-address=0.0.0.0:%d", IngestHTTPPort),
		fmt.Sprintf("--remote-write.address=0.0.0.0:%d", IngestRemoteWritePort),
		fmt.Sprintf("--tsdb.path=%s", dataVolumeMountPath),
		fmt.Sprintf("--tsdb.retention=%s", opts.IngestorRetention),
		`--label=replica="$(NAME)"`,
		`--label=receive="true"`,
		fmt.Sprintf("--objstore.config=$(%s)", ingestObjectStoreEnvVarName),
		fmt.Sprintf("--receive.local-endpoint=$(NAME).%s.$(NAMESPACE).svc.cluster.local:%d",
			opts.Name, IngestGRPCPort),
		"--receive.grpc-compression=none",
	}
	return args
}

func labelsForRouter(opts IngesterOptions) map[string]string {
	return map[string]string{
		manifests.NameLabel:      Name,
		manifests.ComponentLabel: RouterComponentName,
		manifests.InstanceLabel:  opts.Name,
		manifests.PartOfLabel:    manifests.DefaultPartOfLabel,
		manifests.ManagedByLabel: manifests.DefaultManagedByLabel,
	}
}

func labelsForIngestor(opts IngesterOptions) map[string]string {
	return map[string]string{
		manifests.NameLabel:      Name,
		manifests.ComponentLabel: IngestComponentName,
		manifests.InstanceLabel:  opts.Name,
		manifests.PartOfLabel:    manifests.DefaultPartOfLabel,
		manifests.ManagedByLabel: manifests.DefaultManagedByLabel,
	}
}

func labels(opts IngesterOptions, mergeWithDefaults map[string]string) map[string]string {
	l := opts.AdditionalLabels
	for k, v := range mergeWithDefaults {
		l[k] = v
	}
	return l
}
