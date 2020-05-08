/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"math"
	"strings"

	"github.com/go-logr/logr"
	"github.com/prometheus/common/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

var (
	nodeUnderDelete = ctrl.Result{}
	finished        = ctrl.Result{}

	nodeGetError    = ctrl.Result{}
	nodeListError   = ctrl.Result{}
	nodeUpdateError = ctrl.Result{}
)

type RouteReflectorConfig struct {
	Min       int
	Max       int
	Ration    float64
	NodeLabel string
}

// RouteReflectorConfigReconciler reconciles a RouteReflectorConfig object
type RouteReflectorConfigReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	config RouteReflectorConfig
}

type reconcileImplClient interface {
	Get(context.Context, client.ObjectKey, runtime.Object) error
	Update(context.Context, runtime.Object, ...client.UpdateOption) error
	List(context.Context, runtime.Object, ...client.ListOption) error
}

type reconcileImplParams struct {
	request ctrl.Request
	client  reconcileImplClient
	config  RouteReflectorConfig
}

// +kubebuilder:rbac:groups=route-reflector.calico-route-reflector-operator.mhmxs.github.com,resources=routereflectorconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route-reflector.calico-route-reflector-operator.mhmxs.github.com,resources=routereflectorconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;update;watch

func (r *RouteReflectorConfigReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("routereflectorconfig", req.NamespacedName)

	return reconcileImpl(&reconcileImplParams{
		request: req,
		client:  r.Client,
		config:  r.config,
	})
}

func reconcileImpl(params *reconcileImplParams) (ctrl.Result, error) {
	node := &corev1.Node{}
	err := params.client.Get(context.Background(), params.request.NamespacedName, node)

	if err != nil && !errors.IsNotFound(err) {
		log.Errorf("Unable to fetch node %s reason %s", params.request.NamespacedName, err.Error())
		return nodeGetError, err
	} else if err == nil && node.GetDeletionTimestamp() != nil {
		return nodeUnderDelete, nil
	}

	nodes := &corev1.NodeList{}
	if err := params.client.List(context.Background(), nodes, &client.ListOptions{}); err != nil {
		log.Errorf("Unable to list nodes ,reason %s", err.Error())
		return nodeListError, err
	}

	expectedNumber := int(math.Round(float64(len(nodes.Items)) * params.config.Ration))
	if expectedNumber < params.config.Min {
		expectedNumber = params.config.Min
	} else if expectedNumber > params.config.Max {
		expectedNumber = params.config.Max
	}
	log.Infof("Expected number of route reflector pods are %d", expectedNumber)

	key, value := getKeyValue(params.config.NodeLabel)
	actualNumber := 0
	for _, n := range nodes.Items {
		if isLabeled(n.GetLabels(), key, value) {
			actualNumber++
		}
	}
	log.Infof("Actual number of route reflector pods are %d", actualNumber)

	if expectedNumber != actualNumber {
		for _, n := range nodes.Items {
			if expectedNumber == actualNumber {
				break
			}
			labeled := isLabeled(n.GetLabels(), key, value)
			if expectedNumber > actualNumber && !labeled {
				log.Infof("Label node %s as route reflector", n.GetName())
				n.Labels[key] = value
				actualNumber++
			} else if expectedNumber < actualNumber && labeled {
				log.Infof("Remove node %s role route reflector", n.GetName())
				delete(n.Labels, key)
				actualNumber--
			} else {
				continue
			}

			if err = params.client.Update(context.Background(), &n); err != nil {
				log.Errorf("Unable to update node %s, reason %s", params.request.NamespacedName, err.Error())
				return nodeUpdateError, err
			}
		}
	}

	return finished, nil
}

func getKeyValue(label string) (string, string) {
	keyValue := strings.Split(label, "=")
	if len(keyValue) == 1 {
		keyValue[1] = ""
	}

	return keyValue[0], keyValue[1]
}

func isLabeled(labels map[string]string, key, value string) bool {
	label, ok := labels[key]
	return ok && label == value
}

type eventFilter struct{}

func (ef eventFilter) Create(event.CreateEvent) bool {
	return true
}

func (ef eventFilter) Delete(e event.DeleteEvent) bool {
	return true
}

func (ef eventFilter) Update(event.UpdateEvent) bool {
	return false
}

func (ef eventFilter) Generic(event.GenericEvent) bool {
	return false
}

func (r *RouteReflectorConfigReconciler) SetupWithManager(mgr ctrl.Manager, config RouteReflectorConfig) error {
	r.config = config
	return ctrl.NewControllerManagedBy(mgr).
		WithEventFilter(eventFilter{}).
		For(&corev1.Node{}).
		Complete(r)
}
