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
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// FortigateConfigSpec defines the desired state of FortigateConfig
type FortigateConfigSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	// foo is an example field of FortigateConfig. Edit fortigateconfig_types.go to remove/update
	// +required
	FortigateName string `json:"fortigateName,omitempty"`
	// +required
	TerraformTemplateS3Key string `json:"terraformTemplateS3Key,omitempty"`
	// +required
	OperatorRule string `json:"operatorRule,omitempty"`

	// ExtraParams contiene i parametri delle OperatorRule "a risorsa
	// singola" (es. create_vip, interface) in formato JSON libero - un
	// OGGETTO, con qualunque struttura tu voglia dentro. La ConfigMap
	// delle regole (vedi internal/controller/rule_configdriven.go) legge
	// i singoli campi tramite "mappings".
	//
	// IMPORTANTE: questo campo deve SEMPRE essere un oggetto JSON
	// (`{...}`), mai una lista (`[...]`) - lo schema generato da
	// controller-gen per runtime.RawExtension è sempre "type: object";
	// una CR con una lista qui sotto viene respinta dall'API server con
	// "must be of type object". Per dati a forma di LISTA usa ExtraItems
	// sotto, non questo campo.
	//
	// Non è validato oltre alla forma "oggetto" - la validazione dei
	// singoli campi (obbligatori, formato CIDR/IP/enum/regex...) avviene
	// a runtime nel controller, secondo la sezione "validate" della
	// regola corrispondente nella ConfigMap.
	//
	// Esempio (create_vip):
	//
	//	spec:
	//	  operatorRule: create_vip
	//	  extraParams:
	//	    vipName: vip_kubeApiServer
	//	    vipExternalIP: "178.239.21.17"
	//	    vipExternalInterface: port1
	//	    vipInternalRange: "172.19.249.254"
	//
	// +optional
	// +kubebuilder:pruning:PreserveUnknownFields
	ExtraParams *runtime.RawExtension `json:"extraParams,omitempty"`

	// ExtraItems contiene i dati delle OperatorRule "a lista" (es.
	// fwrules): una LISTA di oggetti JSON liberi, un file Terraform per
	// elemento (vedi smart_nested_process.go / IndexedResourceFile). Ogni
	// elemento può avere struttura annidata arbitraria (es.
	// internetServices dentro ogni regola).
	//
	// Campo separato da ExtraParams apposta: un *runtime.RawExtension
	// (singolare) accetta solo oggetti, mai array, a livello di schema
	// CRD - da qui la necessità di un campo tipizzato come LISTA a
	// livello Go per i dati a forma di lista.
	//
	// Esempio (fwrules):
	//
	//	spec:
	//	  operatorRule: fwrules
	//	  extraItems:
	//	    - ruleName: "Rule-Kubernetes-2"
	//	      internetServices:
	//	        - name: "Amazon-AWS"
	//	    - ruleName: "Rule-Kubernetes-3"
	//	      internetServices:
	//	        - name: "AdRoll-DNS"
	//	        - name: "AdRoll-FTP"
	//
	// +optional
	ExtraItems []runtime.RawExtension `json:"extraItems,omitempty"`
}

// FortigateConfigStatus defines the observed state of FortigateConfig.
type FortigateConfigStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the FortigateConfig resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	FortigateRuleUUID string `json:"fortigateRuleUUID,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// FortigateConfig is the Schema for the fortigateconfigs API
type FortigateConfig struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of FortigateConfig
	// +required
	Spec FortigateConfigSpec `json:"spec"`

	// status defines the observed state of FortigateConfig
	// +optional
	Status FortigateConfigStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// FortigateConfigList contains a list of FortigateConfig
type FortigateConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []FortigateConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FortigateConfig{}, &FortigateConfigList{})
}