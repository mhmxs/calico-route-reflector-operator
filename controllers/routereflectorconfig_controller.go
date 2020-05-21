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
	nodeNotFound = ctrl.Result{}
	nodeCleaned  = ctrl.Result{Requeue: true}
	finished     = ctrl.Result{}

	nodeGetError     = ctrl.Result{}
	nodeCleanupError = ctrl.Result{}
	nodeListError    = ctrl.Result{}
	nodeUpdateError  = ctrl.Result{}
)

type RouteReflectorConfig struct {
	Min            int
	Max            int
	Ration         float64
	NodeLabelKey   string
	NodeLabelValue string
	ZoneLabel      string
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

// +kubebuilder:rbac:groups=route-reflector.calico-route-reflector-operator.mhmxs.github.com,resources=routereflectorconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route-reflector.calico-route-reflector-operator.mhmxs.github.com,resources=routereflectorconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;update;watch

func (r *RouteReflectorConfigReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("routereflectorconfig", req.NamespacedName)

	node := corev1.Node{}
	err := r.Client.Get(context.Background(), req.NamespacedName, &node)
	if err != nil && !errors.IsNotFound(err) {
		log.Errorf("Unable to fetch node %s reason %s", req.NamespacedName, err.Error())
		return nodeGetError, err
	} else if errors.IsNotFound(err) {
		log.Debugf("Node not found %s", req.NamespacedName)
		return nodeNotFound, nil
	} else if err == nil && node.GetDeletionTimestamp() != nil || !isNodeReady(&node) {
		// Node is deleted right now or has some issues, better to remove form RRs
		if err := r.cleanupLabel(req, &node, r.config.NodeLabelKey); err != nil {
			log.Errorf("Unable to cleanup label on %s because of %s", req.NamespacedName, err.Error())
			return nodeCleanupError, err
		}

		return nodeCleaned, nil
	}

	listOptions := client.ListOptions{}
	if r.config.ZoneLabel != "" {
		if nodeZone, ok := node.GetLabels()[r.config.ZoneLabel]; ok {
			labels := client.MatchingLabels{r.config.ZoneLabel: nodeZone}
			labels.ApplyToList(&listOptions)
		}
	}
	log.Debugf("List options are %v", listOptions)
	nodeList := corev1.NodeList{}
	if err := r.Client.List(context.Background(), &nodeList, &listOptions); err != nil {
		log.Errorf("Unable to list nodes ,reason %s", err.Error())
		return nodeListError, err
	}

	readyNodes := 0
	actualReadyNumber := 0
	nodes := map[*corev1.Node]bool{}
	for _, n := range nodeList.Items {
		nodes[&n] = isNodeReady(&n)
		if nodes[&n] {
			readyNodes++
			if isLabeled(n.GetLabels(), r.config.NodeLabelKey, r.config.NodeLabelValue) {
				actualReadyNumber++
			}
		}
	}
	log.Infof("Nodes are ready %d", readyNodes)
	log.Infof("Actual number of healthy route reflector nodes are %d", actualReadyNumber)

	expectedNumber := int(math.Round(float64(readyNodes) * r.config.Ration))
	if expectedNumber < r.config.Min {
		expectedNumber = r.config.Min
	} else if expectedNumber > r.config.Max {
		expectedNumber = r.config.Max
	}
	log.Infof("Expected number of route reflector nodes are %d", expectedNumber)

	for n, isReady := range nodes {
		if !isReady {
			// Node has some issues, better to remove form RRs
			if err := r.cleanupLabel(req, n, r.config.NodeLabelKey); err != nil {
				log.Errorf("Unable to cleanup label on %s because of %s", req.NamespacedName, err.Error())
				return nodeCleanupError, err
			}

			continue
		} else if expectedNumber == actualReadyNumber {
			continue
		}

		labeled := isLabeled(n.GetLabels(), r.config.NodeLabelKey, r.config.NodeLabelValue)
		if !labeled && expectedNumber > actualReadyNumber {
			log.Infof("Label node %s as route reflector", n.GetName())
			n.Labels[r.config.NodeLabelKey] = r.config.NodeLabelValue
			actualReadyNumber++
		} else if labeled && expectedNumber < actualReadyNumber {
			log.Infof("Remove node %s role route reflector", n.GetName())
			delete(n.Labels, r.config.NodeLabelKey)
			actualReadyNumber--
		} else {
			continue
		}

		log.Infof("Updating labels on node %s to %v", req.NamespacedName, n.Labels)
		if err = r.Client.Update(context.Background(), n); err != nil {
			log.Errorf("Unable to update node %s, reason %s", req.NamespacedName, err.Error())
			return nodeUpdateError, err
		}
	}

	return finished, nil
}

func (r *RouteReflectorConfigReconciler) cleanupLabel(req ctrl.Request, node *corev1.Node, labelKey string) error {
	if _, ok := node.GetLabels()[labelKey]; ok {
		delete(node.Labels, labelKey)

		log.Infof("Removing route reflector label from %s", req.NamespacedName)
		if err := r.Client.Update(context.Background(), node); err != nil {
			log.Errorf("Unable to cleanup node %s, reason %s", req.NamespacedName, err.Error())
			return err
		}
	}

	return nil
}

func isNodeReady(node *corev1.Node) bool {
	for _, c := range node.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return true
		}
	}

	return false
}

func isLabeled(labels map[string]string, key, value string) bool {
	label, ok := labels[key]
	return ok && label == value
}

type eventFilter struct{}

func (ef eventFilter) Create(event.CreateEvent) bool {
	return false
}

func (ef eventFilter) Delete(e event.DeleteEvent) bool {
	return true
}

func (ef eventFilter) Update(event.UpdateEvent) bool {
	return true
}

func (ef eventFilter) Generic(event.GenericEvent) bool {
	return true
}

func (r *RouteReflectorConfigReconciler) SetupWithManager(mgr ctrl.Manager, config RouteReflectorConfig) error {
	r.config = config
	return ctrl.NewControllerManagedBy(mgr).
		WithEventFilter(eventFilter{}).
		For(&corev1.Node{}).
		Complete(r)
}
