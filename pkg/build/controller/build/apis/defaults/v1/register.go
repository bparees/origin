package v1

import (
	"github.com/openshift/origin/pkg/build/controller/build/apis/defaults"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// SchemeGroupVersion is group version used to register these objects
var SchemeGroupVersion = schema.GroupVersion{Group: "", Version: "v1"}

var (
	localSchemeBuilder = runtime.NewSchemeBuilder(
		addKnownTypes,
		defaults.InstallLegacy,
	)
	InstallLegacy = localSchemeBuilder.AddToScheme
)

// Adds the list of known types to api.Scheme.
func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&BuildDefaultsConfig{},
	)
	return nil
}

func (obj *BuildDefaultsConfig) GetObjectKind() schema.ObjectKind { return &obj.TypeMeta }
