package types

import (
	"errors"
	"github.com/Peripli/service-manager/pkg/web"
)

const TenantType ObjectType = web.TenantURL

type VirtualType struct {
	Base
}

func (v VirtualType) Validate() error {
	if v.GetID() == "" {
		return errors.New("validate Settings:  ID is missing")
	}
	return nil
}

func (v VirtualType) Equals(object Object) bool {
	return object.GetID() == v.GetID()
}

type Tenant struct {
	VirtualType
	TenantIdentifier string
}

func (e *Tenant) GetType() ObjectType {
	return TenantType
}

func IsVirtualType(objectType ObjectType) bool {
	return objectType == TenantType
}