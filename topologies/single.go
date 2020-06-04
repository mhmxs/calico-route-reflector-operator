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

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SingleTopology struct {
	Config
}

func (t *SingleTopology) IsLabeled(_ string, labels map[string]string) bool {
	label, ok := labels[t.NodeLabelKey]
	return ok && label == t.NodeLabelValue
}

func (t *SingleTopology) GetClusterID(string) string {
	return fmt.Sprintf(t.ClusterID, 1)
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

func (t *SingleTopology) CalculateExpectedNumber(readyNodes int) int {
	exp := math.Round(float64(readyNodes) * t.Ration)
	exp = math.Max(exp, float64(t.Min))
	exp = math.Min(exp, float64(t.Max))
	exp = math.Min(exp, float64(readyNodes))
	exp = math.RoundToEven(exp)
	return int(exp)
}

func (t *SingleTopology) AddRRSuccess(string) {
}

func (t *SingleTopology) RemoveRRSuccess(string) {
}

func NewSingleTopology(config Config) Topology {
	return &SingleTopology{
		Config: config,
	}
}
