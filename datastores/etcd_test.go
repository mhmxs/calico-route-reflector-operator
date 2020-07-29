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
	"errors"
	"testing"

	"github.com/mhmxs/calico-route-reflector-operator/topologies"
	calicoApi "github.com/projectcalico/libcalico-go/lib/apis/v3"
	calicoClient "github.com/projectcalico/libcalico-go/lib/clientv3"
	"github.com/projectcalico/libcalico-go/lib/options"
	"github.com/projectcalico/libcalico-go/lib/watch"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestRemoveRRStatusListError(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"rr": "0"},
		},
	}

	ds := EtcdDataStore{
		topology: mockTopology{
			getNodeLabel: func() (string, string) {
				return "rr", ""
			},
		},
		calicoClient: mockCalicoClient{
			mockNodeInterface: mockNodeInterface{
				list: func() (*calicoApi.NodeList, error) {
					return nil, errors.New("fatal error")
				},
			},
		},
	}

	err := ds.RemoveRRStatus(node)

	if err == nil {
		t.Error("Error not found")
	}
}

func TestRemoveRRStatusNodeNotFound(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{"rr": "0"},
		},
	}

	ds := EtcdDataStore{
		topology: mockTopology{
			getNodeLabel: func() (string, string) {
				return "rr", ""
			},
		},
		calicoClient: mockCalicoClient{
			mockNodeInterface: mockNodeInterface{
				list: func() (*calicoApi.NodeList, error) {
					return &calicoApi.NodeList{
						Items: []calicoApi.Node{},
					}, nil
				},
			},
		},
	}

	err := ds.RemoveRRStatus(node)

	if err == nil {
		t.Error("Error not found")
	}
}

func TestRemoveRRStatusUpdateError(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"rr":                     "0",
				"kubernetes.io/hostname": "node",
			},
		},
	}

	ds := EtcdDataStore{
		topology: mockTopology{
			getNodeLabel: func() (string, string) {
				return "rr", ""
			},
		},
		calicoClient: mockCalicoClient{
			mockNodeInterface: mockNodeInterface{
				list: func() (*calicoApi.NodeList, error) {
					return &calicoApi.NodeList{
						Items: []calicoApi.Node{
							{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{
										"rr":                     "0",
										"kubernetes.io/hostname": "node",
									},
								},
								Spec: calicoApi.NodeSpec{
									BGP: &calicoApi.NodeBGPSpec{},
								},
							},
						},
					}, nil
				},
				update: func(*calicoApi.Node) (*calicoApi.Node, error) {
					return nil, errors.New("fatal error")
				},
			},
		},
	}

	err := ds.RemoveRRStatus(node)

	if err == nil {
		t.Error("Error not found")
	}
}

func TestRemoveRRStatus(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"rr":                     "0",
				"kubernetes.io/hostname": "node",
			},
		},
	}

	var nodeToUpdate *calicoApi.Node
	ds := EtcdDataStore{
		topology: mockTopology{
			getClusterID: func() string {
				return "clusterID"
			},
			getNodeLabel: func() (string, string) {
				return "rr", ""
			},
		},
		calicoClient: mockCalicoClient{
			mockNodeInterface: mockNodeInterface{
				list: func() (*calicoApi.NodeList, error) {
					return &calicoApi.NodeList{
						Items: []calicoApi.Node{
							{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{
										"rr":                     "0",
										"kubernetes.io/hostname": "node",
									},
								},
								Spec: calicoApi.NodeSpec{
									BGP: &calicoApi.NodeBGPSpec{},
								},
							},
						},
					}, nil
				},
				update: func(node *calicoApi.Node) (*calicoApi.Node, error) {
					nodeToUpdate = node
					return node, nil
				},
			},
		},
	}

	err := ds.RemoveRRStatus(node)

	if err != nil {
		t.Errorf("Error found %s", err.Error())
	}
	if _, ok := node.GetLabels()["rr"]; ok {
		t.Errorf("Label was no removed %v", node.GetLabels())
	}
	if nodeToUpdate.Spec.BGP.RouteReflectorClusterID != "" {
		t.Errorf("Wrong RouteReflectorClusterID was configured %s", nodeToUpdate.Spec.BGP.RouteReflectorClusterID)
	}
}

func TestAddRRStatus(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"kubernetes.io/hostname": "node",
			},
		},
	}

	var nodeToUpdate *calicoApi.Node
	ds := EtcdDataStore{
		topology: mockTopology{
			getClusterID: func() string {
				return "clusterID"
			},
			getNodeLabel: func() (string, string) {
				return "rr", "value"
			},
		},
		calicoClient: mockCalicoClient{
			mockNodeInterface: mockNodeInterface{
				list: func() (*calicoApi.NodeList, error) {
					return &calicoApi.NodeList{
						Items: []calicoApi.Node{
							{
								ObjectMeta: metav1.ObjectMeta{
									Labels: map[string]string{
										"kubernetes.io/hostname": "node",
									},
								},
								Spec: calicoApi.NodeSpec{
									BGP: &calicoApi.NodeBGPSpec{},
								},
							},
						},
					}, nil
				},
				update: func(node *calicoApi.Node) (*calicoApi.Node, error) {
					nodeToUpdate = node
					return node, nil
				},
			},
		},
	}

	err := ds.AddRRStatus(node)

	if err != nil {
		t.Errorf("Error found %s", err.Error())
	}
	if _, ok := node.GetLabels()["rr"]; !ok {
		t.Errorf("Label was not added %v", node.GetLabels())
	}
	if nodeToUpdate.Spec.BGP.RouteReflectorClusterID != "clusterID" {
		t.Errorf("Wrong RouteReflectorClusterID was configured %s", nodeToUpdate.Spec.BGP.RouteReflectorClusterID)
	}
}

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
