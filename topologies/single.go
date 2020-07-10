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
	"sort"

	calicoApi "github.com/projectcalico/libcalico-go/lib/apis/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SingleTopology struct {
	Config
}

func (t *SingleTopology) IsRouteReflector(_ string, labels map[string]string) bool {
	label, ok := labels[t.NodeLabelKey]
	return ok && label == t.NodeLabelValue
}

func (t *SingleTopology) GetClusterID(string, int64) string {
	return t.ClusterID
}

func (t *SingleTopology) GetNodeLabel(string) (string, string) {
	return t.NodeLabelKey, t.NodeLabelValue
}

func (t *SingleTopology) NewNodeListOptions(nodeLabels map[string]string) client.ListOptions {
	listOptions := client.ListOptions{}
	if t.ZoneLabel != "" {
		if nodeZone, ok := nodeLabels[t.ZoneLabel]; ok {
			labels := client.MatchingLabels{t.ZoneLabel: nodeZone}
			labels.ApplyToList(&listOptions)
		} else {
			sel := labels.NewSelector()
			r, err := labels.NewRequirement(t.ZoneLabel, selection.DoesNotExist, nil)
			if err != nil {
				panic(fmt.Sprintf("Unable to create anti label selector because of %s", err.Error()))
			}
			sel = sel.Add(*r)
			listOptions.LabelSelector = sel
		}
	}

	return listOptions
}

func (t *SingleTopology) GetRouteReflectorStatuses(nodes map[*corev1.Node]bool) []RouteReflectorStatus {
	zones := map[string]bool{}
	sorted := []*corev1.Node{}
	for n := range nodes {
		zones[n.GetLabels()[t.ZoneLabel]] = true
		sorted = append(sorted, n)
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].GetCreationTimestamp().UnixNano() < sorted[j].GetCreationTimestamp().UnixNano()
	})

	readyNodes, actualRRs := collectNodeInfo(t, nodes)

	status := RouteReflectorStatus{
		ActualRRs:   actualRRs,
		ExpectedRRs: t.calculateExpectedNumber(readyNodes),
		Nodes:       sorted,
	}

	for z := range zones {
		status.Zones = append(status.Zones, z)
	}

	return []RouteReflectorStatus{status}
}

func (t *SingleTopology) GenerateBGPPeers(_ []corev1.Node, _ map[*corev1.Node]bool, existingPeers *calicoApi.BGPPeerList) ([]calicoApi.BGPPeer, []calicoApi.BGPPeer) {
	bgpPeerConfigs := []calicoApi.BGPPeer{}

	rrConfig := findBGPPeer(existingPeers.Items, DefaultRouteReflectorMeshName)
	if rrConfig == nil {
		rrConfig = generateBGPPeerStub(DefaultRouteReflectorMeshName)
	}
	selector := fmt.Sprintf("has(%s)", t.NodeLabelKey)
	rrConfig.Spec = calicoApi.BGPPeerSpec{
		NodeSelector: selector,
		PeerSelector: selector,
	}

	bgpPeerConfigs = append(bgpPeerConfigs, *rrConfig)

	clientConfigName := fmt.Sprintf(DefaultRouteReflectorClientName, 1)

	clientConfig := findBGPPeer(existingPeers.Items, clientConfigName)
	if clientConfig == nil {
		clientConfig = generateBGPPeerStub(clientConfigName)
	}
	clientConfig.Spec = calicoApi.BGPPeerSpec{
		NodeSelector: "!" + selector,
		PeerSelector: selector,
	}

	bgpPeerConfigs = append(bgpPeerConfigs, *clientConfig)

	return bgpPeerConfigs, []calicoApi.BGPPeer{}
}

func (t *SingleTopology) calculateExpectedNumber(readyNodes int) int {
	exp := math.Ceil(float64(readyNodes) * t.Ration)
	exp = math.Max(exp, float64(t.Min))
	exp = math.Min(exp, float64(t.Max))
	exp = math.Min(exp, float64(readyNodes))
	exp = math.RoundToEven(exp)
	return int(exp)
}

func NewSingleTopology(config Config) Topology {
	return &SingleTopology{
		Config: config,
	}
}
