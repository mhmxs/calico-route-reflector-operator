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
	nodeLabelKey   string
	nodeLabelValue string
	zoneLabel      string
	clusterID      string
	min            int
	max            int
	ration         float64
}

func (t *SingleTopology) IsLabeled(labels map[string]string) bool {
	label, ok := labels[t.nodeLabelKey]
	return ok && label == t.nodeLabelValue
}

func (t *SingleTopology) GetClusterID() string {
	return fmt.Sprintf(t.clusterID, 1)
}

func (t *SingleTopology) GetNodeLabel() (string, string) {
	return t.nodeLabelKey, t.nodeLabelValue
}

func (t *SingleTopology) NewNodeListOptions(nodeLabels map[string]string) client.ListOptions {
	listOptions := client.ListOptions{}
	if t.zoneLabel != "" {
		if nodeZone, ok := nodeLabels[t.zoneLabel]; ok {
			labels := client.MatchingLabels{t.zoneLabel: nodeZone}
			labels.ApplyToList(&listOptions)
		} else {
			sel := labels.NewSelector()
			r, err := labels.NewRequirement(t.zoneLabel, selection.DoesNotExist, nil)
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
	exp := math.Round(float64(readyNodes) * t.ration)
	exp = math.Max(exp, float64(t.min))
	exp = math.Min(exp, float64(t.max))
	exp = math.Min(exp, float64(readyNodes))
	exp = math.RoundToEven(exp)
	return int(exp)
}

func NewSingleTopology(nodeLabelKey, nodeLabelValue, zoneLabel, clusterID string, min, max int, ratio float64) Topology {
	return &SingleTopology{
		nodeLabelKey:   nodeLabelKey,
		nodeLabelValue: nodeLabelValue,
		zoneLabel:      zoneLabel,
		clusterID:      clusterID,
		min:            min,
		max:            max,
		ration:         ratio,
	}
}
