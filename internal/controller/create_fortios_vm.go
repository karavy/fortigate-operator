package controller

import (
	"context"
	"fmt"

	k8sdinovaonev1 "github.com/karavy/k8s-operator-fortigate/api/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/rest"
	kubevirtv1 "kubevirt.io/api/core/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (r *FortigateFirewallReconciler) createFortigateFirewall(ctx context.Context, firewallName string, fortigateVersion string, namespace string, spec k8sdinovaonev1.FortigateFirewallSpec) error {
	existingVM := &kubevirtv1.VirtualMachine{}
	err := r.Get(ctx, client.ObjectKey{Name: firewallName, Namespace: namespace}, existingVM)

	if err == nil {
		fmt.Println("La VMI esiste già, aggiorno")
		interfaces, networks, err := createManifestNIC(spec.Ports)
		if err != nil {
			fmt.Printf("Errore creazione interfacce: %v\n", err)
			return err
		}

		originalVM := existingVM.DeepCopy() // Crea una copia dell'oggetto originale
		existingVM.Spec.Template.Spec.Networks = networks
		existingVM.Spec.Template.Spec.Domain.Devices.Interfaces = interfaces

		if !equality.Semantic.DeepEqual(originalVM.Spec, existingVM.Spec) { 
			if err := r.Patch(ctx, existingVM, client.MergeFrom(originalVM)); err != nil {
				fmt.Printf("Errore durante l'aggiornamento della VM: %v\n", err)
				return err
			}

			fmt.Println("VMI aggiornata con successo, riavvio della VM")

			if err := r.restartVM(ctx, firewallName, namespace); err != nil {
				fmt.Printf("Errore durante il riavvio della VM: %v\n", err)
				return err
			}

			fmt.Println("VMI aggiornata con successo")
		} else {
			fmt.Println("Nessuna modifica rilevata nella VMI, nessun aggiornamento necessario")
		}
		
		return nil
	}

	// la vm non esiste, la creo
	vmi := createManifest(firewallName, fortigateVersion, namespace, spec)

	if err := r.Create(ctx, vmi); err != nil {
		fmt.Printf("Errore durante la creazione della VMI: %v\n", err)
		return err
	}

	return nil
}

func createFortigateService(ctx context.Context, r *FortigateFirewallReconciler, instance *k8sdinovaonev1.FortigateFirewall) error {
	svc := createFortigateFirewallService(instance.Name, instance.Spec.FortigateVersion, instance.Namespace)
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		// Imposta il selettore usando il nome dell'istanza corrente
		svc.Spec.Selector = map[string]string{
			"vmi.kubevirt.io/id": fmt.Sprintf("%s", instance.Name), // Assicurati che questa label corrisponda a quella usata nella VMI
		}

		// Configura il resto delle Spec del servizio
		svc.Spec.Type = v1.ServiceTypeLoadBalancer
		svc.Spec.SessionAffinity = v1.ServiceAffinityNone
		svc.Spec.Ports = []v1.ServicePort{
			{
				Name:       "ssh",
				Protocol:   v1.ProtocolTCP,
				Port:       22,
				TargetPort: intstr.FromInt(22),
			},
			{
				Name:       "gui",
				Protocol:   v1.ProtocolTCP,
				Port:       443,
				TargetPort: intstr.FromInt(443),
			},
		}

		// Imposta l'Owner Reference (Legame di parentela)
		return controllerutil.SetControllerReference(instance, svc, r.Scheme)
	})

	if err != nil {
		return err
	}

	if op != controllerutil.OperationResultNone {
		fmt.Printf("Service fortios-ssh %s\n", op)
	}

	return nil
}

func createFortigateFirewallService(firewallName string, fortigateVersion string, namespace string) *v1.Service {

	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-ssh-gui", firewallName, fortigateVersion),
			Namespace: namespace,
		},
		Spec: v1.ServiceSpec{
			Type:            v1.ServiceTypeLoadBalancer,
			SessionAffinity: v1.ServiceAffinityNone,
			Selector: map[string]string{
				"vmi.kubevirt.io/id": firewallName,
			},
			Ports: []v1.ServicePort{
				{
					Name:       "ssh",
					Protocol:   v1.ProtocolTCP,
					Port:       22,
					TargetPort: intstr.FromInt(22),
				},
				{
					Name:       "gui",
					Protocol:   v1.ProtocolTCP,
					Port:       443,
					TargetPort: intstr.FromInt(443),
				},
			},
		},
	}
}

func DeleteFirewallSvc(ctx context.Context, r *FortigateFirewallReconciler, firewallName string, fortigateVersion string, namespace string) error {
	svcName := fmt.Sprintf("%s-%s-ssh-gui", firewallName, fortigateVersion)
	svc := &v1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: svcName, Namespace: namespace}, svc)
	if err != nil {
		fmt.Printf("Errore durante la lettura del Service '%s': %v\n", svcName, err)
		return err
	}

	err = r.Delete(ctx, svc)
	if err != nil {
		fmt.Printf("Errore durante l'eliminazione del Service '%s': %v\n", svcName, err)
		return err
	}

	fmt.Printf("Service '%s' eliminato con successo!\n", svcName)
	return nil
}

func DeleteFortigateFirewall(ctx context.Context, r *FortigateFirewallReconciler, firewallName string, fortigateVersion string, namespace string) error {
	vm := &unstructured.Unstructured{}
	vm.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "kubevirt.io",
		Version: "v1",
		Kind:    "VirtualMachine", // Attenzione: con il client standard si usa il "Kind" (singolare e maiuscolo), non la Resource
	})

	// 2. Assegniamo il Nome e il Namespace all'oggetto che vogliamo cancellare
	vm.SetName(fmt.Sprintf("%s", firewallName))
	vm.SetNamespace(namespace)

	// 3. Usiamo il client nativo di Kubebuilder per cancellarlo
	err := r.Delete(ctx, vm)

	if err != nil {
		// Nota: in Kubebuilder è meglio usare il logger strutturato anziché fmt.Printf
		fmt.Println("Errore durante l'eliminazione della VirtualMachine")
		return err
	}

	vmi := &unstructured.Unstructured{}
	vmi.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "kubevirt.io",
		Version: "v1",
		Kind:    "VirtualMachineInstance", // Attenzione: con il client standard si usa il "Kind" (singolare e maiuscolo), non la Resource
	})

	// 2. Assegniamo il Nome e il Namespace all'oggetto che vogliamo cancellare
	vmi.SetName(fmt.Sprintf("%s", firewallName))
	vmi.SetNamespace(namespace)

	err = r.Delete(ctx, vmi)

	if err != nil {
		// Nota: in Kubebuilder è meglio usare il logger strutturato anziché fmt.Printf
		fmt.Println("Errore durante l'eliminazione della VirtualMachineInstance")
		return err
	}

	return err
}

func createManifestNIC(ports []k8sdinovaonev1.FortigateInterface) ([]kubevirtv1.Interface, []kubevirtv1.Network, error) {
	var interfaces []kubevirtv1.Interface
	var networks []kubevirtv1.Network

	// crea l'interfaccia di management predefinita (default) con il metodo di binding Masquerade
	interfaces = append(interfaces, kubevirtv1.Interface{
		Name: "default",
		InterfaceBindingMethod: kubevirtv1.InterfaceBindingMethod{
			Masquerade: &kubevirtv1.InterfaceMasquerade{},
		},
	})

	networks = append(networks, kubevirtv1.Network{
		Name: "default",
		NetworkSource: kubevirtv1.NetworkSource{
			Pod: &kubevirtv1.PodNetwork{},
		},
	})

	for _, port := range ports {
		interfaces = append(interfaces, kubevirtv1.Interface{
			Name:  port.Name,
			Model: "virtio",
			InterfaceBindingMethod: kubevirtv1.InterfaceBindingMethod{
				Bridge: &kubevirtv1.InterfaceBridge{},
			},
		})

		networks = append(networks, kubevirtv1.Network{
			Name: port.Name,
			NetworkSource: kubevirtv1.NetworkSource{
				Multus: &kubevirtv1.MultusNetwork{
					NetworkName: port.NetworkName,
				},
			},
		})
	}

	return interfaces, networks, nil
}

func createManifest(firewallName string, fortigateVersion string, namespace string, spec k8sdinovaonev1.FortigateFirewallSpec) *kubevirtv1.VirtualMachine {
	// Definiamo un puntatore a zero per il termination grace period
	var gracePeriod int64 = 0
	runStrategy := kubevirtv1.RunStrategyAlways

	interfaces, networks, err := createManifestNIC(spec.Ports)
	if err != nil {
		fmt.Printf("Errore creazione interfacce: %v", err)
		return nil
	}

	// Creazione dell'oggetto VirtualMachine
	vm := &kubevirtv1.VirtualMachine{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "kubevirt.io/v1",
			Kind:       "VirtualMachine",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      firewallName,
			Namespace: namespace,
		},
		Spec: kubevirtv1.VirtualMachineSpec{
			// Forza Kubevirt ad avviare la VM e a mantenerla attiva (e a ricreare la VMI se il nodo crasha)
			RunStrategy: &runStrategy,
			Template: &kubevirtv1.VirtualMachineInstanceTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"kubevirt.io/vm": firewallName, // Label standard consigliata da Kubevirt
					},
				},
				// Da qui in poi è esattamente la specifica della tua vecchia VMI
				Spec: kubevirtv1.VirtualMachineInstanceSpec{
					TerminationGracePeriodSeconds: &gracePeriod,
					Domain: kubevirtv1.DomainSpec{
						Firmware: &kubevirtv1.Firmware{
							UUID: types.UID(spec.VMUUID),
						},
						Machine: &kubevirtv1.Machine{
							Type: "q35",
						},
						Resources: kubevirtv1.ResourceRequirements{
							Requests: map[v1.ResourceName]resource.Quantity{
								v1.ResourceMemory: resource.MustParse("2G"),
								v1.ResourceCPU:    resource.MustParse("1"),
							},
						},
						Devices: kubevirtv1.Devices{
							Disks: []kubevirtv1.Disk{
								{
									Name: "forti-os-disk",
									DiskDevice: kubevirtv1.DiskDevice{
										Disk: &kubevirtv1.DiskTarget{
											Bus: "virtio",
										},
									},
								},
								{
									Name: "cloudinitdisk",
									DiskDevice: kubevirtv1.DiskDevice{
										CDRom: &kubevirtv1.CDRomTarget{
											Bus: "sata",
										},
									},
								},
							},
							Interfaces: interfaces,
						},
					},
					Networks: networks,
					Volumes: []kubevirtv1.Volume{
						{
							Name: "forti-os-disk",
							VolumeSource: kubevirtv1.VolumeSource{
								PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
									PersistentVolumeClaimVolumeSource: v1.PersistentVolumeClaimVolumeSource{
										ClaimName: fmt.Sprintf("%s-%s-fgt", firewallName, fortigateVersion),
									},
								},
							},
						},
						{
							Name: "cloudinitdisk",
							VolumeSource: kubevirtv1.VolumeSource{
								PersistentVolumeClaim: &kubevirtv1.PersistentVolumeClaimVolumeSource{
									PersistentVolumeClaimVolumeSource: v1.PersistentVolumeClaimVolumeSource{
										ClaimName: fmt.Sprintf("%s-%s-cloud-init", firewallName, fortigateVersion),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Stampa di controllo: serializziamo l'oggetto in JSON per vedere se è corretto

	return (vm)
}
func (r *FortigateFirewallReconciler) restartVM(ctx context.Context, firewallName string, namespace string) error {
	existingVM := &kubevirtv1.VirtualMachine{}
	err := r.Get(ctx, client.ObjectKey{Name: firewallName, Namespace: namespace}, existingVM)
	if err != nil {
		fmt.Printf("Errore durante la lettura della VM: %v\n", err)
		return err
	}

	cfg, err := config.GetConfig()
	if err != nil {
		fmt.Printf("Errore durante l'ottenimento della configurazione del cluster: %v\n", err)
		return err
	}

	restConfig := rest.CopyConfig(cfg)
	restConfig.APIPath = "/apis"
	restConfig.GroupVersion = &schema.GroupVersion{Group: "subresources.kubevirt.io", Version: "v1"}
	restConfig.NegotiatedSerializer = scheme.Codecs.WithoutConversion()

	restClient, err := rest.RESTClientFor(restConfig)
	if err != nil {
		return err
	}

	// Kind/apiVersion del body seguono il gruppo "kubevirt.io", non "subresources.kubevirt.io"
	restartBody := []byte(`{"apiVersion":"kubevirt.io/v1","kind":"RestartOptions"}`)

	apiPath := fmt.Sprintf("/apis/subresources.kubevirt.io/v1/namespaces/%s/virtualmachines/%s/restart",
		existingVM.Namespace,
		existingVM.Name,
	)

	err = restClient.Put().
		AbsPath(apiPath).
		Body(restartBody).
		SetHeader("Content-Type", "application/json").
		Do(ctx).
		Error()

	if err != nil {
		fmt.Printf("Errore chiamata subresource restart: %v\n", err)
		return err
	}

	fmt.Println("Riavvio avviato con successo!")
	return nil
}