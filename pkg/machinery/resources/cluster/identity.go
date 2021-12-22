// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package cluster

import (
	"fmt"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
)

// IdentityType is type of Identity resource.
const IdentityType = resource.Type("Identities.cluster.talos.dev")

// LocalIdentity is the resource ID for the local node identity.
const LocalIdentity = resource.ID("local")

// Identity resource holds node identity (as a member of the cluster).
type Identity struct {
	md   resource.Metadata
	spec IdentitySpec
}

// IdentitySpec describes status of rendered secrets.
//
// Note: IdentitySpec is persisted on disk in the STATE partition,
// so YAML serialization should be kept backwards compatible.
type IdentitySpec struct {
	// NodeID is a random value which is persisted across reboots,
	// but it gets reset on wipe.
	NodeID string `yaml:"nodeId"`
}

// NewIdentity initializes a Identity resource.
func NewIdentity(namespace resource.Namespace, id resource.ID) *Identity {
	r := &Identity{
		md:   resource.NewMetadata(namespace, IdentityType, id, resource.VersionUndefined),
		spec: IdentitySpec{},
	}

	r.md.BumpVersion()

	return r
}

// Metadata implements resource.Resource.
func (r *Identity) Metadata() *resource.Metadata {
	return &r.md
}

// Spec implements resource.Resource.
func (r *Identity) Spec() interface{} {
	return r.spec
}

func (r *Identity) String() string {
	return fmt.Sprintf("cluster.Identity(%q)", r.md.ID())
}

// DeepCopy implements resource.Resource.
func (r *Identity) DeepCopy() resource.Resource {
	return &Identity{
		md:   r.md,
		spec: r.spec,
	}
}

// ResourceDefinition implements meta.ResourceDefinitionProvider interface.
func (r *Identity) ResourceDefinition() meta.ResourceDefinitionSpec {
	return meta.ResourceDefinitionSpec{
		Type:             IdentityType,
		Aliases:          []resource.Type{},
		DefaultNamespace: NamespaceName,
		PrintColumns: []meta.PrintColumn{
			{
				Name:     "ID",
				JSONPath: `{.nodeId}`,
			},
		},
	}
}

// TypedSpec allows to access the Spec with the proper type.
func (r *Identity) TypedSpec() *IdentitySpec {
	return &r.spec
}
