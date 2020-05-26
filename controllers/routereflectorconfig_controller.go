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
	"fmt"
	"math"

	"github.com/go-logr/logr"
	"github.com/prometheus/common/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	types "k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	calicoApiConfig "github.com/projectcalico/libcalico-go/lib/apiconfig"
	calicoApi "github.com/projectcalico/libcalico-go/lib/apis/v3"
	calicoClient "github.com/projectcalico/libcalico-go/lib/clientv3"
	"github.com/projectcalico/libcalico-go/lib/options"
)

const (
	routeReflectorClusterIDAnnotation = "projectcalico.org/RouteReflectorClusterID"
)

var (
	nodeNotFound = ctrl.Result{}
	nodeCleaned  = ctrl.Result{Requeue: true}
	nodeReverted = ctrl.Result{Requeue: true}
	finished     = ctrl.Result{}

	nodeGetError          = ctrl.Result{}
	nodeCleanupError      = ctrl.Result{}
	labelSelectorError    = ctrl.Result{}
	nodeListError         = ctrl.Result{}
	nodeRevertError       = ctrl.Result{}
	calicoNodeGetError    = ctrl.Result{}
	calicoNodeUpdateError = ctrl.Result{}
	nodeUpdateError       = ctrl.Result{}
)

var routeReflectorsUnderOperation = map[types.UID]bool{}

type RouteReflectorConfig struct {
	DataStoreType  calicoApiConfig.DatastoreType
	ClusterID      string
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
	CalicoClient calicoClient.Interface
	Log          logr.Logger
	Scheme       *runtime.Scheme
	config       RouteReflectorConfig
}

type reconcileImplClient interface {
	Get(context.Context, client.ObjectKey, runtime.Object) error
	Update(context.Context, runtime.Object, ...client.UpdateOption) error
	List(context.Context, runtime.Object, ...client.ListOption) error
}

// +kubebuilder:rbac:groups=route-reflector.calico-route-reflector-operator.mhmxs.github.com,resources=routereflectorconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route-reflector.calico-route-reflector-operator.mhmxs.github.com,resources=routereflectorconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;update;watch
// +kubebuilder:rbac:groups="projectcalico.org/v3",resources=nodes,verbs=list;update

func (r *RouteReflectorConfigReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("routereflectorconfig", req.Name)

	node := corev1.Node{}
	if err := r.Client.Get(context.Background(), req.NamespacedName, &node); err != nil && !errors.IsNotFound(err) {
		log.Errorf("Unable to fetch node %s because of %s", req.Name, err.Error())
		return nodeGetError, err
	} else if errors.IsNotFound(err) {
		log.Debugf("Node not found %s", req.Name)
		return nodeNotFound, nil
	} else if err == nil && isLabeled(node.GetLabels(), r.config.NodeLabelKey, r.config.NodeLabelValue) && node.GetDeletionTimestamp() != nil ||
		!isNodeReady(&node) || !isNodeSchedulable(&node) {
		// Node is deleted right now or has some issues, better to remove form RRs
		if err := r.cleanupBGPStatus(req, &node); err != nil {
			log.Errorf("Unable to cleanup label on %s because of %s", req.Name, err.Error())
			return nodeCleanupError, err
		}

		log.Infof("Label was removed from node %s time to re-reconcile", req.Name)
		return nodeCleaned, nil
	}

	listOptions := r.newNodeListOptions(&node)
	log.Debugf("List options are %v", listOptions)
	nodeList := corev1.NodeList{}
	if err := r.Client.List(context.Background(), &nodeList, &listOptions); err != nil {
		log.Errorf("Unable to list nodes because of %s", err.Error())
		return nodeListError, err
	}

	readyNodes, actualReadyNumber, nodes := r.collectNodeInfo(nodeList.Items)
	log.Infof("Nodes are ready %d", readyNodes)
	log.Infof("Actual number of healthy route reflector nodes are %d", actualReadyNumber)

	expectedNumber := r.calculateExpectedNumber(readyNodes)
	log.Infof("Expected number of route reflector nodes are %d", expectedNumber)

	for n := range nodes {
		if status, ok := routeReflectorsUnderOperation[n.GetUID()]; ok {
			// Node was under operation, better to revert it
			return r.revertKubernetesNode(status, n)
		}
	}

	for n, isReady := range nodes {
		if !isReady || expectedNumber == actualReadyNumber {
			continue
		}

		if diff := expectedNumber - actualReadyNumber; diff != 0 {
			if updated, err := r.updateBGPStatus(req, n, diff); err != nil {
				log.Errorf("Unable to update node %s because of %s", n.GetName(), err.Error())
				return nodeUpdateError, err
			} else if updated && diff > 0 {
				actualReadyNumber++
			} else if updated && diff < 0 {
				actualReadyNumber--
			}
		}
	}

	return finished, nil
}

func (r *RouteReflectorConfigReconciler) calculateExpectedNumber(readyNodes int) int {
	exp := math.Round(float64(readyNodes) * r.config.Ration)
	exp = math.Max(exp, float64(r.config.Min))
	exp = math.Min(exp, float64(r.config.Max))
	exp = math.Min(exp, float64(readyNodes))
	exp = math.RoundToEven(exp)
	return int(exp)
}

func (r *RouteReflectorConfigReconciler) collectNodeInfo(allNodes []corev1.Node) (readyNodes int, actualReadyNumber int, filtered map[*corev1.Node]bool) {
	filtered = map[*corev1.Node]bool{}

	for _, n := range allNodes {
		isReady := isNodeReady(&n)
		isSchedulable := isNodeSchedulable(&n)
		filtered[&n] = isReady && isSchedulable
		if isReady && isSchedulable {
			readyNodes++
			if isLabeled(n.GetLabels(), r.config.NodeLabelKey, r.config.NodeLabelValue) {
				actualReadyNumber++
			}
		}
	}

	return
}

func (r *RouteReflectorConfigReconciler) cleanupBGPStatus(req ctrl.Request, node *corev1.Node) error {
	delete(node.Labels, r.config.NodeLabelKey)
	if r.config.DataStoreType == calicoApiConfig.Kubernetes {
		delete(node.Annotations, routeReflectorClusterIDAnnotation)
	}

	log.Infof("Removing route reflector label from %s", node.GetName())
	if err := r.Client.Update(context.Background(), node); err != nil {
		log.Errorf("Unable to cleanup node %s because of %s", node.GetName(), err.Error())
		return err
	}

	if r.config.DataStoreType == calicoApiConfig.EtcdV3 {
		if err := r.updateRouteReflectorClusterID(req, node, ""); err != nil {
			log.Errorf("Unable to cleanup Calico node %s because of %s", node.GetName(), err.Error())
			return err
		}
	}

	return nil
}

func (r *RouteReflectorConfigReconciler) updateBGPStatus(req ctrl.Request, node *corev1.Node, diff int) (bool, error) {
	if labeled := isLabeled(node.GetLabels(), r.config.NodeLabelKey, r.config.NodeLabelValue); labeled && diff < 0 {
		return true, r.cleanupBGPStatus(req, node)
	} else if labeled || diff <= 0 {
		return false, nil
	}

	node.Labels[r.config.NodeLabelKey] = r.config.NodeLabelValue
	if r.config.DataStoreType == calicoApiConfig.Kubernetes {
		node.Annotations[routeReflectorClusterIDAnnotation] = r.config.ClusterID
	}

	log.Infof("Adding route reflector label to %s", node.GetName())
	if err := r.Client.Update(context.Background(), node); err != nil {
		log.Errorf("Unable to update node %s because of %s", node.GetName(), err.Error())
		return false, err
	}

	if r.config.DataStoreType == calicoApiConfig.EtcdV3 {
		if err := r.updateRouteReflectorClusterID(req, node, r.config.ClusterID); err != nil {
			log.Errorf("Unable to update Calico node %s because of %s", node.GetName(), err.Error())
			return false, err
		}
	}

	return true, nil
}

func (r *RouteReflectorConfigReconciler) updateRouteReflectorClusterID(req ctrl.Request, node *corev1.Node, clusterID string) error {
	routeReflectorsUnderOperation[node.GetUID()] = clusterID != ""

	log.Debugf("Fetching Calico node object of %s", node.GetName())
	calicoNodes, err := r.CalicoClient.Nodes().List(context.Background(), options.ListOptions{})
	if err != nil {
		log.Errorf("Unable to fetch Calico nodes %s because of %s", node.GetName(), err.Error())
		return err
	}

	var calicoNode *calicoApi.Node
	for _, cn := range calicoNodes.Items {
		if hostname, ok := cn.GetLabels()["kubernetes.io/hostname"]; ok && hostname == node.GetLabels()["kubernetes.io/hostname"] {
			log.Infof("Calico node found %s for %s-%s", cn.GetName(), node.GetNamespace(), node.GetName())
			calicoNode = &cn
			break
		}
	}
	if calicoNode == nil {
		err := fmt.Errorf("Unable to find Calico node for %s", node.GetName())
		log.Error(err.Error())
		return err
	}

	calicoNode.Spec.BGP.RouteReflectorClusterID = clusterID

	log.Infof("Adding route reflector cluster ID in %s to %s for %s", calicoNode.GetName(), clusterID, node.GetName())
	calicoNode, err = r.CalicoClient.Nodes().Update(context.Background(), calicoNode, options.SetOptions{})
	if err != nil {
		log.Errorf("Unable to update Calico node %s because of %s", node.GetName(), err.Error())
		return err
	}

	delete(routeReflectorsUnderOperation, node.GetUID())

	return nil
}

func (r *RouteReflectorConfigReconciler) newNodeListOptions(node *corev1.Node) client.ListOptions {
	listOptions := client.ListOptions{}
	if r.config.ZoneLabel != "" {
		if nodeZone, ok := node.GetLabels()[r.config.ZoneLabel]; ok {
			labels := client.MatchingLabels{r.config.ZoneLabel: nodeZone}
			labels.ApplyToList(&listOptions)
		} else {
			sel := labels.NewSelector()
			r, err := labels.NewRequirement(r.config.ZoneLabel, selection.DoesNotExist, nil)
			if err != nil {
				log.Fatalf("Unable to create anti label selector because of %s", err.Error())
			}
			sel = sel.Add(*r)
			listOptions.LabelSelector = sel
		}
	}

	return listOptions
}

func (r *RouteReflectorConfigReconciler) revertKubernetesNode(status bool, node *corev1.Node) (ctrl.Result, error) {
	if status {
		delete(node.Labels, r.config.NodeLabelKey)
		if r.config.DataStoreType == calicoApiConfig.Kubernetes {
			delete(node.Annotations, routeReflectorClusterIDAnnotation)
		}
	} else {
		node.Labels[r.config.NodeLabelKey] = r.config.NodeLabelValue
		if r.config.DataStoreType == calicoApiConfig.Kubernetes {
			node.Annotations[routeReflectorClusterIDAnnotation] = r.config.ClusterID
		}
	}

	log.Infof("Revert route reflector label on %s to %t", node.GetName(), !status)
	if err := r.Client.Update(context.Background(), node); err != nil && !errors.IsNotFound(err) {
		log.Errorf("Failed to revert node %s because of %s", node.GetName(), err.Error())
		return nodeRevertError, err
	}

	delete(routeReflectorsUnderOperation, node.GetUID())

	return nodeReverted, nil
}

func isNodeReady(node *corev1.Node) bool {
	for _, c := range node.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return true
		}
	}

	return false
}

func isNodeSchedulable(node *corev1.Node) bool {
	if node.Spec.Unschedulable == true {
		return false
	}
	return true
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
	log.Infof("Given configuration is: %v", config)
	r.config = config
	// WARNING !!! The reconcile implementation IS NOT THREAD SAFE and HAS STATE !!! PLease DO NOT inrease number of instances more than 1 !!!
	return ctrl.NewControllerManagedBy(mgr).
		WithEventFilter(eventFilter{}).
		For(&corev1.Node{}).
		Complete(r)
}
