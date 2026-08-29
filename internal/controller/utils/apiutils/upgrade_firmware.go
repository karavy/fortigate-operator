package apiutils

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	fileutils "github.com/karavy/k8s-operator-fortigate/internal/controller/utils/fileutils"
	s3utils "github.com/karavy/k8s-operator-fortigate/internal/controller/utils/s3utils"
)

func getFirmwareNameFromVersion(version, S3BucketName, accessKeyID, secretAccessKey, s3Url string) (string, error) {
	if firmware, err := fileutils.ListAllFilesInDir("/firmwares/" + version + "/"); err == nil && len(firmware) > 0 {
		return firmware[0], nil
	}

	if err := s3utils.CopyS3DirContent(S3BucketName, accessKeyID, secretAccessKey, s3Url, "firmwares/"+version, "/"); err != nil {
		fmt.Println(err)
		return "", err
	}

	firmware, err := fileutils.ListAllFilesInDir("/firmwares/" + version + "/")
	if err != nil {
		fmt.Printf("Impossibile recuperare il nome del file: %s", err)
		return "", err
	}

	return firmware[0], nil
}

func UpgradeFortigateFirmware(ctx context.Context, f *FortigateClient, version, S3BucketName, accessKeyID, secretAccessKey, s3Url string) (APIResponse, error) {
	fmt.Printf("Inizio dell'upgrade del firmware alla versione %s\n", version)

	filePath, err := getFirmwareNameFromVersion("v"+version, S3BucketName, accessKeyID, secretAccessKey, s3Url)
	if err != nil {
		fmt.Printf("Errore durante il recupero del nome del firmware dalla versione: %v", err)
		return APIResponse{}, fmt.Errorf("failed to get firmware name from version: %w", err)
	}

	// 1. Crea il file temporaneo su disco
	tmpFile, err := os.CreateTemp("", "fortigate-multipart-*.tmp")
	if err != nil {
		return APIResponse{}, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// 2. Inizializza il writer puntando AL FILE TEMPORANEO
	writer := multipart.NewWriter(tmpFile)

	// 3. Scrivi i campi (source e file binario)
	if err := writer.WriteField("source", "upload"); err != nil {
		return APIResponse{}, err
	}

	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return APIResponse{}, err
	}

	inputFile, err := os.Open(filePath)
	if err != nil {
		return APIResponse{}, err
	}
	defer inputFile.Close()

	// Copia il firmware (in streaming su disco)
	if _, err := io.Copy(part, inputFile); err != nil {
		return APIResponse{}, err
	}

	// --- CRUCIALE ---
	// Chiudiamo il writer ADESSO. Questo scrive i byte finali del boundary sul file temporaneo.
	writer.Close()

	// --- CRUCIALE ---
	// Solo ORA che il file è totalmente scritto e chiuso dal multipart,
	// chiediamo al sistema operativo la dimensione esatta di TUTTO il file temporaneo.
	tmpFileInfo, err := tmpFile.Stat()
	if err != nil {
		return APIResponse{}, err
	}
	totalLength := tmpFileInfo.Size() // Questa dimensione include TUTTO al 100% senza errori

	// 4. Riporta il cursore del file all'inizio per permettere l'invio HTTP
	_, _ = tmpFile.Seek(0, 0)

	// 5. Prepara la richiesta HTTP passando l'intero file temporaneo come Body
	url := fmt.Sprintf("https://%s/api/v2/monitor/system/firmware/upgrade", f.address)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, tmpFile)
	if err != nil {
		return APIResponse{}, err
	}

	req.Header.Set("Authorization", "Bearer "+f.token)

	// Usiamo lo STESSO writer che ha scritto il file per estrarre l'header Content-Type corretto
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Assegniamo la lunghezza al millimetro
	req.ContentLength = totalLength

	// 6. Invia la richiesta
	resp, err := sendRequest(f.hClient, req, FORMATFORTIGATE)
	if err != nil {
		return APIResponse{}, err
	}

	return resp.(APIResponse), nil
}
