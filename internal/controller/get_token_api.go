package controller

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
)

func getAPIToken(ctx context.Context, r *FortigateFirewallReconciler, req ctrl.Request, stdin io.WriteCloser, stdout io.Reader) (string, error) {

	creds, err := getSecretValues(ctx, r.Client, req.Namespace, "forti-credentials", []string{"adminPassword", "apiUserName"})
	if err != nil {
		return "", fmt.Errorf("Errore durante la lettura dei secret: %v", err)
	}

	apiUserName := creds["apiUserName"]
	adminPassword := creds["adminPassword"]

	// 4. Invia i comandi in sequenza nello stesso flusso, come se li digitassi tu.
	// Inviamo prima il comando di generazione, poi una riga vuota, poi la password.
	go func() {
		defer stdin.Close()

		// Un piccolo trucco: premiamo invio una volta per pulire il prompt
		fmt.Fprint(stdin, "\r")
		time.Sleep(200 * time.Millisecond)

		// Inviamo il comando di generazione del token
		command := fmt.Sprintf("execute api-user generate-key %s\r", apiUserName)
		fmt.Fprint(stdin, command)
		time.Sleep(500 * time.Millisecond) // Tempo per far apparire il prompt della password

		// Inviamo la password dell'amministratore
		fmt.Fprint(stdin, adminPassword+"\r")
		time.Sleep(500 * time.Millisecond)

		// Chiudiamo la sessione digitando exit
		fmt.Fprint(stdin, "exit\r")
	}()

	// 5. Leggi l'output continuo fino alla fine della sessione (EOF della shell)
	var stdoutBuffer bytes.Buffer
	_, err = io.Copy(&stdoutBuffer, stdout)
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("Errore durante la lettura dell'output: %v", err)
	}

	output := stdoutBuffer.String()

	// 6. Parsing del token rimasto identico
	re := regexp.MustCompile(`New API key:\s*([a-zA-Z0-9]+)`)
	matches := re.FindStringSubmatch(output)

	if len(matches) < 2 {
		return "", fmt.Errorf("Token non trovato. Output completo del firewall per debug:\n%s", output)
	}

	apiToken := strings.TrimSpace(matches[1])
	
	return apiToken, nil
}
