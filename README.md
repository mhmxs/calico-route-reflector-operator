[![Go Report Card](https://goreportcard.com/badge/github.com/mhmxs/calico-route-reflector-operator)](https://goreportcard.com/report/mhmxs/calico-route-reflector-operator) [![Build Status](https://travis-ci.org/mhmxs/calico-route-reflector-operator.svg?branch=master)](https://travis-ci.org/mhmxs/calico-route-reflector-operator) [![Docker Repository on Quay](https://quay.io/repository/mhmxs/calico-route-reflector-controller/status "Docker Repository on Quay")](https://quay.io/repository/mhmxs/calico-route-reflector-controller) [![Active](http://img.shields.io/badge/Status-Active-green.svg)](https://github.com/mhmxs/calico-route-reflector-operator) [![PR's Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg?style=flat)](https://github.com/mhmxs/calico-route-reflector-operator/pulls) [![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

# Calico Route Reflector Operator

### Use your own risk !!!

### Proposal documentation found here: https://github.com/mhmxs/calico-route-reflector-operator-proposal. Please feel free to share your ideas !!!

## Prerequisites

 * Kubernetes cluster up and running
 * Calico network on Kubernetes data store a.k.a. `KDD`.
 * Configured Calico `BGPPeer`s one for route reflector mesh and an other for clients. [More info](https://docs.projectcalico.org/getting-started/kubernetes/hardway/configure-bgp-peering)

This Kubernetes operator can monitor and scale Calico route refloctor pods based on node number per zone. The operator owns a few environment variables:
 * `ROUTE_REFLECTOR_CLUSTER_ID` Route reflector cluster ID, default `224.0.0.%d`
 * `ROUTE_REFLECTOR_MIN` Minimum number of route reflector pods per zone, default `3`
 * `ROUTE_REFLECTOR_MAX` Maximum number of route reflector pods per zone, default `25`
 * `ROUTE_REFLECTOR_RATIO` Node / route reflector pod ratio, default `0.005` (`1000 * 0.005 = 5`)
 * `ROUTE_REFLECTOR_NODE_LABEL` Node label of the route reflector nodes, default `calico-route-reflector=`
 * `ROUTE_REFLECTOR_ZONE_LABEL` Node label of the zone, default ``

During the `api/core/v1/Node` reconcile phases it calculates the right number of route refloctor nodes per zone. It supports linear scaling only and it multiplies the number of nodes with the given ratio and than updates the route reflector replicas to the expected number.

## Usage

This is a standard Kubebuilder opertor so building and deploying process is similar as a [stock Kubebuilder project](https://book.kubebuilder.io/cronjob-tutorial/running.html).

Use official image:
```
kustomize build config/crd | kubectl apply -f -
kustomize build config/default | kubectl apply -f -
```

Build your own image:
`IMG_REPO=[IMG_REPO] IMG_NAME=[IMG_NAME] IMG_VERSION=[IMG_VERSION] make test docker-push install deploy`

## Roadmap

 * Etcd data store support (Currently you have to edit [manager](config/manager/manager.yaml)'s yaml manually)
 * Use custom resource instead of environment variables
 * Dedicated or preferred node label
 * Disallow node label
 * Handle taints and tolerations

# Contributing

We appreciate your help!

To contribute, please read our contribution guidelines: [CONTRIBUTION.md](CONTRIBUTION.md)
