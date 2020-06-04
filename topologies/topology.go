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

import "sigs.k8s.io/controller-runtime/pkg/client"

type Topology interface {
	IsLabeled(map[string]string) bool
	GetClusterID() string
	GetNodeLabel() (string, string)
	NewNodeListOptions(labels map[string]string) client.ListOptions
	CalculateExpectedNumber(int) int
}
