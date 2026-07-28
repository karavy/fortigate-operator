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
	"errors"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/Masterminds/semver/v3"
	k8sdinovaonev1 "github.com/karavy/k8s-operator-fortigate/api/v1"
)

var ErrResourceNotFound = errors.New("resource not found")

// FortigateUpdateReconciler reconciles a FortigateUpdate object
type FortigateUpdateReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=k8s.dinova.one,resources=fortigateupdates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=k8s.dinova.one,resources=fortigateupdates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=k8s.dinova.one,resources=fortigateupdates/finalizers,verbs=update
// +kubebuilder:rbac:groups=snapshot.kubevirt.io,resources=virtualmachinesnapshots,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the FortigateUpdate object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.3/pkg/reconcile
func (r *FortigateUpdateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)
	instance := &k8sdinovaonev1.FortigateUpdate{}

	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		fmt.Printf("Errore durante la lettura dell'istanza per l'aggiornamento dello status: %v\n", err)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	requestedFw := types.NamespacedName{
		Name:      instance.Spec.FortigateName, // Il nome esatto della risorsa su K8s
		Namespace: req.Namespace,               // Puoi usare il namespace della richiesta corrente
	}
	fwInstance := &k8sdinovaonev1.FortigateFirewall{}
	err = r.Get(ctx, requestedFw, fwInstance)
	if err != nil {
		fmt.Printf("Errore durante la lettura dell'istanza firewall per l'aggiornamento dello status: %v\n", err)
		return ctrl.Result{}, err
	}

	if fwInstance.Status.Token == "" {
		fmt.Println("Token non disponibile. Attendere che il token sia generato prima di procedere con l'aggiornamento.")
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	statusInfo, err := doReconcile(ctx, r, instance, fwInstance, instance.Status)
	if err != nil {
		fmt.Printf("Errore durante il processo di aggiornamento: %v\n", err)
		if errors.Is(err, ErrResourceNotFound) {
			fmt.Println("Risorsa non trovata, non riesco a procedere con l'aggiornamento.")
			return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
		}
		return ctrl.Result{}, err
	}

	if err := updateFortigateUpgradeStatus(r, instance, statusInfo); err != nil {
		fmt.Printf("Errore durante l'aggiornamento dello status dell'update: %v\n", err)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func doReconcile(ctx context.Context, r *FortigateUpdateReconciler, instance *k8sdinovaonev1.FortigateUpdate, fwInstance *k8sdinovaonev1.FortigateFirewall, statusInfo k8sdinovaonev1.FortigateUpdateStatus) (k8sdinovaonev1.FortigateUpdateStatus, error) {
	fortiIP := fmt.Sprintf("%s-%s-ssh-gui.%s.svc.cluster.local", fwInstance.Name, fwInstance.Spec.FortigateVersion, fwInstance.Namespace)

	// 1. Get upgrade path based on model and version
	model, err := SendCommandApiGet(fwInstance.Name, ctx, fortiIP, fwInstance.Status.Token, GETFWMODEL, "", "", "", "", nil)
	if err != nil {
		fmt.Printf("Errore durante la lettura del modello del firewall: %v\n", err)
		return statusInfo, err
	}

	version, err := SendCommandApiGet(fwInstance.Name, ctx, fortiIP, fwInstance.Status.Token, GETFWVERSION, "", "", "", "", nil)
	if err != nil {
		fmt.Printf("Errore durante la lettura della versione del firewall: %v\n", err)
		return statusInfo, err
	}

	fmt.Printf("Modello del firewall: %s\n", model)
	fmt.Printf("Versione del firewall: %s\n", version)

	if version != instance.Spec.NewVersion {
		//TODO: deve recuperare il path dalla risorsa FortigateUpdatePath
		pathStatus, err := r.GetStatusByParams(ctx, instance.Namespace, model, instance.Spec.NewVersion, version)
		if err != nil {
			fmt.Printf("Errore durante il recupero del percorso di aggiornamento: %v\n", err)
			return statusInfo, fmt.Errorf("nessun percorso di aggiornamento trovato per modello %s e nuova versione %s: %w", model, instance.Spec.NewVersion, ErrResourceNotFound)
		}

		backupFilename, snapshotName, err := backupForUpgradeFirewall(fwInstance, fwInstance.Spec.FortigateVersion, instance, fwInstance.Status.Token, ctx, r, statusInfo)
		if err != nil {
			fmt.Printf("Errore durante l'aggiornamento del firewall: %v\n", err)
			statusInfo.BackupName = backupFilename
			// aggiorno lo status della risorsa FortigateUpdate con il nome del backup anche in caso di errore
			return statusInfo, err
		}

		fmt.Println("Backup e snapshot completati con successo. Nome del backup:", backupFilename, "Nome della snapshot:", snapshotName)
		// Aggiorniamo lo status della risorsa FortigateUpdate con il nome del backup
		
		statusInfo.BackupName = backupFilename
		if err := updateFortigateUpgradeStatus(r, instance, statusInfo); err != nil {
			fmt.Printf("Errore durante l'aggiornamento dello status dell'update: %v\n", err)
			return statusInfo, err
		}

		// recupera le credenziali AWS dal secret specificato nella risorsa FortigateFirewall
		awsKey, err := getSecretValues(ctx, r.Client, instance.Namespace, fwInstance.Spec.AWSCredentialSecretName, []string{"s3Url", "accessKeyID", "secretAccessKey"})
		if err != nil {
			fmt.Printf("Errore nel recupero dei valori del secret: %v", err)
			return statusInfo, err
		}

		for vd, upgradePath := range pathStatus.UpdatePath.Steps {
			if vd > 0 {
				fmt.Printf("Percorso di aggiornamento per VD %d: %v\n", vd, upgradePath.Version)

				// TODO: Abbiamo fortimanager? Se sì, dobbiamo interagire per gestire l'upgrade (es. caricare la nuova image, creare il task di upgrade, monitorare lo stato del task)

				resp, err := SendCommandApiPost(ctx, fortiIP, fwInstance.Status.Token, upgradePath.Version, UPGRADEFIRMWARE, fwInstance.Spec.S3BucketName, awsKey["accessKeyID"], awsKey["secretAccessKey"], awsKey["s3Url"])
				if err != nil {
					fmt.Printf("Errore nell'upgrade del firmware: %v", err)
					return statusInfo, err
				} else {
					fmt.Printf("Firmware upgrade response: %s\n", resp)

					if err := deleteKubeVirtVMSnapshot(ctx, r, fwInstance.Namespace, snapshotName); err != nil {
						fmt.Printf("Errore durante la cancellazione della snapshot: %v", err)
						return statusInfo, err
					}
				}
			}
		}
	} else {
		fmt.Println("Il firewall è già aggiornato all'ultima versione disponibile.")
	}
	return statusInfo, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FortigateUpdateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&k8sdinovaonev1.FortigateUpdate{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named("fortigateupdate").
		Complete(r)
}

func (r *FortigateUpdateReconciler) GetStatusByParams(ctx context.Context, namespace, nmodel, newVersion, currentVersion string) (*k8sdinovaonev1.FortigateUpdatePathStatus, error) {
    var list k8sdinovaonev1.FortigateUpdatePathList

    // 1. Riduciamo il set di dati filtrando in cache solo per il modello esatto
    listOpts := []client.ListOption{
        client.MatchingFields{"spec.model": nmodel},
    }

	fmt.Printf("Tentativo di recupero dello status del percorso di aggiornamento per modello=%s, target=%s\n", nmodel, newVersion)

    if err := r.List(ctx, &list, listOpts...); err != nil {
        fmt.Printf("Errore durante la list: %v\n", err)
        return nil, fmt.Errorf("errore durante la list: %w", err)
    }

    // Parsea la versione da cercare (target)
    targetVer, err := semver.NewVersion(newVersion)
    if err != nil {
		fmt.Printf("Errore durante il parsing della versione target '%s': %v\n", newVersion, err)
        return nil, fmt.Errorf("la versione target '%s' non è un semver valido: %w", newVersion, err)
    }

	fmt.Printf("Versione target parsata correttamente: %s\n", targetVer.String())

    // 2. Filtriamo in memoria per verificare i range di versione (Min/Max) e la startVersion
    for _, item := range list.Items {
        // Parsea i limiti della risorsa (Assumendo che la tua CRD abbia i campi Spec.MinVersion e Spec.MaxVersion)
        minVer, errMin := semver.NewVersion(item.Spec.StartVersion)
        maxVer, errMax := semver.NewVersion(item.Spec.NewVersion)
        
        if errMin != nil || errMax != nil {
            fmt.Printf("Errore durante il parsing delle versioni per la risorsa %s: StartVersion='%s' (err: %v), NewVersion='%s' (err: %v)\n", item.Name, item.Spec.StartVersion, errMin, item.Spec.NewVersion, errMax)
            continue
        }

		fmt.Printf("Controllando risorsa: %s, StartVersion: %s, NewVersion: %s, TargetVersion: %s\n", item.Name, minVer.String(), maxVer.String(), targetVer.String())

        if (targetVer.GreaterThan(minVer) || targetVer.Equal(minVer)) && 
           (targetVer.LessThan(maxVer) || targetVer.Equal(maxVer)) {
            
			fmt.Printf("Prima risorsa adatta trovata: %s, StartVersion: %s, NewVersion: %s\n", item.Name, minVer.String(), maxVer.String())
			return &item.Status, nil
        }
    }

    fmt.Printf("Nessuna risorsa adatta trovata per modello=%s, target=%s\n", nmodel, newVersion)

	if err := r.createFortigateUpdatePath(currentVersion, newVersion, nmodel, namespace); err != nil {
		fmt.Printf("Errore durante la creazione del percorso di aggiornamento: %v\n", err)
		return nil, fmt.Errorf("errore durante la creazione del percorso di aggiornamento: %w", err)
	}

    return nil, fmt.Errorf("nessuna risorsa trovata nel range di versioni specificato")
}

func (r *FortigateUpdateReconciler) createFortigateUpdatePath(minVersion, maxVersion, model, namespace string) error {
	newUpgradePath := &k8sdinovaonev1.FortigateUpdatePath{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("updatepath-%s-%s-to-%s", strings.ToLower(model), minVersion, maxVersion),
			Namespace: namespace, // Cambia con il namespace appropriato
		},
		Spec: k8sdinovaonev1.FortigateUpdatePathSpec{
			Model:        model,
			StartVersion: minVersion,
			NewVersion:   maxVersion,
		},
	}
	
	// Qui dovresti avere un client Kubernetes per creare la risorsa
	if err := r.Create(context.Background(), newUpgradePath); err != nil {
		fmt.Printf("Errore durante la creazione della risorsa FortigateUpdatePath: %v\n", err)
		return fmt.Errorf("errore durante la creazione della risorsa FortigateUpdatePath: %w", err)
	}

	return nil
}

func updateFortigateUpgradeStatus(r *FortigateUpdateReconciler, fu *k8sdinovaonev1.FortigateUpdate, statusInfo k8sdinovaonev1.FortigateUpdateStatus) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Modifichiamo direttamente l'oggetto passato come puntatore (*fu)
	fu.Status.Conditions = []metav1.Condition{
		{
			Type:               "Available",
			Status:             metav1.ConditionTrue,
			Reason:             "FortigateConfigApplied",
			Message:            "La configurazione è stata applicata con successo a Fortigate",
			LastTransitionTime: metav1.Now(), // È buona pratica aggiungere il timestamp nelle condizioni
		},
	}

	fmt.Println("Tentativo di aggiornamento dello status dell'update nel cluster...")

	fu.Status.BackupName = statusInfo.BackupName

	// 2. Eseguiamo l'update dello status
	if err := r.Status().Update(ctx, fu); err != nil {
		fmt.Printf("Errore durante r.Status().Update: %v\n", err)
		return err
	}

	fmt.Println("Stato aggiornato con successo sul cluster!")
	return nil
}
