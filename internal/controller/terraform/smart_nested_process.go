package terraform

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	k8sdinovaonev1 "github.com/karavy/k8s-operator-fortigate/api/v1"
)

const (
	startTag = "<FGT_FOR_STR>"
	endTag   = "<FGT_FOR_END>"
)

// normalizzaMappaInMaiuscolo converte ricorsivamente tutte le chiavi in MAIUSCOLO
// (invariata rispetto alla versione originale).
func normalizzaMappaInMaiuscolo(v interface{}) interface{} {
	switch item := v.(type) {
	case map[string]interface{}:
		nuovaMappa := make(map[string]interface{})
		for k, val := range item {
			nuovaMappa[strings.ToUpper(k)] = normalizzaMappaInMaiuscolo(val)
		}
		return nuovaMappa
	case []interface{}:
		for i, val := range item {
			item[i] = normalizzaMappaInMaiuscolo(val)
		}
		return item
	default:
		return v
	}
}

// processVariables confronta il tag del template con il path concatenato a
// runtime (invariata rispetto alla versione originale - stesso identico
// comportamento, incluso il fallback che aggiunge "_<index>" ai tag non
// risolti, di cui dipende rule_fwrules.go).
func processVariables(text string, index int, currentItemData map[string]interface{}, currentPath string) string {
	reVars := regexp.MustCompile(`<([^>]+)>`)
	return reVars.ReplaceAllStringFunc(text, func(match string) string {
		varName := match[1 : len(match)-1]

		if varName == "FGT_FOR_STR" || varName == "FGT_FOR_END" || strings.HasPrefix(varName, "FGT_FOR_STR") {
			return match
		}

		tagPulito := strings.ToUpper(strings.TrimPrefix(varName, "FGT_"))

		for chiaveCRD, valore := range currentItemData {
			if _, ok := valore.([]interface{}); ok {
				continue
			}
			if _, ok := valore.(map[string]interface{}); ok {
				continue
			}

			var pathVariabileAttesa string
			if currentPath == "" {
				pathVariabileAttesa = chiaveCRD
			} else {
				pathVariabileAttesa = fmt.Sprintf("%s_%s", currentPath, chiaveCRD)
			}

			if tagPulito == pathVariabileAttesa {
				if strVal, ok := valore.(string); ok {
					return strVal
				}
				return fmt.Sprintf("%v", valore)
			}
		}

		return fmt.Sprintf("<%s_%d>", varName, index)
	})
}

// processVariablesNoSuffix si comporta come processVariables per i tag
// CHE VENGONO risolti, ma lascia un tag non risolto ESATTAMENTE come
// scritto, invece di aggiungergli un suffisso "_<index>". Serve per la
// sostituzione di scalari FUORI da qualunque ciclo, dove un indice di
// iterazione non ha alcun senso e romperebbe la compatibilità con tag
// come <FGT_PORT_NAME>/<FGT_VIP_NAME>/<FGT_RESOURCE_NAME> che restano
// intenzionalmente da riempire più avanti, fuori da questo motore (vedi
// rule_interface.go / rule_vip.go): quei tag DEVONO restare invariati se
// non corrispondono a un campo dello spec, non diventare <FGT_X_0>.
func processVariablesNoSuffix(text string, currentItemData map[string]interface{}, currentPath string) string {
	reVars := regexp.MustCompile(`<([^>]+)>`)
	return reVars.ReplaceAllStringFunc(text, func(match string) string {
		varName := match[1 : len(match)-1]

		if varName == "FGT_FOR_STR" || varName == "FGT_FOR_END" || strings.HasPrefix(varName, "FGT_FOR_STR") {
			return match
		}

		tagPulito := strings.ToUpper(strings.TrimPrefix(varName, "FGT_"))

		for chiaveCRD, valore := range currentItemData {
			if _, ok := valore.([]interface{}); ok {
				continue
			}
			if _, ok := valore.(map[string]interface{}); ok {
				continue
			}

			var pathVariabileAttesa string
			if currentPath == "" {
				pathVariabileAttesa = chiaveCRD
			} else {
				pathVariabileAttesa = fmt.Sprintf("%s_%s", currentPath, chiaveCRD)
			}

			if tagPulito == pathVariabileAttesa {
				if strVal, ok := valore.(string); ok {
					return strVal
				}
				return fmt.Sprintf("%v", valore)
			}
		}

		return match // invariato, nessun suffisso
	})
}

// loopBlock è un blocco <FGT_FOR_STR><TAG>...<FGT_FOR_END> individuato
// tramite matching bilanciato (quindi un blocco annidato dentro un altro
// non viene mai restituito come "di primo livello" dal blocco che lo
// contiene: resta parte del suo Body e verrà gestito quando quel corpo
// viene a sua volta scandito).
type loopBlock struct {
	tag   string // es. "FIREWALLRULES", già in maiuscolo
	body  string // contenuto tra <TAG> e il <FGT_FOR_END> corrispondente
	start int    // offset di inizio (byte di "<FGT_FOR_STR>") in content
	end   int    // offset subito dopo il "<FGT_FOR_END>" corrispondente
}

// findTopLevelLoops scandisce content e restituisce, in ordine, ogni
// blocco <FGT_FOR_STR> trovato al livello più esterno raggiungibile
// (cicli fratelli/paralleli inclusi), usando un conteggio di profondità
// per accoppiare correttamente ogni apertura con la sua vera chiusura
// anche in presenza di cicli annidati.
func findTopLevelLoops(content string) ([]loopBlock, error) {
	var blocks []loopBlock
	pos := 0
	for {
		idx := strings.Index(content[pos:], startTag)
		if idx == -1 {
			break
		}
		start := pos + idx
		afterStart := start + len(startTag)

		if afterStart >= len(content) || content[afterStart] != '<' {
			return nil, fmt.Errorf("%s non seguito da <NOME> (posizione %d)", startTag, start)
		}
		nameEnd := strings.IndexByte(content[afterStart:], '>')
		if nameEnd == -1 {
			return nil, fmt.Errorf("tag <NOME> dopo %s non chiuso (posizione %d)", startTag, start)
		}
		tag := strings.ToUpper(content[afterStart+1 : afterStart+nameEnd])
		bodyStart := afterStart + nameEnd + 1

		depth := 1
		scan := bodyStart
		for depth > 0 {
			nextStart := strings.Index(content[scan:], startTag)
			nextEnd := strings.Index(content[scan:], endTag)
			if nextEnd == -1 {
				return nil, fmt.Errorf("%s mancante per il blocco <%s>", endTag, tag)
			}
			if nextStart != -1 && nextStart < nextEnd {
				depth++
				scan += nextStart + len(startTag)
				continue
			}
			depth--
			scan += nextEnd + len(endTag)
		}
		end := scan

		blocks = append(blocks, loopBlock{
			tag:   tag,
			body:  content[bodyStart : end-len(endTag)],
			start: start,
			end:   end,
		})
		pos = end
	}
	return blocks, nil
}

// resolveItem normalizza un elemento di lista in una mappa su cui
// processVariables possa lavorare: se è già un oggetto lo usa così com'è;
// se è uno scalare (stringa/numero/bool - liste di questo tipo, es.
// members: ["a", "b"]), lo avvolge come {"VALUE": elemento}, così un
// template può referenziarlo con <TAG_VALUE> (stessa regola del prefisso
// cumulativo di tutti gli altri campi). Prima questo caso veniva
// silenziosamente scartato.
func resolveItem(item interface{}) (map[string]interface{}, bool) {
	if m, ok := item.(map[string]interface{}); ok {
		return m, true
	}
	if item == nil {
		return nil, false
	}
	return map[string]interface{}{"VALUE": item}, true
}

// resolveListPath cerca blk.tag dentro container e restituisce la lista
// corrispondente. Prova prima un match ESATTO su una singola chiave (il
// comportamento originale, es. "FIREWALLRULES"): se container ha
// letteralmente quella chiave ed è una lista, la usa subito.
//
// Se il match esatto fallisce, interpreta tag come un PERCORSO composto
// separato da "_" (es. "EXTRAPARAMS_B" -> container["EXTRAPARAMS"]["B"]),
// per raggiungere una lista annidata dentro un campo che NON è esso
// stesso una lista (es. una lista "b" dentro un oggetto "extraParams" che
// è un singolo oggetto, non un array). Ogni segmento deve corrispondere a
// una chiave la cui parte di "grado" superiore sia essa stessa un
// map[string]interface{} - se un qualunque passaggio intermedio non è una
// mappa (es. è già la lista, ma ci sono altri segmenti dopo, o è uno
// scalare), la risoluzione fallisce.
//
// Questo funziona in modo affidabile perché i nomi di campo reali in
// questo sistema sono camelCase (es. "extraParams", "firewallRules") e
// quindi, una volta maiuscolizzati, NON contengono mai un underscore al
// loro interno - ogni underscore nel tag è quindi sempre e solo un
// separatore di livello, mai parte del nome di un singolo campo.
func resolveListPath(container map[string]interface{}, tag string) ([]interface{}, bool) {
	if v, exists := container[tag]; exists {
		if list, ok := v.([]interface{}); ok {
			return list, true
		}
	}

	parts := strings.Split(tag, "_")
	if len(parts) < 2 {
		return nil, false
	}

	var cur interface{} = container
	for _, part := range parts {
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

	list, ok := cur.([]interface{})
	return list, ok
}

// renderLoop espande UN blocco: cerca blk.tag dentro parentMap (anche come
// percorso composto, vedi resolveListPath), e per ogni elemento della
// lista genera il testo del corpo con gli eventuali cicli annidati al suo
// interno già risolti e le variabili sostituite. basePath è il prefisso
// cumulativo ereditato dai livelli superiori ("" al primo livello,
// "FIREWALLRULES" per un ciclo annidato dentro un elemento di
// FIREWALLRULES, ecc.).
func renderLoop(blk loopBlock, parentMap map[string]interface{}, basePath string) (string, error) {
	listData, found := resolveListPath(parentMap, blk.tag)
	if !found {
		// NOTA: prima restituiva silenziosamente "", nil - un ciclo non
		// in testa al file (renderInPlace, non splitIntoFiles) il cui tag
		// non risolveva produceva semplicemente una sezione vuota, senza
		// nessun segnale d'errore. Stessa correzione già fatta per
		// splitIntoFiles: un tag di ciclo che non risolve è quasi
		// certamente un refuso, non un caso da far sparire silenziosamente.
		return "", fmt.Errorf("nessuna lista trovata per <%s> (né come campo diretto né come percorso composto tipo <PARENT_FIGLIO>) - controlla il tag nel template contro i nomi reali dei campi CR", blk.tag)
	}

	subPath := blk.tag
	if basePath != "" {
		subPath = basePath + "_" + blk.tag
	}

	nested, err := findTopLevelLoops(blk.body)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	for i, rawItem := range listData {
		itemMap, ok := resolveItem(rawItem)
		if !ok {
			continue
		}
		rendered, err := renderBlockBody(blk.body, nested, itemMap, subPath, i+1)
		if err != nil {
			return "", err
		}
		b.WriteString(rendered)
	}
	return b.String(), nil
}

// renderBlockBody espande gli eventuali cicli annidati trovati dentro il
// corpo di un blocco (nested, con gli offset relativi a body), poi passa
// il risultato a processVariables per le sostituzioni scalari rimanenti.
func renderBlockBody(body string, nested []loopBlock, itemMap map[string]interface{}, path string, index int) (string, error) {
	var b strings.Builder
	pos := 0
	for _, nb := range nested {
		b.WriteString(body[pos:nb.start])
		rendered, err := renderLoop(nb, itemMap, path)
		if err != nil {
			return "", err
		}
		b.WriteString(rendered)
		pos = nb.end
	}
	b.WriteString(body[pos:])
	return processVariables(b.String(), index, itemMap, path), nil
}

// renderInPlace espande TUTTI i blocchi di primo livello trovati in
// content ripetendoli IN LINEA nello stesso testo (nessuno split in più
// file) - questo è il caso di più cicli paralleli/fratelli, o di un
// singolo ciclo che non apre la prima riga del file. Il testo che sta
// FUORI da ogni blocco viene comunque passato a processVariables contro
// la root dello spec (path ""), per supportare placeholder scalari come
// <HOSTNAME> usati fuori da qualunque ciclo.
func renderInPlace(content string, blocks []loopBlock, specMap map[string]interface{}) (string, error) {
	var b strings.Builder
	pos := 0
	for _, blk := range blocks {
		b.WriteString(processVariablesNoSuffix(content[pos:blk.start], specMap, ""))
		rendered, err := renderLoop(blk, specMap, "")
		if err != nil {
			return "", err
		}
		b.WriteString(rendered)
		pos = blk.end
	}
	b.WriteString(processVariablesNoSuffix(content[pos:], specMap, ""))
	return b.String(), nil
}

// splitIntoFiles gestisce il caso "la prima riga del file apre un ciclo":
// genera un file per ciascun elemento della lista di quel PRIMO blocco
// (anche se raggiunta tramite un percorso composto, es. "EXTRAPARAMS_B" ->
// spec.extraParams.b - vedi resolveListPath). Se dopo la chiusura di quel
// blocco esistono ALTRI cicli di primo livello (fratelli), questi vengono
// ripetuti in linea (via renderInPlace, contro la root dello spec) dentro
// OGNI file generato.
func splitIntoFiles(content, filename string, blocks []loopBlock, specMap map[string]interface{}) error {
	first := blocks[0]

	listData, found := resolveListPath(specMap, first.tag)
	if !found {
		// NOTA: prima questo caso cancellava silenziosamente il file
		// originale senza generare nulla (os.Remove senza errore
		// riportato al chiamante) - un nome di tag sbagliato in un
		// template a split faceva sparire il template senza nessun
		// segnale. Ora restituiamo un errore esplicito: un template
		// il cui ciclo di testa non trova corrispondenza nello spec è
		// quasi certamente un refuso nel tag o nel CRD, non qualcosa
		// da far sparire silenziosamente.
		return fmt.Errorf("nessuna lista trovata per <%s> (né come campo diretto né come percorso composto tipo <PARENT_FIGLIO>) - controlla il tag nel template contro i nomi reali dei campi CR", first.tag)
	}

	afterFirst := content[first.end:]
	siblingBlocks, err := findTopLevelLoops(afterFirst)
	if err != nil {
		return err
	}

	nested, err := findTopLevelLoops(first.body)
	if err != nil {
		return err
	}

	base, ext := splitTerraformExt(filename)

	fmt.Printf("Rilevato ciclo di testa su '%s' (%d elementi). Avvio lo split...\n", first.tag, len(listData))

	for i, rawItem := range listData {
		itemMap, ok := resolveItem(rawItem)
		if !ok {
			continue
		}

		mainContent, err := renderBlockBody(first.body, nested, itemMap, first.tag, i+1)
		if err != nil {
			return err
		}

		siblingContent, err := renderInPlace(afterFirst, siblingBlocks, specMap)
		if err != nil {
			return err
		}

		newFilename := fmt.Sprintf("%s_%d%s", base, i+1, ext)
		if err := os.WriteFile(newFilename, []byte(mainContent+siblingContent), 0644); err != nil {
			return fmt.Errorf("errore scrittura file %s: %w", newFilename, err)
		}
		fmt.Printf("Generato file separato: %s\n", newFilename)
	}

	return os.Remove(filename)
}

func buildSpecMap(instance *k8sdinovaonev1.FortigateConfig) (map[string]interface{}, error) {
	specBytes, err := json.Marshal(instance.Spec)
	if err != nil {
		return nil, fmt.Errorf("errore di marshalling della spec: %w", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(specBytes, &raw); err != nil {
		return nil, fmt.Errorf("errore di unmarshalling in mappa generica: %w", err)
	}
	final, ok := normalizzaMappaInMaiuscolo(raw).(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("errore nel cast della mappa normalizzata")
	}
	return final, nil
}

// smartNestedProcess è il punto di ingresso, stessa firma di prima.
//
// Comportamento:
//   - nessun ciclo nel file -> sostituzione scalari contro la root dello
//     spec (novità: prima non veniva chiamato affatto processVariables in
//     questo caso); un tag NON risolto resta invariato (nessun suffisso
//     "_N", per non rompere tag come <FGT_PORT_NAME>/<FGT_VIP_NAME> che
//     restano intenzionalmente da riempire più avanti, fuori da questo
//     motore - vedi rule_interface.go / rule_vip.go).
//   - il PRIMO ciclo di primo livello trovato apre esattamente la prima
//     riga del file -> split in file separati (comportamento originale),
//     con eventuali cicli fratelli dopo di esso ripetuti in linea in ogni
//     file generato (novità). Qui un tag non risolto DENTRO al ciclo
//     riceve il suffisso "_<indice>" (comportamento originale invariato -
//     rule_fwrules.go dipende da questo). Il tag del ciclo può anche
//     essere un percorso composto tipo "EXTRAPARAMS_B" per raggiungere
//     una lista annidata dentro un campo che non è esso stesso una lista
//     (novità - vedi resolveListPath). Se il tag (semplice o composto) non
//     risolve a nessuna lista, ora viene restituito un ERRORE esplicito
//     (prima il file veniva cancellato silenziosamente senza generare
//     nulla - comportamento corretto perché era un pericolo silente, non
//     perché qualcuno lo usasse intenzionalmente).
//   - altrimenti -> tutti i cicli di primo livello trovati (anche più di
//     uno, "paralleli") vengono ripetuti in linea nello stesso file
//     (novità: prima non venivano proprio processati).
//
// writeAndEnsureDotTf scrive "rendered" nel file corrispondente a
// templateFilename, garantendo che il nome FINALE su disco finisca sempre
// in ".tf" (stessa garanzia già usata in splitIntoFiles/tfCommon, vedi
// splitTerraformExt in rule_helpers.go) - anche quando templateFilename
// arriva ancora con ".tmpl" nel nome (es. i template a risorsa singola
// come create_vip/interface, che passano da qui - non da splitIntoFiles -
// dato che non hanno nessun ciclo <FGT_FOR_STR>). Se templateFilename
// finiva già in ".tf", il comportamento è identico a un semplice
// os.WriteFile su quello stesso path (nessuna rinomina, nessun file
// residuo).
func writeAndEnsureDotTf(templateFilename, rendered string) error {
	base, ext := splitTerraformExt(templateFilename)
	finalName := base + ext

	if err := os.WriteFile(finalName, []byte(rendered), 0644); err != nil {
		return fmt.Errorf("scrittura di %s fallita: %w", finalName, err)
	}

	if finalName != templateFilename {
		if err := os.Remove(templateFilename); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("rimozione del file sorgente %s dopo aver scritto %s fallita: %w", templateFilename, finalName, err)
		}
	}
	return nil
}

func smartNestedProcess(templateFilename string, instance *k8sdinovaonev1.FortigateConfig) error {
	specMapFinal, err := buildSpecMap(instance)
	if err != nil {
		return err
	}

	contentBytes, err := os.ReadFile(templateFilename)
	if err != nil {
		return fmt.Errorf("errore lettura template: %w", err)
	}
	content := strings.TrimSpace(string(contentBytes))

	blocks, err := findTopLevelLoops(content)
	if err != nil {
		return err
	}

	if len(blocks) == 0 {
		rendered := processVariablesNoSuffix(content, specMapFinal, "")
		return writeAndEnsureDotTf(templateFilename, rendered)
	}

	if blocks[0].start == 0 {
		return splitIntoFiles(content, templateFilename, blocks, specMapFinal)
	}

	rendered, err := renderInPlace(content, blocks, specMapFinal)
	if err != nil {
		return err
	}
	return writeAndEnsureDotTf(templateFilename, rendered)
}