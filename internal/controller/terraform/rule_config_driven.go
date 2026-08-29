package terraform

// Questo file sostituisce la necessità di scrivere un rule_xxx.go per ogni
// nuovo file Terraform: le regole vivono in dati (un file JSON, pensato
// per essere il contenuto di una ConfigMap montata nel Pod), non in
// codice Go. Aggiungere supporto per un nuovo Terraform template diventa:
//
//  1. Scrivere il template .tf.tmpl con i suoi tag <FGT_XXX> / <FGT_FOR_STR>.
//  2. Aggiungere una voce nella ConfigMap (vedi rules.example.json) che
//     dice quale campo della CR riempie ciascun tag.
//  3. Nessuna modifica al codice Go, nessuna ricompilazione/redeploy del
//     controller.
//
// Il caricamento avviene chiamando LoadOperatorRulesFromFile (o
// LoadOperatorRulesFromBytes) UNA VOLTA all'avvio, es. da main() dopo il
// setup del manager - non da un init(), così i test e altri entrypoint
// possono controllare esattamente quando/con quale file caricarle.
import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	k8sdinovaonev1 "github.com/karavy/k8s-operator-fortigate/api/v1"
	configs "github.com/karavy/k8s-operator-fortigate/internal/controller/utils/configs"
)

// RuleMappingConfig descrive come riempire i placeholder del template
// Terraform di UNA OperatorRule.
type RuleMappingConfig struct {
	// ListField, se impostato, rende questa una regola "a lista": è un
	// percorso puntato verso un campo della CR che è un array (es.
	// "spec.firewallRules"). smartNestedProcess ha già splittato baseFile
	// in un file per elemento (vedi IndexedResourceFile) - qui iteriamo
	// la stessa lista per generare le sostituzioni di ciascun file.
	ListField string `json:"listField,omitempty"`

	// Mappings si usa quando ListField è vuoto: tag (senza < >) -> percorso
	// puntato nella CR, es. "FGT_PORT_NAME": "spec.portConfigurationParams.portName".
	// Il percorso speciale "$cr.name" restituisce il nome della risorsa
	// Kubernetes stessa (cfg.Name), non un campo dello spec.
	Mappings map[string]string `json:"mappings,omitempty"`

	// ItemMappings si usa quando ListField è impostato: tag -> percorso
	// RELATIVO A CIASCUN ELEMENTO della lista, es. "FGT_POLICY_NAME": "ruleName".
	// "$cr.name" resta disponibile anche qui per riferirsi al nome della CR.
	ItemMappings map[string]string `json:"itemMappings,omitempty"`

	// Validate descrive le regole di validazione per i tag elencati in
	// Mappings (regole singola-risorsa) o ItemMappings (regole a lista) -
	// chiave = stesso nome tag usato lì. Pensato soprattutto per i campi
	// che vivono sotto spec.extraParams/spec.extraItems (non tipizzati,
	// quindi non già validati dallo schema OpenAPI del CRD), ma funziona
	// per qualunque percorso.
	Validate map[string]FieldValidation `json:"validate,omitempty"`

	// ValidateLists valida i campi DENTRO liste gestite direttamente dal
	// motore (smart_nested_process.go tramite un tag <FGT_FOR_STR> nel
	// template stesso, es. spec.extraParams.storageDisks) - NON le liste
	// di ListField/ItemMappings sopra, che sono un meccanismo diverso.
	// Chiave = percorso puntato verso la lista (lo stesso percorso che il
	// tag del template referenzia, es. "spec.extraParams.storageDisks");
	// valore = mappa "nome campo dell'elemento" -> FieldValidation, stesso
	// significato di Validate ma applicata a OGNI elemento della lista.
	//
	// Nota sull'ordine: questa validazione gira DOPO che
	// smartNestedProcess ha già scritto il file con i valori sostituiti
	// (la regola "valida prima di scrivere" vale per ListField/
	// ItemMappings, non per queste liste, che il motore gestisce prima
	// ancora che questa regola venga interpellata) - se fallisce, il file
	// generato contiene già i valori (anche quelli non validi), ma
	// l'intero OperatorRule fallisce comunque PRIMA che si arrivi a
	// eseguire Terraform, quindi non ha conseguenze pratiche.
	ValidateLists map[string]map[string]FieldValidation `json:"validateLists,omitempty"`
}

// FieldValidation descrive un controllo da eseguire A RUNTIME (non nello
// schema OpenAPI del CRD) sul valore risolto per un tag.
type FieldValidation struct {
	// Required: se true e il campo è assente/vuoto, la regola fallisce.
	Required bool `json:"required,omitempty"`

	// Type: "string" (default, nessun controllo aggiuntivo), "int",
	// "bool", "cidr", "ip", "enum". Un Type sconosciuto viene ignorato
	// silenziosamente (probabile refuso nella ConfigMap, ma non blocchiamo
	// l'intera regola per quello).
	Type string `json:"type,omitempty"`

	// Enum: valori ammessi, usato quando Type == "enum".
	Enum []string `json:"enum,omitempty"`

	// Pattern: regex facoltativa applicata al valore (in aggiunta a Type).
	Pattern string `json:"pattern,omitempty"`
}

// RulesFile è il contenuto atteso del file/ConfigMap: una voce "common"
// (facoltativa) per i tag applicati a OGNI CR indipendentemente dalla
// regola, più una voce per ogni fortiConfig.Spec.OperatorRule che si
// vuole supportare.
type RulesFile struct {
	Common configs.CommonMappingConfig          `json:"common,omitempty"`
	Rules  map[string]RuleMappingConfig `json:"rules"`
}

// LoadOperatorRulesFromFile legge un file JSON (tipicamente il file
// montato da una ConfigMap) e registra un OperatorRuleHandler generico per
// ciascuna voce trovata.
func LoadOperatorRulesFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("lettura file regole %s: %w", path, err)
	}
	return LoadOperatorRulesFromBytes(data)
}

// LoadOperatorRulesFromDirectory scansiona ricorsivamente dir alla ricerca
// di file *.json (uno per ConfigMap, ciascuno con la STESSA forma di
// RulesFile: "common" e/o "rules") e li unisce tutti in un unico ruleset
// attivo. Pensata per quando una singola ConfigMap "rules.json" diventa
// scomoda (limite di 1MB per ConfigMap, o solo per tenere ogni regola in
// un oggetto Kubernetes separato) - una ConfigMap per regola, ciascuna
// montata sul proprio percorso (nessun subPath: ogni ConfigMap ha il
// proprio volume/volumeMount dedicato, proiettata come sottocartella di
// dir), invece di un unico file monolitico.
//
// Regole di merge, applicate in modo esplicito e verificabile:
//   - "rules": l'unione di tutte le regole trovate in tutti i file. Lo
//     STESSO nome di regola definito in più di un file è un errore
//     esplicito all'avvio (mai una scelta silenziosa su quale vince).
//   - "common": deve essere definita in AL PIÙ UN file. Se più di un file
//     la definisce, è un errore esplicito.
//
// L'ordine di scansione delle directory non è significativo per il
// risultato finale (gli errori sopra impediscono qualunque dipendenza
// dall'ordine) - può cambiare da un avvio all'altro senza conseguenze.
func LoadOperatorRulesFromDirectory(dir string) error {
	merged := RulesFile{Rules: map[string]RuleMappingConfig{}}
	commonDefinedIn := ""
	filesRead := 0

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("attraversamento di %s fallito: %w", path, err)
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("lettura di %s fallita: %w", path, err)
		}

		var part RulesFile
		if err := json.Unmarshal(data, &part); err != nil {
			return fmt.Errorf("parsing di %s fallito: %w", path, err)
		}
		filesRead++

		for name, cfg := range part.Rules {
			if _, exists := merged.Rules[name]; exists {
				return fmt.Errorf("regola %q definita in più di un file (trovata di nuovo in %s) - ogni regola deve stare in un solo file", name, path)
			}
			merged.Rules[name] = cfg
		}

		if len(part.Common.Mappings) > 0 {
			if commonDefinedIn != "" {
				return fmt.Errorf("sezione \"common\" definita sia in %s che in %s - deve stare in un solo file", commonDefinedIn, path)
			}
			merged.Common = part.Common
			commonDefinedIn = path
		}

		return nil
	})
	if err != nil {
		return err
	}

	return applyRulesFile(merged, fmt.Sprintf("%d file in %s", filesRead, dir))
}

// LoadOperatorRulesFromBytes è la stessa cosa di LoadOperatorRulesFromFile
// ma partendo da bytes già in memoria - comodo per i test e per chi non
// vuole passare da un file su disco.
func LoadOperatorRulesFromBytes(data []byte) error {
	var rf RulesFile
	if err := json.Unmarshal(data, &rf); err != nil {
		return fmt.Errorf("parsing file regole: %w", err)
	}
	return applyRulesFile(rf, "1 file")
}

// applyRulesFile registra effettivamente un RulesFile (già unito, se
// proveniente da più file) presso il registro delle regole e la
// configurazione comune - punto unico condiviso da
// LoadOperatorRulesFromBytes (un solo file) e LoadOperatorRulesFromDirectory
// (più file), così non c'è logica duplicata tra i due.
func applyRulesFile(rf RulesFile, source string) error {
	configs.SetCommonConfig(rf.Common)
	names := make([]string, 0, len(rf.Rules))
	for name, cfg := range rf.Rules {
		cfg := cfg // cattura per closure
		RegisterOperatorRule(name, OperatorRuleFunc(
			func(fortiConfig k8sdinovaonev1.FortigateConfig, baseFile string) error {
				return applyConfigDrivenRule(cfg, fortiConfig, baseFile)
			},
		))
		names = append(names, name)
	}
	fmt.Printf("[operator-rules] caricate %d regole da %s: %v (mapping comuni: %d)\n", len(names), source, names, len(configs.CurrentCommonMappings()))
	return nil
}

// applyConfigDrivenRule è L'UNICA funzione che sa come applicare
// qualunque regola descritta da RuleMappingConfig - sostituisce tutti i
// singoli applyXxxRule scritti a mano prima.
func applyConfigDrivenRule(rc RuleMappingConfig, cfg k8sdinovaonev1.FortigateConfig, baseFile string) error {
	specMap, err := specToMap(cfg)
	if err != nil {
		return err
	}

	if err := validateEngineLists(rc.ValidateLists, cfg.Name, specMap); err != nil {
		return fmt.Errorf("CR %q: %w", cfg.Name, err)
	}

	if rc.ListField == "" {
		if err := validateFields(rc.Validate, rc.Mappings, cfg.Name, specMap, ""); err != nil {
			return fmt.Errorf("CR %q: %w", cfg.Name, err)
		}
		mods := buildMods(rc.Mappings, cfg.Name, specMap, "")
		return applyMods(baseFile, mods)
	}

	rawList, ok := lookupRawPath(specMap, rc.ListField)
	if !ok {
		return fmt.Errorf("campo lista %q non trovato nella CR", rc.ListField)
	}
	list, ok := rawList.([]interface{})
	if !ok {
		return fmt.Errorf("campo %q non è una lista", rc.ListField)
	}

	// Prima passata: valida OGNI elemento. Se anche uno solo non va bene,
	// non scriviamo NESSUN file - meglio fallire l'intero batch che
	// generare Terraform parziale/incoerente per una CR con un solo
	// elemento sbagliato su tanti.
	for i, rawItem := range list {
		itemMap, ok := rawItem.(map[string]interface{})
		if !ok {
			continue
		}
		suffix := fmt.Sprintf("_%d", i+1)
		if err := validateFields(rc.Validate, rc.ItemMappings, cfg.Name, itemMap, suffix); err != nil {
			return fmt.Errorf("CR %q, elemento %d di %q: %w", cfg.Name, i+1, rc.ListField, err)
		}
	}

	// Seconda passata: tutti gli elementi sono validi, applichiamo.
	for i, rawItem := range list {
		itemMap, ok := rawItem.(map[string]interface{})
		if !ok {
			continue
		}
		suffix := fmt.Sprintf("_%d", i+1)
		mods := buildMods(rc.ItemMappings, cfg.Name, itemMap, suffix)

		file := IndexedResourceFile(baseFile, i)
		if err := applyMods(file, mods); err != nil {
			return err
		}
	}
	return nil
}

// specToMap converte cfg.Spec in una mappa generica (chiavi JSON naturali,
// es. "portConfigurationParams", NON maiuscolizzate - a differenza della
// mappa usata da smartNestedProcess, qui i percorsi nella ConfigMap sono
// scritti dall'utente usando i nomi JSON reali).
func specToMap(cfg k8sdinovaonev1.FortigateConfig) (map[string]interface{}, error) {
	b, err := json.Marshal(cfg.Spec)
	if err != nil {
		return nil, fmt.Errorf("marshalling dello spec: %w", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("unmarshalling dello spec in mappa generica: %w", err)
	}
	return m, nil
}

// buildMods risolve ogni (tag -> percorso) contro dataMap/cfgName e
// produce le modification corrispondenti. suffix va aggiunto al tag per
// le regole a lista (es. "_1", "_2", ...), coerentemente con il suffisso
// che smartNestedProcess appende ai tag non risolti nei file splittati -
// vedi IndexedResourceFile/smart_nested_process.go. Un percorso non
// risolvibile (campo assente/opzionale) produce una stringa vuota invece
// di far fallire l'intera regola.
func buildMods(mappings map[string]string, cfgName string, dataMap map[string]interface{}, suffix string) map[string]string {
	mods := make(map[string]string, len(mappings))
	for tag, path := range mappings {
		val, _ := lookupPath(cfgName, dataMap, path)
		mods[fmt.Sprintf("<%s%s>", tag, suffix)] = val
	}
	return mods
}

// lookupPath risolve un percorso puntato (es.
// "spec.portConfigurationParams.portName", o solo "ruleName" quando
// dataMap è già l'elemento di una lista) contro dataMap. Il percorso
// speciale "$cr.name" restituisce sempre cfgName, a prescindere da
// dataMap. Il prefisso "spec." è facoltativo ed equivalente alla sua
// assenza.
func lookupPath(cfgName string, dataMap map[string]interface{}, path string) (string, bool) {
	if path == "$cr.name" {
		return cfgName, true
	}
	path = strings.TrimPrefix(path, "spec.")
	if path == "" {
		return "", false
	}

	cur, ok := lookupRawPath(dataMap, path)
	if !ok {
		return "", false
	}
	switch v := cur.(type) {
	case string:
		return v, true
	case nil:
		return "", false
	case map[string]interface{}, []interface{}:
		return "", false // un percorso deve puntare a uno scalare
	default:
		return fmt.Sprintf("%v", v), true
	}
}

// lookupRawPath è come lookupPath ma restituisce il valore così com'è
// (senza convertirlo a stringa) - usato per ListField, che deve risolvere
// a un []interface{}, non a uno scalare.
func lookupRawPath(dataMap map[string]interface{}, path string) (interface{}, bool) {
	path = strings.TrimPrefix(path, "spec.")
	var cur interface{} = dataMap
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			continue
		}
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil, false
		}
		v, exists := m[part]
		if !exists {
			return nil, false
		}
		cur = v
	}
	return cur, true
}

// validateFields controlla ogni tag elencato in rules (Validate) che ha
// anche un mapping corrispondente in mappings (Mappings/ItemMappings),
// risolvendolo contro dataMap/cfgName esattamente come farebbe buildMods.
// Un tag presente in rules ma assente da mappings viene ignorato (nessun
// percorso da validare). Restituisce un unico errore che elenca TUTTI i
// problemi trovati, non solo il primo - più comodo per correggere una CR
// in un colpo solo.
// validateEngineLists valida i campi di ogni elemento delle liste
// referenziate in listValidations - queste liste sono gestite DIRETTAMENTE
// dal motore (un tag <FGT_FOR_STR> nel template), non da ListField/
// ItemMappings di questa stessa regola - qui quindi NON generiamo nessuna
// modification, controlliamo solo che i valori siano validi. Una lista
// referenziata ma assente dallo spec non è un errore qui (se era
// obbligatoria che il motore la trovi, è il motore stesso a segnalarlo
// quando prova a iterarla per la sostituzione). Accumula TUTTI i problemi
// trovati, non si ferma al primo - stessa filosofia di validateFields.
func validateEngineLists(listValidations map[string]map[string]FieldValidation, cfgName string, specMap map[string]interface{}) error {
	if len(listValidations) == 0 {
		return nil
	}

	var problems []string
	for listPath, fieldRules := range listValidations {
		rawList, ok := lookupRawPath(specMap, listPath)
		if !ok {
			continue
		}
		list, ok := rawList.([]interface{})
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: non è una lista", listPath))
			continue
		}

		for i, rawItem := range list {
			itemMap, ok := rawItem.(map[string]interface{})
			if !ok {
				continue
			}
			itemLabel := fmt.Sprintf("%s[%d]", listPath, i+1)

			for fieldName, rule := range fieldRules {
				val, found := lookupPath(cfgName, itemMap, fieldName)
				if !found || val == "" {
					if rule.Required {
						problems = append(problems, fmt.Sprintf("%s, campo %q: obbligatorio mancante", itemLabel, fieldName))
					}
					continue
				}
				if err := checkType(val, rule); err != nil {
					problems = append(problems, fmt.Sprintf("%s, campo %q: %v", itemLabel, fieldName, err))
				}
			}
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("validazione liste fallita: %s", strings.Join(problems, "; "))
}

func validateFields(rules map[string]FieldValidation, mappings map[string]string, cfgName string, dataMap map[string]interface{}, suffix string) error {
	if len(rules) == 0 {
		return nil
	}

	var problems []string
	for tag, rule := range rules {
		path, hasMapping := mappings[tag]
		if !hasMapping {
			continue
		}

		val, found := lookupPath(cfgName, dataMap, path)
		if !found || val == "" {
			if rule.Required {
				problems = append(problems, fmt.Sprintf("%s%s: campo obbligatorio mancante (percorso %q)", tag, suffix, path))
			}
			continue
		}

		if err := checkType(val, rule); err != nil {
			problems = append(problems, fmt.Sprintf("%s%s: %v", tag, suffix, err))
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("validazione fallita: %s", strings.Join(problems, "; "))
}

// checkType applica il controllo Type/Enum/Pattern di una FieldValidation
// a un valore già risolto (stringa). Un Type sconosciuto/non impostato non
// blocca nulla oltre al controllo Required già fatto dal chiamante.
func checkType(val string, rule FieldValidation) error {
	switch rule.Type {
	case "", "string":
		// nessun controllo di formato oltre a Required
	case "int":
		if _, err := strconv.Atoi(val); err != nil {
			return fmt.Errorf("atteso un intero, trovato %q", val)
		}
	case "bool":
		if _, err := strconv.ParseBool(val); err != nil {
			return fmt.Errorf("atteso un booleano, trovato %q", val)
		}
	case "cidr":
		if _, _, err := net.ParseCIDR(val); err != nil {
			return fmt.Errorf("atteso un CIDR valido (es. 10.0.0.0/24), trovato %q", val)
		}
	case "ip":
		if net.ParseIP(val) == nil {
			return fmt.Errorf("atteso un indirizzo IP valido, trovato %q", val)
		}
	case "enum":
		if !containsString(rule.Enum, val) {
			return fmt.Errorf("valore %q non tra quelli ammessi %v", val, rule.Enum)
		}
	}

	if rule.Pattern != "" {
		matched, err := regexp.MatchString(rule.Pattern, val)
		if err != nil {
			return fmt.Errorf("pattern di validazione %q non valido nella ConfigMap: %w", rule.Pattern, err)
		}
		if !matched {
			return fmt.Errorf("valore %q non corrisponde al pattern %q", val, rule.Pattern)
		}
	}
	return nil
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}