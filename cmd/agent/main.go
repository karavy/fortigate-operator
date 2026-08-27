package main

import (
	"flag"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	netv1alpha1 "github.com/karavy/k8s-operator-fortigate/api/v1"
	"github.com/karavy/k8s-operator-fortigate/internal/agent"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("agent")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(netv1alpha1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8082", "indirizzo per l'endpoint /metrics")
	opts := zap.Options{Development: false}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		setupLog.Error(fmt.Errorf("variabile NODE_NAME non impostata"),
			"l'agent richiede NODE_NAME dalla downward API")
		os.Exit(1)
	}

	// Manager separato da quello del controller principale: nessuna leader
	// election, perché ogni agent è indipendente e reconcilia solo il proprio nodo.
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:         scheme,
		Metrics:        metricsserver.Options{BindAddress: metricsAddr},
		LeaderElection: false,
	})
	if err != nil {
		setupLog.Error(err, "impossibile avviare il manager dell'agent")
		os.Exit(1)
	}

	if err := (&agent.NodeBridgeAgentReconciler{
		Client:   mgr.GetClient(),
		NodeName: nodeName,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "impossibile creare il reconciler dell'agent")
		os.Exit(1)
	}

	setupLog.Info("avvio agent", "node", nodeName)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "errore durante l'esecuzione dell'agent")
		os.Exit(1)
	}
}
