/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// NodeBridgeSpec defines the desired state of NodeBridge
type NodeBridgeSpec struct {
	// UplinkInterface è l'interfaccia fisica del nodo da agganciare al bridge (es. eth1).
	// Se non impostata, il bridge viene comunque creato ma senza alcuna interfaccia agganciata.
	// +optional
	UplinkInterface string `json:"uplinkInterface,omitempty"`

	// VlanID, se impostato, configura il bridge per operare in modalità VLAN-aware
	// e riferisce il tag VLAN atteso dalle NetworkAttachmentDefinition collegate.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4094
	VlanID *int32 `json:"vlanId,omitempty"`

	// VlanAware abilita vlan_filtering sul bridge Linux. Default: true.
	// +optional
	// +kubebuilder:default=true
	VlanAware *bool `json:"vlanAware,omitempty"`

	// NodeSelector limita i nodi su cui il bridge deve essere creato.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// BridgeName è il nome del bridge Linux da creare. Deve essere univoco per nodo.
	// +optional
	BridgeName 	string `json:"bridgeName,omitempty"`

	// GatewayIP, se impostato, è l'indirizzo (in notazione CIDR, es. 192.168.10.254/24)
	// da assegnare sul nodo alla sub-interface 802.1q della VLAN indicata da VlanID.
	// Richiede che VlanID sia impostato.
	// +optional
	GatewayIP string `json:"gatewayIP,omitempty"`
}

// NodeBridgeNodeStatus è lo stato riportato da un singolo agent per il proprio nodo.
type NodeBridgeNodeStatus struct {
	// Ready indica se il bridge è stato creato correttamente su questo nodo.
	Ready bool `json:"ready"`

	// Message riporta l'ultimo errore, se presente.
	// +optional
	Message string `json:"message,omitempty"`

	// LastReconcileTime è il timestamp dell'ultima riconciliazione riuscita o fallita.
	// +optional
	LastReconcileTime metav1.Time `json:"lastReconcileTime,omitempty"`

	// +optional
	DisplayName string `json:"displayName,omitempty"`
}

// NodeBridgeStatus aggrega lo stato riportato da tutti gli agent che hanno reconciliato la risorsa.
type NodeBridgeStatus struct {
	// NodeStatuses mappa nome-nodo -> stato riportato dall'agent su quel nodo.
	// +optional
	NodeStatuses map[string]NodeBridgeNodeStatus `json:"nodeStatuses,omitempty"`

	// ObservedGeneration è la generation della Spec l'ultima volta osservata dal controller principale.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// NodeBridge is the Schema for the nodebridges API
type NodeBridge struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of NodeBridge
	// +required
	Spec NodeBridgeSpec `json:"spec"`

	// status defines the observed state of NodeBridge
	// +optional
	Status NodeBridgeStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// NodeBridgeList contains a list of NodeBridge
type NodeBridgeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []NodeBridge `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NodeBridge{}, &NodeBridgeList{})
}
