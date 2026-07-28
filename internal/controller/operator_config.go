package controller

// Questo file gestisce le impostazioni GENERALI dell'operator (non
// specifiche di una singola OperatorRule) - separato apposta da
// rule_configdriven.go/rules.json, che invece descrivono come riempire i
// tag di UNA regola. Qui invece vivono cose come il nome del file
// Terraform "comune" (provider/backend), il nome del file di stato, la
// region S3 di default - impostazioni che valgono per ogni CR, a
// prescindere da quale OperatorRule usino.
import (
	"encoding/json"
	"fmt"
	"os"
)

// OperatorConfig raccoglie le impostazioni generali dell'operator. I
// valori zero di ogni campo NON sono usati direttamente: LoadOperatorConfigFromFile
// parte da DefaultOperatorConfig() e sovrascrive solo i campi presenti nel
// file, quindi un file di config parziale (anche vuoto, "{}") è valido e
// lascia i default per tutto il resto.
type OperatorConfig struct {
	// CommonTemplateSuffix è il suffisso del file "comune" (provider/
	// backend Terraform, uguale per ogni CR) - viene concatenato al nome
	// della CR per ottenere il file su cui applicare tfCommon, es.
	// suffisso "_common.tf" + CR "myfw" -> "myfw_common.tf".
	CommonTemplateSuffix string `json:"commonTemplateSuffix,omitempty"`

	// TerraformStateFileName è il nome del file di stato Terraform/Tofu
	// dentro il percorso S3 (dopo "<cr>/<uuid>/<cr>/"), es.
	// "firewall.tfstate".
	TerraformStateFileName string `json:"terraformStateFileName,omitempty"`

	// S3Region è la region passata al backend S3 di Terraform/Tofu
	// (prima era fissa a "us-east-1" nel codice - utile da poter
	// cambiare per storage S3-compatibili on-prem/MinIO/Ceph RGW dove
	// "region" è spesso un valore arbitrario/placeholder diverso).
	S3Region string `json:"s3Region,omitempty"`
}

// DefaultOperatorConfig restituisce i valori di default - esattamente
// quelli che erano hardcoded nel codice originale, così il comportamento
// resta identico finché non fornisci un file di configurazione (o finché
// non ne sovrascrivi solo alcuni campi).
func DefaultOperatorConfig() OperatorConfig {
	return OperatorConfig{
		CommonTemplateSuffix:   "00_common.tf",
		TerraformStateFileName: "firewall.tfstate",
		S3Region:               "us-east-1",
	}
}

// activeOperatorConfig è lo stato attivo, letto da tfCommon/
// selectOperatorRule tramite CurrentOperatorConfig(). Parte dai default,
// così l'operator funziona anche senza mai chiamare
// LoadOperatorConfigFromFile/SetOperatorConfig (es. nei test).
var activeOperatorConfig = DefaultOperatorConfig()

// SetOperatorConfig sostituisce la configurazione attiva. Va chiamata UNA
// VOLTA all'avvio (es. da main(), dopo aver letto il file), non da un
// init() - stesso principio già seguito per LoadOperatorRulesFromFile,
// per lasciare a test/entrypoint alternativi il controllo su
// quando/se caricarla.
func SetOperatorConfig(cfg OperatorConfig) {
	activeOperatorConfig = cfg
}

// CurrentOperatorConfig restituisce la configurazione attiva.
func CurrentOperatorConfig() OperatorConfig {
	return activeOperatorConfig
}

// LoadOperatorConfigFromFile legge un file JSON di configurazione,
// applicandolo SOPRA i default (un file parziale è valido: i campi assenti
// restano ai valori di default). Non chiama SetOperatorConfig da sola -
// il chiamante decide esplicitamente quando attivarla, così puoi anche
// solo leggerla/ispezionarla senza cambiare lo stato globale se ti serve.
func LoadOperatorConfigFromFile(path string) (OperatorConfig, error) {
	cfg := DefaultOperatorConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return OperatorConfig{}, fmt.Errorf("lettura configurazione operator %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return OperatorConfig{}, fmt.Errorf("parsing configurazione operator %s: %w", path, err)
	}
	return cfg, nil
}