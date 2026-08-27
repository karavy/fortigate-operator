package agent

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	netv1 "github.com/karavy/k8s-operator-fortigate/api/v1"
)

// NodeBridgeAgentReconciler gira come DaemonSet, un'istanza per nodo. Applica
// operazioni netlink privilegiate sull'host e riporta lo stato solo per il
// proprio nodo in status.nodeStatuses[NodeName].
type NodeBridgeAgentReconciler struct {
	client.Client
	NodeName string
}

// RBAC volutamente ristretto: nessun create/delete su nodebridges, solo lettura
// e scrittura dello status.
// +kubebuilder:rbac:groups=k8s.dinova.one,resources=nodebridges,verbs=get;list;watch
// +kubebuilder:rbac:groups=k8s.dinova.one,resources=nodebridges/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

func (r *NodeBridgeAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("node", r.NodeName)

	var nb netv1.NodeBridge
	if err := r.Get(ctx, req.NamespacedName, &nb); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !r.targetsThisNode(&nb) {
		// La NodeBridge non seleziona questo nodo: nessuna azione.
		return ctrl.Result{}, nil
	}

	vlanAware := true
	if nb.Spec.VlanAware != nil {
		vlanAware = *nb.Spec.VlanAware
	}

	if err := ensureBridge(nb.Spec.UplinkInterface, nb.Spec.VlanID, vlanAware, nb.Spec.BridgeName); err != nil {
		logger.Error(err, "creazione bridge fallita", "nodeBridge", nb.Name)
		if statusErr := r.updateNodeStatus(ctx, req.NamespacedName, false, err.Error()); statusErr != nil {
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, err // fa scattare il requeue standard con backoff
	}

	logger.Info("bridge applicato con successo", "nodeBridge", nb.Name)
	return ctrl.Result{}, r.updateNodeStatus(ctx, req.NamespacedName, true, "")
}

// targetsThisNode valuta lo spec.nodeSelector della NodeBridge contro le label
// del proprio Node. Un selector vuoto significa "tutti i nodi".
func (r *NodeBridgeAgentReconciler) targetsThisNode(nb *netv1.NodeBridge) bool {
	if len(nb.Spec.NodeSelector) == 0 {
		return true
	}

	var node corev1.Node
	if err := r.Get(context.Background(), client.ObjectKey{Name: r.NodeName}, &node); err != nil {
		return false
	}

	sel := labels.SelectorFromSet(nb.Spec.NodeSelector)
	return sel.Matches(labels.Set(node.GetLabels()))
}

// updateNodeStatus applica retry-on-conflict: più agent su nodi diversi possono
// aggiornare lo status della stessa NodeBridge in parallelo.
func (r *NodeBridgeAgentReconciler) updateNodeStatus(ctx context.Context, key client.ObjectKey, ready bool, message string) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var latest netv1.NodeBridge
		if err := r.Get(ctx, key, &latest); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}

		if latest.Status.NodeStatuses == nil {
			latest.Status.NodeStatuses = map[string]netv1.NodeBridgeNodeStatus{}
		}
		latest.Status.NodeStatuses[r.NodeName] = netv1.NodeBridgeNodeStatus{
			Ready:              ready,
			Message:            message,
			LastReconcileTime:  metav1.Now(),
		}

		return r.Status().Update(ctx, &latest)
	})
}

func (r *NodeBridgeAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&netv1.NodeBridge{}).
		// Reagisce solo ai cambi di Spec (generation), non agli status-update
		// scritti da altri agent sulla stessa risorsa: evita reconcile a catena
		// su ogni nodo ogni volta che uno di essi scrive il proprio status.
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Complete(r)
}
