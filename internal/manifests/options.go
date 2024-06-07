package manifests

// Options is a struct that holds the options for the common manifests
type Options struct {
	// Name is the name of the object
	Name string
	// Namespace is the namespace of the object
	Namespace string
	// Labels is the labels for the object
	Labels map[string]string
	// Image is the image to use for the component
	Image string
}
