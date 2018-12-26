package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SQLCredentialSpec defines the desired state of SQLCredential
type SQLCredentialSpec struct {
	Driver       string
	Host         string
	Revision     int64
	Database     string
	User         string
	Role         string
	MasterSecret string
}

// SQLCredentialStatus defines the observed state of SQLCredential
type SQLCredentialStatus struct {
	ActiveRevisions []string
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SQLCredential is the Schema for the sqlcredentials API
// +k8s:openapi-gen=true
type SQLCredential struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SQLCredentialSpec   `json:"spec,omitempty"`
	Status SQLCredentialStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SQLCredentialList contains a list of SQLCredential
type SQLCredentialList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SQLCredential `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SQLCredential{}, &SQLCredentialList{})
}
