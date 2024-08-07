// +groupName=getport.io
package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	GroupVersion = schema.GroupVersion{Group: "getport.io", Version: "v1alpha1"}
)

type InvocationMethod struct {
	Type                 string `json:"type"`
	Org                  string `json:"org"`
	Repo                 string `json:"repo"`
	Workflow             string `json:"workflow"`
	ReportWorkflowStatus bool   `json:"reportWorkflowStatus"`
}

type Automation struct {
	InvocationMethod InvocationMethod `json:"invocationMethod"`
}

type Discover struct {
	Filter                   string     `json:"filter"`
	PropertiesDenyList       []string   `json:"propertiesDenyList,omitempty"`
	PropertiesAllowList      []string   `json:"propertiesAllowList,omitempty"`
	PropertiesAllowJQPattern string     `json:"propertiesAllowJQPattern,omitempty"`
	PropertiesDenyJQPattern  string     `json:"propertiesDenyJQPattern,omitempty"`
	Automation               Automation `json:"automation,omitempty"`
}

type CrdSyncerSpec struct {
	Discover []Discover `json:"discover"`
}

type Status struct {
	Conditions []string `json:"conditions"`
}

type CrdSyncer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CrdSyncerSpec `json:"spec,omitempty"`
	Status Status        `json:"status,omitempty"`
}

type CrdSyncerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []CrdSyncer `json:"items"`
}
