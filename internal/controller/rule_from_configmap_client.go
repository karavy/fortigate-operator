package controller

// Questo file contiene SOLO la parte che parla con l'API di Kubernetes -
// la logica di merge vera e propria (testata per intero) vive in
// rule_from_configmaps.go, senza dipendere da questi import.
//
// NON COMPILATO/TESTATO IN QUESTO SANDBOX: sigs.k8s.io/controller-runtime
// e k8s.io/api risiedono su domini bloccati qui, stesso limite di rete
// già segnalato più volte in questa conversazione. Fai `go build ./...`
// nel tuo repo reale prima di fidartene al 100% - la parte delicata
// (i conflitti di merge) è comunque già verificata a parte, indipendente
// da questo file.

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch

// LoadOperatorRulesFromConfigMaps elenca, tramite l'API di Kubernetes,
// tutte le ConfigMap nel namespace indicato che combaciano con
// labelSelector, e le unisce in un unico ruleset attivo (stessa forma e
// stesse regole di merge di LoadOperatorRulesFromDirectory - vedi
// mergeConfigMapRuleSources in rule_from_configmaps.go). Va chiamata
// almeno una volta all'avvio; se vuoi che ConfigMap nuove/modificate
// vengano recepite SENZA riavviare il pod, richiamala periodicamente (un
// semplice time.Ticker) o collegala a un watch - non l'ho aggiunto qui
// per non decidere al posto tuo quanto "vivo" vuoi che sia il reload.
//
// Il parametro è client.Reader (solo Get+List), non client.Client
// (che ha anche Create/Update/Delete/Patch/...) - qui serve solo List,
// quindi accettiamo l'interfaccia più piccola possibile. Questo ha anche
// un vantaggio pratico: sia mgr.GetClient() (client.Client, che include
// client.Reader) SIA mgr.GetAPIReader() (client.Reader puro, utile per
// letture di avvio PRIMA che la cache del manager sia partita, altrimenti
// rischi un errore "cache not started") vanno bene qui, senza dover
// scegliere in anticipo quale dei due passerai.
//
// Esempio di chiamata da main.go, dopo ctrl.NewManager:
//
//	sel, _ := labels.Parse("app.kubernetes.io/component=fgt-operator-rule")
//	if err := controller.LoadOperatorRulesFromConfigMaps(context.Background(), mgr.GetAPIReader(), "default", sel); err != nil {
//	    setupLog.Error(err, "Failed to load operator rules from ConfigMaps")
//	    os.Exit(1)
//	}
func LoadOperatorRulesFromConfigMaps(ctx context.Context, c client.Reader, namespace string, labelSelector labels.Selector) error {
	var cmList corev1.ConfigMapList
	if err := c.List(ctx, &cmList,
		client.InNamespace(namespace),
		client.MatchingLabelsSelector{Selector: labelSelector},
	); err != nil {
		return fmt.Errorf("elenco ConfigMap con selettore %q nel namespace %q fallito: %w", labelSelector, namespace, err)
	}

	sources := make([]configMapRuleSource, 0, len(cmList.Items))
	for _, cm := range cmList.Items {
		sources = append(sources, configMapRuleSource{
			Namespace: cm.Namespace,
			Name:      cm.Name,
			Data:      cm.Data,
		})
	}

	merged, sourceDesc, err := mergeConfigMapRuleSources(sources)
	if err != nil {
		return err
	}
	return applyRulesFile(merged, fmt.Sprintf("%s (selettore %q, namespace %q)", sourceDesc, labelSelector, namespace))
}