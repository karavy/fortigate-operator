package configs

// Questo file rende configurabili via ConfigMap i tag "comuni" (provider/
// backend Terraform: IP, token, bucket S3, ecc.) applicati a OGNI CR a
// prescindere dalla sua OperatorRule - prima erano una lista fissa
// hardcoded dentro tfCommon (controller.go). A differenza dei mapping per
// OperatorRule (rule_configdriven.go), qui il valore di ciascun tag non
// viene da un percorso nello spec della CR: viene da un piccolo insieme
// FISSO di parametri già noti a runtime a chi chiama tfCommon (l'IP del
// FortiGate, il token API, il bucket S3, ...) - per questo il "percorso"
// nella ConfigMap è semplicemente il NOME di uno di questi parametri, non
// un dot-path.
import "fmt"

// CommonMappingConfig descrive il mapping tag -> nome del parametro
// runtime, per la sezione "common" di rules.json/della ConfigMap. Se
// Mappings è vuoto/assente, si usano i default (identici al comportamento
// hardcoded originale).
type CommonMappingConfig struct {
	Mappings map[string]string `json:"mappings,omitempty"`
}

// commonRuntimeValueNames elenca i nomi di parametro riconosciuti dal lato
// "common" - usali come valore (non come chiave) nel campo "mappings"
// della sezione "common" della ConfigMap:
//
//	"fortiIP"    -> indirizzo/nome del FortiGate
//	"token"      -> token API del FortiGate
//	"tfStateKey" -> chiave dello stato Terraform/Tofu su S3
//	"s3Bucket"   -> nome del bucket S3
//	"s3Region"   -> region S3 (da OperatorConfig.S3Region)
//	"s3Endpoint" -> URL dell'endpoint S3
var commonRuntimeValueNames = []string{
	"fortiIP", "token", "tfStateKey", "s3Bucket", "s3Region", "s3Endpoint",
}

// defaultCommonMappings riproduce ESATTAMENTE la lista che era hardcoded
// in tfCommon prima di questa modifica - così il comportamento resta
// identico finché non fornisci (o fornisci parzialmente) una sezione
// "common" nella ConfigMap.
func defaultCommonMappings() map[string]string {
	return map[string]string{
		"FGT_IP_OR_NAME":       "fortiIP",
		"FGT_API_TOKEN":        "token",
		"FGT_S3STATE_KEY":      "tfStateKey",
		"FGT_S3STATE_BUCKET":   "s3Bucket",
		"FGT_S3STATE_REGION":   "s3Region",
		"FGT_S3STATE_ENDPOINT": "s3Endpoint",
	}
}

// activeCommonMappings è lo stato attivo, letto da tfCommon tramite
// CurrentCommonMappings(). Parte dai default, così l'operator funziona
// anche senza mai chiamare SetCommonConfig (es. nei test o se rules.json
// non ha affatto una sezione "common").
var activeCommonMappings = defaultCommonMappings()

// SetCommonConfig sostituisce il mapping comune attivo. Un
// CommonMappingConfig con Mappings vuoto/nil ripristina i default,
// piuttosto che azzerare tutto (una ConfigMap senza sezione "common", o
// con "common": {} esplicito, si comporta quindi come se non fosse stata
// toccata affatto).
func SetCommonConfig(cfg CommonMappingConfig) {
	if len(cfg.Mappings) == 0 {
		activeCommonMappings = defaultCommonMappings()
		return
	}
	activeCommonMappings = cfg.Mappings
}

// CurrentCommonMappings restituisce il mapping comune attivo (tag -> nome
// parametro runtime).
func CurrentCommonMappings() map[string]string {
	return activeCommonMappings
}

// commonRuntimeValues raccoglie i valori REALI dei parametri runtime
// riconosciuti (vedi commonRuntimeValueNames), a partire dagli argomenti
// che tfCommon riceve già.
func commonRuntimeValues(token, fortiIP, bucketName, tfStateName, s3Url string) map[string]string {
	return map[string]string{
		"fortiIP":    fortiIP,
		"token":      token,
		"tfStateKey": tfStateName,
		"s3Bucket":   bucketName,
		"s3Region":   CurrentOperatorConfig().S3Region,
		"s3Endpoint": s3Url,
	}
}

// buildCommonMods costruisce le modification per i tag "comuni",
// risolvendo ciascun mapping (tag -> nome parametro) contro i valori
// runtime reali. Un nome di parametro sconosciuto (refuso nella
// ConfigMap) produce un valore vuoto E un avviso su stderr, invece di
// bloccare l'intera regola - coerente con come rule_configdriven.go
// gestisce già i percorsi non risolvibili.
func BuildCommonMods(token, fortiIP, bucketName, tfStateName, s3Url string) map[string]string {
	values := commonRuntimeValues(token, fortiIP, bucketName, tfStateName, s3Url)
	mappings := CurrentCommonMappings()

	mods := make(map[string]string, len(mappings))
	for tag, valueName := range mappings {
		val, known := values[valueName]
		if !known {
			fmt.Printf("[tfCommon] attenzione: %q non è un nome di parametro runtime riconosciuto (validi: %v) - il tag <%s> resterà vuoto\n", valueName, commonRuntimeValueNames, tag)
		}
		mods[fmt.Sprintf("<%s>", tag)] = val
	}
	return mods
}