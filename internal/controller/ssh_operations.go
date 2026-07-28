package controller

import (
	"context"
	"fmt"
	"os"
	"time"

	k8sdinovaonev1 "github.com/karavy/k8s-operator-fortigate/api/v1"
	"golang.org/x/crypto/ssh"
	ctrl "sigs.k8s.io/controller-runtime"
)

type SSHOperations int

// 2. Crea le costanti usando iota (parte da 0 e aumenta di 1 a ogni riga)
const (
	GETTOKEN SSHOperations = iota
	REGISTERLICENSE
	FIREWALLREADY
	FIREWALLREADYWITHLICENSE
	UPGRADEFIRMWARE_SSH
	UPGRADEFIRMWARE_API
)

func DoSSHOperations(ctx context.Context, r *FortigateFirewallReconciler, req ctrl.Request, operation SSHOperations, instance *k8sdinovaonev1.FortigateFirewall) (any, error) {
	// 1. Configura i parametri di connessione
	fortiIP := fmt.Sprintf("%s-%s-ssh-gui.%s.svc.cluster.local:22", instance.Name, instance.Spec.FortigateVersion, instance.Namespace)
	sshUser := "admin"
	//firewallName := instance.Name

	// Sostituisci con il percorso reale della TUA chiave privata (id_rsa)
	privateKeyPath := "/ssh/id_rsa"

	// 2. Carica la chiave privata SSH per l'autenticazione senza password
	key, err := os.ReadFile(privateKeyPath)
	if err != nil {
		fmt.Printf("Impossibile leggere la chiave privata: %v", err)
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		fmt.Printf("Impossibile parsare la chiave privata: %v", err)
		return nil, err
	}

	config := &ssh.ClientConfig{
		User:            sshUser,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	client, err := ssh.Dial("tcp", fortiIP, config)
	if err != nil {
		fmt.Printf("Connessione fallita: %v", err)
		return nil, err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		fmt.Printf("Impossibile creare sessione: %v", err)
		return nil, err
	}
	defer session.Close()

	// --- LA CORREZIONE È QUI: RICHIEDERE UN TERMINALE VIRTUALE (PTY) ---
	// 1. Richiedi il PTY come prima
	modes := ssh.TerminalModes{
		ssh.ECHO:          0,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm", 80, 40, modes); err != nil {
		fmt.Printf("Impossibile richiedere il PTY: %v", err)
		return nil, err
	}

	// 2. Collega i canali di Input e Output in modo sincrono
	// Usiamo i pipe nativi che creano un flusso continuo senza interruzioni
	stdin, err := session.StdinPipe()
	if err != nil {
		fmt.Printf("Impossibile creare StdinPipe: %v", err)
		return nil, err
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		fmt.Printf("Impossibile creare StdoutPipe: %v", err)
		return nil, err
	}

	// 3. Avvia una vera SHELL interattiva, invece di un singolo comando isolato
	if err := session.Shell(); err != nil {
		fmt.Printf("Impossibile avviare la Shell: %v", err)
		return nil, err
	}

	switch operation {
	case GETTOKEN:
		return getAPIToken(ctx, r, req, stdin, stdout)
	case REGISTERLICENSE:
		return registerLicense(ctx, r, req, stdin, stdout, instance)
	case FIREWALLREADY:
		return executeFirewallReadyCommand(stdin, stdout, false)
	case FIREWALLREADYWITHLICENSE:
		return executeFirewallReadyCommand(stdin, stdout, true)
	case UPGRADEFIRMWARE_SSH:
		return upgradeFirmwareSSH(stdin, stdout, "http://firmware-server:80/FGT_7.2.0_Firewall.out")
	default:
		fmt.Printf("Operazione SSH non riconosciuta: %v", operation)
		return nil, fmt.Errorf("operazione SSH non riconosciuta: %v", operation)
	}
}
