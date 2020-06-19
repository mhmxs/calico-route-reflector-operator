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

package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	calicoApiConfig "github.com/projectcalico/libcalico-go/lib/apiconfig"
	calicoClient "github.com/projectcalico/libcalico-go/lib/clientv3"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	routereflectorv1 "github.com/mhmxs/calico-route-reflector-operator/api/v1"
	"github.com/mhmxs/calico-route-reflector-operator/bgppeer"
	"github.com/mhmxs/calico-route-reflector-operator/controllers"
	"github.com/mhmxs/calico-route-reflector-operator/datastores"
	"github.com/mhmxs/calico-route-reflector-operator/topologies"
	"github.com/prometheus/common/log"
	// +kubebuilder:scaffold:imports
)

const (
	defaultClusterID              = "224.0.0.0"
	defaultRouteReflectorMin      = 3
	defaultRouteReflectorMax      = 10
	defaultRouteReflectorRatio    = 0.005
	defaultRouteReflectorRatioMin = 0.001
	defaultRouteReflectorRatioMax = 0.05
	defaultRouteReflectorLabel    = "calico-route-reflector"

	shift                         = 100000
	routeReflectorRatioMinShifted = int(defaultRouteReflectorRatioMin * shift)
	routeReflectorRatioMaxShifted = int(defaultRouteReflectorRatioMax * shift)
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = routereflectorv1.AddToScheme(scheme)

	// calicoGroupVersion := &schema.GroupVersion{
	// 	Group:   "crd.projectcalico.org",
	// 	Version: "v1",
	// }

	// schemeBuilder := runtime.NewSchemeBuilder(
	// 	func(scheme *runtime.Scheme) error {
	// 		scheme.AddKnownTypes(
	// 			*calicoGroupVersion,
	// 			&calicoApi.BGPPeer{},
	// 			&calicoApi.BGPPeerList{},
	// 		)
	// 		return nil
	// 	})

	// _ = schemeBuilder.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Error(fmt.Errorf("%v", r), "")
			os.Exit(1)
		}
	}()

	var metricsAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "e5e2e31b.calico-route-reflector-operator.mhmxs.github.com",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		panic(err)
	}

	min, max, clusterID, ratio, nodeLabelKey, nodeLabelValue, zoneLabel, incompatibleLabels := parseEnv()
	topologyConfig := topologies.Config{
		NodeLabelKey:   nodeLabelKey,
		NodeLabelValue: nodeLabelValue,
		ZoneLabel:      zoneLabel,
		ClusterID:      clusterID,
		Min:            min,
		Max:            max,
		Ration:         ratio,
	}
	log.Infof("Topology config: %v", topologyConfig)

	var topology topologies.Topology
	// TODO Validation on topology
	if t, ok := os.LookupEnv("ROUTE_REFLECTOR_TOPOLOGY"); ok && t == "multi" {
		topology = topologies.NewMultiTopology(topologyConfig)
	} else {
		topology = topologies.NewSingleTopology(topologyConfig)
	}

	dsType, calicoClient, err := newCalicoClient(os.Getenv("DATASTORE_TYPE"))
	if err != nil {
		setupLog.Error(err, "unable create Calico config")
		panic(err)
	}

	var datastore datastores.Datastore
	switch dsType {
	case calicoApiConfig.Kubernetes:
		datastore = datastores.NewKddDatastore(&topology)
	case calicoApiConfig.EtcdV3:
		datastore = datastores.NewEtcdDatastore(&topology, calicoClient)
	default:
		panic(fmt.Errorf("Unsupported DS %s", dsType))
	}

	if err = (&controllers.RouteReflectorConfigReconciler{
		Client:             mgr.GetClient(),
		Log:                ctrl.Log.WithName("controllers").WithName("RouteReflectorConfig"),
		Scheme:             mgr.GetScheme(),
		NodeLabelKey:       nodeLabelKey,
		IncompatibleLabels: incompatibleLabels,
		Topology:           topology,
		Datastore:          datastore,
		BGPPeer: bgppeer.BGPPeer{
			CalicoClient: calicoClient,
		},
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "RouteReflectorConfig")
		panic(err)
	}
	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		panic(err)
	}
}

// TODO more sophisticated env parse and validation or use CRD
func parseEnv() (int, int, string, float64, string, string, string, map[string]*string) {
	var err error
	clusterID := defaultClusterID
	if v, ok := os.LookupEnv("ROUTE_REFLECTOR_CLUSTER_ID"); ok {
		clusterID = v
	}
	min := defaultRouteReflectorMin
	if v, ok := os.LookupEnv("ROUTE_REFLECTOR_MIN"); ok {
		min, err = strconv.Atoi(v)
		if err != nil {
			setupLog.Error(err, "ROUTE_REFLECTOR_MIN is not an integer")
			panic(err)
		} else if min < 1 || min > 50 {
			err = errors.New("ROUTE_REFLECTOR_MIN must be positive number between 1 and 50, current: " + fmt.Sprintf("%d", min))
			setupLog.Error(err, err.Error())
			panic(err)
		}
	}
	max := defaultRouteReflectorMax
	if v, ok := os.LookupEnv("ROUTE_REFLECTOR_MAX"); ok {
		max, err = strconv.Atoi(v)
		if err != nil {
			setupLog.Error(err, "ROUTE_REFLECTOR_MAX is not an integer")
			panic(err)
		} else if max < 5 || max > 50 {
			err = errors.New("ROUTE_REFLECTOR_MIN must be positive number between 5 and 50, current: " + fmt.Sprintf("%d", max))
			setupLog.Error(err, err.Error())
			panic(err)
		}
	}
	ratio := defaultRouteReflectorRatio
	if v, ok := os.LookupEnv("ROUTE_REFLECTOR_RATIO"); ok {
		ratio, err = strconv.ParseFloat(v, 32)
		if err != nil {
			setupLog.Error(err, "ROUTE_REFLECTOR_RATIO is not a valid number")
			panic(err)
		} else if ratioShifted := int(ratio * shift); ratioShifted < routeReflectorRatioMinShifted || ratioShifted > routeReflectorRatioMaxShifted {
			err = errors.New("ROUTE_REFLECTOR_MIN must be a number between 0.001 and 0.05, current: " + fmt.Sprintf("%f", ratio))
			setupLog.Error(err, err.Error())
			panic(err)
		}
	}

	nodeLabelKey := defaultRouteReflectorLabel
	nodeLabelValue := ""
	if v, ok := os.LookupEnv("ROUTE_REFLECTOR_NODE_LABEL"); ok {
		nodeLabelKey, nodeLabelValue = getKeyValue(v)
	}

	zoneLable := os.Getenv("ROUTE_REFLECTOR_ZONE_LABEL")

	incompatibleLabels := map[string]*string{}
	if v, ok := os.LookupEnv("ROUTE_REFLECTOR_INCOMPATIBLE_NODE_LABELS"); ok {
		for _, l := range strings.Split(v, ",") {
			key, value := getKeyValue(strings.Trim(l, " "))
			if strings.Contains(l, "=") {
				incompatibleLabels[key] = &value
			} else {
				incompatibleLabels[key] = nil
			}
		}
	}

	return min, max, clusterID, ratio, nodeLabelKey, nodeLabelValue, zoneLable, incompatibleLabels
}

func getKeyValue(label string) (string, string) {
	keyValue := strings.Split(label, "=")
	if len(keyValue) == 1 {
		keyValue[1] = ""
	}

	return keyValue[0], keyValue[1]
}

func fetchAPIToken(path string) string {
	content, err := ioutil.ReadFile(path)
	if err != nil {
		setupLog.Error(err, "unable to find token")
		panic(err)
	}

	return string(content)
}

func newCalicoClient(dataStoreType string) (calicoApiConfig.DatastoreType, calicoClient.Interface, error) {
	switch dataStoreType {
	case "incluster":
		calicoConfig := calicoApiConfig.NewCalicoAPIConfig()
		calicoConfig.Spec = calicoApiConfig.CalicoAPIConfigSpec{
			DatastoreType: calicoApiConfig.Kubernetes,
			KubeConfig: calicoApiConfig.KubeConfig{
				K8sAPIEndpoint: os.Getenv("K8S_API_ENDPOINT"),
				K8sCAFile:      "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
				K8sAPIToken:    fetchAPIToken("/var/run/secrets/kubernetes.io/serviceaccount/token"),
			},
		}
		client, err := calicoClient.New(*calicoConfig)

		return calicoApiConfig.Kubernetes, client, err
	case "kubernetes":
		client, err := calicoClient.NewFromEnv()

		return calicoApiConfig.Kubernetes, client, err
	case "etcdv3":
		client, err := calicoClient.NewFromEnv()

		return calicoApiConfig.EtcdV3, client, err
	default:
		panic("Type not supported: " + dataStoreType)
	}
}
