package terraform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/hashicorp/terraform-exec/tfexec"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"

	k8sdinovaonev1 "github.com/karavy/k8s-operator-fortigate/api/v1"
	fileutils "github.com/karavy/k8s-operator-fortigate/internal/controller/utils/fileutils"
	s3utils "github.com/karavy/k8s-operator-fortigate/internal/controller/utils/s3utils"
	configs "github.com/karavy/k8s-operator-fortigate/internal/controller/utils/configs"
)

// 1. copia il template terraform da s3 in una directory temporanea dedicata al firewall (es: /tmp/fortigate1)
// 2. se esiste già una directory temporanea per quel firewall, usala direttamente (così da mantenere lo stato e i plugin scaricati)
// 3. inizializza terraform con backend s3 puntando alla directory del firewall (es: bucket/fortigate1/*.tfstate)
// 4. esegui terraform plan e se ci sono modifiche esegui terraform apply

// all'avvio copia tutti i file template .tf in una directory temporanea da s3
func copyTerraformFiles(firewallInstance *k8sdinovaonev1.FortigateFirewall, workingDir string, templateName string) (*tfexec.Terraform, error) {
	fortiConfigName := firewallInstance.Name

	tf, err := createNewTerraform(workingDir)
	if err != nil {
		fmt.Printf("Cannot create terraform")
		return nil, err
	}

	cfg := configs.CurrentOperatorConfig()

	// copia il file common dalla directory temporanea al working dir di terrform
	//common deve sempre essere copiato perché contiene la definizione del provider e del backend, che sono necessari per terraform init
	if err := fileutils.CopyFiles(workingDir+"/rules-templates/"+cfg.CommonTemplateSuffix, workingDir, fortiConfigName); err != nil {
		fmt.Printf("Errore durante la copia dei file: %v\n", err)
		return nil, err
	}

	// copia il file dalla directory temporanea al working dir di terrform
	// modificando i parmetri necessari

	if err := fileutils.CopyFiles(workingDir+"/rules-templates/"+templateName, workingDir, fortiConfigName); err != nil {
		fmt.Printf("Errore durante la copia dei file: %v\n", err)
		return nil, err
	}

	return tf, nil
}

func PrepareTerraformEnvironment(fortiConfig k8sdinovaonev1.FortigateConfig, firewallInstance *k8sdinovaonev1.FortigateFirewall, s3Url string, accessKeyID string, secretAccessKey string) (string, error) {
	// 1. Crea una directory temporanea per il firewall specifico (es: /tmp/fortigate1)
	// se esiste già continua ad usarla (così da mantenere lo stato e i plugin scaricati)
	// non genera errori se la directory esiste già
	var effectiveID string

	effectiveID = string(fortiConfig.ObjectMeta.UID)

	if effectiveID == "" {
		fmt.Println("UID non disponibile, errore nella risorsa kubernetes")
		return "", fmt.Errorf("UID non disponibile, errore nella risorsa kubernetes")
	}

	fmt.Printf("Effective ID per il firewall '%s': %s\n", fortiConfig.Name, effectiveID)

	if err := fileutils.CreateDirectory("/tmp/" + firewallInstance.Name + "/" + fortiConfig.Name + "/" + effectiveID + "/data-dir"); err != nil {
		fmt.Println(err)
		return "", err
	}

	if err := fileutils.CreateDirectory("/tmp/" + firewallInstance.Name + "/plugin-cache-dir"); err != nil {
		fmt.Println(err)
		return "", err
	}

	cfg := configs.CurrentOperatorConfig()

	//copia sempre il common terraform
	if err := s3utils.CopyS3DirContent(firewallInstance.Spec.S3BucketName, accessKeyID, secretAccessKey, s3Url,
		"rules-templates/" + cfg.CommonTemplateSuffix,
		"/tmp/" + firewallInstance.Name + "/" + fortiConfig.Name + "/" + effectiveID + "/"); err != nil {
		fmt.Println(err)
		return "", err
	}

	//copia il template terraform da s3 nella directory temporanea dedicata al firewall (es: /tmp/fortigate1)
	if err := s3utils.CopyS3DirContent(firewallInstance.Spec.S3BucketName, accessKeyID, secretAccessKey, s3Url,
		"rules-templates/"+fortiConfig.Spec.TerraformTemplateS3Key,
		"/tmp/" + firewallInstance.Name + "/" + fortiConfig.Name + "/" + effectiveID + "/"); err != nil {
		fmt.Println(err)
		return "", err
	}

	//scarica anche lo stato da s3. Importante che dopo ogni esecuzione riuscita, lo stato venga caricato su s3 così da essere sempre aggiornato
	//in questo modo, se il controller si riavvia, scarica l'ultimo stato aggiornato da s3 e lo usa per il prossimo terraform plan/apply
	// se esiste nella directory deve controllare quale dei due sia più aggiornato.
	//VERIFICA: LO STATO E' SEMPRE SU S3 O LO MANTIENE ANCHE IN LOCALE? SE LO MANTIENE IN LOCALE, DEVI GESTIRE I CASI DI RIAVVIO DEL CONTROLLER E DI CONCORRENZA TRA PIU' ISTANZE DEL CONTROLLER CHE POTREBBERO AGIRE SULLO STESSO FIREWALL
	//TODO

	return effectiveID, nil
}

func deleteTerraform(tf *tfexec.Terraform, fortiConfig k8sdinovaonev1.FortigateConfig, firewallInstance *k8sdinovaonev1.FortigateFirewall, s3Url string, accessKeyID string, secretAccessKey string, id string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	tf, err := initTerraform(tf, fortiConfig, id, firewallInstance, ctx, s3Url, accessKeyID, secretAccessKey)
	if err != nil {
		fmt.Println(err)
		return "", err
	}

	if err := tf.Destroy(ctx); err != nil {
		fmt.Printf("Errore durante terraform destroy: %v", err)
		return "", err
	}

	return "", nil
}

func execTerraform(tf *tfexec.Terraform, fortiConfig k8sdinovaonev1.FortigateConfig, firewallInstance *k8sdinovaonev1.FortigateFirewall, s3Url string, accessKeyID string, secretAccessKey string, id string) (string, error) {
	// Copia i file .tf nella directory temporanea.

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	tf, err := initTerraform(tf, fortiConfig, id, firewallInstance, ctx, s3Url, accessKeyID, secretAccessKey)
	if err != nil {
		fmt.Println(err)
		return "", err
	}

	if err := applyTerraform(tf, ctx); err != nil {
		fmt.Println(err)
		return "", err
	}

	return "", nil
}

func createNewTerraform(workingDir string) (*tfexec.Terraform, error) {
	execPath, err := exec.LookPath("tofu")
	if err != nil {
		fmt.Printf("Terraform (tofu) non trovato nel PATH: %v", err)
		return nil, err
	}

	// 2. Creiamo un contesto con timeout (buona norma per non lasciare processi appesi)

	// 3. Inizializziamo il client Terraform Exec
	tf, err := tfexec.NewTerraform(workingDir, execPath)
	if err != nil {
		fmt.Printf("Impossibile inizializzare tfexec: %v", err)
		return nil, err
	}

	return tf, nil
}

func initTerraform(tf *tfexec.Terraform, fortiConfig k8sdinovaonev1.FortigateConfig, id string, firewallInstance *k8sdinovaonev1.FortigateFirewall, ctx context.Context, s3Url string, accessKeyID string, secretAccessKey string) (*tfexec.Terraform, error) {
	//tf, err := createNewTerraform("/tmp/" + firewallName + "/" + id)

	// Opzionale: Reindirizziamo l'output di Terraform direttamente sulla console di Go
	// In questo modo vedrai i log di Terraform colorati nel tuo terminale Go
	tf.SetStdout(os.Stdout)
	tf.SetStderr(os.Stderr)

	// 4. Eseguiamo il comando: terraform init
	fmt.Println("🚀 Esecuzione di terraform init...")

	//fmt.Printf("PIPPO: %s %s %s %s %s\n", tfStateName, bucketName, s3Url, accessKeyID, secretAccessKey)

	initOptions := []tfexec.InitOption{
		tfexec.ForceCopy(true),
		tfexec.Upgrade(true),
	}

	os.Setenv("AWS_ACCESS_KEY_ID", accessKeyID)
	os.Setenv("AWS_SECRET_ACCESS_KEY", secretAccessKey)

	os.Setenv("TF_DATA_DIR", "/tmp/"+firewallInstance.Name+"/"+fortiConfig.Name+"/"+id+"/data-dir")
	os.Setenv("TF_PLUGIN_CACHE_DIR", "/tmp/"+firewallInstance.Name+"/plugin-cache-dir")

	// Verifica se ci sia bisogno di eseguire init. Lo faccio solo se è cambiata la versione del provider fortios
	// oppure se non esiste il file .terraform.lock.hcl

	tfLockFile := "/tmp/"+firewallInstance.Name+"/"+fortiConfig.Name+"/"+id+"/.terraform.lock.hcl"
	commonTfFile := "/tmp/"+firewallInstance.Name+"/"+fortiConfig.Name+"/"+id+"/"+firewallInstance.Name+"_"+ configs.DefaultOperatorConfig().CommonTemplateSuffix

	if _, err := os.Stat(tfLockFile); os.IsNotExist(err) {
		fmt.Println("⚠️ Il file .terraform.lock.hcl non esiste, eseguo terraform init...")
	} else {
		fmt.Println("✅ Il file .terraform.lock.hcl esiste, verifico se il provider sia allineato...")

		isAligned, err := isAligned(commonTfFile, tfLockFile)
		if err != nil {
			fmt.Printf("Errore durante il controllo dell'allineamento del provider: %v", err)
			return nil, err
		}
		if !isAligned {
			fmt.Println("⚠️ Il provider non è allineato, eseguo terraform init...")
		} else {
			fmt.Println("✅ Il provider è allineato, non eseguo terraform init.")
			return tf, nil
		}

		return tf, nil
	}

	err := tf.Init(ctx, initOptions...)
	if err != nil {
		fmt.Printf("Errore durante terraform init: %v", err)
		return nil, err
	}

	return tf, nil
}

func applyTerraform(tf *tfexec.Terraform, ctx context.Context) error {
	// 5. Eseguiamo il comando: terraform plan
	fmt.Println("📋 Esecuzione di terraform plan...")
	changes, err := tf.Plan(ctx)
	if err != nil {
		fmt.Printf("Errore durante terraform plan: %v", err)
		return err
	}

	if changes {
		fmt.Println("🔍 Sono state rilevate modifiche infrastrutturali!")

		// 6. Eseguiamo il comando: terraform apply
		fmt.Println("🛠️ Applicazione delle modifiche (terraform apply)...")
		err = tf.Apply(ctx)
		if err != nil {
			fmt.Printf("Errore durante terraform apply: %v", err)
			return err
		}
		fmt.Println("✅ Infrastruttura aggiornata con successo!")
	} else {
		fmt.Println("😎 Nessuna modifica da applicare, l'infrastruttura è già aggiornata.")
	}

	return nil
}

// --- Parsing del common.tf (required_providers) ---

type RequiredProvider struct {
	Source  string
	Version string
}

func parseRequiredProviders(path string) (map[string]RequiredProvider, error) {
	parser := hclparse.NewParser()
	f, diags := parser.ParseHCLFile(path)
	if diags.HasErrors() {
		return nil, diags
	}

	result := make(map[string]RequiredProvider)

	// content del blocco "terraform"
	rootContent, _, diags := f.Body.PartialContent(&hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{{Type: "terraform"}},
	})
	if diags.HasErrors() {
		return nil, diags
	}

	for _, tfBlock := range rootContent.Blocks {
		// dentro "terraform", cerco "required_providers"
		tfContent, _, diags := tfBlock.Body.PartialContent(&hcl.BodySchema{
			Blocks: []hcl.BlockHeaderSchema{{Type: "required_providers"}},
		})
		if diags.HasErrors() {
			return nil, diags
		}

		for _, rpBlock := range tfContent.Blocks {
			// required_providers non ha schema fisso (i nomi provider sono dinamici)
			// quindi uso JustAttributes
			attrs, diags := rpBlock.Body.JustAttributes()
			if diags.HasErrors() {
				return nil, diags
			}

			for providerName, attr := range attrs {
				val, diags := attr.Expr.Value(nil)
				if diags.HasErrors() {
					return nil, diags
				}
				if !val.Type().IsObjectType() {
					continue
				}

				rp := RequiredProvider{}
				valMap := val.AsValueMap()
				if s, ok := valMap["source"]; ok {
					rp.Source = s.AsString()
				}
				if v, ok := valMap["version"]; ok {
					rp.Version = v.AsString()
				}
				result[providerName] = rp
			}
		}
	}

	return result, nil
}

// --- Parsing del lock file (.terraform.lock.hcl) ---

type ProviderLock struct {
	Source  string
	Version string
}

func parseLockFile(path string) (map[string]ProviderLock, error) {
	parser := hclparse.NewParser()
	f, diags := parser.ParseHCLFile(path)
	if diags.HasErrors() {
		return nil, diags
	}

	locks := make(map[string]ProviderLock)

	content, _, diags := f.Body.PartialContent(&hcl.BodySchema{
		Blocks: []hcl.BlockHeaderSchema{
			{Type: "provider", LabelNames: []string{"source"}},
		},
	})
	if diags.HasErrors() {
		return nil, diags
	}

	for _, block := range content.Blocks {
		source := block.Labels[0]

		bContent, _, diags := block.Body.PartialContent(&hcl.BodySchema{
			Attributes: []hcl.AttributeSchema{{Name: "version"}},
		})
		if diags.HasErrors() {
			return nil, diags
		}

		var version string
		if attr, ok := bContent.Attributes["version"]; ok {
			val, diags := attr.Expr.Value(nil)
			if diags.HasErrors() {
				return nil, diags
			}
			version = val.AsString()
		}

		locks[source] = ProviderLock{Source: source, Version: version}
	}

	return locks, nil
}

// --- Confronto ---

func isAligned(mainTfPath, lockFilePath string) (bool, error) {
	required, err := parseRequiredProviders(mainTfPath)
	if err != nil {
		return false, fmt.Errorf("errore parsing common: %w", err)
	}

	locked, err := parseLockFile(lockFilePath)
	if err != nil {
		return false, fmt.Errorf("errore parsing lock file: %w", err)
	}

	for name, req := range required {
		found := false
		for lockSource, lock := range locked {
			// match sul suffisso, ignora l'host del registry
			// (registry.opentofu.org vs registry.terraform.io)
			if strings.HasSuffix(lockSource, "/"+req.Source) {
				found = true
				if lock.Version != req.Version {
					return false, nil // versione richiesta diversa da quella pinnata
				}
			}
		}
		if !found {
			return false, nil // provider mai inizializzato
		}
		_ = name
	}

	return true, nil
}