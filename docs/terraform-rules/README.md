# Refactor: motore FOR paralleli + dispatch delle OperatorRule via registry

## 🆕 Aggiornamento: validazione dei campi DENTRO liste gestite dal motore

Buco reale trovato grazie alla tua domanda: `Validate`/`Mappings` in
`rule_configdriven.go` non vedono MAI i campi di `accProfiles`/
`storageDisks`, perché quelle liste sono gestite direttamente dal motore
(tag `<FGT_FOR_STR>` nel template) - `rule_configdriven.go` non ne sa
nulla. Non esisteva quindi nessun modo di validare, ad esempio, che
`storageDisks[].size` sia un intero o che `accProfiles[].cliConfig` sia
`"enable"`/`"disable"`.

**Nuovo campo `validateLists`** in `RuleMappingConfig`: chiave = percorso
della lista (lo stesso che il tag del template referenzia, es.
`spec.extraParams.storageDisks`), valore = mappa nome-campo→`FieldValidation`,
applicata a **ogni elemento** della lista.

```json
"validateLists": {
  "spec.extraParams.storageDisks": {
    "size": { "required": true, "type": "int" },
    "device": { "required": true, "pattern": "^/dev/.+" }
  }
}
```

**Comportamento**: accumula tutti i problemi trovati (non si ferma al
primo), messaggio con indice dell'elemento (`storageDisks[2], campo
"partition": obbligatorio mancante`). Una lista referenziata ma assente
dallo spec **non** è un errore qui - se era obbligatoria che il motore la
trovi, è compito del motore segnalarlo quando prova a iterarla per la
sostituzione (vedi la correzione precedente su `renderLoop`).

⚠️ **Limite d'ordine, dichiarato**: questa validazione gira **dopo** che
`smartNestedProcess` ha già scritto il file con i valori sostituiti - a
differenza di `ListField`/`ItemMappings` (che validano *prima* di
scrivere). Se fallisce, il file generato contiene già i valori (anche
quelli non validi), ma l'intero `OperatorRule` fallisce comunque prima di
arrivare a eseguire Terraform - nessuna conseguenza pratica, ma volevo
dichiararlo invece di lasciartelo scoprire.

Verificato con 5 test: dati validi (nessun errore), `size` non numerico,
`cliConfig` fuori enum, campo obbligatorio mancante in un solo elemento su
due (con l'indice giusto nel messaggio), lista del tutto assente (nessun
errore).

---


## 🐛 Correzione: i cicli NON in testa al file fallivano silenziosamente

Scoperto grazie al tuo `system_global.tf` (i due cicli `accProfiles`/
`storageDisks` non sono sulla prima riga del file, essendoci prima un
blocco `resource "fortios_system_global"`): quando un tag di ciclo non
risolveva, `renderLoop` (usato per i cicli **non** in testa - il percorso
`renderInPlace`, diverso da `splitIntoFiles`) restituiva silenziosamente
una sezione **vuota**, senza nessun errore. Avevo già corretto questo
esatto problema per `splitIntoFiles` (i cicli in testa), ma non per questo
secondo percorso - inconsistenza reale tra i due, ora eliminata.

**Correzione**: `renderLoop` ora restituisce un errore esplicito quando un
tag (semplice o composto) non risolve a nessuna lista - identico
messaggio/comportamento di `splitIntoFiles`, propagato correttamente sia
per i cicli di primo livello sia per quelli annidati dentro un elemento.

### ⚠️ Conseguenza da conoscere, non solo per te

Un campo lista **annidato facoltativo che un elemento omette del tutto**
(non lo mette a `[]` vuoto, proprio non lo scrive) ora produce un errore,
dove prima produceva silenziosamente zero iterazioni per quell'elemento.
Una lista **presente ma vuota** (`[]`) resta invece perfettamente valida,
zero iterazioni, nessun errore - il problema riguarda solo l'assenza
completa del campo. Se nel tuo dominio un campo del genere è
legittimamente facoltativo (es. `internetServices` per una
`firewallRule`), scrivilo esplicitamente come lista vuota nella CR
piuttosto che ometterlo.

Verificato con 4 nuovi test: il tuo caso esatto (`<FGT_ACCPROFILES>`,
prefisso sbagliato) → errore esplicito; lo stesso con il tag corretto
(`<EXTRAPARAMS_ACCPROFILES>`) → funziona; lista presente ma vuota →
nessun errore; campo annidato del tutto assente → ora un errore (la
conseguenza sopra, documentata esplicitamente nel test).

---


## 🆕 Aggiornamento: una ConfigMap per regola, invece di un unico `rules.json`

Su tua richiesta: invece di un'unica ConfigMap `fgt-operator-rules` con
dentro tutte le regole (rischio di superare il limite di 1MB per
ConfigMap se le regole crescono molto), ora **ogni regola vive nella
propria ConfigMap**, montata nella propria sottocartella - **nessun
`subPath`**, coerentemente con quanto già stabilito: ogni ConfigMap ha il
proprio volume/mount dedicato, esattamente come già fai per
`operator-config`/`webhook-certs`.

```
/etc/fgt-operator/rules.d/
├── common/rule.json       (ConfigMap fgt-rule-common)
├── interface/rule.json    (ConfigMap fgt-rule-interface)
├── create-vip/rule.json   (ConfigMap fgt-rule-create-vip)
└── fwrules/rule.json      (ConfigMap fgt-rule-fwrules)
```

### Nuova funzione: `LoadOperatorRulesFromDirectory`

Scansiona **ricorsivamente** la directory alla ricerca di file `*.json`
(ogni sottocartella = una ConfigMap), e li unisce in un unico ruleset
attivo. Ogni file ha la STESSA forma di prima (`{"common": {...}}` e/o
`{"rules": {...}}`) - puoi mettere anche più di una regola in un solo
file se vuoi raggruppare, non è un vincolo "una regola = un file
per forza".

**Regole di merge, esplicite e verificabili** (niente vince "silenziosamente"):
- **`rules`**: unione di tutte le regole trovate. La STESSA regola
  definita in due file diversi è un **errore all'avvio**, non una
  sovrascrittura.
- **`common`**: deve essere definita in **al più un file**. Se più di uno
  la definisce, è un errore all'avvio.

`LoadOperatorRulesFromFile`/`LoadOperatorRulesFromBytes` (singolo file)
**restano invariate** e continuano a funzionare esattamente come prima -
non è un cambio distruttivo, è un'alternativa in più. Ho estratto la
logica comune di registrazione in `applyRulesFile`, usata da entrambe le
strade, per non duplicarla.

### Nomi: ConfigMap vs regola

`create_vip` (nome della regola, con underscore, usato in `operatorRule:
create_vip` nelle CR) e `fgt-rule-create-vip` (nome della ConfigMap, con
trattino - i nomi degli oggetti Kubernetes non ammettono underscore) sono
**deliberatamente diversi** - il nome della regola vive nel *contenuto*
JSON, non nel nome della ConfigMap, quindi non c'è nessun vincolo di
farli combaciare.

### File aggiornati/nuovi

- `rule_configdriven.go` — `LoadOperatorRulesFromDirectory` + `applyRulesFile` (refactoring, nessuna duplicazione)
- `rules-configmaps-split.yaml` — le 4 ConfigMap di esempio
- `manager.yaml` — 4 volumi/mount al posto di uno, env `FGT_OPERATOR_RULES_DIR` al posto di `FGT_OPERATOR_RULES_FILE`
- `MAIN_GO_SNIPPET_TO_ADD.go.txt` — aggiornato per chiamare `LoadOperatorRulesFromDirectory`

### Verificato con test

4 nuovi test: merge di 3 ConfigMap in un unico ruleset (regole +
`common`), regola duplicata tra due file → errore esplicito, `common`
duplicato tra due file → errore esplicito, file non-`.json` nella stessa
cartella (es. i file `..data` che Kubernetes crea per i mount di
ConfigMap) → ignorati senza errore. Più tutti i 27 test precedenti,
nessuna regressione.

---


## 🐛 Correzione (di nuovo): il problema `.tmpl`→`.tf` c'era anche nei template senza ciclo

La correzione precedente (`tfCommon`, `splitIntoFiles`) non copriva **tutti** i percorsi. `smartNestedProcess` ha 3 uscite possibili:

1. nessun ciclo (`create_vip`/`interface`, tag piatti) → scriveva sullo
   stesso nome in ingresso
2. split di testa (`fwrules`) → **già corretto** la volta scorsa
3. cicli fratelli senza split → scriveva sullo stesso nome in ingresso

Avevo corretto solo il caso 2. I casi 1 e 3 - cioè esattamente `create_vip`
(il caso della CR `fortigateconfig-vip-trial`) - riscrivevano ancora sul
nome originale, `.tmpl` compreso.

**Correzione**: nuova funzione `writeAndEnsureDotTf` (in
`smart_nested_process.go`), usata da entrambi i casi 1 e 3, stessa
garanzia già data da `splitTerraformExt`/`tfCommon`.

**Conseguenza a catena, corretta anche questa**: se il nome cambia
(`.tmpl` → `.tf`), `selectOperatorRule` non può più passare a
`handler.Apply` il nome ORIGINALE (che non esiste più, rinominato) - va
ricalcolato lo stesso nome finale con `splitTerraformExt` prima di
chiamare `handler.Apply`, esattamente come già fatto per l'allineamento
`firewallInstance.Name`/`fortiConfig.Name`. Un collega ha giustamente
chiesto di tenere la correzione contenuta (in `selectOperatorRule`, non
spostata più in alto in `modifyTFFiles`, che non ha visibilità sui nomi
interni) - fatto così.

Verificato con `TestSelectOperatorRuleEndToEndWithTmplSuffixedSingleObjectTemplate`:
passa da `selectOperatorRule` per intero (come farebbe il Reconciler),
template sorgente `..._50_firewall_vip.tf.tmpl`, verifica che il `.tmpl`
non esista più, che il `.tf` finale esista con tutti i valori sostituiti
correttamente (sia quelli della regola config-driven sia il nome
risorsa).

---


## 🐛 Correzione: anche il file "common" doveva finire in `.tf`

Confermato dal tuo `ls -la`: `fortigatefirewall-trial_00_common.tf.tmpl` —
la sostituzione (hostname/token/bucket/key) **aveva funzionato**, ma il
file finiva in `.tmpl`, quindi Tofu lo ignorava del tutto (stesso identico
bug già corretto per i file splittati, ma qui il path arriva da
`CurrentOperatorConfig().CommonTemplateSuffix`, configurato nel tuo
ambiente con un suffisso che include ancora `.tmpl`).

**Perché non basta cambiare l'estensione PRIMA di chiamare
`modifyFileValue`**: quella funzione legge e scrive sullo stesso
`filename` che riceve. Se lo si modifica prima (togliendo `.tmpl`),
`modifyFileValue` cerca di leggere un file che non esiste ancora con quel
nome — il file reale su disco (scritto da `copyTerraformFiles`) ha ancora
`.tmpl`. Fallirebbe subito in lettura.

**Correzione, dentro `tfCommon` stessa** (non serve più toccare
`selectOperatorRule` per questo): `modifyFileValue` legge/scrive prima sul
path REALE (invariato); **solo dopo**, a sostituzione avvenuta, il file
viene rinominato in un path che finisce sempre in `.tf` (stessa garanzia
di `splitTerraformExt`, già usata per i file splittati - riusata qui, non
duplicata). Se il file finiva già in `.tf`, non succede nessuna rinomina
inutile.

Verificato con `TestTfCommonRenamesTmplSuffixedFileToDotTf` (riproduce
esattamente il tuo scenario: file sorgente `..._00_common.tf.tmpl`,
verifica che il `.tmpl` non esista più dopo e che il nuovo `.tf` contenga
i valori sostituiti) e `TestTfCommonLeavesAlreadyDotTfFileNameUnchanged`
(nessuna rinomina superflua se il nome è già corretto).

---


## 🆕 Aggiornamento: anche i tag "comuni" (`tfCommon`) ora da ConfigMap, non più hardcoded

`tfCommon` aveva ancora una lista fissa di 6 tag scritta in Go
(`<FGT_IP_OR_NAME>`, `<FGT_API_TOKEN>`, ecc.) - lo stesso pattern che
avevamo già sostituito per le `OperatorRule` con `rule_configdriven.go`,
ma mai applicato qui. Ora è configurabile allo stesso modo, riusando
**lo stesso `rules.json`** (nuova chiave `"common"`, facoltativa):

```json
{
  "common": {
    "mappings": {
      "FGT_IP_OR_NAME": "fortiIP",
      "FGT_API_TOKEN": "token",
      "FGT_S3STATE_KEY": "tfStateKey",
      "FGT_S3STATE_BUCKET": "s3Bucket",
      "FGT_S3STATE_REGION": "s3Region",
      "FGT_S3STATE_ENDPOINT": "s3Endpoint"
    }
  },
  "rules": { "...": "..." }
}
```

**Differenza importante rispetto ai mapping delle `OperatorRule`**: qui il
valore di ogni tag NON è un percorso nello spec della CR - è il **nome**
di uno dei 6 parametri già noti a runtime a chi chiama `tfCommon`
(`fortiIP`, `token`, `tfStateKey`, `s3Bucket`, `s3Region`, `s3Endpoint` -
elencati in `commonRuntimeValueNames` in `common_config.go`). Non puoi
inventare un settimo valore arbitrario: solo questi 6 esistono, dato che
non derivano dalla CR ma dai parametri di `selectOperatorRule`.

Se ometti del tutto `"common"` (o metti `"common": {}`), si usano i
default - **identici ai 6 tag che erano hardcoded prima**, quindi nessuna
migrazione obbligatoria: funziona così com'è finché non vuoi
rinominare/aggiungere/togliere qualche tag senza ricompilare.

Un nome di parametro sconosciuto nella ConfigMap (refuso) produce un
avviso su stderr e un valore vuoto per quel tag, invece di bloccare la
regola - coerente con come `rule_configdriven.go` gestisce già i percorsi
CR non risolvibili.

### File

- `common_config.go` (nuovo) — `CommonMappingConfig`,
  `SetCommonConfig`/`CurrentCommonMappings`, `buildCommonMods`
- `rule_configdriven.go` — `RulesFile` ha ora anche il campo `Common`;
  `LoadOperatorRulesFromBytes` chiama `SetCommonConfig` automaticamente
- `controller.go` — `tfCommon` usa `buildCommonMods(...)` invece della
  lista hardcoded

### Verificato con test

Default identici ai tag originali; mapping custom (nomi diversi, sottoinsieme
dei parametri); nome parametro sconosciuto → vuoto + avviso, non un
crash; caricamento della sezione `"common"` insieme alle regole nello
stesso file.

---


## 🐛 Correzione: `firewallInstance.Name` vs `fortiConfig.Name` disallineati

Bug reale che hai avuto in produzione (e che avevo segnalato come rischio
fin dalla primissima analisi del tuo codice originale, senza poterlo
verificare allora): `selectOperatorRule` calcolava **due percorsi
diversi** per lo stesso file.

- `smartNestedProcess` scriveva/splittava i file usando
  `firewallInstance.Name` (es. `fortigatefirewall-trial_..._1.tf`)
- `handler.Apply` (la regola config-driven) li cercava usando
  `fortiConfig.Name` (es. `fortigateconfig-rules-sample_..._1.tf`)

Se `FortigateFirewall` e `FortigateConfig` hanno nomi diversi (il caso
normale: un `FortigateFirewall` fisico può avere più CR `FortigateConfig`
associate, con nomi propri), il secondo cercava un file che non esisteva
con quel nome - esattamente l'errore "no such file or directory" che hai
avuto.

**Correzione**: un'unica variabile `fileName`, calcolata una sola volta,
usata sia per `smartNestedProcess` sia per `handler.Apply` - non possono
più disallinearsi, dato che letteralmente non esistono due calcoli
separati. Ho scelto `firewallInstance.Name` (non `fortiConfig.Name`)
perché è quello che il tuo log mostrava essere il nome **realmente
usato** per i file scritti su disco dal motore.

⚠️ **Non ho `copyTerraformFiles`**, quindi non so con certezza se questa
fosse l'intenzione originale o se invece fosse `copyTerraformFiles` a
dover usare `fortiConfig.Name` (nel qual caso andrebbe corretto lì, non
qui) - ma dato che il tuo log mostra `smartNestedProcess` scrivere
correttamente con `firewallInstance.Name`, allineare tutto su quel nome è
la correzione minima e coerente con quanto osservato. Se la tua intenzione
era l'opposto (un file per ogni `FortigateConfig`, non condiviso per
`FortigateFirewall`), dimmelo e invertiamo la scelta - toccherebbe anche
`copyTerraformFiles`, che dovresti incollarmi.

Verificato con `TestSelectOperatorRuleUsesConsistentFileNameEvenWhenNamesDiffer`:
riproduce esattamente lo scenario (nomi CR diversi), verifica che il file
finale sia trovabile senza errori.

---


## 🆕 Nuovo: file di configurazione generale dell'operator

Distinto apposta dalla ConfigMap delle regole (`rules.json`): quella
descrive **come riempire i tag di UNA OperatorRule**; questo nuovo file
(`operator_config.go`) descrive **impostazioni valide per ogni CR**, a
prescindere dalla regola usata - quelle che nel tuo codice originale erano
hardcoded:

| Campo | Valore originale hardcoded | Dove veniva usato |
|---|---|---|
| `commonTemplateSuffix` | `"_common.tf"` | nome del file comune (`<cr>_common.tf`) su cui gira `tfCommon` |
| `terraformStateFileName` | `"firewall.tfstate"` | nome del file di stato dentro il path S3 |
| `s3Region` | `"us-east-1"` | region passata al backend S3 di Terraform/Tofu |

Un file parziale è valido: i campi assenti restano ai default (identici ai
valori che erano hardcoded, quindi **comportamento invariato finché non
fornisci un file o fornisci un file parziale**).

### File

- `operator_config.go` — `OperatorConfig`, `DefaultOperatorConfig()`,
  `LoadOperatorConfigFromFile(path)`, `SetOperatorConfig`/`CurrentOperatorConfig`
- `operator-config.example.json` — esempio di contenuto
- `operator-configmap.example.yaml` — manifest ConfigMap + `volumeMount`
  nel Deployment (separato dalla ConfigMap delle regole, due file distinti
  montati in percorsi diversi)
- `controller.go` aggiornato — `tfCommon`/`selectOperatorRule` ora
  leggono `CurrentOperatorConfig()` invece dei valori hardcoded

### Wiring in `main.go` (da aggiungere ACCANTO a `LoadOperatorRulesFromFile`, che avevi già collegato)

```go
var operatorConfigPath string
// ... accanto agli altri flag.StringVar ...
flag.StringVar(&operatorConfigPath, "operator-config-path", "/etc/fgt-operator/operator-config.json",
    "Path al file JSON (montato da ConfigMap) con le impostazioni generali dell'operator.")

// ... dopo ctrl.NewManager, accanto alla chiamata a LoadOperatorRulesFromFile ...
if opCfg, err := controller.LoadOperatorConfigFromFile(operatorConfigPath); err != nil {
    setupLog.Error(err, "Failed to load operator config", "path", operatorConfigPath)
    os.Exit(1)
} else {
    controller.SetOperatorConfig(opCfg)
}
```

⚠️ A differenza di `LoadOperatorRulesFromFile` (che fallisce sempre se il
file manca), qui potresti preferire tollerare l'assenza del file e usare
silenziosamente i default (dato che sono già identici al comportamento
originale) - se preferisci quel comportamento invece di un errore fatale,
dimmelo e cambio la gestione dell'errore in `main.go`.

### Verificato con test

`DefaultOperatorConfig()` produce esattamente i valori prima hardcoded;
un file parziale (solo `s3Region`) lascia gli altri due campi ai default;
un file inesistente produce un errore; `SetOperatorConfig` cambia
effettivamente cosa scrive `tfCommon` (verificato sostituendo la region e
controllando l'output) e cosa calcolano `selectOperatorRule` per nome file
comune/nome file di stato.

---


## 🐛 Correzione: i file generati non finivano sempre in `.tf`

Hai segnalato che Tofu/Terraform ignora i file generati se non finiscono
in `.tf`. Causa confermata: se il file template sorgente ha ancora il
suffisso `.tmpl` (come **tutti** quelli che ti ho consegnato, es.
`firewall_policy.tf.tmpl`), il codice riusava `filepath.Ext(filename)` così
com'era - cioè `.tmpl` - per nominare i file splittati:

```
filename    = "workingDir/myfw_firewall_policy.tf.tmpl"
ext (prima) = ".tmpl"
generato    = "workingDir/myfw_firewall_policy.tf_1.tmpl"   <- sbagliato
```

Il `.tf` finiva in mezzo al nome, l'estensione reale restava `.tmpl` -
esattamente il tipo di file che Tofu/Terraform ignora silenziosamente.

**Correzione**: nuova funzione condivisa `splitTerraformExt` (in
`rule_helpers.go`) che toglie sempre prima un eventuale `.tmpl` finale, poi
calcola l'estensione vera - garantendo `.tf` in output a prescindere da
come si chiama il file sorgente. Usata sia da `splitIntoFiles`
(`smart_nested_process.go`, dove nascono i file) sia da
`IndexedResourceFile` (`rule_helpers.go`, dove `rule_configdriven.go` li
ritrova per applicare le sostituzioni) - **un solo punto di verità**, così
le due funzioni non possono disallinearsi tra loro (rischio che avevo già
segnalato in precedenza, ora eliminato).

Verificato con `TestSplitFilesAlwaysEndInDotTf`: template sorgente
`myfw_firewall_policy.tf.tmpl` → generati correttamente
`myfw_firewall_policy_1.tf` e `_2.tf`, nessun file con `.tmpl` residuo nel
nome, e `IndexedResourceFile` produce esattamente gli stessi nomi che il
motore ha davvero scritto su disco.

⚠️ **Non so con certezza se questa fosse davvero la causa nel tuo
ambiente** (non ho `copyTerraformFiles`, che potrebbe già rinominare i
file prima che il motore li veda) - ma il bug che ho riprodotto è reale e
coerente con il sintomo che descrivi, quindi l'ho corretto. Se il problema
persiste dopo questo fix, il file sorgente arriva al motore con un nome
diverso da quello che mi aspetto: incollami il path esatto che vedi nei
log/nella working dir e verifico di nuovo.

---


## 🆕 Aggiornamento: percorsi composti per raggiungere liste annidate dentro oggetti

Caso reale che mi hai segnalato: `extraParams: {a: 1, b: [{c:1, d:2}]}` -
un oggetto singolo (non una lista) con una lista annidata al suo interno.
Prima, `smart_nested_process.go` non riusciva a raggiungere `b` con un tag
di primo livello, perché cercava sempre `specMap[TAG]` con un match esatto
su una singola chiave - nessun modo di "entrare" in `extraParams` senza
che fosse esso stesso un ciclo.

Ora un tag può essere un **percorso composto separato da `_`**:
`<FGT_FOR_STR><EXTRAPARAMS_B>` risolve correttamente a
`spec.extraParams.b`, anche se `extraParams` è un oggetto singolo, non una
lista. Funziona sia per il ciclo di testa (file-split) sia per un ciclo
annidato dentro un elemento di un altro ciclo. Verificato con
`TestShape4c_CompoundPathReachesNestedListInsideObject`: 2 elementi in
`extraParams.b`, 2 file generati, valori corretti in entrambi.

**Come funziona**: `resolveListPath` prova prima un match esatto su tutto
il tag (comportamento originale, invariato per tag semplici come
`FIREWALLRULES`); se fallisce, spezza il tag su `_` e naviga la catena di
oggetti annidati un pezzo alla volta. Funziona in modo affidabile perché i
nomi di campo reali in questo sistema sono camelCase (`extraParams`,
`firewallRules`) e quindi, una volta maiuscolizzati, non contengono mai un
underscore al loro interno - ogni underscore nel tag è sempre un
separatore di livello, mai parte del nome di un singolo campo.

### 🐛 Correzione collegata, trovata testando questo (non l'hai chiesta esplicitamente, la segnalo comunque)

Prima, quando il tag del ciclo di testa non trovava NESSUNA corrispondenza
(né come match esatto né come percorso composto), `splitIntoFiles`
**cancellava silenziosamente il file originale senza generare nulla e
senza restituire un errore** - un tag scritto male in un template a split
faceva sparire il template senza nessun segnale. Ora restituisce un
errore esplicito con il nome del tag che non ha risolto, così l'errore
arriva su fino a `selectOperatorRule`/al tuo Reconciler invece di sparire
nei log. Se per qualche motivo ti serviva il comportamento vecchio
(cancellazione silenziosa), dimmelo - ma non vedo un caso d'uso legittimo
per questo, sembra un difetto puro.

Verificato con `TestShape4b_SimpleTagNotAtRootNowReturnsExplicitError`: un
tag che non risolve ora produce un errore, e il file originale resta
intatto (non viene toccato prima che l'errore sia stato accertato).

### Nessuna modifica a `rule_configdriven.go`

Questa correzione riguarda **solo** `smart_nested_process.go` (i cicli nel
template). `rule_configdriven.go` già supportava percorsi composti nei
`mappings`/`itemMappings` fin dall'inizio (il suo `lookupPath` naviga
qualunque catena di oggetti, non richiede che ogni passaggio sia esso
stesso un ciclo) - è per questo che la Forma 4 funzionava già lato
ConfigMap prima di questa correzione, e solo lato template no.

### Tutti i 26 test passano

7 nuovi (le 4 forme di dato + la correzione dell'errore esplicito + la
risoluzione del percorso composto) più i 19 precedenti, nessuna
regressione.

---


## 🔴 Correzione: `extraParams` da solo NON basta per le regole a lista

Ho scoperto (grazie a un errore reale che hai avuto applicando una CR)
che la mia assunzione precedente era **sbagliata**: pensavo che
`*runtime.RawExtension` accettasse sia un oggetto che un array JSON. Non
è così — **`controller-gen` genera sempre `type: object`** per un campo
`runtime.RawExtension`, indipendentemente da `+kubebuilder:pruning:
PreserveUnknownFields`. Un CR con una lista sotto quel campo viene
respinto dall'API server con esattamente l'errore che hai visto:

```
spec.extraParams: Invalid value: "array": spec.extraParams in body must be of type object: "array"
```

Non avevo potuto verificarlo perché non ho un toolchain kubebuilder in
questo sandbox per far girare `controller-gen` per davvero — un'assunzione
che avrei dovuto segnalarti come non verificata anziché darla per buona.

**Correzione**: due campi invece di uno.

- `ExtraParams *runtime.RawExtension` (json:"extraParams") → regole a
  risorsa singola (`interface`, `create_vip`) — **invariato**, era già
  corretto per il caso oggetto.
- `ExtraItems []runtime.RawExtension` (json:"extraItems") → regole a
  lista (`fwrules`) — **campo nuovo**, tipizzato come lista a livello Go
  apposta, in modo che lo schema OpenAPI generato sia `type: array` con
  ogni elemento libero di avere struttura annidata arbitraria (mantiene
  il supporto a `internetServices` dentro ogni regola, che era il punto di
  tutto questo).

Di conseguenza sono cambiati anche: `rules.example.json`/
`configmap.example.yaml` (`fwrules.listField` ora è `spec.extraItems`,
non più `spec.extraParams`) e `firewall_policy.tf.tmpl` (tag rinominati
`EXTRAITEMS*` invece di `EXTRAPARAMS*` - stesso discorso di prima: il tag
del ciclo di testa deve corrispondere esattamente al nome del campo CR).
`interface`/`create_vip` restano su `spec.extraParams`, nessun cambiamento
per loro.

**`rule_configdriven.go` non ha richiesto NESSUNA modifica** per questa
correzione: la sua logica di risoluzione percorsi è generica e non sa (né
le importa) se il campo Go sottostante è `*RawExtension` o
`[]RawExtension` - solo JSON/types sono cambiati, confermando ancora una
volta il vantaggio del design a dati invece che a codice.

⚠️ **Anche questa correzione non l'ho potuta verificare contro un vero
CRD generato** (stesso limite di rete di prima) — l'ho ragionata da
quanto riportato nel tuo errore reale e da come so che controller-gen
tratta tipicamente `[]runtime.RawExtension` (schema `array` con
`items: {type: object, x-kubernetes-preserve-unknown-fields: true}`), ma
**fai tu `make manifests` e controlla lo schema generato per `extraItems`
prima di fidartene al 100%** - se anche questa non fosse la forma esatta
che ti aspetti, dimmelo con l'errore preciso, così correggiamo di nuovo su
basi certe invece che su ragionamento.

---


## 🆕 Aggiornamento: unificazione completa in un solo campo `ExtraParams`

Su tua richiesta esplicita, **tutte e 3 le regole** (`interface`,
`create_vip`, `fwrules`) ora leggono da un **unico campo generico**
`spec.extraParams`, non più dai campi tipizzati dedicati
(`PortConfigurationParams`, `VIPConfigurationParams`, `FirewallRules`).

### Una sola conseguenza concreta, dichiarata prima di farla

`fwrules` usa un ciclo `<FGT_FOR_STR>` nel template, e quel tag **deve
corrispondere esattamente al nome del campo CR** (lo legge
`smart_nested_process.go`, non `rule_configdriven.go`). Unificare
`fwrules` ha quindi richiesto di **rinominare i tag** nel template già
validato: `firewall_policy.tf.tmpl` consegnato ora usa `<EXTRAPARAMS>` /
`<EXTRAPARAMS_RULENAME>` / `<EXTRAPARAMS_INTERNETSERVICES_NAME>` al posto
di `FIREWALLRULES*`. `interface`/`create_vip` invece non avevano cicli, quindi
per loro è cambiato solo il JSON delle regole, zero tocchi ai template.

### Perché un solo campo, e non due (`ExtraParams`+`ExtraItems`)

`ExtraParams` ora è `*runtime.RawExtension` (da
`k8s.io/apimachinery/pkg/runtime` - **libreria che il tuo `cmd/main.go`
già importa direttamente**, `runtime.NewScheme()`, quindi zero dipendenze
nuove nel tuo `go.mod`) invece di `map[string]string`. Accetta JSON
arbitrario: un oggetto per le regole a risorsa singola, una lista per
quelle a lista - **compreso nesting reale** (`internetServices` dentro
ogni `firewallRule`, impossibile con la precedente `[]map[string]string`
piatta). Vedi `TYPES_SNIPPET_TO_ADD.go.txt` aggiornato.

### ⚠️ Trasparenza sul test di questa parte specifica

Non sono riuscito a compilare/testare questa parte contro il **vero**
`k8s.io/apimachinery/pkg/runtime.RawExtension` in questo sandbox: un
dominio (`golang.org`, dipendenza transitiva di `apimachinery` tramite
`gogo/protobuf`) è bloccato qui, oltre a `proxy.golang.org` stesso. Ho
verificato la LOGICA (che `rule_configdriven.go` risolva correttamente
percorsi annidati quando il valore arriva da bytes JSON grezzi, esattamente
come farebbe `RawExtension.MarshalJSON`) con un tipo locale equivalente
(stesso comportamento di `MarshalJSON`, nessuna logica diversa) — non con
il tipo Kubernetes vero. È un test valido sulla logica, ma **non è la
stessa cosa di compilarlo nel tuo repo reale con la dipendenza vera** —
fallo tu prima di fidartene al 100%.

### Verificato (dove ho potuto testare per intero)

Nuovi test end-to-end (motore reale + regola config-driven insieme):
`interface` con `extraParams` come oggetto, e `fwrules` con `extraParams`
come lista **con `internetServices` annidato dentro ogni regola**,
verificando che il primo file generato abbia esattamente 1
`internet_service_name` e il secondo esattamente 2. Tutti gli altri test
precedenti (19 totali) restano verdi con i percorsi aggiornati.

### File aggiornati in questo passaggio

- `TYPES_SNIPPET_TO_ADD.go.txt` — un solo campo `ExtraParams *runtime.RawExtension`
- `rules.example.json` / `configmap.example.yaml` — tutte e 3 le regole su `spec.extraParams`, con `validate` aggiunta (ora più importante, avendo perso la validazione OpenAPI su questi campi)
- `firewall_policy.tf.tmpl` — **nuovo file consegnato**, tag rinominati `EXTRAPARAMS*`
- `create_vip_manual_test.go` — aggiornato a `spec.extraParams`

---


## 🆕 Aggiornamento: campi generici + validazione da ConfigMap per regole DAVVERO nuove

Fin qui, "aggiungere una regola" richiedeva comunque che i campi CR
referenziati **esistessero già** nella tua struct Go (`PortConfigurationParams`,
`FirewallRules`, ecc.), perché `rule_configdriven.go` legge quei campi via
`json.Marshal(cfg.Spec)`. Se la regola nuova ha bisogno di un campo che
**non esiste ancora**, restavano necessari: modifica ai types, `make
generate`, `make manifests`, `kubectl apply` del CRD.

Questo aggiornamento aggiunge una via alternativa per evitare quel giro,
quando ti va bene rinunciare alla validazione OpenAPI su quei campi
specifici:

### 1. Due campi generici da aggiungere UNA VOLA ai types (vedi `TYPES_SNIPPET_TO_ADD.go.txt`)

```go
// +optional
ExtraParams map[string]string `json:"extraParams,omitempty"`

// +optional
ExtraItems []map[string]string `json:"extraItems,omitempty"`
```

`ExtraParams` per regole a risorsa singola con campi nuovi;
`ExtraItems` per regole a lista con campi nuovi (un file per elemento,
come `fwrules`). **Limite dichiarato**: sono oggetti PIATTI
(stringa→stringa) — niente liste/oggetti annidati dentro un elemento. Se
ti serve nidificazione vera, dimmelo: serve un tipo diverso
(`apiextensionsv1.JSON`/`runtime.RawExtension`), con altre implicazioni
sul CRD che preferisco discutere con te prima di sceglierlo al posto tuo.

Questo è l'**unico** tocco ai types che resta necessario, e va fatto una
volta sola — non per ogni nuova regola, solo se non hai già un
`ExtraParams`/`ExtraItems` (o equivalente) in giro.

### 2. Validazione dichiarata nella ConfigMap, applicata a runtime in Go

`RuleMappingConfig` ora supporta una sezione `validate` (chiave = stesso
nome tag usato in `mappings`/`itemMappings`):

```json
{
  "rules": {
    "my_new_rule": {
      "mappings": {
        "FGT_RESOURCE_NAME": "$cr.name",
        "FGT_NEW_CIDR": "spec.extraParams.newCidr",
        "FGT_NEW_MODE": "spec.extraParams.newMode"
      },
      "validate": {
        "FGT_NEW_CIDR": { "required": true, "type": "cidr" },
        "FGT_NEW_MODE": { "required": true, "type": "enum", "enum": ["strict", "loose"] }
      }
    },
    "my_new_list_rule": {
      "listField": "spec.extraItems",
      "itemMappings": {
        "FGT_RESOURCE_NAME": "$cr.name",
        "FGT_ITEM_IP": "ip",
        "FGT_ITEM_HOSTNAME": "hostname"
      },
      "validate": {
        "FGT_ITEM_IP": { "required": true, "type": "ip" },
        "FGT_ITEM_HOSTNAME": { "required": true, "pattern": "^[a-z0-9-]+$" }
      }
    }
  }
}
```

(questi due esempi **non sono** in `rules.example.json` consegnato — li
ho tenuti solo qui nel README, così non finiscono per sbaglio come regole
reali se copi il file così com'è nella tua ConfigMap; il file consegnato
contiene solo le 3 regole che già esistevano davvero: `interface`,
`create_vip`, `fwrules`)

Tipi di validazione supportati: `string` (default), `int`, `bool`,
`cidr`, `ip`, `enum` (con `enum: [...]`), più un `pattern` (regex)
applicabile in aggiunta a qualunque tipo. Un campo con `"required": true`
assente/vuoto fa fallire la regola con un errore che elenca **tutti** i
problemi trovati in un colpo solo (non si ferma al primo).

**Per una regola a lista, se anche un solo elemento fallisce la
validazione, NESSUN file viene scritto** — nemmeno per gli elementi
validi. L'ho scelto deliberatamente (meglio bloccare l'intero batch che
generare Terraform parziale/incoerente per una CR con un elemento
sbagliato su tanti) — se preferisci il comportamento opposto (applica i
validi, salta solo quello sbagliato), dimmelo e lo cambio.

### Flusso completo per una regola davvero nuova

1. Scrivi il `.tf.tmpl`.
2. (solo se non l'hai già) aggiungi `ExtraParams`/`ExtraItems` ai types,
   `make generate && make manifests`, applica il CRD aggiornato — **una
   tantum**, non per ogni regola futura.
3. La CR usa `spec.extraParams`/`spec.extraItems` per i campi nuovi.
4. Aggiungi la voce in `rules.json` (`mappings`/`listField`+`itemMappings`
   + `validate`).
5. `kubectl apply` sulla ConfigMap. Zero Go, zero ricompilazione, zero
   redeploy del controller.

### Verificato con test

6 nuovi test in aggiunta agli 11 precedenti (17 totali, tutti passano):
campo obbligatorio mancante, tipo errato (CIDR non valido), enum non
ammesso, più errori accumulati in un solo messaggio, caso valido che
passa, e il caso a lista dove un solo elemento su due non è valido —
verificato che in quel caso **non venga scritto nessun file**, nemmeno
per l'elemento corretto.

---


## 🆕 Aggiornamento: regole guidate da ConfigMap (niente più un `.go` per ogni template)

`rule_interface.go`, `rule_vip.go` e `rule_fwrules.go` **sono stati
rimossi** e sostituiti da **un unico file generico**,
`rule_configdriven.go`, che legge il mapping tag→campo CR da un file
JSON (pensato per essere montato da una ConfigMap). Aggiungere supporto
per un nuovo file Terraform ora è:

1. scrivere il `.tf.tmpl` con i suoi tag `<FGT_XXX>` / `<FGT_FOR_STR>`;
2. aggiungere una voce alla ConfigMap (vedi `rules.example.json` /
   `configmap.example.yaml`);
3. **nessun codice Go, nessuna ricompilazione, nessun redeploy del
   controller** — solo `kubectl apply` sulla ConfigMap (ed eventuale
   restart del pod per rileggerla, vedi nota sotto).

### Formato del file regole

```json
{
  "rules": {
    "interface": {
      "mappings": {
        "FGT_RESOURCE_NAME": "$cr.name",
        "FGT_PORT_NAME": "spec.portConfigurationParams.portName",
        "FGT_PORT_ADDRESS": "spec.portConfigurationParams.portAddress",
        "FGT_PORT_MASK": "spec.portConfigurationParams.portMask"
      }
    },
    "fwrules": {
      "listField": "spec.firewallRules",
      "itemMappings": {
        "FGT_POLICY_NAME": "ruleName",
        "FGT_RESOURCE_NAME": "ruleName"
      }
    }
  }
}
```

Due modalità per regola:
- **`mappings`** (risorsa singola, come `interface`/`create_vip`): tag →
  percorso puntato nella CR. Applicato una volta a `baseFile`.
- **`listField` + `itemMappings`** (risorsa a lista, come `fwrules`):
  `listField` è il percorso verso il campo-lista della CR; per ciascun
  elemento, `itemMappings` risolve i percorsi **relativi a
  quell'elemento**, e il tag riceve automaticamente il suffisso `_N`
  (stessa convenzione di `smartNestedProcess`/`IndexedResourceFile` —
  vedi il test `TestEndToEndConfigDrivenFwrulesMatchesEngineSuffixes`,
  che verifica esattamente questo allineamento).

Il percorso speciale **`$cr.name`** restituisce il nome della risorsa
Kubernetes stessa (`cfg.Name`), utilizzabile sia in `mappings` che in
`itemMappings`. Il prefisso `spec.` è facoltativo. Un campo assente o non
risolvibile produce una stringa vuota invece di far fallire l'intera
regola (utile per campi opzionali del CRD).

### ⚠️ Verifica i nomi dei campi

Ho scritto i percorsi in `rules.example.json` assumendo che il tuo CRD
usi tag JSON standard in camelCase (`portName`, `vipExternalIP`, ...) —
non ho il codice del tuo `api/v1` con le struct reali, quindi **controlla
che corrispondano esattamente ai tag `json:"..."` che hai messo sui
campi** di `PortConfigurationParams`/`VIPConfigurationParams`/
`FirewallRule`. Se differiscono, basta correggere le stringhe nel file
regole — non serve toccare `rule_configdriven.go`.

### Wiring in `main.go`

`LoadOperatorRulesFromFile` va chiamato **una volta all'avvio**, non da
un `init()` (così i test possono controllare quando/con quale file
caricarle):

```go
func main() {
    // ... setup del manager controller-runtime ...

    rulesPath := os.Getenv("FGT_OPERATOR_RULES_FILE")
    if rulesPath == "" {
        rulesPath = "/etc/fgt-operator/rules.json" // default se monti sempre lì
    }
    if err := controller.LoadOperatorRulesFromFile(rulesPath); err != nil {
        setupLog.Error(err, "impossibile caricare le regole dell'operator")
        os.Exit(1)
    }

    // ... mgr.Start(ctx) ...
}
```

Vedi `configmap.example.yaml` per il manifest completo (ConfigMap +
`volumeMount` nel Deployment del controller).

**Nota sul reload**: `LoadOperatorRulesFromFile` legge il file una sola
volta all'avvio. Se aggiorni la ConfigMap mentre il controller è già in
esecuzione, i pod la rileggono solo dopo un restart (comportamento
standard per un volume ConfigMap non-watched) — se ti serve hot-reload
senza restart, dimmelo e aggiungo un watcher sul file.

### Verificato con test

- `TestLoadOperatorRulesFromBytesRegistersAllRules` — il file di esempio
  registra correttamente tutte e 3 le regole;
- `TestConfigDrivenSingleResourceRule` — regola a risorsa singola
  (`interface`), tutti i campi sostituiti correttamente, incluso
  `$cr.name`;
- `TestConfigDrivenListRule` — regola a lista (`fwrules`), un file per
  elemento con i tag correttamente suffissati;
- `TestEndToEndConfigDrivenFwrulesMatchesEngineSuffixes` — il test che
  conta davvero: genera i file con il **motore reale**
  (`smartNestedProcess`), poi applica la regola config-driven sugli
  stessi file, e verifica che i suffissi `_N` che il motore lascia sui
  tag non risolti coincidano esattamente con quelli che la regola
  cerca — zero tag `<FGT_...>` residui nell'output finale.

---


## 🆕 Aggiornamento: `smart_nested_process.go` — motore corretto

Rispetto alla versione che mi avevi incollato, ho **riscritto** (stessa
firma pubblica `smartNestedProcess(templateFilename string, instance
*k8sdinovaonev1.FortigateConfig) error`, quindi zero modifiche al resto
del controller) risolvendo 3 limiti reali, verificati empiricamente sul
tuo codice esatto prima di correggerli:

1. **Cicli paralleli/fratelli ora funzionano.** Prima, `smartNestedProcess`
   guardava solo la prima riga del file: se non era `<FGT_FOR_STR>`, tutto
   il resto (anche cicli altrove nel file) restava testo letterale non
   espanso. Ora la funzione individua **tutti** i blocchi di primo livello
   tramite matching bilanciato (`findTopLevelLoops`), e:
   - se il **primo** blocco trovato apre esattamente la riga 1 → split in
     file separati (comportamento originale, invariato) — con eventuali
     **cicli fratelli successivi ripetuti in ogni file generato**;
   - altrimenti → **tutti** i cicli di primo livello trovati (uno o più)
     vengono ripetuti in linea nello stesso file.
2. **Liste di stringhe semplici** (es. `members: ["a", "b"]`) prima
   venivano scartate silenziosamente (il codice gestiva solo elementi
   `map[string]interface{}`). Ora un elemento scalare viene avvolto come
   `{"VALUE": elemento}`, referenziabile con `<TAG_VALUE>` (stessa regola
   del prefisso cumulativo di tutti gli altri campi, es.
   `<ADDRESSGROUPS_MEMBERS_VALUE>`).
3. **Sostituzione di scalari root fuori da ogni ciclo** (novità, non
   c'era prima): un placeholder come `<HOSTNAME>` usato fuori da
   qualunque `<FGT_FOR_STR>` ora viene risolto contro lo spec root, invece
   di restare intoccato.

**Il comportamento del fallback su tag non risolti resta identico
*dentro* un ciclo** (`<TAG_N>` con suffisso indice) — quindi
`rule_fwrules.go` resta valido così com'è.

**Fuori da un ciclo, invece, un tag non risolto ora resta invariato,
SENZA alcun suffisso.** La mia prima versione di questa correzione
applicava il suffisso `_0` anche qui, il che avrebbe rotto silenziosamente
`rule_interface.go`/`rule_vip.go` (che cercano `<FGT_PORT_NAME>` senza
suffisso) — l'ho trovato scrivendo apposta un test di regressione per
quello scenario prima di consegnare, e corretto con una funzione dedicata
(`processVariablesNoSuffix`) usata solo per la sostituzione fuori-ciclo.

Ho verificato tutto con test che usano il tuo codice/i tuoi template
**esatti**, non un mio motore separato:
- il tuo esempio originale (regola annidata) → invariato, stesso output;
- due cicli fratelli in un file che non inizia con `<FGT_FOR_STR>` → ora
  espansi correttamente (prima restavano letterali);
- liste di stringhe semplici → ora sostituite correttamente;
- scalari root fuori da un ciclo → ora sostituiti, e i tag NON risolti in
  quel contesto (quelli destinati a `rule_interface.go`/`rule_vip.go`)
  restano invariati come prima;
- split-di-testa + ciclo fratello dopo di esso → il fratello viene
  ripetuto in ogni file generato;
- **tutti e 9 i file `.tf.tmpl`** che ti avevo consegnato in precedenza,
  fatti girare per intero contro questo motore corretto → zero tag
  irrisolti, parentesi bilanciate in ognuno.

---


## Cosa cambia

Prima: aggiungere una nuova `OperatorRule` significava scrivere una nuova
`tfXxx(...)` **e** aggiungere un caso nello `switch` dentro
`selectOperatorRule`. Ora: si scrive **un solo file nuovo** che si
auto-registra — `selectOperatorRule` e tutto il resto del controller non
cambiano mai più.

```go
// rule_reboot.go — file NUOVO, nessuna modifica altrove
func init() {
    RegisterOperatorRule("reboot_device", OperatorRuleFunc(applyRebootRule))
}

func applyRebootRule(cfg k8sdinovaonev1.FortigateConfig, baseFile string) error {
    mods := []modification{
        {oldValue: "<FGT_RESOURCE_NAME>", newValue: cfg.Name},
    }
    return applyMods(baseFile, mods)
}
```

Questo è tutto: nessuna modifica a `controller.go`, `operator_rules.go` o
qualsiasi altro file esistente. L'ho verificato scrivendo un test che
registra una regola completamente nuova (`reboot_device_demo`) da un file
di test separato, esattamente come farebbe chi estende l'operator — vedi
sotto.

## File

| File | Contenuto |
|---|---|
| `operator_rules.go` | L'interfaccia `OperatorRuleHandler`, il tipo di comodo `OperatorRuleFunc`, il registry (`RegisterOperatorRule`/`lookupOperatorRule`/`KnownOperatorRules`) |
| `rule_helpers.go` | `IndexedResourceFile` (naming file per regole basate su liste) + `applyMods` (wrapper di `modifyFileValue` con errore uniforme) |
| `rule_configdriven.go` | **Sostituisce** `rule_interface.go`/`rule_vip.go`/`rule_fwrules.go`: un unico handler generico che legge il mapping tag→campo da un file JSON (vedi sezione ConfigMap in cima a questo README), più la validazione runtime (`validate`) |
| `TYPES_SNIPPET_TO_ADD.go.txt` | Snippet da **aggiungere** (non un file completo) dentro la tua `FortigateConfigSpec` esistente: i due campi generici `ExtraParams`/`ExtraItems` |
| `controller.go` | `tfCommon` (invariata) + `selectOperatorRule` semplificata: copia file, `smartNestedProcess`, poi **un'unica chiamata** `lookupOperatorRule` + `handler.Apply(...)`, poi backend comune ed esecuzione Terraform |

## Bug corretti nel refactor

1. **Errori ignorati nello switch**: `tfCreateInterface(fortiConfig, ...)` e
   `tfCreateVIP(fortiConfig, ...)` venivano chiamate nello switch senza
   controllare il valore di ritorno — se fallivano, l'esecuzione
   proseguiva silenziosamente. Ora `handler.Apply(...)` restituisce solo
   `error` ed è sempre controllato.
2. **`tfPolicyRule` aveva tutti i `newValue: ""`** — non venivano mai
   valorizzati con i dati della regola. In `rule_fwrules.go` ora uso
   `rule.RuleName` come minimo indispensabile; ho lasciato un commento
   esplicito dove aggiungere gli altri campi (`SrcIntf`/`DstIntf`/...) se e
   quando diventano CR-driven invece che fissi nel template.
3. **Calcolo di `defFile` duplicato 3 volte, leggermente diverso ogni
   volta**: per le regole "singola risorsa" (`interface`, `create_vip`) ho
   verificato che quel calcolo, nel caso normale, ricostruiva
   semplicemente lo stesso `filename` già ricevuto come parametro (era
   ridondante) — quindi ora quelle regole modificano `baseFile`
   direttamente. Per `fwrules` (basata su lista) ho estratto la logica in
   `IndexedResourceFile`, usata una sola volta invece di essere
   reimplementata per ogni futura regola a lista.

## ⚠️ Cosa NON ho potuto verificare

Non ho il codice di `smartNestedProcess`, `modifyFileValue`,
`copyTerraformFiles`, `execTerraform`, `deleteTerraform`, `deleteS3Dir`, né
il vero package `api/v1` — li ho sostituiti con stub minimi (stessa firma,
comportamento finto) solo per poter compilare e testare *la logica del
dispatch* con `go build`/`go test` in questo ambiente. **Il codice che ti
consegno è quello vero**, non gli stub — ma va ricompilato nel tuo repo
reale prima di fidartene del tutto.

In particolare: `IndexedResourceFile` riproduce la convenzione di naming
che usava il tuo `tfPolicyRule` originale (`_<n+1>` come suffisso). **Se
`smartNestedProcess` usa una convenzione diversa per nominare i file che
genera da un blocco `<FGT_FOR_STR>` di primo livello** (es. uno slug
anziché un indice numerico), le due cose andrebbero disallineate: la
regola `fwrules` cercherebbe di modificare un file con un nome diverso da
quello che `smartNestedProcess` ha realmente creato. Se puoi incollarmi
anche `smartNestedProcess`, allineo `IndexedResourceFile` di conseguenza
(o, meglio ancora, faccio in modo che `smartNestedProcess` stesso esponga
una funzione di naming che tutte le regole a lista riusano, così la
convenzione vive in un unico posto invece che in due file diversi che
devono restare in sincrono a mano).

## Test

Ho scritto un file di test (`controller_test.go`, non incluso nella
consegna perché usa gli stub — dimmi se lo vuoi comunque) che verifica:

- le 3 regole esistenti funzionano tramite il registry esattamente come
  prima;
- una regola sconosciuta restituisce un errore con l'elenco di quelle
  disponibili;
- registrare due volte lo stesso nome fallisce subito (panic a startup,
  non un bug silenzioso a runtime);
- **una regola completamente nuova, registrata da un file esterno, viene
  trovata e invocata correttamente senza toccare nient'altro**;
- `fwrules` produce un file per elemento della lista con naming coerente.

Tutti i test passano (`go test ./...` → PASS).