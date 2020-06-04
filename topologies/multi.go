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

// import (
// 	"fmt"
// 	"math"
// 	"strconv"
// )

// // MultiTopology represent a multi cluster topology
// type MultiTopology struct {
// 	nodeLabelKey string
// }

// func (t *MultiTopology) IsLabeled(labels map[string]string, key, value string) bool {
// 	_, ok := labels[key]
// 	return ok
// }

// func (t *MultiTopology) GetClusterID() string {
// 	nodeID := int(math.Abs(float64(diff)))
// 	return fmt.Sprintf("224.0.0.%d", nodeID)
// }

// func (t *MultiTopology) GetNodeLabel() (string, string) {
// 	return t.nodeLabelKey, strconv.Itoa(nodeID)
// }

// // NewTopology creates a new topology instance
// func NewTopology(nodeLabelKey string) *MultiTopology {
// 	return &MultiTopology{
// 		nodeLabelKey: nodeLabelKey,
// 	}
// }
