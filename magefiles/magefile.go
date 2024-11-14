//go:build mage
// +build mage

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/magefile/mage/sh"
	"github.com/thanos-community/thanos-operator/test/utils"
)

const (
	operatorNamespace = "thanos-operator-system"
	projectImage      = "quay.io/thanos/thanos-operator"
)

func Build() error {
	if err := Kind(); err != nil {
		return err
	}
	if err := PromOperator(); err != nil {
		return err
	}
	if err := CertManager(); err != nil {
		return err
	}

	if err := sh.Run("kubectl", "create", "namespace", "thanos-operator-system"); err != nil {
		return err
	}

	if err := Minio(); err != nil {
		return err
	}

	if err := BuildLocalImage(); err != nil {
		return err
	}

	if err := LoadLocalImage(); err != nil {
		return err
	}

	if err := InstallCRDS(); err != nil {
		return err
	}

	if err := InstallSamples(); err != nil {
		return err
	}

	if err := SetupReflector(); err != nil {
		return err
	}

	if err := SetupCertificates(); err != nil {
		return err
	}

	if err := RunPrometheus(); err != nil {
		return err
	}

	return nil
}

func Kind() error {
	return sh.Run("kind", "create", "cluster", "--name", "kind")
}

func PromOperator() error {
	return utils.InstallPrometheusOperator()
}

func CertManager() error {
	return utils.InstallCertManager()
}

func Minio() error {
	err := utils.InstallMinIO()
	if err != nil {
		return err
	}
	return utils.CreateMinioObjectStorageSecret()
}

func BuildLocalImage() error {
	return sh.Run("docker", "build", ".", "-t", getImageName())
}

func LoadLocalImage() error {
	return utils.LoadImageToKindClusterWithName(getImageName())
}

func InstallCRDS() error {
	return sh.Run("kubectl", "apply", "--server-side", "-f", "config/crd/bases/")
}

func InstallSamples() error {
	if err := sh.Run("make", "install-sample"); err != nil {
		return err
	}
	return nil
}

func SetupReflector() error {
	if err := sh.Run("helm", "repo", "add", "emberstack", "https://emberstack.github.io/helm-charts"); err != nil {
		return err
	}
	if err := sh.Run("helm", "repo", "update"); err != nil {
		return err
	}
	return sh.Run("helm", "upgrade", "--install", "reflector", "emberstack/reflector")
}

func SetupCertificates() error {
	content := `
---
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: selfsigned-issuer
  namespace: default
spec:
  selfSigned: {}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: selfsigned-ca
  namespace: default
spec:
  isCA: true
  commonName: selfsigned-ca
  secretName: root-secret
  issuerRef:
    name: selfsigned-issuer
    kind: ClusterIssuer
    group: cert-manager.io
---
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: ca-issuer
  namespace: default
spec:
  ca:
    secretName: root-secret
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: dev-client
  namespace: default
spec:
  secretName: dev-client-secret
  isCA: false
  usages:
    - client auth
  emailAddresses:
    - dev@prom.com
  issuerRef:
    name: ca-issuer
    kind: Issuer
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: prod-client
  namespace: default
spec:
  secretName: prod-client-secret
  isCA: false
  usages:
    - client auth
  emailAddresses:
    - prod@prom.com
  issuerRef:
    name: ca-issuer
    kind: Issuer
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: envoy-proxy
spec:
  secretName: envoy-proxy-remote-write                   
  isCA: false
  usages: 
    - server auth                           
  dnsNames:
    - thanos-gateway-test.thanos-operator-system.svc.cluster.local                                  
  issuerRef:
    name: ca-issuer                         
    kind: Issuer
  secretTemplate:
    annotations:
      reflector.v1.k8s.emberstack.com/reflection-allowed: "true"
      reflector.v1.k8s.emberstack.com/reflection-allowed-namespaces: "thanos-operator-system"
      reflector.v1.k8s.emberstack.com/reflection-auto-enabled: "true"
      reflector.v1.k8s.emberstack.com/reflection-auto-namespaces: "thanos-operator-system"

`
	_, err := applyKubeResources(content)
	return err
}

func RunPrometheus() error {
	content := `
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: prometheus
rules:
- apiGroups: [""]
  resources:
  - nodes
  - nodes/metrics
  - services
  - endpoints
  - pods
  verbs: ["get", "list", "watch"]
- apiGroups: [""]
  resources:
  - configmaps
  verbs: ["get"]
- apiGroups:
  - discovery.k8s.io
  resources:
  - endpointslices
  verbs: ["get", "list", "watch"]
- apiGroups:
  - networking.k8s.io
  resources:
  - ingresses
  verbs: ["get", "list", "watch"]
- nonResourceURLs: ["/metrics"]
  verbs: ["get"]
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: prometheus
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: prometheus
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: prometheus
subjects:
- kind: ServiceAccount
  name: prometheus
  namespace: default
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: example-app
spec:
  replicas: 3
  selector:
    matchLabels:
      app: example-app
  template:
    metadata:
      labels:
        app: example-app
    spec:
      containers:
      - name: example-app
        image: quay.io/brancz/prometheus-example-app:v0.5.0
        ports:
        - name: web
          containerPort: 8080
---
kind: Service
apiVersion: v1
metadata:
  name: example-app
  labels:
    app: example-app
spec:
  selector:
    app: example-app
  ports:
  - name: web
    port: 8080
---
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: example-app
  labels:
    team: frontend
spec:
  selector:
    matchLabels:
      app: example-app
  endpoints:
  - port: web
---
apiVersion: monitoring.coreos.com/v1
kind: Prometheus
metadata:
  name: prometheus-dev
spec:
  secrets: ['dev-client-secret']
  serviceAccountName: prometheus
  externalLabels:
    env: dev
  serviceMonitorSelector:
    matchLabels:
      team: frontend
  remoteWrite:
    - url: https://thanos-gateway-test.thanos-operator-system.svc.cluster.local:8081/api/v1/receive
      name: thanos-receive-router
      tlsConfig:
        caFile: /etc/prometheus/secrets/dev-client-secret/ca.crt
        certFile: /etc/prometheus/secrets/dev-client-secret/tls.crt
        keyFile: /etc/prometheus/secrets/dev-client-secret/tls.key
---
apiVersion: monitoring.coreos.com/v1
kind: Prometheus
metadata:
  name: prometheus-prod
spec:
  secrets: ['prod-client-secret']
  serviceAccountName: prometheus
  externalLabels:
    env: prod
  serviceMonitorSelector:
    matchLabels:
      team: frontend
  remoteWrite:
    - url: https://thanos-gateway-test.thanos-operator-system.svc.cluster.local:8081/api/v1/receive
      name: thanos-receive-router
      tlsConfig:
        caFile: /etc/prometheus/secrets/prod-client-secret/ca.crt
        certFile: /etc/prometheus/secrets/prod-client-secret/tls.crt
        keyFile: /etc/prometheus/secrets/prod-client-secret/tls.key
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: alice
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: bob
`
	_, err := applyNamespacedKubeResources(content, "default")
	return err
}

func getImageName() string {
	x, err := sh.Output("git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		panic(err)
	}
	y, err := sh.Output("date", "+%Y-%m-%d")
	if err != nil {
		panic(err)
	}
	z, err := sh.Output("git", "rev-parse", "--short", "HEAD")
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%s:%s-%s-%s", projectImage, x, y, z)
}

func GetToken(forSomeone string) (string, error) {
	o, err := sh.Output("kubectl", "create", "token", forSomeone)
	if err != nil {
		return "", err
	}
	fmt.Println(o)
	return o, nil
}
func GetTokenOutput(forSomeone string) error {
	o, err := sh.Output("kubectl", "create", "token", forSomeone)
	if err != nil {
		return err
	}
	fmt.Println(o)
	return nil
}

func DoTokenReview(forSomeone string) error {
	token, err := GetToken(forSomeone)
	if err != nil {
		return err
	}
	content := fmt.Sprintf(`
kind: TokenReview
apiVersion: authentication.k8s.io/v1
metadata:
   name: test
spec:
  token: %s
`, token)
	result, err := applyKubeResources(content, "-o", "yaml")
	if err != nil {
		return err
	}
	fmt.Println(result)
	return nil
}

func applyKubeResources(resources string, withArgs ...string) (string, error) {
	in := []byte(resources)
	dir, err := os.MkdirTemp("", "resources")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "tmpfile")
	if err := os.WriteFile(file, in, 0666); err != nil {
		return "", err
	}
	args := append([]string{"apply", "-f", file}, withArgs...)
	return sh.Output("kubectl", args...)
}

func applyNamespacedKubeResources(resources, namespace string) (string, error) {
	in := []byte(resources)
	dir, err := os.MkdirTemp("", "resources")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "tmpfile")
	if err := os.WriteFile(file, in, 0666); err != nil {
		return "", err
	}
	return sh.Output("kubectl", "-n", namespace, "apply", "-f", file)
}

//func DoTokenReview() error {
//
//	return utils.DoTokenReview()
//}

// kubectl -n thanos-operator-system port-forward svc/thanos-gateway-test 8080
// TOKEN=$(kubectl -n default create token default)
//  curl -H 'Authorization: Bearer ${TOKEN}' 'http://localhost:8080/anything/api/v1/query?query=up' -v                                                                                 ok | 17:06:48
//  kubectl apply -f "/Users/pgough/Desktop/projects/thanos-operator/config/samples/crb.yaml"
