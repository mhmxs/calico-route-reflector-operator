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
package datastores

import (
	"fmt"
	"hash/fnv"

	"github.com/mhmxs/calico-route-reflector-operator/topologies"
	uuid "github.com/satori/go.uuid"
	corev1 "k8s.io/api/core/v1"
)

const (
	routeReflectorClusterIDAnnotation = "projectcalico.org/RouteReflectorClusterID"
)

type KddDataStore struct {
	topology topologies.Topology
}

func (d *KddDataStore) RemoveRRStatus(node *corev1.Node) error {
	nodeLabelKey, _ := d.topology.GetNodeLabel()
	delete(node.Labels, nodeLabelKey)
	delete(node.Annotations, routeReflectorClusterIDAnnotation)

	return nil
}

func (d *KddDataStore) AddRRStatus(node *corev1.Node) error {
	labelKey, labelValue := d.topology.GetNodeLabel()
	h := fnv.New32a()
	h.Write([]byte(uuid.NewV4().String()))
	node.Labels[labelKey] = fmt.Sprintf("%s-%d", labelValue, h.Sum32())

	clusterID := d.topology.GetClusterID()
	node.Annotations[routeReflectorClusterIDAnnotation] = clusterID

	return nil
}

func NewKddDatastore(topology *topologies.Topology) Datastore {
	return &KddDataStore{
		topology: *topology,
	}
}
