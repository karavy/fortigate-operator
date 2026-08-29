package terraform

import (
	"fmt"
	"path/filepath"
	"strings"

	fileutils "github.com/karavy/k8s-operator-fortigate/internal/controller/utils/fileutils"
)

// splitTerraformExt calcola (base, ext) garantendo che ext sia SEMPRE
// l'estensione Terraform reale (tipicamente ".tf"), anche se filename
// arriva ancora con il suffisso ".tmpl" del file sorgente del template
// (es. "myfw_firewall_policy.tf.tmpl"). Serve perché Tofu/Terraform
// ignorano silenziosamente qualunque file che non finisca in ".tf" (o
// ".tf.json") - un file generato come "..._1.tmpl" (o peggio,
// "...tf_1.tmpl", con ".tf" finito in mezzo al nome) semplicemente non
// viene applicato, senza errore.
//
// Usata sia da IndexedResourceFile qui sotto sia da splitIntoFiles in
// smart_nested_process.go - unico punto di verità per il naming dei file
// splittati, così le due funzioni non possono disallinearsi.
func splitTerraformExt(filename string) (base, ext string) {
	if before, ok := strings.CutSuffix(filename, ".tmpl"); ok  {
		filename = before
	}
	ext = filepath.Ext(filename)
	if ext == "" {
		// Nessuna estensione riconoscibile nemmeno dopo aver tolto
		// ".tmpl": garantiamo comunque ".tf", dato che questo motore
		// genera sempre file Terraform.
		return filename, ".tf"
	}
	base = strings.TrimSuffix(filename, ext)
	return base, ext
}

// IndexedResourceFile returns the Nth (0-based index in, 1-based suffix
// out) generated Terraform file path for OperatorRules whose CR contains a
// list (one CR -> many files), e.g. baseFile "wd/myfw_fwrules.tf" and
// index 0 -> "wd/myfw_fwrules_1.tf". Sempre con estensione ".tf" reale,
// anche se baseFile arriva come "wd/myfw_fwrules.tf.tmpl" - vedi
// splitTerraformExt.
//
// IMPORTANT: this must match whatever naming convention smartNestedProcess
// actually gives the files it splits out from a file-level <FGT_FOR_STR>
// loop (vedi splitIntoFiles in smart_nested_process.go, che usa la stessa
// splitTerraformExt) - ogni regola a lista passa da qui, quindi c'è un
// solo posto da correggere.
func IndexedResourceFile(baseFile string, index int) string {
	base, ext := splitTerraformExt(baseFile)
	return fmt.Sprintf("%s_%d%s", base, index+1, ext)
}

// applyMods runs modifyFileValue against path and wraps any error with
// which file/rule it belongs to, so handlers don't each need their own
// fmt.Printf("Errore durante la modifica dei file: %v\n", err) copy.
func applyMods(path string, mods map[string]string) error {
	if err := fileutils.ModifyFileValue(path, mods); err != nil {
		return fmt.Errorf("modifica di %s fallita: %w", path, err)
	}
	return nil
}