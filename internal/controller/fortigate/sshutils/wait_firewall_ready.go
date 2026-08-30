package sshutils

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"time"

	k8sdinovaonev1 "github.com/karavy/k8s-operator-fortigate/api/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetFirewallReady(ctx context.Context, r client.Client, req ctrl.Request, instance *k8sdinovaonev1.Firewall) bool {
	return waitFirewallReady(ctx, r, req, false, instance)
}

func GetFirewallReadyWithLicense(ctx context.Context, r client.Client, req ctrl.Request, instance *k8sdinovaonev1.Firewall) bool {
	return waitFirewallReady(ctx, r, req, true, instance)
}

func waitFirewallReady(ctx context.Context, r client.Client, req ctrl.Request, withLicense bool, instance *k8sdinovaonev1.Firewall) bool {
	counter := 0

	for {
		typeOperation := FIREWALLREADY
		if withLicense {
			typeOperation = FIREWALLREADYWITHLICENSE
		}

		_, err := DoSSHOperations(ctx, r, req, typeOperation, instance)
		if err == nil {
			// Se l'errore è nil, la funzione ha avuto successo!
			break // Esce dal ciclo for
		}

		// Se siamo qui, la funzione ha ritornato un errore
		fmt.Printf("Verifica fallita: %v. Riprovo tra 2 secondi...\n", err)
		//TODO: Dopo un po' deve uscire, altrimenti blocca completamente il processo!!!
		counter++
		if counter >= 20 {
			// Dopo 20 tentativi, proviamo a registrare di nuovo la licenza (se siamo in modalità withLicense)
			fmt.Println("Tentativi di verifica superati. Provo a registrare di nuovo la licenza...")
			return false
		}
		// Pausa per evitare di sovraccaricare la CPU con un ciclo infinito velocissimo
		time.Sleep(2 * time.Second)
	}

	return true
}

func executeFirewallReadyCommand(stdin io.WriteCloser, stdout io.Reader, validLicense bool) (string, error) {
	go func() {
		defer stdin.Close()

		// Un piccolo trucco: premiamo invio una volta per pulire il prompt
		fmt.Fprint(stdin, "\r")
		time.Sleep(200 * time.Millisecond)

		// Inviamo il comando di generazione del token
		command := "get system status\r"
		fmt.Fprint(stdin, command)
		time.Sleep(500 * time.Millisecond) // Tempo per far apparire il prompt della password

		// Chiudiamo la sessione digitando exit
		fmt.Fprint(stdin, "exit\r")
	}()

	var stdoutBuffer bytes.Buffer
	_, err := io.Copy(&stdoutBuffer, stdout)
	if err != nil && err != io.EOF {
		fmt.Printf("Errore durante la lettura dell'output: %v", err)
	}

	output := stdoutBuffer.String()

	if !validLicense {
		re := regexp.MustCompile(`License Status:\s*(?P<license>[a-zA-Z]+)`)
		matches := re.FindStringSubmatch(output)

		if len(matches) < 2 {
			fmt.Printf("Firewall non pronto. Output completo del firewall per debug:\n%s", output)
			return output, fmt.Errorf("firewall non pronto")
		}
	} else {
		if parseFortigateStatus(output) == false {
			fmt.Printf("Firewall non pronto. Output completo del firewall per debug:\n%s", output)
			return output, fmt.Errorf("firewall non pronto")
		}
	}

	//fmt.Printf("Firewall pronto. Output completo del firewall per debug:\n%s", output)

	return output, nil
}

func parseFortigateStatus(output string) bool {

	re := regexp.MustCompile(`License Status:\s*(?P<license>[a-zA-Z]+)`)
	matches := re.FindStringSubmatch(output)

	if len(matches) == 0 {
		return false
	}

	// Mappiamo i named groups nei campi della struct
	for i, name := range re.SubexpNames() {
		if i != 0 && name != "" {
			switch name {
			case "license":
				return matches[i] == "Valid"
			}
		}
	}

	return false
}
