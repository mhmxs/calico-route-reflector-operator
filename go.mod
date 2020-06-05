module github.com/mhmxs/calico-route-reflector-operator

go 1.13

require (
	github.com/go-logr/logr v0.1.0
	github.com/onsi/ginkgo v1.11.0
	github.com/onsi/gomega v1.8.1
	github.com/projectcalico/libcalico-go v1.7.2-0.20200427180741-f197f7370140
	github.com/prometheus/common v0.4.1
	k8s.io/api v0.17.2
	k8s.io/apimachinery v0.17.2
	k8s.io/client-go v0.17.2
	sigs.k8s.io/controller-runtime v0.5.0
)
