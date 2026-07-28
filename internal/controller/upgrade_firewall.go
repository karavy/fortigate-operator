// execute restore image url http://192.168.50.57/FGT_VM64_KVM-v8.0.0.F-build0167-FORTINET.out
// execute reboot
// creare una configmap con l'associazione di versione e url dell'immagine
// inserire anche la distinzione tra image di installazione e image di upgrade (se necessario)
// deve essere specificato anche l'upgrade path
package controller

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	k8sdinovaonev1 "github.com/karavy/k8s-operator-fortigate/api/v1"
)

func backupForUpgradeFirewall(fwInstance *k8sdinovaonev1.FortigateFirewall, firewallVersion string, instance *k8sdinovaonev1.FortigateUpdate, token string, ctx context.Context, r *FortigateUpdateReconciler, statusInfo k8sdinovaonev1.FortigateUpdateStatus) (string, string, error) {
	var backupFilename string

	firewallNewVersion := instance.Spec.NewVersion
	namespace := instance.Namespace

	firewallName := fwInstance.Name
	fortiIP := fmt.Sprintf("%s-%s-ssh-gui.%s.svc.cluster.local", firewallName, firewallVersion, namespace)

	awsKey, err := getSecretValues(ctx, r.Client, namespace, fwInstance.Spec.AWSCredentialSecretName, []string{"s3Url", "accessKeyID", "secretAccessKey"})
	if err != nil {
		fmt.Printf("Errore nel recupero dei valori del secret: %v", err)
		return "", "", err
	}

	backupFilename, err = SendCommandApiGet(firewallName, ctx, fortiIP, token, BACKUP, fwInstance.Spec.S3BucketName, awsKey["accessKeyID"], awsKey["secretAccessKey"], awsKey["s3Url"], instance)
	if err != nil {
		fmt.Printf("Errore durante il backup del firewall: %v", err)
		return backupFilename, "", err
	}

	statusInfo.BackupName = backupFilename

	if err := updateFortigateUpgradeStatus(r, instance, statusInfo); err != nil {
		fmt.Printf("Errore durante l'aggiornamento dello status dell'update: %v\n", err)
		return backupFilename, "", err
	}

	fmt.Printf("Backup completato con successo: %s\n", backupFilename)
	snapshotName := fmt.Sprintf("%s-%s-%s", firewallName, firewallVersion, firewallNewVersion)
	if err := triggerKubeVirtVMSnapshot(ctx, r, namespace, firewallName, snapshotName); err != nil {
		fmt.Printf("Errore: %v", err)
		return backupFilename, snapshotName, err
	}

	return backupFilename, snapshotName, nil
}

func getFirmwareNameFromVersion(version, S3BucketName, accessKeyID, secretAccessKey, s3Url string) (string, error) {
	if firmware, err := listAllFilesInDir("/firmwares/" + version + "/"); err == nil && len(firmware) > 0 {
		return firmware[0], nil
	}
	
	if err := copyS3DirContent(S3BucketName, accessKeyID, secretAccessKey, s3Url, "firmwares/"+version, "/"); err != nil {
		fmt.Println(err)
		return "", err
	}

	firmware, err := listAllFilesInDir("/firmwares/" + version + "/")
	if err != nil {
		fmt.Printf("Impossibile recuperare il nome del file: %s", err)
		return "", err
	}

	return firmware[0], nil
}

func upgradeFortigateFirmware(ctx context.Context, f *FortigateClient, version, S3BucketName, accessKeyID, secretAccessKey, s3Url string) (apiResponse, error) {
	fmt.Printf("Inizio dell'upgrade del firmware alla versione %s\n", version)

	filePath, err := getFirmwareNameFromVersion("v" + version, S3BucketName, accessKeyID, secretAccessKey, s3Url)
	if err != nil {
		fmt.Printf("Errore durante il recupero del nome del firmware dalla versione: %v", err)
		return apiResponse{}, fmt.Errorf("failed to get firmware name from version: %w", err)
	}

	// 1. Crea il file temporaneo su disco
	tmpFile, err := os.CreateTemp("", "fortigate-multipart-*.tmp")
	if err != nil {
		return apiResponse{}, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// 2. Inizializza il writer puntando AL FILE TEMPORANEO
	writer := multipart.NewWriter(tmpFile)

	// 3. Scrivi i campi (source e file binario)
	if err := writer.WriteField("source", "upload"); err != nil {
		return apiResponse{}, err
	}

	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return apiResponse{}, err
	}

	inputFile, err := os.Open(filePath)
	if err != nil {
		return apiResponse{}, err
	}
	defer inputFile.Close()

	// Copia il firmware (in streaming su disco)
	if _, err := io.Copy(part, inputFile); err != nil {
		return apiResponse{}, err
	}

	// --- CRUCIALE ---
	// Chiudiamo il writer ADESSO. Questo scrive i byte finali del boundary sul file temporaneo.
	writer.Close()

	// --- CRUCIALE ---
	// Solo ORA che il file è totalmente scritto e chiuso dal multipart,
	// chiediamo al sistema operativo la dimensione esatta di TUTTO il file temporaneo.
	tmpFileInfo, err := tmpFile.Stat()
	if err != nil {
		return apiResponse{}, err
	}
	totalLength := tmpFileInfo.Size() // Questa dimensione include TUTTO al 100% senza errori

	// 4. Riporta il cursore del file all'inizio per permettere l'invio HTTP
	_, _ = tmpFile.Seek(0, 0)

	// 5. Prepara la richiesta HTTP passando l'intero file temporaneo come Body
	url := fmt.Sprintf("https://%s/api/v2/monitor/system/firmware/upgrade", f.address)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, tmpFile)
	if err != nil {
		return apiResponse{}, err
	}

	req.Header.Set("Authorization", "Bearer "+f.token)

	// Usiamo lo STESSO writer che ha scritto il file per estrarre l'header Content-Type corretto
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Assegniamo la lunghezza al millimetro
	req.ContentLength = totalLength

	// 6. Invia la richiesta
	resp, err := sendRequest(f.hClient, req, FORMATFORTIGATE)
	if err != nil {
		return apiResponse{}, err
	}

	return resp.(apiResponse), nil
}
