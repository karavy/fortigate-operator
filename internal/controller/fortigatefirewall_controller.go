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

	"k8s.io/client-go/rest"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	k8sdinovaonev1 "github.com/karavy/k8s-operator-fortigate/api/v1"
)

// FortigateFirewallReconciler reconciles a FortigateFirewall object
type FortigateFirewallReconciler struct {
	client.Client
	APIReader  client.Reader
	Scheme *runtime.Scheme
	RESTConfig *rest.Config
}

// +kubebuilder:rbac:groups=k8s.dinova.one,resources=fortigatefirewalls,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=k8s.dinova.one,resources=fortigatefirewalls/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=k8s.dinova.one,resources=fortigatefirewalls/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=services,verbs=list;watch;get;create;update;patch;delete
// +kubebuilder:rbac:groups="storage.k8s.io",resources=storageclasses,verbs=list;watch;get
// +kubebuilder:rbac:groups="kubevirt.io",resources=virtualmachines/restart,verbs=create;watch;list;get
// +kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=subresources.kubevirt.io,resources=virtualmachines/start,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=subresources.kubevirt.io,resources=virtualmachines/stop,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=subresources.kubevirt.io,resources=virtualmachines/restart,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=subresources.kubevirt.io,resources=virtualmachines/pause,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=subresources.kubevirt.io,resources=virtualmachines/unpause,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=subresources.kubevirt.io,resources=virtualmachines/migrate,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=k8s.cni.cncf.io,resources=network-attachment-definitions,verbs=get;list;watch;create;update;patch;delete

func (r *FortigateFirewallReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)

	var fw k8sdinovaonev1.FortigateFirewall

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
			if err := r.deleteExternalResources(&fw); err != nil {
				// Se fallisce, rimaniamo in coda e non rimuoviamo il finalizer
				return ctrl.Result{}, err
			}

			// Rimuovi il finalizer per dire a Kubernetes che la pulizia è completata
			controllerutil.RemoveFinalizer(&fw, myFinalizerName)
			if err := r.Update(ctx, &fw); err != nil {
				return ctrl.Result{}, err
			}
		}

		// Riconciliazione finita, l'oggetto sparirà dal cluster
		return ctrl.Result{}, nil
	}

	instance := &k8sdinovaonev1.FortigateFirewall{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		fmt.Printf("Errore durante la lettura dell'istanza per l'aggiornamento dello status: %v\n", err)
		return ctrl.Result{}, err
	}

	awsCredentialSecretName := instance.Spec.AWSCredentialSecretName

	if awsCredentialSecretName == "" {
		fmt.Println("Fortigate URL o nome del secret AWS non specificati. Verifica la configurazione.")
		err := fmt.Errorf("Fortigate URL o nome del secret AWS non specificati. Verifica la configurazione.")
		return ctrl.Result{}, err
	}

	awsKey, err := getSecretValues(ctx, r.Client, req.Namespace, awsCredentialSecretName, []string{"s3Url", "accessKeyID", "secretAccessKey"})
	if err != nil {
		return ctrl.Result{}, err
	}

	statusInfo, err := doReconcileFirewall(ctx, r, req, instance)
	if err != nil {
		fmt.Printf("Errore durante la riconciliazione del firewall: %v\n", err)
		return ctrl.Result{}, err
	}

	if syncTerraformS3Bucket(awsKey["s3Url"], awsKey["accessKeyID"], awsKey["secretAccessKey"], instance.Spec.S3BucketName) != true {
		fmt.Println("Errore durante la sincronizzazione del bucket S3")
		return ctrl.Result{}, fmt.Errorf("errore durante la sincronizzazione del bucket S3")
	}

	err = updateFortigateFirewallStatus(r, instance, statusInfo)
	if err != nil {
		fmt.Println("Errore nell'aggiornamento dello stato del firewall")
		return ctrl.Result{}, err // Ritorna l'errore per fare il requeue
	}

	return ctrl.Result{}, nil
}

func (r *FortigateFirewallReconciler) deleteExternalResources(resource *k8sdinovaonev1.FortigateFirewall) error {
	// Qui puoi implementare la logica per eliminare le risorse esterne associate alla tua CRD.
	// Ad esempio, se hai creato un PVC, puoi eliminarlo qui.
	// Puoi usare r.Client per interagire con il cluster e cancellare le risorse.
	// 1. Elimina il servizio k8s
	// TODO: elimina le regole relative del firewall
	// TODO: elimina i fortigateupdate

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	fmt.Printf("Inizio della cancellazione delle risorse esterne per il firewall: %s\n", resource.Name)

	if err := DeleteFortigateFirewall(ctx, r, resource.Name, resource.Spec.FortigateVersion, resource.Namespace); err != nil {
		return err
	}

	if err := DeleteFortigateFirewallSvc(ctx, r, resource.Name, resource.Spec.FortigateVersion, resource.Namespace); err != nil {
		return err
	}

	if err := DeleteFirewallPVC(ctx, r, resource.Name, resource.Spec.FortigateVersion, resource.Namespace); err != nil {
		return err
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FortigateFirewallReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&k8sdinovaonev1.FortigateFirewall{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named("fortigatefirewall").
		Complete(r)
}

func doReconcileFirewall(ctx context.Context, r *FortigateFirewallReconciler, req ctrl.Request, instance *k8sdinovaonev1.FortigateFirewall) (k8sdinovaonev1.FortigateFirewallStatus, error) {
	statusInfo := k8sdinovaonev1.FortigateFirewallStatus{}

	if err := CreateNewFortigateFirewallPVC(instance.Name, instance.Spec.FortigateVersion, instance.Namespace, instance.Spec.PVCStorageClass, ctx, r, "fortigate"); err != nil {
		fmt.Printf("Errore durante la creazione del PVC: %v\n", err)
		return statusInfo, err
	}
	if err := r.createFortigateFirewall(ctx, instance.Name, instance.Spec.FortigateVersion, instance.Namespace, instance.Spec); err != nil {
		fmt.Printf("Errore durante la creazione della VMI: %v\n", err)
		return statusInfo, err
	}

	if err := createFortigateService(ctx, r, instance); err != nil {
		fmt.Printf("Errore durante la creazione del Service: %v\n", err)
		return statusInfo, err
	}

	// aspetta che il firewall sia pronto prima di procedere con la registrazione della licenza
	if GetFirewallReady(ctx, r, req, instance) == false {
		return statusInfo, fmt.Errorf("firewall non pronto")
	}

	switch instance.Spec.LicenseType {
	case "trial":
		if _, err := DoSSHOperations(ctx, r, req, REGISTERLICENSE, instance); err != nil {
			fmt.Printf("Errore durante la registrazione della licenza: %v\n", err)
			return statusInfo, err
		}

		if GetFirewallReadyWithLicense(ctx, r, req, instance) == false {
			fmt.Printf("Firewall non pronto dopo la registrazione della licenza. Verifica manuale consigliata.\n")
			return statusInfo, fmt.Errorf("firewall non pronto dopo la registrazione della licenza")
		}
	case "none":
		// Non fare nulla, procedi senza registrare la licenza
		fmt.Println("Nessuna licenza richiesta o licenza già esistente")
	case "flex":
		//TODO
	case "file":
		//TODO
	default:
		fmt.Printf("Tipo di licenza non valido: %s\n", instance.Spec.LicenseType)
		return statusInfo, fmt.Errorf("tipo di licenza non valido: %s", instance.Spec.LicenseType)
	}

	if instance.Status.Token == "" {
		token, err := DoSSHOperations(ctx, r, req, GETTOKEN, instance)
		if err != nil {
			fmt.Println("Impossibile ottenere il token")
		} else {
			statusInfo.Token = token.(string)
			fmt.Println("Token ottenuto")
		}
	} else {
		statusInfo.Token = instance.Status.Token
	}

	return statusInfo, nil
}

func updateFortigateFirewallStatus(r *FortigateFirewallReconciler, fw *k8sdinovaonev1.FortigateFirewall, statusInfo k8sdinovaonev1.FortigateFirewallStatus) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Modifichiamo direttamente l'oggetto passato come puntatore (*fw)
	fw.Status.Conditions = []metav1.Condition{
		{
			Type:               "Available",
			Status:             metav1.ConditionTrue,
			Reason:             "FortigateConfigApplied",
			Message:            "La configurazione è stata applicata con successo a Fortigate",
			LastTransitionTime: metav1.Now(), // È buona pratica aggiungere il timestamp nelle condizioni
		},
	}

	fw.Status.Token = statusInfo.Token

	fmt.Println("Tentativo di aggiornamento dello status nel cluster...")

	// 2. Eseguiamo l'update dello status
	if err := r.Status().Update(ctx, fw); err != nil {
		fmt.Printf("Errore durante r.Status().Update: %v\n", err)
		return err
	}

	fmt.Println("Stato aggiornato con successo sul cluster!")
	return nil
}
