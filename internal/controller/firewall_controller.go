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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	k8sdinovaonev1 "github.com/karavy/k8s-operator-fortigate/api/v1"

	_ "github.com/karavy/k8s-operator-fortigate/internal/controller/vyos"
)

const (
	conditionTypeNetworkReady = "NetworkReady"
	reasonNADConflict         = "NetworkAttachmentDefinitionConflict"
	reasonNetworkProvisioned  = "NetworkProvisioned"
)

type firewallPortNAD struct {
	bridgeName string
	portName   string
}

type nadConflictError struct {
	msg string
}
func (e *nadConflictError) Error() string { return e.msg }

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

	bridgeName := bridgeNameFor(fw.Namespace, fw.Name)

	if err := r.ensureNodeBridges(ctx, &fw, bridgeName); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensureNodeBridges: %w", err)
	}

	var portsNADs []firewallPortNAD

	if ps, conflictErr := r.ensureNetworkAttachmentDefinitions(ctx, &fw, bridgeName); conflictErr != nil {
		var nadConflict *nadConflictError
		if errors.As(conflictErr, &nadConflict) {
			// Conflitto su un NAD esistente non di nostra proprietà: non è un
			// errore transiente che vogliamo far ritentare in loop. Lo
			// registriamo come condition e interrompiamo qui, PRIMA di
			// createFirewall, senza restituire un errore (niente requeue
			// automatico). L'utente deve risolvere il conflitto manualmente;
			// una volta risolto, la prossima modifica alla Spec (o un touch
			// manuale della CR) farà ripartire la reconcile.
			meta.SetStatusCondition(&fw.Status.Conditions, metav1.Condition{
				Type:    conditionTypeNetworkReady,
				Status:  metav1.ConditionFalse,
				Reason:  reasonNADConflict,
				Message: nadConflict.Error(),
			})
			if statusErr := r.Status().Update(ctx, &fw); statusErr != nil {
				return ctrl.Result{}, fmt.Errorf("aggiornamento status dopo conflitto NAD: %w", statusErr)
			}
			return ctrl.Result{}, nil
		}

		// Errore generico/transitorio (es. API server irraggiungibile): lascia
		// che il retry standard del controller-runtime se ne occupi.
		return ctrl.Result{}, fmt.Errorf("ensureNetworkAttachmentDefinitions: %w", conflictErr)
	} else {
		portsNADs = ps
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

	_, err := createFirewall(r, ctx, &fw, portsNADs)
	if err != nil {
		return ctrl.Result{}, err
	}

	//vyos.InitVyosClient()

	return ctrl.Result{}, nil
}

func (r *FirewallReconciler) ensureNodeBridges(ctx context.Context, fw *k8sdinovaonev1.Firewall, bridgeName string) error {
	for _, port := range fw.Spec.Ports {
		nb := &k8sdinovaonev1.NodeBridge{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s", fw.Name, port.Name),
				Namespace: fw.Namespace,
				Labels: map[string]string{
					"firewall": fw.Name,
					"port":     port.Name,
				},
			},
		}
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, nb, func() error {
			nb.Spec.UplinkInterface = port.UplinkInterface
			nb.Spec.VlanID = port.VlanID
			nb.Spec.BridgeName = bridgeName
			nb.Spec.GatewayIP = port.GatewayIP
			return controllerutil.SetControllerReference(fw, nb, r.Scheme)
		})
		if err != nil {
			return fmt.Errorf("nodebridge per porta %s: %w", port.Name, err)
		}
	}
	return nil
}

func (r *FirewallReconciler) ensureNetworkAttachmentDefinitions(ctx context.Context, fw *k8sdinovaonev1.Firewall, bridgeName string) ([]firewallPortNAD, error) {
	logger := logf.FromContext(ctx)

	var portsNADs []firewallPortNAD

	var nadGVK = schema.GroupVersionKind{
		Group:   "k8s.cni.cncf.io",
		Version: "v1",
		Kind:    "NetworkAttachmentDefinition",
	}

	for _, port := range fw.Spec.Ports {
		nadName := fmt.Sprintf("%s-%s", fw.Name, port.Name)

		existing := &unstructured.Unstructured{}
		existing.SetGroupVersionKind(nadGVK)

		err := r.Get(ctx, client.ObjectKey{Name: nadName, Namespace: fw.Namespace}, existing)
		switch {
		case err == nil:
			owned := false
			for _, ref := range existing.GetOwnerReferences() {
				if ref.UID == fw.UID {
					owned = true
					break
				}
			}
			if !owned {
				logger.Error(
					fmt.Errorf("NetworkAttachmentDefinition esistente non di proprietà di questo Firewall"),
					"conflitto NAD: reconcile interrotta, il Firewall non verrà creato/aggiornato",
					"nad", nadName, "namespace", fw.Namespace, "firewall", fw.Name, "port", port.Name,
				)
				return nil, &nadConflictError{msg: fmt.Sprintf(
					"NetworkAttachmentDefinition %s/%s esiste già e non è gestita da questo Firewall: risolvi il conflitto manualmente prima di riprovare",
					fw.Namespace, nadName,
				)}
			}
			// esiste ed è di nostra proprietà: procede con l'update sotto.
		case apierrors.IsNotFound(err):
			// non esiste ancora: procede con la creazione sotto.
		default:
			return nil, fmt.Errorf("verifica NetworkAttachmentDefinition %s: %w", nadName, err)
		}

		config := fmt.Sprintf(
			`{"cniVersion":"0.4.0","type":"bridge","bridge":%q,"vlan":%s,"ipam":{}}`,
			bridgeName, vlanIDForCNIConfig(port.VlanID),
		)

		nad := &unstructured.Unstructured{}
		nad.SetGroupVersionKind(nadGVK)
		nad.SetName(nadName)
		nad.SetNamespace(fw.Namespace)

		portsNADs = append(portsNADs, firewallPortNAD{
			bridgeName: nadName,
			portName:   port.Name,})

		_, err = controllerutil.CreateOrUpdate(ctx, r.Client, nad, func() error {
			nad.SetLabels(map[string]string{
				"firewall": fw.Name,
				"port":     port.Name,
			})
			if err := unstructured.SetNestedField(nad.Object, config, "spec", "config"); err != nil {
				return fmt.Errorf("impostazione spec.config del NAD %s: %w", nadName, err)
			}
			return controllerutil.SetControllerReference(fw, nad, r.Scheme)
		})

		if err != nil {
			return nil, fmt.Errorf("creazione/aggiornamento NetworkAttachmentDefinition %s: %w", nadName, err)
		}
	}

	return portsNADs, nil
}

func bridgeNameFor(namespace, name string) string {
	sum := sha256.Sum256([]byte(namespace + "/" + name))
	hash := hex.EncodeToString(sum[:])[:12]
	return fmt.Sprintf("br-%s", hash)
}

// vlanIDForCNIConfig serializza il VlanID come literal JSON: "null" se assente,
// altrimenti il numero. Evita di dover marshalare l'intera config con json.Marshal
// solo per questo campo opzionale.
func vlanIDForCNIConfig(v *int32) string {
	if v == nil {
		return "null"
	}
	return fmt.Sprintf("%d", *v)
}

// SetupWithManager sets up the controller with the Manager.
func (r *FirewallReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&k8sdinovaonev1.Firewall{}).
		Owns(&k8sdinovaonev1.NodeBridge{}).
		Named("firewall").
		Complete(r)
}
