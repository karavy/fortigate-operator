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

// FortigateBackupSpec defines the desired state of FortigateBackup
type FortigateBackupSpec struct {
	// Frequency: ogni quanto eseguire il backup.

	// +optional
	// +kubebuilder:default="Daily"
	// +kubebuilder:validation:Enum=Hourly;Daily;Weekly;Monthly
	Frequency string `json:"frequency"`
 
	// At: a che ora del giorno eseguirlo, formato "HH:MM" (24 ore).
	// Ignorato se Frequency è "Hourly".
	// +optional
	// +kubebuilder:default="02:00"
	// +kubebuilder:validation:Pattern=`^([01][0-9]|2[0-3]):[0-5][0-9]$`
	At string `json:"at,omitempty"`
 
	// DayOfWeek: usato solo se Frequency è "Weekly".
	// +optional
	// +kubebuilder:validation:Enum=Monday;Tuesday;Wednesday;Thursday;Friday;Saturday;Sunday
	DayOfWeek string `json:"dayOfWeek,omitempty"`
 
	// DayOfMonth: usato solo se Frequency è "Monthly". Limitato a 1-31 per
	// evitare l'ambiguità dei mesi più corti (niente "31 di febbraio").
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=31
	DayOfMonth int32 `json:"dayOfMonth,omitempty"`
 
	// Suspend mette in pausa la creazione di nuovi Backup senza cancellare
	// la BackupSchedule stessa - stesso pattern di CronJob.spec.suspend.
	// +optional
	Suspend bool `json:"suspend,omitempty"`
 
	// Template descrive cosa deve contenere ogni Backup generato da questa
	// schedule - stessi campi di BackupSpec, riusati qui.
	// +required
	Template BackupSpec `json:"template"`
 
	// MaxBackups: quanti Backup completati con successo tenere (i più
	// vecchi oltre questo numero vengono cancellati dal controller insieme
	// al loro artefatto S3, grazie al finalizer su Backup).
	// +optional
	// +kubebuilder:default=7
	// +kubebuilder:validation:Minimum=1
	MaxBackups int32 `json:"maxBackups,omitempty"`
}
 

type BackupSpec struct {
	// Source referenzia la risorsa da cui fare il backup PER NOME - non
	// duplicare qui bucket/credenziali/altri campi già presenti su quella
	// risorsa, il controller li risolve lui.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="source è immutabile dopo la creazione"
	Source BackupSourceRef `json:"source"`
 
	// Destination: dove va salvato l'artefatto. Bucket/region/endpoint
	// possono essere ereditati da OperatorConfig (come già fai per
	// s3Region) se omessi qui - lascia solo il necessario per override.
	// +optional
	Destination BackupDestination `json:"destination,omitempty"`
 
	// RetentionDays: dopo quanti giorni dal completamento questo Backup (e
	// il suo artefatto S3, se il finalizer lo gestisce) può essere
	// cancellato automaticamente - alternativa/complemento a MaxBackups
	// sulla Schedule per chi crea Backup singoli senza una Schedule.
	// +optional
	// +kubebuilder:validation:Minimum=1
	RetentionDays int32 `json:"retentionDays,omitempty"`
}

type BackupSourceRef struct {
	// +required
	// +kubebuilder:validation:Enum=FortigateFirewall;FortigateConfig
	Kind string `json:"kind"`
	// +required
	Name string `json:"name"`
}
 
type BackupDestination struct {
	// +optional
	BucketName string `json:"bucketName,omitempty"`
	// +optional
	Prefix string `json:"prefix,omitempty"`
}

// FortigateBackupStatus defines the observed state of FortigateBackup.
type FortigateBackupStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the FortigateBackup resource.
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

	// +optional
	LastBackupTime *metav1.Time `json:"lastBackupTime,omitempty"`
 
	// ObservedGeneration permette al controller/a chi legge di sapere se lo
	// status riflette l'ultima versione dello spec o è ancora "vecchio".
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// FortigateBackup is the Schema for the fortigatebackups API
type FortigateBackup struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of FortigateBackup
	// +required
	Spec FortigateBackupSpec `json:"spec"`

	// status defines the observed state of FortigateBackup
	// +optional
	Status FortigateBackupStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// FortigateBackupList contains a list of FortigateBackup
type FortigateBackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []FortigateBackup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FortigateBackup{}, &FortigateBackupList{})
}
