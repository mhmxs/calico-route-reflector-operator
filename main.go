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
	"os"
	"strconv"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	routereflectorv1 "github.com/mhmxs/calico-route-reflector-operator/api/v1"
	"github.com/mhmxs/calico-route-reflector-operator/controllers"
	"github.com/prometheus/common/log"
	// +kubebuilder:scaffold:imports
)

const (
	routeReflectorMin      = 3
	routeReflectorMax      = 10
	routeReflectorRatio    = 0.2
	routeReflectorMaxRatio = 0.5
	routeReflectorLabel    = "calico-route-reflector="
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = routereflectorv1.AddToScheme(scheme)
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

	min, max, ratio, nodeLabel := parseEnv()

	if err = (&controllers.RouteReflectorConfigReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("RouteReflectorConfig"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr, controllers.RouteReflectorConfig{
		Min:       min,
		Max:       max,
		Ration:    ratio,
		NodeLabel: nodeLabel,
	}); err != nil {
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

func parseEnv() (int, int, float64, string) {
	var err error
	min := routeReflectorMin
	if v, ok := os.LookupEnv("ROUTE_REFLECTOR_MIN"); ok {
		min, err = strconv.Atoi(v)
		if err != nil {
			setupLog.Error(err, "ROUTE_REFLECTOR_MIN is not an integer")
			panic(err)
		} else if min < 3 || min > 999 {
			err = errors.New("ROUTE_REFLECTOR_MIN must be positive number between 3 and 999")
			setupLog.Error(err, err.Error())
			panic(err)
		}
	}
	max := routeReflectorMax
	if v, ok := os.LookupEnv("ROUTE_REFLECTOR_MAX"); ok {
		max, err = strconv.Atoi(v)
		if err != nil {
			setupLog.Error(err, "ROUTE_REFLECTOR_MAX is not an integer")
			panic(err)
		} else if max < 5 || max > 2500 {
			err = errors.New("ROUTE_REFLECTOR_MIN must be positive number between 5 and 2500")
			setupLog.Error(err, err.Error())
			panic(err)
		}
	}
	ratio := routeReflectorRatio
	if v, ok := os.LookupEnv("ROUTE_REFLECTOR_RATIO"); ok {
		ratio, err = strconv.ParseFloat(v, 32)
		if err != nil {
			setupLog.Error(err, "ROUTE_REFLECTOR_RATIO is not a float")
			panic(err)
		} else if ratio > routeReflectorMaxRatio {
			err = fmt.Errorf("ROUTE_REFLECTOR_RATIO is bigger than %f", routeReflectorMaxRatio)
			setupLog.Error(err, err.Error())
			panic(err)
		}
	}
	nodeLabel := routeReflectorLabel
	if v, ok := os.LookupEnv("ROUTE_REFLECTOR_NODE_LABEL"); ok {
		nodeLabel = v
	}

	return min, max, ratio, nodeLabel
}
