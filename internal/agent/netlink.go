package agent

import (
	"fmt"

	"github.com/vishvananda/netlink"
)

// bridgeName è fisso qui per semplicità dello scaffold: nella versione reale
// probabilmente diventa un campo esplicito di NodeBridgeSpec (es. spec.bridgeName)
// invece di essere derivato dal nome della CR — valuta in base a come vuoi
// che l'utente/controller Firewall lo referenzi nei NAD.
func ensureBridge(uplinkIface string, vlanID *int32, vlanAware bool, bridgeName string, gatewayIP string) error {

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

	if br, ok := link.(*netlink.Bridge); ok {
		if br.VlanFiltering == nil || *br.VlanFiltering != vlanAware {
			if err := netlink.BridgeSetVlanFiltering(link, vlanAware); err != nil {
				return fmt.Errorf("impostazione vlan_filtering=%v su bridge %s: %w", vlanAware, bridgeName, err)
			}
		}
	}

	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("attivazione bridge %s: %w", bridgeName, err)
	}

	if uplinkIface != "" {
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
	}

	if vlanID == nil {
		return fmt.Errorf("VlanID non specificato per il bridge %s: necessario per creare la sub-interface VLAN", bridgeName)
	}

	if err := ensureVlanSubinterface(link, *vlanID, gatewayIP); err != nil {
		return err
	}

	return nil
}

// ensureVlanSubinterface applica l'equivalente di:
//
//	bridge vlan add dev <bridge> vid <vlanID> self
//	ip link add link <bridge> name vlan<vlanID> type vlan id <vlanID>
//	ip link set vlan<vlanID> up
//	ip addr add <gatewayIP> dev vlan<vlanID>
//
// Il nome della sub-interface non viene derivato da bridgeName perché i nomi
// interfaccia Linux sono limitati a 15 caratteri (IFNAMSIZ): concatenare
// "<bridgeName>.<vlanID>" può superare il limite sui bridge con nomi già
// lunghi (es. quelli auto-generati da Docker). "vlan<vlanID>" resta breve
// indipendentemente dal nome del bridge; il parametro Link/ParentIndex è
// sufficiente ad associarla al bridge corretto.
func ensureVlanSubinterface(bridge netlink.Link, vlanID int32, gatewayIP string) error {
	if err := netlink.BridgeVlanAdd(bridge, uint16(vlanID), false, false, true, false); err != nil {
		return fmt.Errorf("bridge vlan add vid %d su %s: %w", vlanID, bridge.Attrs().Name, err)
	}

	vlanIfaceName := fmt.Sprintf("vlan%d", vlanID)

	vlanLink, err := netlink.LinkByName(vlanIfaceName)
	if err != nil {
		if _, ok := err.(netlink.LinkNotFoundError); !ok {
			return fmt.Errorf("verifica esistenza sub-interface %s: %w", vlanIfaceName, err)
		}
		vlanLink = &netlink.Vlan{
			LinkAttrs: netlink.LinkAttrs{
				Name:        vlanIfaceName,
				ParentIndex: bridge.Attrs().Index,
			},
			VlanId: int(vlanID),
		}
		if err := netlink.LinkAdd(vlanLink); err != nil {
			return fmt.Errorf("creazione sub-interface %s: %w", vlanIfaceName, err)
		}
	}

	if err := netlink.LinkSetUp(vlanLink); err != nil {
		return fmt.Errorf("attivazione sub-interface %s: %w", vlanIfaceName, err)
	}

	if gatewayIP == "" {
		return fmt.Errorf("gatewayIP non presente per %s", vlanIfaceName)
	}

	addr, err := netlink.ParseAddr(gatewayIP)
	if err != nil {
		return fmt.Errorf("gatewayIP %q non valido per %s: %w", gatewayIP, vlanIfaceName, err)
	}

	existing, err := netlink.AddrList(vlanLink, netlink.FAMILY_ALL)
	if err != nil {
		return fmt.Errorf("lettura indirizzi di %s: %w", vlanIfaceName, err)
	}
	for _, a := range existing {
		if a.Equal(*addr) {
			return nil
		}
	}

	if err := netlink.AddrAdd(vlanLink, addr); err != nil {
		return fmt.Errorf("assegnazione indirizzo %s a %s: %w", gatewayIP, vlanIfaceName, err)
	}

	return nil
}
