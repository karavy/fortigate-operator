package fortigate

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	k8sdinovaonev1 "github.com/karavy/k8s-operator-fortigate/api/v1"

	secretsutils "github.com/karavy/k8s-operator-fortigate/internal/controller/utils/secretsutils"
	s3utils "github.com/karavy/k8s-operator-fortigate/internal/controller/utils/s3utils"
	sshutils "github.com/karavy/k8s-operator-fortigate/internal/controller/fortigate/sshutils"
	pvcutils "github.com/karavy/k8s-operator-fortigate/internal/controller/utils/pvcutils"
)

func DoFirewallStartup(ctx context.Context, r client.Client, req ctrl.Request, instance *k8sdinovaonev1.FortigateFirewall, schema runtime.Scheme) (k8sdinovaonev1.FortigateFirewallStatus, error) {
	awsCredentialSecretName := instance.Spec.AWSCredentialSecretName

	if awsCredentialSecretName == "" {
		fmt.Println("Fortigate URL o nome del secret AWS non specificati. Verifica la configurazione.")
		err := fmt.Errorf("Fortigate URL o nome del secret AWS non specificati. Verifica la configurazione.")
		return k8sdinovaonev1.FortigateFirewallStatus{}, err
	}

	awsKey, err := secretsutils.GetSecretValues(ctx, r, req.Namespace, awsCredentialSecretName, []string{"s3Url", "accessKeyID", "secretAccessKey"})
	if err != nil {
		return k8sdinovaonev1.FortigateFirewallStatus{}, err
	}

	statusInfo, err := doReconcileFirewall(ctx, r, req, instance, schema)
	if err != nil {
		fmt.Printf("Errore durante la riconciliazione del firewall: %v\n", err)
		return k8sdinovaonev1.FortigateFirewallStatus{}, err
	}

	if s3utils.SyncTerraformS3Bucket(awsKey["s3Url"], awsKey["accessKeyID"], awsKey["secretAccessKey"], instance.Spec.S3BucketName) != true {
		fmt.Println("Errore durante la sincronizzazione del bucket S3")
		return k8sdinovaonev1.FortigateFirewallStatus{}, fmt.Errorf("errore durante la sincronizzazione del bucket S3")
	}

	return statusInfo, nil
}

func doReconcileFirewall(ctx context.Context, r client.Client, req ctrl.Request, instance *k8sdinovaonev1.FortigateFirewall, schema runtime.Scheme) (k8sdinovaonev1.FortigateFirewallStatus, error) {
	statusInfo := k8sdinovaonev1.FortigateFirewallStatus{}

	if err := pvcutils.CreateNewFortigateFirewallPVC(instance.Name, instance.Spec.FortigateVersion, instance.Namespace, instance.Spec.PVCStorageClass, ctx, r, "fortigate"); err != nil {
		fmt.Printf("Errore durante la creazione del PVC: %v\n", err)
		return statusInfo, err
	}

	var err error
	if err = createFortigateFirewall(ctx, instance.Name, instance.Spec.FortigateVersion, instance.Namespace, instance.Spec, r); err != nil {
		fmt.Printf("Errore durante la creazione del firewall: %v\n", err)
		return statusInfo, err
	}

	if err := createFortigateService(ctx, r, instance, r.Scheme()); err != nil {
		fmt.Printf("Errore durante la creazione del Service: %v\n", err)
		return statusInfo, err
	}

	// aspetta che il firewall sia pronto prima di procedere con la registrazione della licenza
	if sshutils.GetFirewallReady(ctx, r, req, instance) == false {
		return statusInfo, fmt.Errorf("firewall non pronto")
	}

	switch instance.Spec.LicenseType {
	case "trial":
		if _, err := sshutils.DoSSHOperations(ctx, r, req, sshutils.REGISTERLICENSE, instance); err != nil {
			fmt.Printf("Errore durante la registrazione della licenza: %v\n", err)
			return statusInfo, err
		}

		if sshutils.GetFirewallReadyWithLicense(ctx, r, req, instance) == false {
			fmt.Printf("Firewall non pronto dopo la registrazione della licenza. Verifica manuale consigliata.\n")
			return statusInfo, fmt.Errorf("firewall non pronto dopo la registrazione della licenza")
		}
	case "none":
		// Non fare nulla, procedi senza registrare la licenza
		fmt.Println("Nessuna licenza richiesta o licenza già esistente")
	case "flex":
		//TODO
	case "file":
		//TODO
	default:
		fmt.Printf("Tipo di licenza non valido: %s\n", instance.Spec.LicenseType)
		return statusInfo, fmt.Errorf("tipo di licenza non valido: %s", instance.Spec.LicenseType)
	}

	if instance.Status.Token == "" {
		token, err := sshutils.DoSSHOperations(ctx, r, req, sshutils.GETTOKEN, instance)
		if err != nil {
			fmt.Println("Impossibile ottenere il token")
		} else {
			statusInfo.Token = token.(string)
			fmt.Println("Token ottenuto")
		}
	} else {
		statusInfo.Token = instance.Status.Token
	}

	return statusInfo, nil
}
