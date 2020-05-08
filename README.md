[![Go Report Card](https://goreportcard.com/badge/github.com/mhmxs/calico-route-reflector-operator)](https://goreportcard.com/report/mhmxs/calico-route-reflector-operator) [![Active](http://img.shields.io/badge/Status-Active-green.svg)](https://github.com/mhmxs/calico-route-reflector-operator) [![PR's Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen.svg?style=flat)](https://github.com/mhmxs/calico-route-reflector-operator/pulls) [![License](https://img.shields.io/badge/License-BSD%203--Clause-blue.svg)](https://opensource.org/licenses/BSD-3-Clause)

# Calico Route Reflector Operator

This project is work in progress !!!
Use your own risk !!!

This Kubernetes operator can monitor and scale Calico route refloctor pods based on cluster size. The operator has a few environment variable:
 * `ROUTE_REFLECTOR_MIN` Minimum number of route reflector pods, default `3`
 * `ROUTE_REFLECTOR_MAX` Maximum number of route reflector pods, default `10`
 * `ROUTE_REFLECTOR_RATIO` Node / route reflector pod ratio, default `0.2` (`100 * 0.2 = 20`)
 * `ROUTE_REFLECTOR_NODE_LABEL` Node label of the route reflector nodes, default `calico-route-reflector=`
 
During the `api/core/v1/Node` reconcile phases it calculates the right number of route refloctor pods by multiply the number of nodes with the given ratio.
It updates the route reflector replicas to the expected number.

## Usage

This is a standard Kubebuilder opertor so building and deploying process is similar as a (stock Kubebuilder project)[https://book.kubebuilder.io/cronjob-tutorial/running.html].

Use official image (Don't trust me! Build your own :D):
`make install deploy`

Build your own image:
`IMG_REPO=[IMG_REPO] IMG_NAME=[IMG_NAME] IMG_VERSION=[IMG_VERSION] make docker-push install deploy`

## Roadmap

 * Use custom resource instead of environment variables
 * Auto balancing of route reflector pods between zones
 * Take care on Node status, current POC checks only number of nodes
 * Dedicated or preferred node label
 * Disallow node label