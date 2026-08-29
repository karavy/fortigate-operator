package apiutils

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	k8sdinovaonev1 "github.com/karavy/k8s-operator-fortigate/api/v1"
	s3utils "github.com/karavy/k8s-operator-fortigate/internal/controller/utils/s3utils"
)

// FortigateClient gestisce la comunicazione REST con l'appliance
type FortigateClient struct {
	hClient *http.Client
	address string
	token   string
}

const (
	GETFWVERSION int = iota
	UPGRADEFIRMWARE
	BACKUP
	GETFWMODEL
)

const (
	FORMATFORTIGATE int = iota
	FORMATSTATUS
	FORMATBACKUP
)

// NewFortigateClient inizializza il client HTTP configurando il TLS (spesso i firewall hanno cert self-signed)
func newFortigateClient(address, token string) *FortigateClient {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // Ignora cert self-signed in dev
	}
	return &FortigateClient{
		address: address,
		token:   token,
		hClient: &http.Client{Transport: tr, Timeout: 10 * time.Second},
	}
}

// UpdateHostname esegue la chiamata REST specifica per cambiare l'hostname su FortiOS
func SendCommandApiGet(firewallName string, ctx context.Context, address string, token string, op int, bucketName string, accessKeyID string, secretAccessKey string, s3Url string, instance any) (string, error) {
	// 2. Inizializziamo il client Fortigate usando i dati della CR
	fortiClient := newFortigateClient(address, token) // Pass empty bodyInfo for GET requests

	// 3. Eseguiamo la chiamata REST
	switch op {
	case GETFWVERSION:
		version, err := getFwVersion(ctx, fortiClient)
		if err != nil {
			fmt.Printf("Failed to get firewall version: %v\n", err)
			return "", fmt.Errorf("failed to get firewall version: %w", err)
		}
		fmt.Printf("Current firewall version: %s\n", version)
		return version, nil
	case GETFWMODEL:
		model, err := getFwModel(ctx, fortiClient)
		if err != nil {
			fmt.Printf("Failed to get firewall model: %v\n", err)
			return "", fmt.Errorf("failed to get firewall model: %w", err)
		}
		fmt.Printf("Current firewall model: %s\n", model)
		return model, nil
	case BACKUP:
		filename, err := getFwBackup(firewallName, ctx, fortiClient, bucketName, accessKeyID, secretAccessKey, s3Url, instance.(*k8sdinovaonev1.FortigateUpdate))
		if err != nil {
			fmt.Printf("Errore durante il backup del firewall: %v", err)
			return "", err
		}
		return filename, nil
	default:
		fmt.Printf("Operazione non supportata: %d\n", op)
		return "", fmt.Errorf("unsupported operation: %d", op)
	}
}

func SendCommandApiPost(ctx context.Context, address string, token string, bodyInfo string, op int, S3BucketName string, accessKeyID string, secretAccessKey string, s3Url string) (string, error) {
	fortiClient := newFortigateClient(address, token)

	switch op {
	case UPGRADEFIRMWARE:
		resp, err := UpgradeFortigateFirmware(ctx, fortiClient, bodyInfo, S3BucketName, accessKeyID, secretAccessKey, s3Url)
		if err != nil {
			fmt.Printf("Failed to upgrade firmware: %v\n", err)
			return "", fmt.Errorf("failed to upgrade firmware: %w", err)
		}
		fmt.Printf("Firmware upgraded successfully: %s\n", resp.Status)
		return resp.Status, nil
	default:
		fmt.Printf("Operazione non supportata: %d\n", op)
		return "", fmt.Errorf("unsupported operation: %d", op)
	}
}

func prepareRequest(url string, token string, body []byte, method string, contentType string, ctx context.Context) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		fmt.Printf("Failed to create HTTP request: %v\n", err)
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set degli Header obbligatori (Fortigate richiede l'Authorization Token così)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	if body != nil {
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
	}

	return req, nil
}

func sendRequest(client *http.Client, req *http.Request, format int) (any, error) {
	// Invio della richiesta
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("HTTP request failed: %v\n", err)
		return APIResponse{}, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Verifica dello status code (Fortigate restituisce 200 OK se la CMDB viene aggiornata)
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("fortigate API returned error status: %s (%d)", resp.Status, resp.StatusCode)
		return APIResponse{}, fmt.Errorf("fortigate API error: status=%s code=%d", resp.Status, resp.StatusCode)
	}

	switch format {
	case FORMATFORTIGATE:
		var apiResp APIResponse

		fmt.Println(resp.Body)

		// Parsing della risposta JSON (Fortigate restituisce un oggetto con la versione del firmware)

		if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
			return APIResponse{}, fmt.Errorf("failed to decode API response: %w", err)
		}

		return apiResp, nil
	case FORMATSTATUS:
		var statusResp FortigateSystemStatus
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return FortigateSystemStatus{}, fmt.Errorf("failed to read response body: %w", err)
		}

		if err := json.Unmarshal(bodyBytes, &statusResp); err != nil {
			return FortigateSystemStatus{}, fmt.Errorf("failed to decode status response: %w", err)
		}
		return statusResp, nil
	default:
		var apiResp APIResponse

		// Se non è il formato Fortigate, leggiamo semplicemente il body come stringa

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Printf("Failed to read response body: %v\n", err)
			return APIResponse{}, fmt.Errorf("failed to read response body: %w", err)
		}

		apiResp.Status = string(bodyBytes)

		return apiResp, nil
	}
}

func getFwVersion(ctx context.Context, f *FortigateClient) (string, error) {
	getFwStatus, err := getFwStatus(ctx, f)
	if err != nil {
		fmt.Printf("Failed to get firewall status: %v\n", err)
		return "", fmt.Errorf("failed to get firewall status: %w", err)
	}

	return getFwStatus.Version, nil
}

func getFwModel(ctx context.Context, f *FortigateClient) (string, error) {
	getFwStatus, err := getFwStatus(ctx, f)
	if err != nil {
		fmt.Printf("Failed to get firewall status: %v\n", err)
		return "", fmt.Errorf("failed to get firewall status: %w", err)
	}

	return getFwStatus.Results.Model, nil
}

func getFwStatus(ctx context.Context, f *FortigateClient) (FortigateSystemStatus, error) {
	// Endpoint standard Fortigate per la configurazione del sistema globale
	url := fmt.Sprintf("https://%s/api/v2/monitor/system/status", f.address)

	preparedReq, err := prepareRequest(url, f.token, nil, http.MethodGet, "application/json", ctx)
	if err != nil {
		fmt.Printf("Failed to prepare HTTP request: %v\n", err)
		return FortigateSystemStatus{}, fmt.Errorf("failed to prepare HTTP request: %w", err)
	}

	resp, err := sendRequest(f.hClient, preparedReq, FORMATSTATUS)
	if err != nil {
		fmt.Printf("Failed to send HTTP request: %v\n", err)
		return FortigateSystemStatus{}, fmt.Errorf("failed to send HTTP request: %w", err)
	}

	return resp.(FortigateSystemStatus), nil
}

func backupFortigateConfig(firewallName string, ctx context.Context, f *FortigateClient, instance *k8sdinovaonev1.FortigateUpdate) (string, string, error) {
	// Endpoint standard Fortigate per il backup della configurazione
	url := fmt.Sprintf("https://%s/api/v2/monitor/system/config/backup", f.address)

	// se esiste già un backup per il fortigate specifico non ne salvo altri. Lo verifico dallo stato
	// In qualsiasi punto si blocchi la procedura, allora avrò il backup.
	// Salvo il nome del backup nello stato dell'update. Se il campo nello stato è vuoto,
	// allora faccio partire il backup nuovamente. Se invece il campo è valorizzato, allora non faccio partire il backup.

	if instance.Status.BackupName == "" {
		filename := fmt.Sprintf("upgrade-backup/%s-%s.conf", firewallName, time.Now().Format("20060102-150405"))

		body := []byte(`{"destination":"file","scope":"global"}`)

		preparedReq, err := prepareRequest(url, f.token, body, http.MethodPost, "application/json", ctx)
		if err != nil {
			fmt.Printf("Failed to prepare HTTP request for backup: %v\n", err)
			return "", "", fmt.Errorf("failed to prepare HTTP request for backup: %w", err)
		}

		backupContent, err := sendRequest(f.hClient, preparedReq, FORMATBACKUP)
		if err != nil {
			fmt.Printf("Failed to send HTTP request for backup: %v\n", err)
			return "", "", fmt.Errorf("failed to send HTTP request for backup: %w", err)
		}

		return backupContent.(APIResponse).Status, filename, nil
	} else {
		fmt.Println("Backup della configurazione già esistente, non verrà creato un nuovo backup.")

		return "", instance.Status.BackupName, nil
	}
}

func getFwBackup(firewallName string, ctx context.Context, f *FortigateClient, bucketName, accessKeyID, secretAccessKey, s3Url string, instance *k8sdinovaonev1.FortigateUpdate) (string, error) {
	backupContent, filename, err := backupFortigateConfig(firewallName, ctx, f, instance)
	if err != nil {
		fmt.Printf("Errore durante il backup del firewall: %v", err)
		return "", err
	}
	if _, err := saveBackupToS3(filename, backupContent, bucketName, accessKeyID, secretAccessKey, s3Url); err != nil {
		fmt.Printf("Errore durante il salvataggio del backup su S3: %v", err)
		return "", err
	}

	return filename, nil
}

func saveBackupToS3(filename string, content string, bucketName, accessKeyID, secretAccessKey, s3Url string) (string, error) {
	client, err := s3utils.CreateS3Client(accessKeyID, secretAccessKey, s3Url)
	if err != nil {
		fmt.Printf("Failed to create S3 client: %v\n", err)
		return "", fmt.Errorf("failed to create S3 client: %w", err)
	}

	if err := s3utils.WriteS3Data(bucketName, filename, content, client, false); err != nil {
		fmt.Printf("Failed to write backup to S3: %v\n", err)
		return "", fmt.Errorf("failed to write backup to S3: %w", err)
	}

	fmt.Println("Backup della configurazione completato con successo!")
	return filename, nil
}
