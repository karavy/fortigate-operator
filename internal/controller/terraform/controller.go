package terraform

import (
	"fmt"
	"os"

	k8sdinovaonev1 "github.com/karavy/k8s-operator-fortigate/api/v1"
	fileutils "github.com/karavy/k8s-operator-fortigate/internal/controller/utils/fileutils"
	s3utils "github.com/karavy/k8s-operator-fortigate/internal/controller/utils/s3utils"

	configs "github.com/karavy/k8s-operator-fortigate/internal/controller/utils/configs"
)

// tfCommon applies the substitutions shared by every OperatorRule (S3
// backend / provider wiring). I tag applicati e a quale parametro runtime
// corrispondono NON sono più hardcoded qui: vengono da
// CurrentCommonMappings() (vedi common_config.go), configurabile tramite
// la sezione "common" di rules.json/della ConfigMap - stesso principio
// già applicato alle OperatorRule con rule_configdriven.go.
//
// mods è oldValue -> newValue (mappa, tipo standard) invece di uno slice
// di struct dedicata: ogni tag <FGT_XXX> compare come chiave una sola
// volta per chiamata, quindi non serve preservare un ordine o permettere
// duplicati.
func tfCommon(token, fortiIP, bucketName, tfStateName, s3Url, filename string) (map[string]string, error) {
	mods := configs.BuildCommonMods(token, fortiIP, bucketName, tfStateName, s3Url)

	// modifyFileValue legge E scrive su "filename" così com'è - deve
	// restare il path REALE su cui copyTerraformFiles ha messo il file
	// (che può ancora avere ".tmpl" nel nome, es.
	// "..._00_common.tf.tmpl"). Non tocchiamo filename PRIMA di questa
	// chiamata: modifyFileValue fallirebbe in lettura cercando un file
	// che non esiste con un nome diverso da quello reale su disco.
	if err := fileutils.ModifyFileValue(filename, mods); err != nil {
		return nil, fmt.Errorf("modifica di %s fallita: %w", filename, err)
	}

	// SOLO ORA, a sostituzione avvenuta, garantiamo che il file finale
	// (quello che Tofu/Terraform vedrà davvero) finisca sempre in ".tf" -
	// stessa garanzia di splitTerraformExt già usata per i file
	// splittati (vedi rule_helpers.go). Se filename finiva già in ".tf",
	// finalName coincide con filename e non facciamo nulla.
	base, ext := splitTerraformExt(filename)
	finalName := base + ext
	if finalName != filename {
		if err := os.Rename(filename, finalName); err != nil {
			return nil, fmt.Errorf("rinomina di %s in %s fallita: %w", filename, finalName, err)
		}
	}

	return mods, nil
}

func SelectOperatorRule(template, workingDir, token, fortiIP string, fortiConfig k8sdinovaonev1.FortigateConfig, firewallInstance *k8sdinovaonev1.FortigateFirewall, s3Url, accessKeyID, secretAccessKey, uuid string, createOrUpdate bool) error {

	// il file sarà copiato in workingDir/templateName, quindi il nome del file sarà workingDir/templateName
	tf, err := copyTerraformFiles(firewallInstance, workingDir, template)
	if err != nil {
		return fmt.Errorf("copia dei file Terraform fallita: %w", err)
	}

	// NOTA: fileName e baseFile DEVONO riferirsi allo STESSO percorso -
	// prima non era così (fileName usava firewallInstance.Name, baseFile
	// usava fortiConfig.Name), e se i due nomi differiscono (com'è il
	// caso in pratica: un FortigateFirewall può avere più CR
	// FortigateConfig, con nomi diversi) il motore scrive i file in un
	// posto e la regola config-driven li cerca in un altro,
	// fallendo con "no such file or directory". Ora c'è UNA SOLA variabile,
	// calcolata una volta, usata da entrambi.
	fileName := fmt.Sprintf("%s/%s_%s", workingDir, firewallInstance.Name, template)
	if err := smartNestedProcess(fileName, &fortiConfig); err != nil {
		return fmt.Errorf("elaborazione dei blocchi FOR fallita: %w", err)
	}

	// smartNestedProcess garantisce che il file finale su disco finisca
	// sempre in ".tf" (vedi writeAndEnsureDotTf/splitIntoFiles in
	// smart_nested_process.go) - se "template" arriva ancora con ".tmpl"
	// (es. "50_firewall_vip.tf.tmpl"), fileName com'è sopra NON esiste
	// più su disco dopo la rinomina. Ricalcoliamo qui lo stesso nome
	// finale (stessa funzione, splitTerraformExt, già usata ovunque in
	// questo file) prima di passarlo a handler.Apply, altrimenti cerca
	// un file che non c'è più - stesso tipo di disallineamento già
	// corretto per firewallInstance.Name/fortiConfig.Name.
	finalBase, finalExt := splitTerraformExt(fileName)
	fileName = finalBase + finalExt

	// --- unico punto di dispatch: nessuna modifica qui per aggiungere una
	// nuova OperatorRule - vedi operator_rules.go per come registrarne una.
	handler, ok := lookupOperatorRule(fortiConfig.Spec.OperatorRule)
	if !ok {
		return fmt.Errorf(
			"OperatorRule %q non riconosciuta (regole disponibili: %v)",
			fortiConfig.Spec.OperatorRule, KnownOperatorRules(),
		)
	}

	if err := handler.Apply(fortiConfig, fileName); err != nil {
		return fmt.Errorf("applicazione della regola %q fallita: %w", fortiConfig.Spec.OperatorRule, err)
	}
	// --- fine punto di dispatch ---

	tfStateName := firewallInstance.Name + "/" + uuid + "/" + fortiConfig.Name + "/" + configs.CurrentOperatorConfig().TerraformStateFileName
	commonFile := workingDir + "/" + firewallInstance.Name + "_" + configs.CurrentOperatorConfig().CommonTemplateSuffix

	if _, err := tfCommon(token, fortiIP, firewallInstance.Spec.S3BucketName, tfStateName, s3Url, commonFile); err != nil {
		return fmt.Errorf("creazione delle modifiche comuni fallita: %w", err)
	}

	if createOrUpdate {
		if _, err := execTerraform(tf, fortiConfig, firewallInstance, s3Url, accessKeyID, secretAccessKey, uuid); err != nil {
			return fmt.Errorf("esecuzione di Terraform fallita: %w", err)
		}
	} else {
		if _, err := deleteTerraform(tf, fortiConfig, firewallInstance, s3Url, accessKeyID, secretAccessKey, uuid); err != nil {
			return fmt.Errorf("cancellazione delle risorse Terraform fallita: %w", err)
		}
	
		// Stesso allineamento di tfStateName sopra: la directory S3 da
		// cancellare deve corrispondere a quella in cui lo stato è stato
		// davvero salvato (firewallInstance.Name), non a fortiConfig.Name -
		// altrimenti si cancella la directory sbagliata, lasciando lo stato
		// reale orfano su S3.
		s3Key := firewallInstance.Name + "/" + uuid
		if err := s3utils.DeleteS3Dir(firewallInstance.Spec.S3BucketName, s3Key, accessKeyID, secretAccessKey, s3Url); err != nil {
			return fmt.Errorf("cancellazione della directory S3 fallita: %w", err)
		}
	}

	return nil
}