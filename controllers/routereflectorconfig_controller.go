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

	"github.com/go-logr/logr"
	"github.com/mhmxs/calico-route-reflector-operator/bgppeer"
	"github.com/mhmxs/calico-route-reflector-operator/datastores"
	"github.com/mhmxs/calico-route-reflector-operator/topologies"
	calicoApi "github.com/projectcalico/libcalico-go/lib/apis/v3"
	"github.com/prometheus/common/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	calicoClient "github.com/projectcalico/libcalico-go/lib/clientv3"
)

var routeReflectorsUnderOperation = map[types.UID]bool{}

var notReadyTaints = map[string]bool{
	"node.kubernetes.io/not-ready":                   false,
	"node.kubernetes.io/unreachable":                 false,
	"node.kubernetes.io/out-of-disk":                 false,
	"node.kubernetes.io/memory-pressure":             false,
	"node.kubernetes.io/disk-pressure":               false,
	"node.kubernetes.io/network-unavailable":         false,
	"node.kubernetes.io/unschedulable":               false,
	"node.cloudprovider.kubernetes.io/uninitialized": false,
}

var (
	nodeNotFound = ctrl.Result{}
	nodeCleaned  = ctrl.Result{Requeue: true}
	nodeReverted = ctrl.Result{Requeue: true}
	finished     = ctrl.Result{}

	nodeGetError          = ctrl.Result{}
	nodeCleanupError      = ctrl.Result{}
	nodeListError         = ctrl.Result{}
	nodeRevertError       = ctrl.Result{}
	nodeRevertUpdateError = ctrl.Result{}
	nodeUpdateError       = ctrl.Result{}
	rrListError           = ctrl.Result{}
	rrPeerListError       = ctrl.Result{}
	bgpPeerError          = ctrl.Result{}
	bgpPeerRemoveError    = ctrl.Result{}
)

// RouteReflectorConfigReconciler reconciles a RouteReflectorConfig object
type RouteReflectorConfigReconciler struct {
	client.Client
	CalicoClient       calicoClient.Interface
	Log                logr.Logger
	Scheme             *runtime.Scheme
	NodeLabelKey       string
	IncompatibleLabels map[string]*string
	Topology           topologies.Topology
	Datastore          datastores.Datastore
	BGPPeer            bgppeer.BGPPeer
}

type reconcileImplClient interface {
	Get(context.Context, client.ObjectKey, runtime.Object) error
	Update(context.Context, runtime.Object, ...client.UpdateOption) error
	List(context.Context, runtime.Object, ...client.ListOption) error
}

// +kubebuilder:rbac:groups=route-reflector.calico-route-reflector-operator.mhmxs.github.com,resources=routereflectorconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=route-reflector.calico-route-reflector-operator.mhmxs.github.com,resources=routereflectorconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;update;watch
// +kubebuilder:rbac:groups="crd.projectcalico.org",resources=bgppeers,verbs=get;list;create;update;delete

func (r *RouteReflectorConfigReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("routereflectorconfig", req.Name)

	currentNode := corev1.Node{}
	if err := r.Client.Get(context.Background(), req.NamespacedName, &currentNode); err != nil && !errors.IsNotFound(err) {
		log.Errorf("Unable to fetch node %s because of %s", req.Name, err.Error())
		return nodeGetError, err
	} else if errors.IsNotFound(err) {
		log.Debugf("Node not found %s", req.Name)
		return nodeNotFound, nil
	} else if err == nil && r.Topology.IsRouteReflector(string(currentNode.GetUID()), currentNode.GetLabels()) && currentNode.GetDeletionTimestamp() != nil ||
		!isNodeReady(&currentNode) || !isNodeSchedulable(&currentNode) || !r.isNodeCompatible(&currentNode) {
		// Node is deleted right now or has some issues, better to remove form RRs
		if err := r.removeRRStatus(req, &currentNode); err != nil {
			log.Errorf("Unable to cleanup label on %s because of %s", req.Name, err.Error())
			return nodeCleanupError, err
		}

		log.Infof("Label was removed from node %s time to re-reconcile", req.Name)
		return nodeCleaned, nil
	}

	listOptions := r.Topology.NewNodeListOptions(currentNode.GetLabels())
	log.Debugf("List options are %v", listOptions)
	nodeList := corev1.NodeList{}
	if err := r.Client.List(context.Background(), &nodeList, &listOptions); err != nil {
		log.Errorf("Unable to list nodes because of %s", err.Error())
		return nodeListError, err
	}
	log.Debugf("Total number of nodes %d", len(nodeList.Items))

	readyNodes, actualRRNumber, nodes := r.collectNodeInfo(nodeList.Items)
	log.Infof("Nodes are ready %d", readyNodes)
	log.Infof("Actual number of healthy route reflector nodes are %d", actualRRNumber)

	expectedRRNumber := r.Topology.CalculateExpectedNumber(readyNodes)
	log.Infof("Expected number of route reflector nodes are %d", expectedRRNumber)

	for n, isReady := range nodes {
		if status, ok := routeReflectorsUnderOperation[n.GetUID()]; ok {
			// Node was under operation, better to revert it
			var err error
			if status {
				err = r.Datastore.RemoveRRStatus(n)
			} else {
				err = r.Datastore.AddRRStatus(n)
			}
			if err != nil {
				log.Errorf("Failed to revert node %s because of %s", n.GetName(), err.Error())
				return nodeRevertError, err
			}

			log.Infof("Revert route reflector label on %s to %t", n.GetName(), !status)
			if err := r.Client.Update(context.Background(), n); err != nil && !errors.IsNotFound(err) {
				log.Errorf("Failed to revert update node %s because of %s", n.GetName(), err.Error())
				return nodeRevertUpdateError, err
			}

			delete(routeReflectorsUnderOperation, n.GetUID())

			return nodeReverted, nil
		}

		if !isReady || expectedRRNumber == actualRRNumber {
			continue
		}

		if diff := expectedRRNumber - actualRRNumber; diff != 0 {
			if updated, err := r.updateRRStatus(n, diff); err != nil {
				log.Errorf("Unable to update node %s because of %s", n.GetName(), err.Error())
				return nodeUpdateError, err
			} else if updated && diff > 0 {
				actualRRNumber++
			} else if updated && diff < 0 {
				actualRRNumber--
			}
		}
	}

	if expectedRRNumber != actualRRNumber {
		log.Errorf("Actual number %d is different than expected %d", actualRRNumber, expectedRRNumber)
	}

	rrLables := client.HasLabels{r.NodeLabelKey}
	rrListOptions := client.ListOptions{}
	rrLables.ApplyToList(&rrListOptions)
	log.Debugf("RR list options are %v", rrListOptions)

	rrList := corev1.NodeList{}
	if err := r.Client.List(context.Background(), &rrList, &rrListOptions); err != nil {
		log.Errorf("Unable to list route reflectors because of %s", err.Error())
		return rrListError, err
	}
	log.Debugf("Route reflectors are: %v", rrList.Items)

	existingBGPPeers, err := r.BGPPeer.ListBGPPeers()
	if err != nil {
		log.Errorf("Unable to list BGP peers because of %s", err.Error())
		return rrPeerListError, err
	}

	log.Debugf("Existing BGPeers are: %v", existingBGPPeers.Items)

	toRefresh, toDelete := r.Topology.GenerateBGPPeers(rrList.Items, nodes, existingBGPPeers)

	log.Infof("BGPPeers to refresh: %v", len(toRefresh))
	// log.Debugf("To refresh BGPeers are: %v", toRefresh)
	log.Infof("BGPPeers to delete: %v", len(toDelete))
	// log.Debugf("To delete BGPeers are: %v", toDelete)

	for _, bp := range toRefresh {
		log.Infof("Saving %s BGPPeer", bp.Name)
		if err := r.BGPPeer.SaveBGPPeer(&bp); err != nil {
			log.Errorf("Unable to save BGPPeer because of %s", err.Error())
			return bgpPeerError, err
		}
	}

	for _, p := range toDelete {
		log.Debugf("Removing BGPPeer: %s", p.GetName())
		if err := r.BGPPeer.RemoveBGPPeer(&p); err != nil {
			log.Errorf("Unable to remove BGPPeer because of %s", err.Error())
			return bgpPeerRemoveError, err
		}
	}

	return finished, nil
}

func (r *RouteReflectorConfigReconciler) removeRRStatus(req ctrl.Request, node *corev1.Node) error {
	routeReflectorsUnderOperation[node.GetUID()] = false

	if err := r.Datastore.RemoveRRStatus(node); err != nil {
		log.Errorf("Unable to cleanup RR status %s because of %s", node.GetName(), err.Error())
		return err
	}

	log.Infof("Removing route reflector label from %s", node.GetName())
	if err := r.Client.Update(context.Background(), node); err != nil {
		log.Errorf("Unable to cleanup node %s because of %s", node.GetName(), err.Error())
		return err
	}

	delete(routeReflectorsUnderOperation, node.GetUID())

	return nil
}

func (r *RouteReflectorConfigReconciler) updateRRStatus(node *corev1.Node, diff int) (bool, error) {
	if labeled := r.Topology.IsRouteReflector(string(node.GetUID()), node.GetLabels()); labeled && diff < 0 {
		return true, r.Datastore.RemoveRRStatus(node)
	} else if labeled || diff <= 0 {
		return false, nil
	}

	routeReflectorsUnderOperation[node.GetUID()] = true

	if err := r.Datastore.AddRRStatus(node); err != nil {
		log.Errorf("Unable to add RR status %s because of %s", node.GetName(), err.Error())
		return false, err
	}

	log.Infof("Adding route reflector label to %s", node.GetName())
	if err := r.Client.Update(context.Background(), node); err != nil {
		log.Errorf("Unable to update node %s because of %s", node.GetName(), err.Error())
		return false, err
	}

	delete(routeReflectorsUnderOperation, node.GetUID())

	return true, nil
}

func (r *RouteReflectorConfigReconciler) collectNodeInfo(allNodes []corev1.Node) (readyNodes int, actualReadyNumber int, filtered map[*corev1.Node]bool) {
	filtered = map[*corev1.Node]bool{}

	for i, n := range allNodes {
		isOK := isNodeReady(&n) && isNodeSchedulable(&n) && r.isNodeCompatible(&n)

		filtered[&allNodes[i]] = isOK
		if isOK {
			readyNodes++
			if r.Topology.IsRouteReflector(string(n.GetUID()), n.GetLabels()) {
				actualReadyNumber++
			}
		}
	}

	return
}

func (r *RouteReflectorConfigReconciler) isNodeCompatible(node *corev1.Node) bool {
	for k, v := range node.GetLabels() {
		if iv, ok := r.IncompatibleLabels[k]; ok && (iv == nil || *iv == v) {
			return false
		}
	}

	return true
}

func isNodeReady(node *corev1.Node) bool {
	for _, c := range node.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == "True"
		}
	}

	return false
}

func isNodeSchedulable(node *corev1.Node) bool {
	if node.Spec.Unschedulable == true {
		return false
	}
	for _, taint := range node.Spec.Taints {
		if _, ok := notReadyTaints[taint.Key]; ok {
			return false
		}
	}

	return true
}

func findBGPPeer(peers []calicoApi.BGPPeer, name string) bool {
	for _, p := range peers {
		if p.GetName() == name {
			return true
		}
	}

	return false
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

func (r *RouteReflectorConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// WARNING !!! The reconcile implementation IS NOT THREAD SAFE and HAS STATE !!! PLease DO NOT inrease number of instances more than 1 !!!
	return ctrl.NewControllerManagedBy(mgr).
		WithEventFilter(eventFilter{}).
		For(&corev1.Node{}).
		Complete(r)
}
