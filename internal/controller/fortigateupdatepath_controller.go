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
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	k8sdinovaonev1 "github.com/karavy/k8s-operator-fortigate/api/v1"
	upgrade "github.com/karavy/k8s-operator-fortigate/internal/controller/fortigate/upgrade"
)

// FortigateUpdatePathReconciler reconciles a FortigateUpdatePath object
type FortigateUpdatePathReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=k8s.dinova.one,resources=fortigateupdatepaths,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=k8s.dinova.one,resources=fortigateupdatepaths/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=k8s.dinova.one,resources=fortigateupdatepaths/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the FortigateUpdatePath object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.3/pkg/reconcile
func (r *FortigateUpdatePathReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)

	// 1. Recupera la risorsa corrente che ha scatenato il loop
	var currentResource k8sdinovaonev1.FortigateUpdatePath
	if err := r.Get(ctx, req.NamespacedName, &currentResource); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Se la risorsa è in fase di cancellazione, non fare il controllo dei duplicati
	if !currentResource.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	// 2. Recupera TUTTE le risorse dello stesso tipo nel cluster (essendo ClusterWide)
	var allResources k8sdinovaonev1.FortigateUpdatePathList
	if err := r.List(ctx, &allResources); err != nil {
		fmt.Println("Impossibile recuperare la lista delle risorse:", err)
		return ctrl.Result{}, err
	}

	// 3. Loop di confronto per cercare duplicati
	for _, existing := range allResources.Items {
		// Salta il confronto se è la stessa identica risorsa (stesso Nome)
		if existing.Name == currentResource.Name {
			continue
		}

		// ⚠️ ESEMPIO DI CONTROLLO: confrontiamo il parametro che deve essere univoco
		if existing.Spec.Model == currentResource.Spec.Model && existing.Spec.StartVersion == currentResource.Spec.StartVersion && existing.Spec.NewVersion == currentResource.Spec.NewVersion {
			fmt.Println("Rilevato parametro duplicato! Blocco la creazione/aggiornamento",
				"RisorsaCorrente", currentResource.Name,
				"RisorsaDuplicataCon", existing.Name,
				"Valore", currentResource.Spec.Model)

			// 4. Aggiorna lo Status per avvisare l'utente (Buona pratica Kubernetes)
			/*currentResource.Status.Phase = "Failed"
			            currentResource.Status.Reason = "DuplicateParameter"
			            currentResource.Status.Message = fmt.Sprintf("Il parametro '%s' è già utilizzato dalla risorsa %s",
			                currentResource.Spec.Model, existing.Name)
						if err := r.Status().Update(ctx, &currentResource); err != nil {
			                log.Error(err, "Impossibile aggiornare lo status di errore")
			                return ctrl.Result{}, err
			            }*/

			// Ritorniamo nil senza riaccodare (Result{}), interrompendo il loop
			return ctrl.Result{}, nil
		}
	}

	// --- DA QUI IN POI IL TUO CODICE DI RECONCILE NORMALE ---
	// Se il codice arriva qui, significa che non ci sono duplicati.

	// Ricordati di ripulire lo status se prima era in errore
	/*if currentResource.Status.Phase == "Failed" && currentResource.Status.Reason == "DuplicateParameter" {
	    currentResource.Status.Phase = "Completed" // o la tua fase di default
	    currentResource.Status.Reason = ""
	    currentResource.Status.Message = ""
	    if err := r.Status().Update(ctx, &currentResource); err != nil {
	        return ctrl.Result{}, err
	    }
	}*/

	statusInfo := currentResource.Status

	if statusInfo.UpdatePath.PathStatusValid != true {
		path, err := upgrade.GetUpgradePath(currentResource.Spec.Model, currentResource.Spec.StartVersion, currentResource.Spec.NewVersion)
		if err != nil {
			fmt.Println(err)
			return ctrl.Result{}, err
		}
		
		// TODO: Aggiorna lo status della risorsa con il percorso di aggiornamento trovato
		// TODO: scarica il firmware per il controller (lo mette in s3)
		fmt.Printf("Percorso di aggiornamento trovato per %s: %v\n", currentResource.Name, path)
	
		steps := make([]k8sdinovaonev1.UpdateStep, 0, len(path))
	
		for i := range path {
			updateStep := k8sdinovaonev1.UpdateStep{
				Version:     path[i].Version,
				Build:       path[i].BuildNumber,
				ReleaseType: path[i].Type,
			}
		
			steps = append(steps, updateStep)
		}
	
		statusInfo.UpdatePath.Steps = steps
		statusInfo.UpdatePath.PathStatusValid = true
	
		err = updateFortigateUpdatePathStatus(r, &currentResource, statusInfo)
		if err != nil {
			fmt.Println("Errore nell'aggiornamento dello stato del firewall")
			return ctrl.Result{}, err // Ritorna l'errore per fare il requeue
		}
	} else {
		fmt.Printf("Percorso di aggiornamento già valido per %s, nessuna azione necessaria\n", currentResource.Name)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FortigateUpdatePathReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&k8sdinovaonev1.FortigateUpdatePath{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named("fortigateupdatepath").
		Complete(r)
}

func updateFortigateUpdatePathStatus(r *FortigateUpdatePathReconciler, instance *k8sdinovaonev1.FortigateUpdatePath, statusInfo k8sdinovaonev1.FortigateUpdatePathStatus) error {
	// Aggiorna lo stato dell'istanza con le informazioni ottenute da Fortigate
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	instance.Status.Conditions = []metav1.Condition{
		{

			Type:               "Available",
			Status:             metav1.ConditionTrue,
			Reason:             "FortigateConfigApplied",
			Message:            "La configurazione è stata applicata con successo a Fortigate",
			LastTransitionTime: metav1.Now(), // È buona pratica aggiungere il timestamp nelle condizioni

		},
	}

	instance.Status = statusInfo

	if err := r.Status().Update(ctx, instance); err != nil {
		fmt.Printf("Errore durante r.Status().Update: %v\n", err)
		return err
	}

	fmt.Println("Stato aggiornato con successo sul cluster!")
	return nil
}
