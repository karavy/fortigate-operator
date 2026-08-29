// execute restore image url http://192.168.50.57/FGT_VM64_KVM-v8.0.0.F-build0167-FORTINET.out
// execute reboot
// creare una configmap con l'associazione di versione e url dell'immagine
// inserire anche la distinzione tra image di installazione e image di upgrade (se necessario)
// deve essere specificato anche l'upgrade path
package upgrade

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	k8sdinovaonev1 "github.com/karavy/k8s-operator-fortigate/api/v1"
	apiutils "github.com/karavy/k8s-operator-fortigate/internal/controller/utils/apiutils"
	secretsutils "github.com/karavy/k8s-operator-fortigate/internal/controller/utils/secretsutils"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sshutils "github.com/karavy/k8s-operator-fortigate/internal/controller/fortigate/sshutils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func BackupForUpgradeFirewall(fwInstance *k8sdinovaonev1.FortigateFirewall, firewallVersion string, instance *k8sdinovaonev1.FortigateUpdate, token string, ctx context.Context, r client.Client, statusInfo k8sdinovaonev1.FortigateUpdateStatus) (string, string, error) {
	var backupFilename string

	firewallNewVersion := instance.Spec.NewVersion
	namespace := instance.Namespace

	firewallName := fwInstance.Name
	fortiIP := fmt.Sprintf("%s-%s-ssh-gui.%s.svc.cluster.local", firewallName, firewallVersion, namespace)

	awsKey, err := secretsutils.GetSecretValues(ctx, r, namespace, fwInstance.Spec.AWSCredentialSecretName, []string{"s3Url", "accessKeyID", "secretAccessKey"})
	if err != nil {
		fmt.Printf("Errore nel recupero dei valori del secret: %v", err)
		return "", "", err
	}

	backupFilename, err = apiutils.SendCommandApiGet(firewallName, ctx, fortiIP, token, apiutils.BACKUP, fwInstance.Spec.S3BucketName, awsKey["accessKeyID"], awsKey["secretAccessKey"], awsKey["s3Url"], instance)
	if err != nil {
		fmt.Printf("Errore durante il backup del firewall: %v", err)
		return backupFilename, "", err
	}

	statusInfo.BackupName = backupFilename

	if err := UpdateFortigateUpgradeStatus(r, instance, statusInfo); err != nil {
		fmt.Printf("Errore durante l'aggiornamento dello status dell'update: %v\n", err)
		return backupFilename, "", err
	}

	fmt.Printf("Backup completato con successo: %s\n", backupFilename)
	snapshotName := fmt.Sprintf("%s-%s-%s", firewallName, firewallVersion, firewallNewVersion)
	if err := sshutils.TriggerKubeVirtVMSnapshot(ctx, r, namespace, firewallName, snapshotName); err != nil {
		fmt.Printf("Errore: %v", err)
		return backupFilename, snapshotName, err
	}

	return backupFilename, snapshotName, nil
}

func GetStatusByParams(ctx context.Context, r client.Client, namespace, nmodel, newVersion, currentVersion string) (*k8sdinovaonev1.FortigateUpdatePathStatus, error) {
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

    // Parsa la versione da cercare (target)
    targetVer, err := semver.NewVersion(newVersion)
    if err != nil {
		fmt.Printf("Errore durante il parsing della versione target '%s': %v\n", newVersion, err)
        return nil, fmt.Errorf("la versione target '%s' non è un semver valido: %w", newVersion, err)
    }

	fmt.Printf("Versione target parsata correttamente: %s\n", targetVer.String())

    // 2. Filtriamo in memoria per verificare i range di versione (Min/Max) e la startVersion
    for _, item := range list.Items {
        // Parsa i limiti della risorsa (Assumendo che la tua CRD abbia i campi Spec.MinVersion e Spec.MaxVersion)
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

	if err := createFortigateUpdatePath(currentVersion, newVersion, nmodel, namespace, r); err != nil {
		fmt.Printf("Errore durante la creazione del percorso di aggiornamento: %v\n", err)
		return nil, fmt.Errorf("errore durante la creazione del percorso di aggiornamento: %w", err)
	}

    return nil, fmt.Errorf("nessuna risorsa trovata nel range di versioni specificato")
}

func createFortigateUpdatePath(minVersion, maxVersion, model, namespace string, r client.Client) error {
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

func UpdateFortigateUpgradeStatus(r client.Client, fu *k8sdinovaonev1.FortigateUpdate, statusInfo k8sdinovaonev1.FortigateUpdateStatus) error {
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
