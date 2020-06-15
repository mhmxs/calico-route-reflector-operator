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
	"math"
	"math/rand"
	"strconv"

	calicoApi "github.com/projectcalico/libcalico-go/lib/apis/v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var routeReflectors = map[string]int{}

type MultiTopology struct {
	Config
	single SingleTopology
}

func (t *MultiTopology) IsRouteReflector(nodeID string, labels map[string]string) bool {
	_, ok := labels[t.NodeLabelKey]
	return ok
}

func (t *MultiTopology) GetClusterID(nodeID string) string {
	return fmt.Sprintf(t.ClusterID, getRouteReflectorID(nodeID))
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

	// TODO this could cause rebalancing very ofthen so has performance issues
	rrIndex := -1
	for n, isReady := range nodes {
		if !isReady || t.IsRouteReflector(string(n.GetUID()), n.GetLabels()) {
			continue
		}

		routeReflectorsForNode := []corev1.Node{}

		if t.Config.ZoneLabel != "" {
			nodeZone := n.GetLabels()[t.Config.ZoneLabel]
			rrsSameZone := []corev1.Node{}

			for i, rr := range routeReflectors {
				rrZone := rr.GetLabels()[t.Config.ZoneLabel]
				if nodeZone == rrZone {
					rrsSameZone = append(rrsSameZone, routeReflectors[i])
				}
			}

			if len(rrsSameZone) > 0 {
				rr := rrsSameZone[rand.Int31n(int32(len(rrsSameZone)-1))]
				routeReflectorsForNode = append(routeReflectorsForNode, rr)
			}
		}

		for len(routeReflectorsForNode) <= peers {
			rrIndex++
			if rrIndex == len(routeReflectors) {
				rrIndex = 0
			}

			rr := routeReflectors[rrIndex]

			for _, r := range routeReflectorsForNode {
				if r.GetName() == rr.GetName() {
					continue
				}
			}

			routeReflectorsForNode = append(routeReflectorsForNode, rr)
		}

		// TODO Would be better to create RR groups [1,2,3], [1,2,4], ... to decrease number of BGP peers
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

			bgpPeerConfigs = append(bgpPeerConfigs, *clientConfig)

		}
	}

	return bgpPeerConfigs
}

func (t *MultiTopology) AddRRSuccess(nodeID string) {
	routeReflectors[nodeID] = getRouteReflectorID(nodeID)
}

func (t *MultiTopology) RemoveRRSuccess(nodeID string) {
	delete(routeReflectors, nodeID)
}

func (t *MultiTopology) getNodeLabel(nodeID string) string {
	if t.NodeLabelValue == "" {
		return strconv.Itoa(getRouteReflectorID(nodeID))
	}
	return fmt.Sprintf("%s-%d", t.NodeLabelValue, getRouteReflectorID(nodeID))
}

// TODO this method has several performance issues, needs to fix later
func getRouteReflectorID(nodeID string) int {
	if existing, ok := routeReflectors[nodeID]; ok {
		return existing
	}

	existingIDs := map[int]bool{}
	for _, ID := range routeReflectors {
		existingIDs[ID] = true
	}

	for i := 0; ; i++ {
		if _, ok := existingIDs[i]; !ok {
			return i
		}
	}
}

func NewMultiTopology(config Config) Topology {
	return &MultiTopology{
		Config: config,
		single: SingleTopology{
			Config: config,
		},
	}
}
