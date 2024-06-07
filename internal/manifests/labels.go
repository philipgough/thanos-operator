package manifests

const (
	// The following labels are used to identify the components and will be set on the resources created by the operator.
	// These labels cannot be overridden by the user via additional labels configuration.
	// See https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/#labels

	NameLabel      = "app.kubernetes.io/name"
	ComponentLabel = "app.kubernetes.io/component"
	PartOfLabel    = "app.kubernetes.io/part-of"
	ManagedByLabel = "app.kubernetes.io/managed-by"
	InstanceLabel  = "app.kubernetes.io/instance"

	DefaultPartOfLabel    = "thanos"
	DefaultManagedByLabel = "thanos-operator"
)
