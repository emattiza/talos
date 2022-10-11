// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package runtime

import (
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/resource/protobuf"
	"github.com/cosi-project/runtime/pkg/resource/typed"

	"github.com/talos-systems/talos/pkg/machinery/proto"
	"github.com/talos-systems/talos/pkg/machinery/resources/v1alpha1"
)

// NamespaceName contains configuration resources.
const NamespaceName resource.Namespace = v1alpha1.NamespaceName

// KernelParamSpecType is type of KernelParam resource.
const KernelParamSpecType = resource.Type("KernelParamSpecs.runtime.talos.dev")

// KernelParamDefaultSpecType is type of KernelParam resource for default kernel params.
const KernelParamDefaultSpecType = resource.Type("KernelParamDefaultSpecs.runtime.talos.dev")

// KernelParam interface.
type KernelParam interface {
	TypedSpec() *KernelParamSpecSpec
}

// KernelParamSpec resource holds sysctl flags to define.
type KernelParamSpec = typed.Resource[KernelParamSpecSpec, KernelParamSpecRD]

// KernelParamSpecSpec describes status of the defined sysctls.
//
//gotagsrewrite:gen
type KernelParamSpecSpec struct {
	Value        string `yaml:"value" protobuf:"1"`
	IgnoreErrors bool   `yaml:"ignoreErrors" protobuf:"2"`
}

// NewKernelParamSpec initializes a KernelParamSpec resource.
func NewKernelParamSpec(namespace resource.Namespace, id resource.ID) *KernelParamSpec {
	return typed.NewResource[KernelParamSpecSpec, KernelParamSpecRD](
		resource.NewMetadata(namespace, KernelParamSpecType, id, resource.VersionUndefined),
		KernelParamSpecSpec{},
	)
}

// KernelParamSpecRD is the ResourceDefinition for KernelParamSpec.
type KernelParamSpecRD struct{}

// ResourceDefinition implements meta.ResourceDefinitionProvider interface.
func (KernelParamSpecRD) ResourceDefinition(resource.Metadata, KernelParamSpecSpec) meta.ResourceDefinitionSpec {
	return meta.ResourceDefinitionSpec{
		Type:             KernelParamSpecType,
		Aliases:          []resource.Type{},
		DefaultNamespace: NamespaceName,
		PrintColumns:     []meta.PrintColumn{},
	}
}

// KernelParamDefaultSpec implements meta.ResourceDefinitionProvider interface.
type KernelParamDefaultSpec = typed.Resource[KernelParamDefaultSpecSpec, KernelParamDefaultSpecRD]

// KernelParamDefaultSpecSpec is same as KernelParamSpecSpec.
type KernelParamDefaultSpecSpec = KernelParamSpecSpec

// NewKernelParamDefaultSpec initializes a KernelParamDefaultSpec resource.
func NewKernelParamDefaultSpec(namespace resource.Namespace, id resource.ID) *KernelParamDefaultSpec {
	return typed.NewResource[KernelParamDefaultSpecSpec, KernelParamDefaultSpecRD](
		resource.NewMetadata(namespace, KernelParamDefaultSpecType, id, resource.VersionUndefined),
		KernelParamSpecSpec{},
	)
}

// KernelParamDefaultSpecRD is the ResourceDefinition for KernelParamDefaultSpec.
type KernelParamDefaultSpecRD struct{}

// ResourceDefinition implements meta.ResourceDefinitionProvider interface.
func (KernelParamDefaultSpecRD) ResourceDefinition(resource.Metadata, KernelParamDefaultSpecSpec) meta.ResourceDefinitionSpec {
	return meta.ResourceDefinitionSpec{
		Type:             KernelParamDefaultSpecType,
		Aliases:          []resource.Type{},
		DefaultNamespace: NamespaceName,
		PrintColumns:     []meta.PrintColumn{},
	}
}

func init() {
	proto.RegisterDefaultTypes()

	err := protobuf.RegisterDynamic[KernelParamSpecSpec](KernelParamSpecType, &KernelParamSpec{})
	if err != nil {
		panic(err)
	}

	err = protobuf.RegisterDynamic[KernelParamDefaultSpecSpec](KernelParamDefaultSpecType, &KernelParamDefaultSpec{})
	if err != nil {
		panic(err)
	}
}
