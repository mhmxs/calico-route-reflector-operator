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
	"sort"
	"strings"

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

func (t *MultiTopology) GenerateBGPPeers(routeReflectors []corev1.Node, nodes map[*corev1.Node]bool, existingPeers *calicoApi.BGPPeerList) []calicoApi.BGPPeer {

	// Sorting RRs and Nodes for deterministic RR for Node selection
	sort.Slice(routeReflectors, func(i, j int) bool {
		return routeReflectors[i].GetName() < routeReflectors[j].GetName()
	})

	nodeList := []*corev1.Node{}
	for n := range nodes {
		nodeList = append(nodeList, n)
	}
	sort.Slice(nodeList, func(i, j int) bool {
		return nodeList[i].GetName() < nodeList[j].GetName()
	})

	bgpPeerConfigs := []calicoApi.BGPPeer{}

	rrConfig := findBGPPeer(existingPeers.Items, DefaultRouteReflectorMeshName)
	if rrConfig == nil {
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
	selector := fmt.Sprintf("has(%s)", t.NodeLabelKey)
	rrConfig.Spec = calicoApi.BGPPeerSpec{
		NodeSelector: selector,
		PeerSelector: selector,
	}

	bgpPeerConfigs = append(bgpPeerConfigs, *rrConfig)

	peers := int(math.Min(float64(len(routeReflectors)), 3))

	rrIndex := -1
	rrIndexPerZone := map[string]int{}

	for _, n := range nodeList {
		if t.IsRouteReflector(string(n.GetUID()), n.GetLabels()) {
			continue
		}

		routeReflectorsForNode := []corev1.Node{}

		if t.Config.ZoneLabel != "" {
			log.Debugf("Node's zone: %s", n.GetLabels()[t.Config.ZoneLabel])

			nodeZone := n.GetLabels()[t.Config.ZoneLabel]
			rrsSameZone := []corev1.Node{}

			for i, rr := range routeReflectors {
				rrZone := rr.GetLabels()[t.Config.ZoneLabel]
				log.Debugf("RR:%s's zone: %s", rr.Name, rrZone)
				if nodeZone == rrZone {
					if _, ok := rrIndexPerZone[nodeZone]; !ok {
						rrIndexPerZone[nodeZone] = -1
					}

					log.Debugf("Adding RR:%s to Same Zone RRs", rr.Name)
					rrsSameZone = append(rrsSameZone, routeReflectors[i])
				}
			}

			if len(rrsSameZone) > 0 {
				rrIndexPerZone[nodeZone]++
				if rrIndexPerZone[nodeZone] == len(rrsSameZone) {
					rrIndexPerZone[nodeZone] = 0
				}

				rr := rrsSameZone[rrIndexPerZone[nodeZone]]
				log.Debugf("Adding %s to RRs for Node:%s", rr.Name, n.Name)
				routeReflectorsForNode = append(routeReflectorsForNode, rr)
			}
		}

		for len(routeReflectorsForNode) < peers {
			rrIndex++
			if rrIndex == len(routeReflectors) {
				rrIndex = 0
			}

			rr := routeReflectors[rrIndex]
			if !isAlreadySelected(routeReflectorsForNode, rr) {
				log.Debugf("Adding %s to RRs of %s", rr.GetName(), n.GetName())
				routeReflectorsForNode = append(routeReflectorsForNode, rr)
			}
		}

		for _, rr := range routeReflectorsForNode {
			rrID := getRouteReflectorID(string(rr.GetUID()))
			name := fmt.Sprintf(DefaultRouteReflectorClientName+"-%s", rrID, n.GetUID())

			clientConfig := findBGPPeer(existingPeers.Items, name)
			if clientConfig == nil {
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
				// TODO make configurable
				NodeSelector: fmt.Sprintf("kubernetes.io/hostname=='%s'", n.GetLabels()["kubernetes.io/hostname"]),
				PeerSelector: fmt.Sprintf("%s=='%d'", t.NodeLabelKey, rrID),
			}

			log.Debugf("Adding %s BGPPeers to the refresh list", clientConfig.Name)
			bgpPeerConfigs = append(bgpPeerConfigs, *clientConfig)

		}
	}

	return bgpPeerConfigs
}

func isAlreadySelected(rrs []corev1.Node, r corev1.Node) bool {
	for i := range rrs {
		if rrs[i].GetName() == r.GetName() {
			return true
		}
	}
	return false
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
