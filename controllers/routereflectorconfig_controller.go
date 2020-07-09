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
	"time"

	"github.com/go-logr/logr"
	"github.com/mhmxs/calico-route-reflector-operator/bgppeer"
	"github.com/mhmxs/calico-route-reflector-operator/datastores"
	"github.com/mhmxs/calico-route-reflector-operator/topologies"
	calicoApi "github.com/projectcalico/libcalico-go/lib/apis/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	calicoClient "github.com/projectcalico/libcalico-go/lib/clientv3"

	"github.com/prometheus/common/log"
)

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
	nodeNotFound    = ctrl.Result{}
	nodeCleaned     = ctrl.Result{Requeue: true}
	nodeReverted    = ctrl.Result{Requeue: true}
	bgpPeersUpdated = ctrl.Result{Requeue: true, RequeueAfter: time.Second}
	finished        = ctrl.Result{}

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

var routeReflectorsUnderOperation = map[types.UID]bool{}

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

	res, err := r.reconcile(req)

	if res.RequeueAfter != 0 {
		log.Infof("Give some time to Calico and Bird to establish new topology: %d sec", int(res.RequeueAfter.Seconds()))
		time.Sleep(res.RequeueAfter)
		res.RequeueAfter = 0
	}

	return res, err
}

func (r *RouteReflectorConfigReconciler) reconcile(req ctrl.Request) (ctrl.Result, error) {
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

	nodes := r.collectNodeInfo(nodeList.Items)

	if res, err := r.revertFailedModification(req, nodes); isRequeNeeded(res, err) {
		return res, err
	} else if res, err := r.updateNodeLabels(req, nodes); isRequeNeeded(res, err) {
		return res, err
	} else if res, err := r.updateBGPTopology(req, nodes); isRequeNeeded(res, err) {
		return res, err
	}

	return finished, nil
}

func (r *RouteReflectorConfigReconciler) revertFailedModification(req ctrl.Request, nodes map[*corev1.Node]bool) (ctrl.Result, error) {
	for n := range nodes {
		status, ok := routeReflectorsUnderOperation[n.GetUID()]
		if !ok {
			continue
		}
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

	return ctrl.Result{}, nil
}

func (r *RouteReflectorConfigReconciler) updateNodeLabels(req ctrl.Request, nodes map[*corev1.Node]bool) (ctrl.Result, error) {
	missingRouteReflectors := 0

	for _, status := range r.Topology.GetRouteReflectorStatuses(nodes) {
		status.ExpectedRRs += missingRouteReflectors
		log.Infof("Route refletor status in zone(s) %v (actual/expected) = %d/%d", status.Zones, status.ActualRRs, status.ExpectedRRs)

		for _, n := range status.Nodes {
			isReady := nodes[n]
			if !isReady {
				continue
			} else if status.ExpectedRRs == status.ActualRRs {
				break
			}

			if diff := status.ExpectedRRs - status.ActualRRs; diff != 0 {
				if updated, err := r.updateRRStatus(n, diff); err != nil {
					log.Errorf("Unable to update node %s because of %s", n.GetName(), err.Error())
					return nodeUpdateError, err
				} else if updated && diff > 0 {
					status.ActualRRs++
				} else if updated && diff < 0 {
					status.ActualRRs--
				}
			}
		}

		if status.ExpectedRRs != status.ActualRRs {
			log.Infof("Actual number %d is different than expected %d", status.ActualRRs, status.ExpectedRRs)
			missingRouteReflectors = status.ExpectedRRs - status.ActualRRs
		}
	}

	if missingRouteReflectors != 0 {
		log.Errorf("Actual number is different than expected, missing: %d", missingRouteReflectors)
	}

	return ctrl.Result{}, nil
}

func (r *RouteReflectorConfigReconciler) updateBGPTopology(req ctrl.Request, nodes map[*corev1.Node]bool) (ctrl.Result, error) {
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

	toRefresh, toRemove := r.Topology.GenerateBGPPeers(rrList.Items, nodes, existingBGPPeers)

	log.Infof("Number of BGPPeers to refresh: %v", len(toRefresh))
	log.Debugf("To refresh BGPeers are: %v", toRefresh)
	log.Infof("Number of BGPPeers to delete: %v", len(toRemove))
	log.Debugf("To delete BGPeers are: %v", toRemove)

	for _, bp := range toRefresh {
		log.Infof("Saving %s BGPPeer", bp.Name)
		if err := r.BGPPeer.SaveBGPPeer(&bp); err != nil {
			log.Errorf("Unable to save BGPPeer because of %s", err.Error())
			return bgpPeerError, err
		}
	}

	// Give some time to Calico to establish new connections before deleting old ones
	if len(toRefresh) > 0 {
		return bgpPeersUpdated, nil
	}

	for _, p := range toRemove {
		log.Debugf("Removing BGPPeer: %s", p.GetName())
		if err := r.BGPPeer.RemoveBGPPeer(&p); err != nil {
			log.Errorf("Unable to remove BGPPeer because of %s", err.Error())
			return bgpPeerRemoveError, err
		}
	}

	return ctrl.Result{}, nil
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
	labeled := r.Topology.IsRouteReflector(string(node.GetUID()), node.GetLabels())
	if labeled && diff > 0 || !labeled && diff < 0 {
		return false, nil
	}

	routeReflectorsUnderOperation[node.GetUID()] = true

	if diff < 0 {
		if err := r.Datastore.RemoveRRStatus(node); err != nil {
			log.Errorf("Unable to delete RR status %s because of %s", node.GetName(), err.Error())
			return false, err
		}
		log.Infof("Removing route reflector label to %s", node.GetName())
	} else {
		if err := r.Datastore.AddRRStatus(node); err != nil {
			log.Errorf("Unable to add RR status %s because of %s", node.GetName(), err.Error())
			return false, err
		}
		log.Infof("Adding route reflector label to %s", node.GetName())
	}

	if err := r.Client.Update(context.Background(), node); err != nil {
		log.Errorf("Unable to update node %s because of %s", node.GetName(), err.Error())
		return false, err
	}

	delete(routeReflectorsUnderOperation, node.GetUID())

	return true, nil
}

func (r *RouteReflectorConfigReconciler) collectNodeInfo(allNodes []corev1.Node) (filtered map[*corev1.Node]bool) {
	filtered = map[*corev1.Node]bool{}

	for i, n := range allNodes {
		filtered[&allNodes[i]] = isNodeReady(&n) && isNodeSchedulable(&n) && r.isNodeCompatible(&n)
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

func isRequeNeeded(res ctrl.Result, err error) bool {
	return res.Requeue || res.RequeueAfter != 0 || err != nil
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

func (r *RouteReflectorConfigReconciler) SetupWithManager(mgr ctrl.Manager, waitTimeout time.Duration) error {
	bgpPeersUpdated.RequeueAfter = waitTimeout

	// WARNING !!! The reconcile implementation IS NOT THREAD SAFE and HAS STATE !!! PLease DO NOT inrease number of instances more than 1 !!!
	return ctrl.NewControllerManagedBy(mgr).
		WithEventFilter(eventFilter{}).
		For(&corev1.Node{}).
		Complete(r)
}
