package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	k8sdinovaonev1 "github.com/karavy/k8s-operator-fortigate/api/v1"
)

//Key ID:              GK272b9021c2c5734620f47e94
//Key name:            applicazione-go
//Secret key:          5724fcd8134260790b9e42b3b23a7ca05c9451a136d765f88d85d3f19762235e
//Created:             2026-06-06 08:11:45.739 +00:00
//Validity:            valid
//Expiration:          never
//Can create buckets:  false

const (
	DIR int = iota
	FILE
)

func writeS3File(filename string, content string, bucketName string, accessKeyID string, secretAccessKey string, s3Url string, overwrite bool) error {
	client, err := createS3Client(accessKeyID, secretAccessKey, s3Url)
	if err != nil {
		fmt.Printf("errore durante la creazione del client S3: %v", err)
		return err
	}
	if err := writeS3Data(bucketName, filename, content, client, overwrite); err != nil {
		fmt.Printf("errore durante la scrittura su S3: %v", err)
		return err
	}

	return nil
}

func copyS3DirContent(bucketName string, accessKeyID string, secretAccessKey string, s3Url string, s3Prefix string, localTargetDir string) error {
	// Configurazione dei parametri

	ctx := context.TODO()

	s3Client, err := createS3Client(accessKeyID, secretAccessKey, s3Url)
	if err != nil {
		fmt.Printf("errore durante la creazione del client S3: %v", err)
		return err
	}

	// 2. Inizializza il paginatore per elencare gli oggetti (gestisce in automatico bucket con > 1000 file)
	paginator := s3.NewListObjectsV2Paginator(s3Client, &s3.ListObjectsV2Input{
		Bucket: &bucketName,
		Prefix: &s3Prefix,
	})

	fmt.Println("Inizio download dei file da S3...")

	// 3. Cicla attraverso le pagine dei risultati di S3
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			fmt.Printf("Errore durante l'elencazione degli oggetti: %v\n", err)
			return err
		}

		// 4. Scarica ogni singolo oggetto della pagina corrente
		for _, object := range page.Contents {
			s3Key := *object.Key

			// Salta le chiavi che terminano con "/" perché rappresentano cartelle vuote su S3
			if strings.HasSuffix(s3Key, "/") {
				continue
			}

			// Calcola il percorso locale finale

			localFilePath := filepath.Join(localTargetDir, s3Key)

			fmt.Printf("Scaricando: %s -> %s\n", s3Key, localFilePath)

			err := downloadFile(ctx, s3Client, bucketName, s3Key, localFilePath)
			if err != nil {
				fmt.Printf("Errore nel download di %s: %v\n", s3Key, err)
				// Scegli se bloccare tutto o continuare con il file successivo
				continue
			}
		}
	}

	fmt.Println("Download completato con successo!")

	return nil
}

func downloadFile(ctx context.Context, client *s3.Client, bucket, key, localPath string) error {
	// Assicurati che la struttura delle cartelle locali esista prima di scrivere il file
	err := os.MkdirAll(filepath.Dir(localPath), os.ModePerm)
	if err != nil {
		return fmt.Errorf("impossibile creare la directory locale: %w", err)
	}

	// Richiedi l'oggetto a S3
	output, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &bucket,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("errore GetObject: %w", err)
	}
	defer output.Body.Close()

	// Crea il file locale sulla stessa path
	localFile, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("impossibile creare il file locale: %w", err)
	}
	defer localFile.Close()

	// Copia il contenuto in streaming (evita di caricare il file interamente in RAM)
	_, err = io.Copy(localFile, output.Body)
	if err != nil {
		return fmt.Errorf("errore durante la scrittura del file: %w", err)
	}

	return nil
}

func readS3FileContent(bucketName string, filename string, accessKeyID string, secretAccessKey string, s3Url string) string {
	client, err := createS3Client(accessKeyID, secretAccessKey, s3Url)
	if err != nil {
		fmt.Printf("errore durante la creazione del client S3: %v", err)
		return ""
	}

	allfiles := readS3Dir(bucketName, client)

	for _, object := range allfiles {
		fmt.Println(*object.Key)
		if *object.Key == filename {
			richiestaContenuto, err := client.GetObject(context.TODO(), &s3.GetObjectInput{
				Bucket: aws.String(bucketName),
				Key:    aws.String(*object.Key),
			})
			if err != nil {
				fmt.Printf("Errore durante il recupero del contenuto: %v", err)
				return ""
			}
			// Ricordati di chiudere il Body alla fine per liberare i socket
			defer richiestaContenuto.Body.Close()

			buf := new(bytes.Buffer)
			_, err = io.Copy(buf, richiestaContenuto.Body)
			if err != nil {
				fmt.Printf("Errore durante la lettura del flusso dati: %v", err)
			}

			fmt.Println(buf.String())

			return buf.String()
		}
	}

	return ""
}

func createS3Client(accessKeyID string, secretAccessKey string, s3Url string) (*s3.Client, error) {
	staticProvider := credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, "")

	// 2. Inizializza la configurazione AWS forzando l'endpoint personalizzato di Garage
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithCredentialsProvider(staticProvider),
		config.WithRegion(CurrentOperatorConfig().S3Region), // Garage non ha regioni, ma l'SDK richiede una stringa
	)
	if err != nil {
		fmt.Printf("Impossibile caricare la configurazione: %v", err)
		return nil, err
	}

	// 3. Crea il client S3 specifico per Garage
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(s3Url)
		// Cruciale per Garage: usa il path-style routing (http://endpoint/bucket)
		// invece del virtual-host routing (http://bucket.endpoint)
		o.UsePathStyle = true
	})

	return client, nil
}

func createS3Bucket(bucketName string, accessKeyID string, secretAccessKey string, s3Url string) {
	client, err := createS3Client(accessKeyID, secretAccessKey, s3Url)
	if err != nil {
		fmt.Printf("errore durante la creazione del client S3: %v", err)
		return
	}

	// 4. CREAZIONE DEL BUCKET
	fmt.Printf("Creazione del bucket '%s'...\n", bucketName)
	_, err = client.CreateBucket(context.TODO(), &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		// Nota: Se il bucket esiste già, Garage potrebbe ritornare errore. Puoi gestirlo.
		log.Printf("Nota/Errore durante la creazione del bucket: %v\n", err)
	} else {
		fmt.Println("Bucket creato con successo!")
	}
}

func writeS3Data(bucketName string, fileName string, content string, client *s3.Client, overwrite bool) error {
	data := []byte(content)

	if !overwrite {
		_, err := client.HeadObject(context.TODO(), &s3.HeadObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(fileName),
		})
		if err == nil {
			fmt.Printf("File '%s' già esistente su S3, skip\n", fileName)
			return nil
		}

		var respErr *awshttp.ResponseError
		if errors.As(err, &respErr) && respErr.HTTPStatusCode() == 404 {
			// File non esiste — procedi con la scrittura
		} else {
			return fmt.Errorf("errore verifica file S3: %w", err)
		}
	}

	bufferDati := bytes.NewReader(data)
	_, err := client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:        aws.String(bucketName),
		Key:           aws.String(fileName),
		Body:          bufferDati,
		ContentType:   aws.String("text/plain"),
		ContentLength: aws.Int64(int64(len(data))),
	})
	if err != nil {
		return fmt.Errorf("errore scrittura S3: %w", err)
	}

	fmt.Printf("File '%s' scritto su S3 con successo\n", fileName)
	return nil
}

func getS3Bucket(client *s3.Client, bucketName string) *types.Bucket {
	// 4. RECUPERO DI UN BUCKET SPECIFICO
	buckets := getS3Buckets(client) // Prima recuperiamo l'elenco dei bucket per verificare che esista
	for _, bucket := range buckets {
		if aws.ToString(bucket.Name) == bucketName {
			return &bucket
		}
	}

	return nil
}

func getS3AllBuckets(accessKeyID string, secretAccessKey string, s3Url string) {
	client, err := createS3Client(accessKeyID, secretAccessKey, s3Url)
	if err != nil {
		fmt.Printf("errore durante la creazione del client S3: %v", err)
		return
	}

	buckets := getS3Buckets(client)

	if len(buckets) == 0 {
		fmt.Println("Nessun bucket trovato")
	}

	for _, object := range buckets {
		fmt.Printf("%s %s", *object.Name, *object.Name)
	}
}

func getS3Buckets(client *s3.Client) []types.Bucket {

	// 4. RECUPERO DELL'ELENCO DEI BUCKET
	fmt.Println("=== RECUPERO ELENCO BUCKET ===")
	bucketsOutput, err := client.ListBuckets(context.TODO(), &s3.ListBucketsInput{})
	if err != nil {
		fmt.Printf("Errore durante il recupero dei bucket: %v\n", err)
		return nil
	}

	if len(bucketsOutput.Buckets) == 0 {
		fmt.Println("Nessun bucket trovato nel cluster Garage.")
		return nil
	}

	return bucketsOutput.Buckets
}

func readS3Dir(bucketName string, client *s3.Client) []types.Object {
	bucket := getS3Bucket(client, bucketName)

	if bucket == nil {
		log.Printf("Bucket '%s' non trovato. Impossibile leggere i dati.\n", bucketName)
		return nil
	}

	bName := aws.ToString(bucket.Name)
	fmt.Printf("\nBucket trovato: [%s] (Creato il: %v) %s\n", bName, bucket.CreationDate, *bucket.BucketRegion)
	fmt.Println("  └─ Contenuto:")
	// Chiamata per listare gli oggetti dentro questo specifico bucket
	objectsOutput, err := client.ListObjectsV2(context.TODO(), &s3.ListObjectsV2Input{
		Bucket: aws.String(bName),
	})
	if err != nil {
		log.Printf("  [ERRORE] Impossibile leggere il contenuto del bucket %s: %v\n", bName, err)
	}
	// Se il bucket è vuoto
	if len(objectsOutput.Contents) == 0 {
		fmt.Println("     (Bucket vuoto)")
	}
	// Elenca i file trovati
	for _, object := range objectsOutput.Contents {
		fmt.Printf("     • Nome: %-30s | Dimensione: %d bytes | Ultima Modifica: %v\n",
			aws.ToString(object.Key),
			awsToInt64(object.Size), // Nota: puoi usare object.Size direttamente o aws.ToInt64(object.Size)
			object.LastModified,
		)
	}

	return objectsOutput.Contents
}

// Funzione helper per evitare crash se il puntatore Size è nil (sicurezza SDK v2)
func awsToInt64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

func getAllDirs(folder string, bucketName string, accessKeyID string, secretAccessKey string, s3Url string, elemType string) ([]string, error) {
	var allFiles []string
	var allDirs []string

	// La cartella di partenza che vuoi esplorare (deve finire con /)
	targetFolder := strings.TrimSpace(folder)

	client, err := createS3Client(accessKeyID, secretAccessKey, s3Url)
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 client: %w", err)
	}

	// 2. Prepara la richiesta di navigazione
	input := &s3.ListObjectsV2Input{
		Bucket:    aws.String(bucketName),
		Prefix:    aws.String(targetFolder), // Cerca dentro questa cartella
		Delimiter: aws.String("/"),          // Fondamentale: evita di mostrare i file ricorsivi
	}

	// 3. Esegui la chiamata a S3
	result, err := client.ListObjectsV2(context.TODO(), input)
	if err != nil {
		fmt.Printf("Errore durante il list degli oggetti: %v\n", err)
	}

	// 4. ESTRAI LE SOTTOCARTELLE (CommonPrefixes)
	fmt.Println("--- Sottodirectory Trovate: ---")
	for _, commonPrefix := range result.CommonPrefixes {
		// Stampa i nomi delle "cartelle" (es: logs/2026/, logs/system/)
		fmt.Printf("📂 %s\n", *commonPrefix.Prefix)
		allDirs = append(allDirs, *commonPrefix.Prefix)
	}

	// 5. ESTRAI I FILE NELLA DIRECTORY CORRENTE (Contents)
	fmt.Println("\n--- File nella directory corrente: ---")
	for _, object := range result.Contents {
		// Escludiamo il placeholder della cartella stessa se S3 lo restituisce
		if *object.Key == targetFolder {
			continue
		}
		fmt.Printf("📄 %s (Dimensione: %d bytes)\n", *object.Key, *object.Size)
		allFiles = append(allFiles, *object.Key)
	}

	switch elemType {
	case "DIR":
		return allDirs, nil
	case "FILE":
		return allFiles, nil
	}

	return nil, fmt.Errorf("Tipo di elemento non valido: %s", elemType)
}

func getUUIDFromS3(fortiConfig k8sdinovaonev1.FortigateConfig, firewallInstance *k8sdinovaonev1.FortigateFirewall, accessKeyID string, secretAccessKey string, s3Url string) (string, error) {
	dirs, err := getAllDirs(fortiConfig.Name, firewallInstance.Spec.S3BucketName, accessKeyID, secretAccessKey, s3Url, "DIR")
	if err != nil {
		fmt.Printf("Errore durante il recupero delle directory: %v\n", err)
		return "", err
	}
	if len(dirs) > 1 {
		fmt.Printf("Attenzione: Esistono più directory per '%s' nel bucket S3. Verifica la configurazione.\n", fortiConfig.Name)
		return "", errors.New("Troppe directory trovate nel bucket S3")
	} else if len(dirs) == 1 {
		// devo recuperare l'UUID che è la directory (l'unica) nel percorso
		uuid_dirs, err := getAllDirs(fortiConfig.Name+"/", firewallInstance.Spec.S3BucketName, accessKeyID, secretAccessKey, s3Url, "DIR")
		if err != nil {
			fmt.Printf("Errore durante il recupero delle sottodirectory: %v\n", err)
			return "", err
		}
		if len(uuid_dirs) > 1 {
			fmt.Printf("Attenzione: Esistono più UUID per '%s' nel bucket S3. Verifica la configurazione.\n", fortiConfig.Name)
			return "", errors.New("Troppe sottodirectory trovate nel bucket S3")
		} else if len(uuid_dirs) == 1 {
			input := uuid_dirs[0]
			prefix := fortiConfig.Name + "/"
			id := strings.TrimPrefix(input, prefix)
			id = strings.TrimPrefix(id, prefix)
			id = strings.Trim(id, "/")
			fmt.Printf("Sottodirectory esistente trovata per '%s'. Uso UUID: %s\n", fortiConfig.Name, id)

			return id, nil
		}
	} else if len(dirs) == 0 {
		fmt.Printf("Nessuna directory trovata per '%s' nel bucket S3. Verrà generato un nuovo UUID.\n", fortiConfig.Name)
		return "", nil
	}

	return "", errors.New("Errore imprevisto durante il recupero dell'UUID da S3")
}

func deleteS3Dir(bucketName string, dirName string, accessKeyID string, secretAccessKey string, s3Url string) error {
	client, err := createS3Client(accessKeyID, secretAccessKey, s3Url)
	if err != nil {
		return fmt.Errorf("failed to create S3 client: %w", err)
	}
	ctx := context.Background()

	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
		Prefix: aws.String(dirName),
	})

	// 2. Itera su tutte le "pagine" di risultati (S3 restituisce max 1000 oggetti alla volta)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("impossibile elencare gli oggetti: %w", err)
		}

		// Se la pagina è vuota (la cartella è già vuota), passa oltre
		if len(page.Contents) == 0 {
			continue
		}

		// 3. Prepara l'elenco dei file da cancellare in questa pagina
		var objectsToDelete []types.ObjectIdentifier
		for _, object := range page.Contents {
			objectsToDelete = append(objectsToDelete, types.ObjectIdentifier{
				Key: object.Key,
			})
		}

		// 4. Esegui la cancellazione di massa (DeleteObjects) per la pagina corrente
		output, err := client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucketName),
			Delete: &types.Delete{
				Objects: objectsToDelete,
				Quiet:   aws.Bool(true), // Quiet true evita di farsi restituire la lista dei file cancellati con successo, risparmiando banda
			},
		})
		if err != nil {
			return fmt.Errorf("errore durante la cancellazione di massa: %w", err)
		}

		// Controlla se ci sono stati errori parziali su oggetti specifici
		if len(output.Errors) > 0 {
			return fmt.Errorf("alcuni oggetti non sono stati cancellati: %v", output.Errors[0].Message)
		}

		fmt.Printf("Cancellati %d oggetti con successo...\n", len(objectsToDelete))
	}

	return nil
}
