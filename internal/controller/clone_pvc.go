package controller

import (
	"context"
	"fmt"
	"log"
	"time"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func CreateNewFirewallPVC(firewallName string, fortigateVersion string, namespace string, storageClass string, ctx context.Context, r *FortigateFirewallReconciler) error {
	// verifica che la storage class esista
	exists, err := storageClassExists(ctx, r, storageClass)
	if err != nil {
		fmt.Printf("Errore durante la verifica della StorageClass '%s': %v\n", storageClass, err)
		return err
	}
	if !exists {
		fmt.Printf("La StorageClass '%s' non esiste. Assicurati di averla creata prima di procedere.\n", storageClass)
		return fmt.Errorf("storage class '%s' non trovata", storageClass)
	}

	clonePVC("fortios-"+fortigateVersion+"-fgt", firewallName+"-"+fortigateVersion+"-fgt", namespace, storageClass)
	clonePVC("fortios-"+fortigateVersion+"-cloud-init", firewallName+"-"+fortigateVersion+"-cloud-init", namespace, storageClass)

	CheckFirewallPVC(firewallName+"-"+fortigateVersion+"-fgt", namespace)
	CheckFirewallPVC(firewallName+"-"+fortigateVersion+"-cloud-init", namespace)

	return nil
}

func storageClassExists(ctx context.Context, r *FortigateFirewallReconciler, scName string) (bool, error) {
	storageClass := &storagev1.StorageClass{}

	// Usiamo il client dell'operator per fare la Get.
	// Siccome le StorageClass sono cluster-scoped, lasciamo il Namespace vuoto.
	err := r.Get(ctx, types.NamespacedName{Name: scName}, storageClass)

	if err != nil {
		if errors.IsNotFound(err) {
			// La StorageClass NON esiste.
			// Qui puoi gestire la logica (es. bloccare il reconcile o crearne una di fallback)
			return false, nil
		}
		// Errore generico di comunicazione
		return false, err
	}

	// Se siamo qui, la StorageClass esiste e l'oggetto `storageClass` contiene i suoi dati
	return true, nil
}

func CheckFirewallPVC(name string, namespace string) error {
	// 1. Configura il caricamento del kubeconfig
	config, err := rest.InClusterConfig()
	if err != nil {
		fmt.Printf("Errore nel caricamento della configurazione: %v", err)
		return err
	}

	// 2. Crea il clientset di Kubernetes
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Printf("Errore nella creazione del clientset: %v", err)
		return err
	}

	for {
		// 1. IMPORTANTE: Rileggi lo stato fresco del PVC dal cluster ad ogni giro
		pvc, err := clientset.CoreV1().PersistentVolumeClaims(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			fmt.Printf("Errore nel recupero del PVC: %v\n", err)
			return err
		}

		// 2. Controlla la fase attuale
		if pvc.Status.Phase == corev1.ClaimBound {
			// Se è finalmente pronto, esci dal ciclo
			break
		}

		// Altrimenti mostra lo stato attuale (es. Pending) e aspetta
		fmt.Printf("Stato attuale del PVC '%s': %s. Ricontrollo tra 3 secondi...\n", name, pvc.Status.Phase)
		time.Sleep(3 * time.Second)
	}

	return nil
}

func DeleteFirewallPVC(ctx context.Context, r *FortigateFirewallReconciler, firewallName string, fortigateVersion string, namespace string) error {
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Printf("Errore nel caricamento della configurazione: %v", err)
		return err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Printf("Errore nella creazione del clientset: %v", err)
		return err
	}

	pvcToDelete := []string{firewallName + "-" + fortigateVersion + "-fgt", firewallName + "-" + fortigateVersion + "-cloud-init"}

	for _, pvcName := range pvcToDelete {
		err := clientset.CoreV1().PersistentVolumeClaims(namespace).Delete(context.TODO(), pvcName, metav1.DeleteOptions{})
		if err != nil {
			log.Printf("Errore durante l'eliminazione del PVC '%s': %v", pvcName, err)
			return err
		} else {
			fmt.Printf("PVC '%s' eliminato con successo!\n", pvcName)
		}
	}

	return nil
}

func clonePVC(source string, target string, namespace string, storageClass string) error {
	// 1. Configura il caricamento del kubeconfig
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Printf("Errore nel caricamento della configurazione: %v", err)
		return err
	}

	// 2. Crea il clientset di Kubernetes
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Printf("Errore nella creazione del clientset: %v", err)
		return err
	}

	// Definizione delle variabili di configurazione

	pvcOrigine := source
	pvcClonato := target

	if _, err := clientset.CoreV1().PersistentVolumeClaims(namespace).Get(context.TODO(), pvcClonato, metav1.GetOptions{}); err == nil {
		fmt.Printf("Il PVC '%s' esiste già, non lo creo.\n", pvcClonato)
		return err
	}

	// 3. Definisci la struttura del nuovo PVC con il DataSource
	nuovoPVC := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcClonato,
			Namespace: namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClass,
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce, // Deve essere compatibile con l'originale
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					// La dimensione deve essere UGUALI o SUPERIORE al PVC originale
					corev1.ResourceStorage: pvcOriginalSize(clientset, namespace, pvcOrigine),
				},
			},
			// QUESO BLOCCO ABILITA IL CLONING NATIVO
			DataSource: &corev1.TypedLocalObjectReference{
				APIGroup: pointerToString(""), // Stringa vuota per le API core di K8s
				Kind:     "PersistentVolumeClaim",
				Name:     pvcOrigine,
			},
		},
	}

	// 4. Applica il nuovo PVC sul cluster
	fmt.Printf("Avvio clonazione del PVC '%s' in '%s'...\n", pvcOrigine, pvcClonato)
	result, err := clientset.CoreV1().PersistentVolumeClaims(namespace).Create(
		context.TODO(),
		nuovoPVC,
		metav1.CreateOptions{},
	)
	if err != nil {
		log.Printf("Errore durante la creazione del PVC clonato: %v", err)
		return err
	}

	fmt.Printf("PVC clonato creato con successo! Nome: %s\n", result.GetObjectMeta().GetName())

	return nil
}

// Funzione di supporto per ottenere la dimensione esatta del PVC originale
func pvcOriginalSize(clientset *kubernetes.Clientset, namespace, name string) resource.Quantity {
	origPvc, err := clientset.CoreV1().PersistentVolumeClaims(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		log.Printf("Impossibile recuperare il PVC di origine per verificarne la dimensione: %v", err)
		return resource.Quantity{}
	}
	return origPvc.Spec.Resources.Requests[corev1.ResourceStorage]
}

// Funzione helper per gestire i puntatori a stringa richiesti dalla StorageClassName
func pointerToString(s string) *string {
	return &s
}
