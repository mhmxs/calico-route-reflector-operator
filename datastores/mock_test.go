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
	"context"

	"github.com/mhmxs/calico-route-reflector-operator/topologies"
	calicoApi "github.com/projectcalico/libcalico-go/lib/apis/v3"
	calicoClient "github.com/projectcalico/libcalico-go/lib/clientv3"
	"github.com/projectcalico/libcalico-go/lib/options"
	"github.com/projectcalico/libcalico-go/lib/watch"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type mockTopology struct {
	getClusterID func() string
	getNodeLabel func() (string, string)
}

func (m mockTopology) IsRouteReflector(UID string, _ map[string]string) bool {
	return false
}

func (m mockTopology) GetClusterID(string, int64) string {
	return m.getClusterID()
}

func (m mockTopology) GetNodeLabel(string) (string, string) {
	return m.getNodeLabel()
}

func (m mockTopology) NewNodeListOptions(labels map[string]string) client.ListOptions {
	return client.ListOptions{}
}

func (m mockTopology) GetRouteReflectorStatuses(nodes map[*corev1.Node]bool) []topologies.RouteReflectorStatus {
	return nil
}

func (m mockTopology) GenerateBGPPeers([]corev1.Node, map[*corev1.Node]bool, *calicoApi.BGPPeerList) ([]calicoApi.BGPPeer, []calicoApi.BGPPeer) {
	return nil, nil
}

type mockCalicoClient struct {
	mockNodeInterface calicoClient.NodeInterface
}

func (m mockCalicoClient) Nodes() calicoClient.NodeInterface {
	return m.mockNodeInterface
}

type mockNodeInterface struct {
	update func(*calicoApi.Node) (*calicoApi.Node, error)
	list   func() (*calicoApi.NodeList, error)
}

func (m mockNodeInterface) Create(context.Context, *calicoApi.Node, options.SetOptions) (*calicoApi.Node, error) {
	return nil, nil
}

func (m mockNodeInterface) Update(_ context.Context, node *calicoApi.Node, _ options.SetOptions) (*calicoApi.Node, error) {
	if m.update != nil {
		return m.update(node)
	}
	return nil, nil
}

func (m mockNodeInterface) Delete(context.Context, string, options.DeleteOptions) (*calicoApi.Node, error) {
	return nil, nil
}

func (m mockNodeInterface) Get(context.Context, string, options.GetOptions) (*calicoApi.Node, error) {
	return nil, nil
}

func (m mockNodeInterface) List(context.Context, options.ListOptions) (*calicoApi.NodeList, error) {
	if m.list != nil {
		return m.list()
	}
	return nil, nil
}

func (m mockNodeInterface) Watch(context.Context, options.ListOptions) (watch.Interface, error) {
	return nil, nil
}
