[![Release](https://img.shields.io/github/release/mhmxs/calico-route-reflector-operator.svg)](https://github.com/mhmxs/calico-route-reflector-operator/releases/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/mhmxs/calico-route-reflector-operator)](https://goreportcard.com/report/mhmxs/calico-route-reflector-operator)
[![Codecov branch](https://img.shields.io/codecov/c/github/mhmxs/calico-route-reflector-operator/master.svg)](https://codecov.io/gh/mhmxs/calico-route-reflector-operator)
[![Build Status](https://travis-ci.org/mhmxs/calico-route-reflector-operator.svg?branch=master)](https://travis-ci.org/mhmxs/calico-route-reflector-operator)
[![Docker Repository on Quay](https://quay.io/repository/mhmxs/calico-route-reflector-controller/status "Docker Repository on Quay")](https://quay.io/repository/mhmxs/calico-route-reflector-controller)
[![Active](http://img.shields.io/badge/Status-Active-green.svg)](https://github.com/mhmxs/calico-route-reflector-operator)
[![PR's Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg?style=flat)](https://github.com/mhmxs/calico-route-reflector-operator/pulls)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)



# Calico Route Reflector Operator

### Use your own risk !!!

### Proposal documentation found here: https://github.com/mhmxs/calico-route-reflector-operator-proposal. Please feel free to share your ideas !!!

## Prerequisites

 * Kubernetes cluster up and running

## About

This Kubernetes operator can monitor and scale Calico route reflector topology based on node number. The operator has a few environment variables to configure behaviour:

 * `DATASTORE_TYPE` Calico datastore [`incluster`, `kubernetes`, `etcdv3`], default `incluster`
 * `ROUTE_REFLECTOR_CLUSTER_ID` Route reflector cluster ID, default `224.0.0.0`
 * `ROUTE_REFLECTOR_MIN` Minimum number of route reflector pods per zone, default `3`
 * `ROUTE_REFLECTOR_MAX` Maximum number of route reflector pods per zone, default `25`
 * `ROUTE_REFLECTOR_RATIO` Node / route reflector pod ratio, default `0.005` (`1000 * 0.005 = 5`)
 * `ROUTE_REFLECTOR_NODE_LABEL` Node label of the route reflector nodes, default `calico-route-reflector=`
 * `ROUTE_REFLECTOR_ZONE_LABEL` Node label of the zone, default ``
 * `ROUTE_REFLECTOR_INCOMPATIBLE_NODE_LABELS` Comma separated list of incompatible node labels, default ``
   * `do-not-select-as-rr=true` Full match
   * `do-not-select-as-rr=` Value must be empty
   * `do-not-select-as-rr` Has label
 * `ROUTE_REFLECTOR_TOPOLOGY` Selected topology of route reflectors [simple, multi], default `simple`
 * `ROUTE_REFLECTOR_WAIT_TIMEOUT` Wait timeout in second. In some topology chages operator can wait for Calico to establish new connections, default: `0`

You can edit or add those environment variables at the [manager](config/manager/bases/manager.yaml) manifest. You can add Calico client config related variables and the client will parse them automatically in the background.

During the `api/core/v1/Node` reconcile phases it calculates the right number of route reflector nodes based on selected topology. It supports linear scaling only and it multiplies the number of nodes with the given ratio. Than updates the route reflector replicas to the expected number. After all the nodes are labeled correctly it regenerates BGP peer configurations for the cluster.

## Usage

This is a standard Kubebuilder opertor so building and deploying process is similar as a [stock Kubebuilder project](https://book.kubebuilder.io/cronjob-tutorial/running.html).

After first reconcile phase is done don not forget to disable the [node-to-node mesh](https://docs.projectcalico.org/getting-started/kubernetes/hardway/configure-bgp-peering)!

### Use latest release:

```
kustomize build config/crd | kubectl apply -f -
$(cd config/default && kustomize edit add base ../manager)
$(cd config/manager && kustomize edit set image controller=quay.io/mhmxs/calico-route-reflector-controller:v0.1.0)
kustomize build config/default | kubectl apply -f -
```

### Use official latest master image:

```
kustomize build config/crd | kubectl apply -f -
$(cd config/default && kustomize edit add base ../manager)
kustomize build config/default | kubectl apply -f -
```

### Use custom datastore rather then in-cluster KDD:

* Create secret based on your config at; [ETCD](config/manager/etcd/secret.yaml), [KDD](config/manager/kdd/secret.yaml)
* Edit environment variables based on your secrets at; [ETCD](config/manager/etcd/envs.yaml), [KDD](config/manager/kdd/envs.yaml)
* Add your datastore settings to bases:

```
# $(cd config/default && kustomize edit add base ../manager) # skip that step
$(cd config/default && kustomize edit add base ../manager/etcd) # for ETCD
$(cd config/default && kustomize edit add base ../manager/kdd) # for KDD
```

### Build your own image:

`IMG_REPO=[IMG_REPO] IMG_NAME=[IMG_NAME] IMG_VERSION=[IMG_VERSION] make test docker-push install deploy`

## Limitations

 * In the current implementation each reconcile loop fetches all nodes and all BGP peer configurations which could take too much time in large clusters.
 * Multi cluster topology rebalances the whole cluster on case of nodes are removed. If you are unlicky and all 3 route reflectors of the node gone, 1-2 sec network outage should happen until it gets it's new ones.
 * Multi cluster topology generates 3 BGP peers per node, which can grow in large cluster. Would be better to create BGP peer configuration for each route reflector combination to decrease number of BGP peer configs. For example: `[1,2,3]`, `[1,2,4]`, `[2,3,4]`, `[1,3,4]`

## Roadmap

 * Use custom resource instead of environment variables
 * Dedicated or preferred node label

# Contributing

We appreciate your help!

To contribute, please read our contribution guidelines: [CONTRIBUTION.md](CONTRIBUTION.md)
