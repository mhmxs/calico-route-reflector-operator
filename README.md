[![Go Report Card](https://goreportcard.com/badge/github.com/mhmxs/calico-route-reflector-operator)](https://goreportcard.com/report/mhmxs/calico-route-reflector-operator) [![Build Status](https://travis-ci.org/mhmxs/calico-route-reflector-operator.svg?branch=master)](https://travis-ci.org/mhmxs/calico-route-reflector-operator) [![Docker Repository on Quay](https://quay.io/repository/mhmxs/calico-route-reflector-controller/status "Docker Repository on Quay")](https://quay.io/repository/mhmxs/calico-route-reflector-controller) [![Active](http://img.shields.io/badge/Status-Active-green.svg)](https://github.com/mhmxs/calico-route-reflector-operator) [![PR's Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg?style=flat)](https://github.com/mhmxs/calico-route-reflector-operator/pulls) [![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

# Calico Route Reflector Operator

### Use your own risk !!!

### Proposal documentation found here: https://github.com/mhmxs/calico-route-reflector-operator-proposal. Please feel free to share your ideas !!!

## Prerequisites

 * Kubernetes cluster up and running

This Kubernetes operator can monitor and scale Calico route refloctor pods based on node number per zone. The operator owns a few environment variables:
 * `DATASTORE_TYPE` Calico datastore [`incluster`, `kubernetes`, `etcd`], default `incluster`
 * `K8S_API_ENDPOINT` Kubernetes API endpoint, default `https://kubernetes.default`
 * `ROUTE_REFLECTOR_CLUSTER_ID` Route reflector cluster ID, default `224.0.0.%d`
 * `ROUTE_REFLECTOR_MIN` Minimum number of route reflector pods per zone, default `3`
 * `ROUTE_REFLECTOR_MAX` Maximum number of route reflector pods per zone, default `25`
 * `ROUTE_REFLECTOR_RATIO` Node / route reflector pod ratio, default `0.005` (`1000 * 0.005 = 5`)
 * `ROUTE_REFLECTOR_NODE_LABEL` Node label of the route reflector nodes, default `calico-route-reflector=`
 * `ROUTE_REFLECTOR_ZONE_LABEL` Node label of the zone, default ``
 * `ROUTE_REFLECTOR_TOPOLOGY` Selected topology of route reflectors [simple, multi], default `simple`

You can edit or add those environment variables at the [manager](config/manager/manager.yaml) manifest. You can add Calico client config related variables, Calico lib will parse it in the background.

During the `api/core/v1/Node` reconcile phases it calculates the right number of route refloctor nodes per zone. It supports linear scaling only and it multiplies the number of nodes with the given ratio and than updates the route reflector replicas to the expected number. After all the nodes are labeled correctly it regenerates BGP peer topology for the cluster.

## Usage

This is a standard Kubebuilder opertor so building and deploying process is similar as a [stock Kubebuilder project](https://book.kubebuilder.io/cronjob-tutorial/running.html).

After first reconcile phase is done don not forget to disable the [node-to-node mesh](https://docs.projectcalico.org/getting-started/kubernetes/hardway/configure-bgp-peering)!

Use latest release:
```
kustomize build config/crd | kubectl apply -f -
$(cd config/manager && kustomize edit set image controller=quay.io/mhmxs/calico-route-reflector-controller:v0.0.3)
kustomize build config/default | kubectl apply -f -
```

Use official latest master image:

Edit Calico client environment variables before deploying operator at; [ETCD](config/manager/etcd/envs.yaml), [KDD](config/manager/kdd/envs.yaml)!

```
kustomize build config/crd | kubectl apply -f -
$(cd config/default && kustomize edit add base ../manager) # for in cluster
# $(cd config/default && kustomize edit add base ../manager/etcd) # for ETCD
# $(cd config/default && kustomize edit add base ../manager/kdd) # for KDD
kustomize build config/default | kubectl apply -f -
```

Build your own image:
`IMG_REPO=[IMG_REPO] IMG_NAME=[IMG_NAME] IMG_VERSION=[IMG_VERSION] make test docker-push install deploy`

## Limitations

 * In the current implementation each reconcile loop fetches all nodes and all BGP peer configurations which could take too much time in large clusters.
 * Multi cluster topology rebalances the whole cluster on case of nodes are added. If you are unlicky it could drop all 3 route reflector sessions which chause 1-2 sec network outage.
 * Multi cluster topology generates 3 BGP peers per node, which can grow in large cluster. Would be better to create BGP peer configuration for eahc route reflector combination to decrease number of BGP peer configs. For example: `[1,2,3]`, `[1,2,4]`, `[2,3,4]`, `[1,3,4]`

## Roadmap

 * Use custom resource instead of environment variables
 * Dedicated or preferred node label
 * Disallow node label
 * Handle taints and tolerations

# Contributing

We appreciate your help!

To contribute, please read our contribution guidelines: [CONTRIBUTION.md](CONTRIBUTION.md)
