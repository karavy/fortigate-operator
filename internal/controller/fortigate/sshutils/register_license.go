package sshutils

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	k8sdinovaonev1 "github.com/karavy/k8s-operator-fortigate/api/v1"
	secretsutils "github.com/karavy/k8s-operator-fortigate/internal/controller/utils/secretsutils"
)

func registerLicense(ctx context.Context, r client.Client, req ctrl.Request, stdin io.WriteCloser, stdout io.Reader, firewallInstance *k8sdinovaonev1.FortigateFirewall) (string, error) {

	accountID, accountPassword, err := getFortiPortalCredentials(ctx, r, req.Namespace, firewallInstance)
	if err != nil {
		fmt.Printf("errore durante il recupero delle credenziali FortiPortal: %v", err)
		return "", fmt.Errorf("errore durante il recupero delle credenziali FortiPortal: %v", err)
	}

	if accountID == "" || accountPassword == "" {
		fmt.Printf("credenziali FortiPortal non trovate o incomplete")
		return "", fmt.Errorf("credenziali FortiPortal non trovate o incomplete")
	}

	go func() {
		defer stdin.Close()

		// Un piccolo trucco: premiamo invio una volta per pulire il prompt
		fmt.Fprint(stdin, "\r")
		time.Sleep(200 * time.Millisecond)

		// Inviamo il comando di generazione del token
		command := fmt.Sprintf("execute vm-license-options account-id %s\r", accountID)
		fmt.Fprint(stdin, command)
		time.Sleep(500 * time.Millisecond) // Tempo per far apparire il prompt della password

		command = fmt.Sprintf("execute vm-license-options account-password %s\r", accountPassword)
		fmt.Fprint(stdin, command)
		time.Sleep(500 * time.Millisecond)

		command = "execute vm-license\r"
		fmt.Fprint(stdin, command)
		time.Sleep(500 * time.Millisecond)

		command = "y\r"
		fmt.Fprint(stdin, command)
		time.Sleep(2500 * time.Millisecond)

		// Chiudiamo la sessione digitando exit
		fmt.Fprint(stdin, "exit\r")
	}()

	var stdoutBuffer bytes.Buffer
	_, err = io.Copy(&stdoutBuffer, stdout)
	if err != nil && err != io.EOF {
		fmt.Printf("Errore durante la lettura dell'output: %v", err)
	}

	output := stdoutBuffer.String()

	fmt.Printf("Output completo del firewall per debug:\n%s", output)

	return output, nil
}

func getFortiPortalCredentials(ctx context.Context, r client.Client, namespace string, firewallInstance *k8sdinovaonev1.FortigateFirewall) (username string, password string, err error) {
	portalCredentialSecretName := firewallInstance.Spec.LicenseUserSecretName

	if portalCredentialSecretName == "" {
		fmt.Println("Nome del secret FortiPortal non specificato. Verifica la configurazione.")
		return "", "", nil
	}

	portalKey, err := secretsutils.GetSecretValues(ctx, r, namespace, portalCredentialSecretName, []string{"accountID", "accountPassword"})
	if err != nil {
		return "", "", err
	}

	return portalKey["accountID"], portalKey["accountPassword"], nil
}
