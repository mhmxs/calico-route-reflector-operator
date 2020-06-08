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
	label, ok := labels[t.NodeLabelKey]
	return ok && label == t.getNodeLabel(nodeID)
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

	for n, isReady := range nodes {
		if !isReady {
			continue
		}

		if t.IsRouteReflector(string(n.GetUID()), n.GetLabels()) {
			selector := fmt.Sprintf("has(%s)", t.NodeLabelKey)
			rrConfig := findBGPPeer(DefaultRouteReflectorMeshName, existingPeers)
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
			rrConfig.Spec = calicoApi.BGPPeerSpec{
				NodeSelector: "!" + selector,
				PeerSelector: selector,
			}

			bgpPeerConfigs = append(bgpPeerConfigs, *rrConfig)

			continue
		}

		// TODO Do it in a more sophisticaged way
		for i := 1; i <= 3; i++ {
			rrID := rand.Intn(len(routeReflectors))
			name := fmt.Sprintf(DefaultRouteReflectorClientName, rrID)
			rr := getRouteReflectorID(string(routeReflectors[rrID].GetUID()))

			clientConfig := findBGPPeer(DefaultRouteReflectorMeshName, existingPeers)
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
				PeerSelector: fmt.Sprintf("%s=='%d'", t.NodeLabelKey, rr),
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
