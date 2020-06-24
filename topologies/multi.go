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
package topologies

import (
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"strings"
	"time"

	calicoApi "github.com/projectcalico/libcalico-go/lib/apis/v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/prometheus/common/log"
)

type MultiTopology struct {
	Config
	single SingleTopology
}

func (t *MultiTopology) IsRouteReflector(nodeID string, labels map[string]string) bool {
	_, ok := labels[t.NodeLabelKey]
	return ok
}

func (t *MultiTopology) IsMultiZone(nodes map[*corev1.Node]bool) bool {
	return t.single.IsMultiZone(nodes)
}

func (t *MultiTopology) GetRRsofNode(nodes map[*corev1.Node]bool, existingPeers *calicoApi.BGPPeerList, node *corev1.Node) (rrs map[*corev1.Node]bool) {
	return t.single.GetRRsofNode(nodes, existingPeers, node)
}

func (t *MultiTopology) GetClusterID(nodeID string, seed int64) string {
	count := strings.Count(t.ClusterID, "%d")
	parts := make([]interface{}, 0)

	rand1 := rand.New(rand.NewSource(int64(getRouteReflectorID(nodeID))))
	parts = append(parts, rand1.Int31n(254))

	rand2 := rand.New(rand.NewSource(seed))
	for len(parts) < count {
		parts = append(parts, rand2.Int31n(254))
	}

	return fmt.Sprintf(t.ClusterID, parts...)
}

func (t *MultiTopology) GetNodeLabel(nodeID string) (string, string) {
	return t.NodeLabelKey, t.getNodeLabel(nodeID)
}

func (t *MultiTopology) NewNodeListOptions(nodeLabels map[string]string) client.ListOptions {
	return client.ListOptions{}
}

func (t *MultiTopology) CalculateExpectedNumber(readyNodes int) int {
	return t.single.CalculateExpectedNumber(readyNodes)
}

func (t *MultiTopology) GenerateBGPPeers(routeReflectors []corev1.Node, nodes map[*corev1.Node]bool, existingPeers *calicoApi.BGPPeerList) (toRefresh []calicoApi.BGPPeer, toDelete []calicoApi.BGPPeer) {
	toKeep := map[string]bool{}

	rrConfig := findBGPPeer(existingPeers.Items, DefaultRouteReflectorMeshName)
	selector := fmt.Sprintf("has(%s)", t.NodeLabelKey)

	if rrConfig == nil {
		log.Debugf("Creating new RR full-mesh BGPPeers: %s", DefaultRouteReflectorMeshName)
		rrConfig = &calicoApi.BGPPeer{
			TypeMeta: metav1.TypeMeta{
				Kind:       calicoApi.KindBGPPeer,
				APIVersion: calicoApi.GroupVersionCurrent,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: DefaultRouteReflectorMeshName,
			},
		}
	}
	if rrConfig.Spec.NodeSelector != selector || rrConfig.Spec.PeerSelector != selector {
		rrConfig.Spec = calicoApi.BGPPeerSpec{
			NodeSelector: selector,
			PeerSelector: selector,
		}

		toRefresh = append(toRefresh, *rrConfig)
	}
	toKeep[rrConfig.Name] = true

	rrsPerNode := int(math.Min(float64(len(routeReflectors)), 3))
	isMultiZone := t.IsMultiZone(nodes)

	rand.Seed(time.Now().UnixNano())
	for n := range nodes {
		if t.IsRouteReflector(string(n.GetUID()), n.GetLabels()) {
			continue
		}

		routeReflectorsForNode := t.GetRRsofNode(nodes, existingPeers, n)
		log.Debugf("Found %v RRs for Node:%s", len(routeReflectorsForNode), n.Name)

		// Select the first two RRs from different zones in Multi Zone clusters
		// These aren't necessarily from the same cluster as the Node
		if isMultiZone {

			var firstRR *corev1.Node
			if len(routeReflectorsForNode) == 0 {
				firstRR = &routeReflectors[rand.Intn(len(routeReflectors))]
				log.Debugf("Selecting 1st RR:%s for Node:%s", firstRR.Name, n.Name)
				routeReflectorsForNode[firstRR] = true
			} else if len(routeReflectorsForNode) == 1 {
				// Get the single RR pointer
				for k := range routeReflectorsForNode {
					firstRR = k
					log.Debugf("Found single RR:%s of Node:%s", firstRR.Name, n.Name)
					break
				}
			}

			for 1 < len(routeReflectors) && len(routeReflectorsForNode) < 2 {
				rr := &routeReflectors[rand.Intn(len(routeReflectors))]
				if rr.GetLabels()[t.Config.ZoneLabel] != firstRR.GetLabels()[t.Config.ZoneLabel] {
					log.Debugf("Selecting 2nd RR:%s for Node:%s", rr.Name, n.Name)
					routeReflectorsForNode[rr] = true
				}
			}
		}

		// Select the remaning RRs randomly for MZR clusters
		// For single zone clusters, select all RRs randomly
		for len(routeReflectorsForNode) < rrsPerNode {
			rr := &routeReflectors[rand.Intn(len(routeReflectors))]
			if _, ok := routeReflectorsForNode[rr]; !ok {
				log.Debugf("Selecting Nth RRs:%s for Node:%s", rr.Name, n.Name)
				routeReflectorsForNode[rr] = true
			}
		}

		// This way the selected RRs will be from multiple zones,
		// but not necessarily from the same zone as the Node.
		for rr := range routeReflectorsForNode {
			rrID := getRouteReflectorID(string(rr.GetUID()))
			name := fmt.Sprintf(DefaultRouteReflectorClientName+"-%s", rrID, n.GetUID())

			// TODO make configurable
			newNodeSelector := fmt.Sprintf("kubernetes.io/hostname=='%s'", n.GetLabels()["kubernetes.io/hostname"])
			newPeerSelector := fmt.Sprintf("%s=='%d'", t.NodeLabelKey, rrID)

			clientConfig := findBGPPeer(existingPeers.Items, name)

			// Skip BGPPeers refresh if nothing changed
			if clientConfig != nil && clientConfig.Spec.NodeSelector == newNodeSelector && clientConfig.Spec.PeerSelector == newPeerSelector {
				toKeep[clientConfig.Name] = true
				continue
			}

			if clientConfig == nil {
				log.Debugf("New BGPPeers: %s", name)
				clientConfig = &calicoApi.BGPPeer{
					TypeMeta: metav1.TypeMeta{
						Kind:       calicoApi.KindBGPPeer,
						APIVersion: calicoApi.GroupVersionCurrent,
					},
					ObjectMeta: metav1.ObjectMeta{
						Name: name,
					},
				}
			}

			clientConfig.Spec = calicoApi.BGPPeerSpec{
				NodeSelector: newNodeSelector,
				PeerSelector: newPeerSelector,
			}

			log.Debugf("Adding %s to the BGPPeers refresh list", clientConfig.Name)
			toRefresh = append(toRefresh, *clientConfig)
		}
	}

	for i := range existingPeers.Items {
		if _, ok := toKeep[existingPeers.Items[i].Name]; !ok && findBGPPeer(toRefresh, existingPeers.Items[i].Name) == nil {
			log.Debugf("Adding %s to the BGPPeers delete list", existingPeers.Items[i].Name)
			toDelete = append(toDelete, existingPeers.Items[i])
		}
	}

	return
}

func (t *MultiTopology) getNodeLabel(nodeID string) string {
	if t.NodeLabelValue == "" {
		return fmt.Sprintf("%d", getRouteReflectorID(nodeID))
	}
	return fmt.Sprintf("%s-%d", t.NodeLabelValue, getRouteReflectorID(nodeID))
}

func getRouteReflectorID(nodeID string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(nodeID))
	return h.Sum32()
}

func NewMultiTopology(config Config) Topology {
	t := &MultiTopology{
		Config: config,
		single: SingleTopology{
			Config: config,
		},
	}

	t.ClusterID = strings.Replace(t.ClusterID, ".0", ".%d", -1)

	return t
}
