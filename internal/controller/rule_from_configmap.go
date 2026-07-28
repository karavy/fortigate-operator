package controller

// Logica di merge pura, senza dipendenze da client-go/controller-runtime -
// separata apposta in questo file da rule_from_configmaps_client.go (dove
// vive la chiamata client.List vera e propria), così è testabile con dati
// Go semplici indipendentemente dal client Kubernetes reale.

import (
	"encoding/json"
	"fmt"
)

// configMapRuleSource è la vista MINIMA di una ConfigMap che serve al
// merge - astratta apposta da corev1.ConfigMap per poter testare
// mergeConfigMapRuleSources con dati Go semplici, senza bisogno di un
// client/API server Kubernetes reale.
type configMapRuleSource struct {
	Namespace string
	Name      string
	Data      map[string]string
}

// ruleJSONKey è la chiave dentro ConfigMap.Data che ci si aspetta contenga
// il JSON della regola (stessa convenzione di "rule.json" usata negli
// esempi di ConfigMap già consegnati per il layout a volumi).
const ruleJSONKey = "rule.json"

// mergeConfigMapRuleSources unisce più ConfigMap (già lette, qui come
// dati semplici) in un unico RulesFile - stesse regole di merge di
// LoadOperatorRulesFromDirectory: una regola definita in più di una
// ConfigMap è un errore esplicito, "common" definito in più di una
// ConfigMap è un errore esplicito. Una ConfigMap che matcha il selettore
// ma non ha la chiave "rule.json" viene ignorata silenziosamente (non
// tutte le ConfigMap con quell'etichetta sono necessariamente regole -
// margine di errore accettabile, a differenza di un JSON malformato
// dentro quella chiave, che invece è un errore).
func mergeConfigMapRuleSources(sources []configMapRuleSource) (RulesFile, string, error) {
	merged := RulesFile{Rules: map[string]RuleMappingConfig{}}
	commonDefinedIn := ""
	used := 0

	for _, src := range sources {
		raw, ok := src.Data[ruleJSONKey]
		if !ok {
			continue
		}
		id := src.Namespace + "/" + src.Name
		used++

		var part RulesFile
		if err := json.Unmarshal([]byte(raw), &part); err != nil {
			return RulesFile{}, "", fmt.Errorf("parsing della ConfigMap %s (chiave %q) fallito: %w", id, ruleJSONKey, err)
		}

		for name, cfg := range part.Rules {
			if _, exists := merged.Rules[name]; exists {
				return RulesFile{}, "", fmt.Errorf("regola %q definita in più di una ConfigMap (trovata di nuovo in %s) - ogni regola deve stare in una sola ConfigMap", name, id)
			}
			merged.Rules[name] = cfg
		}

		if len(part.Common.Mappings) > 0 {
			if commonDefinedIn != "" {
				return RulesFile{}, "", fmt.Errorf("sezione \"common\" definita sia in %s che in %s - deve stare in una sola ConfigMap", commonDefinedIn, id)
			}
			merged.Common = part.Common
			commonDefinedIn = id
		}
	}

	return merged, fmt.Sprintf("%d ConfigMap (su %d trovate)", used, len(sources)), nil
}