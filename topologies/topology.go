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
	calicoApi "github.com/projectcalico/libcalico-go/lib/apis/v3"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// TODO Make it configurable
	DefaultRouteReflectorMeshName   = "rrs-to-rrs"
	DefaultRouteReflectorClientName = "peer-to-rrs-%d"
)

type Topology interface {
	IsRouteReflector(string, map[string]string) bool
	GetClusterID(string, int64) string
	GetNodeLabel(string) (string, string)
	NewNodeListOptions(labels map[string]string) client.ListOptions
	CalculateExpectedNumber(int) int
	GenerateBGPPeers([]corev1.Node, map[*corev1.Node]bool, *calicoApi.BGPPeerList) []calicoApi.BGPPeer
}

type Config struct {
	NodeLabelKey   string
	NodeLabelValue string
	ZoneLabel      string
	ClusterID      string
	Min            int
	Max            int
	Ration         float64
}

func findBGPPeer(peers []calicoApi.BGPPeer, name string) *calicoApi.BGPPeer {
	for _, p := range peers {
		if p.GetName() == name {
			return &p
		}
	}

	return nil
}
