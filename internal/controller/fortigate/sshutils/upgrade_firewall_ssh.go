package sshutils

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	// Importa il pacchetto delle API di KubeVirt per gli Snapshot
	snapshotv1 "kubevirt.io/api/snapshot/v1alpha1"
)

func UpgradeFirmwareSSH(stdin io.WriteCloser, stdout io.Reader, url string) (string, error) {

	go func() {
		defer stdin.Close()

		// Un piccolo trucco: premiamo invio una volta per pulire il prompt
		fmt.Fprint(stdin, "\r")
		time.Sleep(200 * time.Millisecond)

		// Inviamo il comando di generazione del token
		command := fmt.Sprintf("execute restore image url %s\r", url)
		fmt.Fprint(stdin, command)
		time.Sleep(500 * time.Millisecond) // Tempo per far apparire il prompt della password

		command = "y\r"
		fmt.Fprint(stdin, command)
		time.Sleep(5000 * time.Millisecond)

		command = "y\r"
		fmt.Fprint(stdin, command)
		time.Sleep(5000 * time.Millisecond)

		command = "y\r"
		fmt.Fprint(stdin, command)
		time.Sleep(5000 * time.Millisecond)

		// Chiudiamo la sessione digitando exit
		fmt.Fprint(stdin, "exit\r")
	}()

	var stdoutBuffer bytes.Buffer
	_, err := io.Copy(&stdoutBuffer, stdout)
	if err != nil && err != io.EOF {
		fmt.Printf("Errore durante la lettura dell'output: %v", err)
	}

	output := stdoutBuffer.String()

	fmt.Printf("Output completo del firewall per debug:\n%s", output)

	return output, nil
}
func TriggerKubeVirtVMSnapshot(ctx context.Context, r client.Client, namespace, vmName, snapshotName string) error {
	// 1. Definizione del VirtualMachineSnapshot
	apiGroup := "kubevirt.io"
	vmSnapshot := &snapshotv1.VirtualMachineSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      snapshotName,
			Namespace: namespace,
		},
		Spec: snapshotv1.VirtualMachineSnapshotSpec{
			Source: corev1.TypedLocalObjectReference{
				APIGroup: &apiGroup,
				Kind:     "VirtualMachine",
				Name:     vmName,
			},
		},
	}

	// 1. Definiamo l'oggetto (vuoto) per verificare se esiste già nel cluster
	existingSnapshot := &snapshotv1.VirtualMachineSnapshot{}
	err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: snapshotName}, existingSnapshot)

	if err == nil {
		// La snapshot esiste già! Scegli l'azione desiderata:
		// Opzione A: Ritorna nil o un log dicendo che esiste già (evita duplicati)
		return nil

		// Opzione B: Ritorna un errore personalizzato se per te è un problema
		// return fmt.Errorf("la snapshot '%s' esiste già nel namespace '%s'", snapshotName, namespace)
	}

	// Se l'errore è diverso da "NotFound", significa che c'è un problema di connessione/permessi
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("errore durante la verifica della snapshot esistente: %w", err)
	}

	// 2. Creazione della risorsa usando il client del reconciler (r.Client)
	// Nota: Kubebuilder usa logr per i log, evita il fmt.Printf nei controller veri e propri
	if err := r.Create(ctx, vmSnapshot); err != nil {
		fmt.Printf("Errore creazione VirtualMachineSnapshot: %v", err)
		return err
	}

	return nil
}

func DeleteKubeVirtVMSnapshot(ctx context.Context, r client.Client, namespace, snapshotName string) error {
	// 1. Definiamo la struttura minima con Name e Namespace
	vmSnapshot := &snapshotv1.VirtualMachineSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      snapshotName,
			Namespace: namespace,
		},
	}

	// 2. Richiesta di eliminazione al cluster
	err := r.Delete(ctx, vmSnapshot)
	if err != nil {
		// Se l'oggetto non esiste già più, consideriamo l'operazione riuscita (idempotenza)
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("errore durante la cancellazione della snapshot '%s': %w", snapshotName, err)
	}

	return nil
}
