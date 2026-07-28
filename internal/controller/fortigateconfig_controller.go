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
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	k8sdinovaonev1 "github.com/karavy/k8s-operator-fortigate/api/v1"
)

// FortigateConfigReconciler reconciles a FortigateConfig object
type FortigateConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=k8s.dinova.one,resources=fortigateconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=k8s.dinova.one,resources=fortigateconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=k8s.dinova.one,resources=fortigateconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachineinstances;virtualmachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch

func (r *FortigateConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)

	var fortiConfig k8sdinovaonev1.FortigateConfig
	err := r.Get(ctx, req.NamespacedName, &fortiConfig)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	fortigateName := fortiConfig.Spec.FortigateName

	requestedFw := types.NamespacedName{
		Name:      fortigateName, // Il nome esatto della risorsa su K8s
		Namespace: req.Namespace, // Puoi usare il namespace della richiesta corrente
	}

	firewallInstance := &k8sdinovaonev1.FortigateFirewall{}
	if err := r.Get(ctx, requestedFw, firewallInstance); err != nil {
		fmt.Printf("Errore durante la lettura dell'istanza per l'aggiornamento dello status: %v\n", err)
		return ctrl.Result{}, err
	}

	s3Url, accessKeyID, secretAccessKey, err := getS3CredentialsFromSecret(ctx, r, firewallInstance, req.Namespace)
	if err != nil {
		fmt.Printf("Errore durante la lettura delle credenziali S3: %v\n", err)
		return ctrl.Result{}, err
	}

	uuid, err := prepareTerraformEnvironment(fortiConfig, firewallInstance, s3Url, accessKeyID, secretAccessKey)
	if err != nil {
		fmt.Printf("Errore durante la preparazione dell'ambiente Terraform: %v\n", err)
		return ctrl.Result{}, err
	}

	forticonfigFinalizerName := "fortigateconfig.dinova.one/finalizer"

	// 2. CONTROLLA SE LA RISORSA È IN FASE DI ELIMINAZIONE
	if fortiConfig.ObjectMeta.DeletionTimestamp.IsZero() {
		// La risorsa NON è in fase di eliminazione.
		// Assicurati che il tuo finalizer sia registrato nell'oggetto.
		if !controllerutil.ContainsFinalizer(&fortiConfig, forticonfigFinalizerName) {
			controllerutil.AddFinalizer(&fortiConfig, forticonfigFinalizerName)
			if err := r.Update(ctx, &fortiConfig); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// LA RISORSA È IN FASE DI ELIMINAZIONE (DeletionTimestamp è presente)
		if controllerutil.ContainsFinalizer(&fortiConfig, forticonfigFinalizerName) {

			// === IL TUO METODO DI CANCELLAZIONE ===
			// Esegui qui tutte le operazioni di pulizia (es. eliminare il PVC clonato in Go)
			if err := r.deleteExternalResources(&fortiConfig, firewallInstance, s3Url, accessKeyID, secretAccessKey, uuid); err != nil {
				// Se fallisce, rimaniamo in coda e non rimuoviamo il finalizer
				return ctrl.Result{}, err
			}

			// Rimuovi il finalizer per dire a Kubernetes che la pulizia è completata
			controllerutil.RemoveFinalizer(&fortiConfig, forticonfigFinalizerName)
			if err := r.Update(ctx, &fortiConfig); err != nil {
				return ctrl.Result{}, err
			}
		}

		// Riconciliazione finita, l'oggetto sparirà dal cluster
		return ctrl.Result{}, nil
	}

	statusInfo, err := doReconcileConfig(ctx, r, req, fortiConfig, firewallInstance)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := updateFortigateConfigStatus(r, &fortiConfig, statusInfo); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func getS3CredentialsFromSecret(ctx context.Context, r *FortigateConfigReconciler, firewallInstance *k8sdinovaonev1.FortigateFirewall, namespace string) (s3Url string, accessKeyId string, secretAccessKey string, reterr error) {

	awsCredentialSecretName := firewallInstance.Spec.AWSCredentialSecretName

	if awsCredentialSecretName == "" {
		fmt.Println("Nome del secret AWS non specificato. Verifica la configurazione.")
		return "", "", "", nil
	}

	awsKey, err := getSecretValues(ctx, r.Client, namespace, awsCredentialSecretName, []string{"s3Url", "accessKeyID", "secretAccessKey"})
	if err != nil {
		return "", "", "", err
	}

	return awsKey["s3Url"], awsKey["accessKeyID"], awsKey["secretAccessKey"], nil
}

func doReconcileConfig(ctx context.Context, r *FortigateConfigReconciler, req ctrl.Request, fortiConfig k8sdinovaonev1.FortigateConfig, firewallInstance *k8sdinovaonev1.FortigateFirewall) (k8sdinovaonev1.FortigateConfigStatus, error) {
	statusInfo := k8sdinovaonev1.FortigateConfigStatus{}

	s3Url, accessKeyID, secretAccessKey, err := getS3CredentialsFromSecret(ctx, r, firewallInstance, req.Namespace)
	if err != nil {
		return statusInfo, err
	}

	uuid, err := prepareTerraformEnvironment(fortiConfig, firewallInstance, s3Url, accessKeyID, secretAccessKey)
	if err != nil {
		fmt.Printf("Errore durante la preparazione dell'ambiente Terraform: %v\n", err)
		return statusInfo, err
	}

	if _, _, err := modifyTFFiles(fortiConfig, firewallInstance, s3Url, accessKeyID, secretAccessKey, uuid); err != nil {
		fmt.Printf("Errore durante la modifica dei file Terraform: %v\n", err)
		return statusInfo, err
	}
	
	return statusInfo, nil

}

func modifyTFFiles(fortiConfig k8sdinovaonev1.FortigateConfig, firewallInstance *k8sdinovaonev1.FortigateFirewall, s3Url string, accessKeyID string, secretAccessKey string, uuid string) (string, string, error) {
	workingDir := "/tmp/" + firewallInstance.Name + "/" + fortiConfig.Name + "/" + uuid 
	template := fortiConfig.Spec.TerraformTemplateS3Key

	fmt.Printf("Working dir: %s\n", workingDir)

	fortiIP := fmt.Sprintf("%s-%s-ssh-gui.%s.svc.cluster.local", firewallInstance.Name, firewallInstance.Spec.FortigateVersion, fortiConfig.Namespace)
	if err := selectOperatorRule(template, workingDir, firewallInstance.Status.Token, fortiIP, fortiConfig, firewallInstance, s3Url, accessKeyID, secretAccessKey, uuid, true); err != nil {
		fmt.Printf("OperatorRule '%v' non riconosciuta. Nessuna azione eseguita.\n", err)
		return "", "", err
	}
	
	return workingDir, template, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FortigateConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&k8sdinovaonev1.FortigateConfig{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named("fortigateconfig").
		Complete(r)
}

func (r *FortigateConfigReconciler) deleteExternalResources(fortiConfig *k8sdinovaonev1.FortigateConfig, firewallInstance *k8sdinovaonev1.FortigateFirewall, s3Url string, accessKeyID string, secretAccessKey string, uuid string) error {
	// Qui puoi implementare la logica per eliminare le risorse esterne associate alla tua CRD.
	fmt.Printf("Inizio della cancellazione delle risorse esterne per la configurazione: %s\n", fortiConfig.Name)

	workingDir, _, err := modifyTFFiles(*fortiConfig, firewallInstance, s3Url, accessKeyID, secretAccessKey, uuid)
	if err != nil {
		fmt.Printf("Errore durante la modifica dei file Terraform: %v\n", err)
		return err
	}

	fortiIP := fmt.Sprintf("%s-%s-ssh-gui.%s.svc.cluster.local", firewallInstance.Name, firewallInstance.Spec.FortigateVersion, fortiConfig.Namespace)
	if err := selectOperatorRule(fortiConfig.Spec.TerraformTemplateS3Key, workingDir, firewallInstance.Status.Token, fortiIP, *fortiConfig, firewallInstance, s3Url, accessKeyID, secretAccessKey, uuid, false); err != nil {
		fmt.Printf("OperatorRule '%v' non riconosciuta. Nessuna azione eseguita.\n", err)
		return err
	}

	fmt.Printf("Cancellazione delle risorse esterne completata per la configurazione: %s\n", fortiConfig.Name)

	return nil
}

func updateFortigateConfigStatus(r *FortigateConfigReconciler, instance *k8sdinovaonev1.FortigateConfig, statusInfo k8sdinovaonev1.FortigateConfigStatus) error {
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

	instance.Status.FortigateRuleUUID = statusInfo.FortigateRuleUUID

	if err := r.Status().Update(ctx, instance); err != nil {
		fmt.Printf("Errore durante r.Status().Update: %v\n", err)
		return err
	}

	fmt.Println("Stato aggiornato con successo sul cluster!")
	return nil
}
