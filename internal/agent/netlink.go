package agent

import (
	"fmt"

	"github.com/vishvananda/netlink"
)

// bridgeName è fisso qui per semplicità dello scaffold: nella versione reale
// probabilmente diventa un campo esplicito di NodeBridgeSpec (es. spec.bridgeName)
// invece di essere derivato dal nome della CR — valuta in base a come vuoi
// che l'utente/controller Firewall lo referenzi nei NAD.
func ensureBridge(uplinkIface string, vlanID *int32, vlanAware bool, bridgeName string) error {

	link, err := netlink.LinkByName(bridgeName)
	if err != nil {
		if _, ok := err.(netlink.LinkNotFoundError); !ok {
			return fmt.Errorf("verifica esistenza bridge %s: %w", bridgeName, err)
		}
		link = nil
	}

	if link == nil {
		br := &netlink.Bridge{
			LinkAttrs:    netlink.LinkAttrs{Name: bridgeName},
			VlanFiltering: &vlanAware,
		}
		if err := netlink.LinkAdd(br); err != nil {
			return fmt.Errorf("creazione bridge %s: %w", bridgeName, err)
		}
		link = br
	}

	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("attivazione bridge %s: %w", bridgeName, err)
	}

	if uplinkIface == "" {
		return nil
	}

	uplink, err := netlink.LinkByName(uplinkIface)
	if err != nil {
		return fmt.Errorf("interfaccia uplink %s non trovata: %w", uplinkIface, err)
	}

	if uplink.Attrs().MasterIndex != link.Attrs().Index {
		if err := netlink.LinkSetMaster(uplink, link); err != nil {
			return fmt.Errorf("attach uplink %s al bridge %s: %w", uplinkIface, bridgeName, err)
		}
	}

	if err := netlink.LinkSetUp(uplink); err != nil {
		return fmt.Errorf("attivazione uplink %s: %w", uplinkIface, err)
	}

	// nota: la gestione del VLAN ID specifico (netlink.BridgeVlanAdd) va aggiunta
	// qui se il tagging deve essere applicato lato bridge invece che lato NAD/CNI.
	_ = vlanID

	return nil
}
