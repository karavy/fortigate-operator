package fileutils

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// CreaDirectory prende in input il percorso della cartella e la cb
func CreateDirectory(path string) error {
	err := os.MkdirAll(path, 0755)
	if err != nil {
		return fmt.Errorf("impossibile creare la directory %s: %w", path, err)
	}
	return nil
}

func CopyFiles(pattern, destinationDir string, filePrefix string) error {
	// 1. Trova tutti i file che corrispondono al wildcard (es. assegna i file .tf)
	files, err := filepath.Glob(pattern)
	if err != nil {
		fmt.Printf("errore nella sintassi del wildcard: %v", err)
		return err
	}

	// Se non trova corrispondenze, esce senza fare danni
	if len(files) == 0 {
		fmt.Printf("Nessun file trovato per il pattern: %s\n", pattern)
		return nil
	}

	// 2. Assicurati che la cartella di destinazione esista
	err = os.MkdirAll(destinationDir, 0755)
	if err != nil {
		fmt.Printf("impossibile creare la cartella di destinazione: %v", err)
		return err
	}

	// 3. Cicla sui file trovati e copiali uno per uno
	for _, fileSorgente := range files {
		// Prendi solo il nome del file (es. da "./progetto/main.tf" prende "main.tf")
		nomeFile := filepath.Base(fileSorgente)
		fileDestinazione := filepath.Join(destinationDir, filePrefix+"_"+nomeFile)

		// Esegui la copia del singolo file
		err := CopyFile(fileSorgente, fileDestinazione)
		if err != nil {
			fmt.Printf("errore durante la copia di %s: %v", fileSorgente, err)
			return err
		}
		fmt.Printf("Copiato: %s ➡️ %s\n", fileSorgente, fileDestinazione)
	}

	return nil
}

func CopyFile(src, dst string) error {
	// 1. Apri il file sorgente in modalità lettura
	sorgente, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("impossibile aprire il file sorgente %s: %w", src, err)
	}
	// Garantisce la chiusura del file sorgente alla fine della funzione
	defer sorgente.Close()

	// 2. Assicurati che la cartella di destinazione esista (stile mkdir -p)
	cartellaDestinazione := filepath.Dir(dst)
	err = CreateDirectory(cartellaDestinazione)
	if err != nil {
		return fmt.Errorf("impossibile creare la cartella di destinazione %s: %w", cartellaDestinazione, err)
	}

	// 3. Crea il file di destinazione (se esiste già, lo sovrascrive/svuota)
	destinazione, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("impossibile creare il file di destinazione %s: %w", dst, err)
	}
	// Garantisce la chiusura del file di destinazione alla fine della funzione
	defer destinazione.Close()

	// 4. Copia il contenuto (streaming efficiente dei byte)
	_, err = io.Copy(destinazione, sorgente)
	if err != nil {
		return fmt.Errorf("errore durante la copia dei dati: %w", err)
	}

	// 5. Sincronizza i dati sul disco (opzionale, ma garantisce che i dati siano scritti fisicamente)
	err = destinazione.Sync()
	if err != nil {
		return fmt.Errorf("errore durante il flush dei dati sul disco: %w", err)
	}

	return nil
}

func ListAllFilesInDir(dirName string) ([]string, error) {
	var files []string

	// Puliamo il percorso iniziale per evitare problemi con i vari "./" o "../"
	rootClean := filepath.Clean(dirName)

	err := filepath.WalkDir(rootClean, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			fmt.Printf("Errore durante la lettura del file: %v\n", err)
			return err
		}

		// 1. Calcoliamo il percorso relativo rispetto alla radice
		// Se root è "mia-cartella" e path è "mia-cartella/sub1/file.txt",
		// rel diventerà "sub1/file.txt"
		rel, err := filepath.Rel(rootClean, path)
		if err != nil {
			fmt.Printf("Errore durante il calcolo del percorso relativo: %v\n", err)
			return err
		}

		// 2. Se rel è ".", significa che siamo esattamente sulla cartella radice (livello 0)
		livello := 0
		if rel != "." {
			// Contiamo quanti separatori di sistema (es. "/" su Linux o "\" su Windows) ci sono nel percorso relativo.
			// Ogni separatore indica che siamo scesi di un livello.
			livello = strings.Count(rel, string(os.PathSeparator)) + 1
		}

		// Ora hai la variabile 'livello' da usare come vuoi!
		if d.IsDir() {
			fmt.Printf("[DIR]  Livello %d: %s\n", livello, path)
		} else {
			files = append(files, path)
			fmt.Printf("[FILE] Livello %d: %s\n", livello, path)
		}

		return nil
	})

	if err != nil {
		fmt.Printf("Errore: %v\n", err)
		return nil, err
	}

	return files, err
}

// modifyFileValue legge un file, sostituisce la vecchia stringa con la nuova e riscalda il file su disco.
// mods è oldValue -> newValue.
func ModifyFileValue(filePath string, mods map[string]string) error {
	// 1. Leggi l'intero contenuto del file originale
	content, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Printf("Errore durante la lettura del file: %v\n", err)
		return fmt.Errorf("impossibile leggere il file: %w", err)
	}

	// 2. Esegui la sostituzione dei valori in memoria
	// strings.ReplaceAll sostituisce TUTTE le occorrenze di oldVal con newVal
	modifiedContent := string(content)
	for oldValue, newValue := range mods {
		modifiedContent = strings.ReplaceAll(modifiedContent, oldValue, newValue)
	}

	// 3. Ottieni i permessi del file originale per mantenerli inalterati
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		fmt.Printf("Errore durante la lettura dei metadati del file: %v\n", err)
		return fmt.Errorf("impossibile leggere i metadati del file: %w", err)
	}
	perm := fileInfo.Mode().Perm()

	// 4. Sovrascrivi il file con il nuovo contenuto mantenendo i permessi originali
	err = os.WriteFile(filePath, []byte(modifiedContent), perm)
	if err != nil {
		fmt.Printf("Errore durante la scrittura del file: %v\n", err)
		return fmt.Errorf("impossibile scrivere le modifiche nel file: %w", err)
	}

	fmt.Printf("File modificato con successo: %s\n", filePath)

	return nil
}

func ReadFileContent(filename string) (string, error) {
	// 1. Leggi il file. Ritorna un array di byte ([]byte) e un errore
	contentBytes, err := os.ReadFile(filename)
	if err != nil {
		// Gestisci l'errore se il file non esiste o non è leggibile
		fmt.Printf("Errore durante la lettura del file: %v\n", err)
		return "", err
	}

	// 2. Converti i byte in stringa
	contentString := string(contentBytes)

	return contentString, nil
}