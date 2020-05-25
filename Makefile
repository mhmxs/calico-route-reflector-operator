IMG_REPO ?= quay.io/mhmxs
IMG_NAME ?= calico-route-reflector-controller
IMG_VERSION ?= latest

# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

all: manager

# Run tests
test: generate fmt vet manifests
	go test ./... -race -coverprofile cover.out

# Build manager binary
manager: generate fmt vet
	go build -race -o bin/manager main.go

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate fmt vet manifests
	go run ./main.go

# Install CRDs into a cluster
install: manifests
	kustomize build config/crd | kubectl apply -f -

# Uninstall CRDs from a cluster
uninstall: manifests
	kustomize build config/crd | kubectl delete -f -

# Deploy controller in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests
	cd config/manager && kustomize edit set image controller=$(IMG_REPO)/$(IMG_NAME):$(IMG_VERSION)
	kustomize build config/default | kubectl apply -f -

# Undeploy deletes resources
undeploy:
	kustomize build config/default | kubectl delete -f -

logs:
	kubectl logs -n calico-route-reflector-operator-system $$(kubectl get po -A | grep calico-route-reflector-operator-system | awk '{print $$2}') manager

logs-f:
	kubectl logs -f -n calico-route-reflector-operator-system $$(kubectl get po -A | grep calico-route-reflector-operator-system | awk '{print $$2}') manager

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

# Generate code
generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

# Build the docker image
docker-build: test
	docker build -t $(IMG_NAME):$(IMG_VERSION) .

# Push the docker image
docker-push: docker-build
	docker tag $(IMG_NAME):$(IMG_VERSION) $(IMG_REPO)/$(IMG_NAME):$(IMG_VERSION)
	docker push $(IMG_REPO)/$(IMG_NAME):$(IMG_VERSION)

# find or download controller-gen
# download controller-gen if necessary
controller-gen:
ifeq (, $(shell which controller-gen))
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go get sigs.k8s.io/controller-tools/cmd/controller-gen@v0.2.5 ;\
	rm -rf $$CONTROLLER_GEN_TMP_DIR ;\
	}
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif
