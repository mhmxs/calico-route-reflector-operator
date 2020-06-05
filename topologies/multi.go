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

	"sigs.k8s.io/controller-runtime/pkg/client"
)

var routeReflectors = map[string]int{}

type MultiTopology struct {
	Config
	single SingleTopology
}

func (t *MultiTopology) IsLabeled(nodeID string, labels map[string]string) bool {
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
	return t.single.NewNodeListOptions(nodeLabels)
}

func (t *MultiTopology) CalculateExpectedNumber(readyNodes int) int {
	return t.single.CalculateExpectedNumber(readyNodes)
}

func (t *MultiTopology) getNodeLabel(nodeID string) string {
	return fmt.Sprintf("%s-%d", t.NodeLabelValue, getRouteReflectorID(nodeID))
}

func (t *MultiTopology) AddRRSuccess(nodeID string) {
	routeReflectors[nodeID] = getRouteReflectorID(nodeID)
}

func (t *MultiTopology) RemoveRRSuccess(nodeID string) {
	delete(routeReflectors, nodeID)
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
