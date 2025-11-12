// Copyright 2025  Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
//	Unless required by applicable law or agreed to in writing, software
//	distributed under the License is distributed on an "AS IS" BASIS,
//	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//	See the License for the specific language governing permissions and
//	limitations under the License.

package models

import (
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func IDGetPrn(id string, serviceName string) string {
	return "prn:::" + serviceName + ":/" + id
}

// Identificable interface of something with ID
type Identificable interface {
	GetID() string
	GetPrn() string
	SetPrn(service string) Identificable
}

// Identification identification fields for a model
type Identification struct {
	ID  string `json:"id" bson:"_id"`
	PRN string `json:"prn" bson:"prn"`
}

// GetID get the id of a Identification
func (identification *Identification) GetID() string {
	return identification.ID
}

// GetPrn get the id of a Identification
func (identification *Identification) GetPrn() string {
	return identification.PRN
}

// GetID get the id of a Identification
func (identification *Identification) SetPrn(service string) Identificable {
	identification.PRN = IDGetPrn(identification.ID, service)

	return identification
}

// Ownable interface of something with ID
type Ownable interface {
	GetOwnerID() string
	GetOwnerPrn() string
	SetOwnerPrn(service string) Ownable
}

// Ownership define owner of a resource
type Ownership struct {
	OwnerID   string `json:"owner_id,omitempty" bson:"owner_id"`
	OwnerPrn  string `json:"owner,omitempty" bson:"owner"`
	OwnerName string `json:"owner_name" bson:"-"`
}

// GetOwnerID get the id of a owner
func (owner *Ownership) GetOwnerID() string {
	return owner.OwnerID
}

// GetOwnerPrn get the prn of a owner
func (owner *Ownership) GetOwnerPrn() string {
	return owner.OwnerPrn
}

// SetOwnerPrn set the prn of a owner
func (owner *Ownership) SetOwnerPrn(service string) Ownable {
	owner.OwnerPrn = IDGetPrn(owner.OwnerID, "accounts")
	return owner
}

// NewIdentification Create new indentification
func NewIdentification(service string) Identification {
	identification := Identification{
		ID: primitive.NewObjectID().Hex(),
	}
	identification.PRN = IDGetPrn(identification.ID, service)

	return identification
}

func NewOwnership(ownerID string) Ownership {
	return Ownership{
		OwnerID:  ownerID,
		OwnerPrn: IDGetPrn(ownerID, "accounts"),
	}
}
