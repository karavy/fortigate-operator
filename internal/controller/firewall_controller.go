/*
Copyright 2026.

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

package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	k8sdinovaonev1 "github.com/karavy/k8s-operator-fortigate/api/v1"
)

// FirewallReconciler reconciles a Firewall object
type FirewallReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=k8s.dinova.one,resources=firewalls,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=k8s.dinova.one,resources=firewalls/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=k8s.dinova.one,resources=firewalls/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Firewall object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.3/pkg/reconcile
func (r *FirewallReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)

	var fw k8sdinovaonev1.Firewall

	if err := r.Get(ctx, req.NamespacedName, &fw); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Definisci il nome del tuo finalizer (stringa arbitraria, di solito legata al dominio)
	myFinalizerName := "fortigatefw.dinova.one/finalizer"

	// 2. CONTROLLA SE LA RISORSA È IN FASE DI ELIMINAZIONE
	if fw.ObjectMeta.DeletionTimestamp.IsZero() {
		// La risorsa NON è in fase di eliminazione.
		// Assicurati che il tuo finalizer sia registrato nell'oggetto.
		if !controllerutil.ContainsFinalizer(&fw, myFinalizerName) {
			controllerutil.AddFinalizer(&fw, myFinalizerName)
			if err := r.Update(ctx, &fw); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// LA RISORSA È IN FASE DI ELIMINAZIONE (DeletionTimestamp è presente)
		if controllerutil.ContainsFinalizer(&fw, myFinalizerName) {

			// === IL TUO METODO DI CANCELLAZIONE ===
			// Esegui qui tutte le operazioni di pulizia (es. eliminare il PVC clonato in Go)
			/*if err := r.deleteExternalResources(&fw); err != nil {
				// Se fallisce, rimaniamo in coda e non rimuoviamo il finalizer
				return ctrl.Result{}, err
			}*/

			// Rimuovi il finalizer per dire a Kubernetes che la pulizia è completata
			controllerutil.RemoveFinalizer(&fw, myFinalizerName)
			if err := r.Update(ctx, &fw); err != nil {
				return ctrl.Result{}, err
			}
		}

		// Riconciliazione finita, l'oggetto sparirà dal cluster
		return ctrl.Result{}, nil
	}

	r.createFirewall(ctx, &fw)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FirewallReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&k8sdinovaonev1.Firewall{}).
		Named("firewall").
		Complete(r)
}
