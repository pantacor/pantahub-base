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

import "time"

// Timeable interface of something with TimeStamp
type Timeable interface {
	SetCreatedAt()
	SetUpdatedAt()
	SetDeletedAt()
	GetCreatedAt() *time.Time
	GetUpdatedAt() *time.Time
	GetDeletedAt() *time.Time
}

// Timestamp timestamp extension for all models
type Timestamp struct {
	TimeCreated  *time.Time `json:"time-created" bson:"time-created"`
	TimeModified *time.Time `json:"time-modified" bson:"time-modified"`
	DeletedAt    *time.Time `json:"deleted-at,omitempty" bson:"deleted-at,omitempty"`
}

// NewTimeStamp create new timestamp
func NewTimeStamp() Timestamp {
	timeNow := time.Now()
	return Timestamp{
		TimeCreated:  &timeNow,
		TimeModified: nil,
		DeletedAt:    nil,
	}
}

// SetUpdatedAt set update to a timestamp
func (t *Timestamp) SetUpdatedAt() {
	timeNow := time.Now()
	t.TimeModified = &timeNow
}

// SetDeletedAt set delete to a timestamp
func (t *Timestamp) SetDeletedAt() {
	if t.DeletedAt == nil {
		timeNow := time.Now()
		t.DeletedAt = &timeNow
	}
}

// SetCreatedAt to a timestamp
func (t *Timestamp) SetCreatedAt() {
	if t.TimeCreated == nil {
		timeNow := time.Now()
		t.TimeCreated = &timeNow
	}
}

// GetCreatedAt get method for created at
func (t *Timestamp) GetCreatedAt() *time.Time {
	return t.TimeCreated
}

// GetUpdatedAt get method for updated at
func (t *Timestamp) GetUpdatedAt() *time.Time {
	return t.TimeModified
}

// GetDeletedAt get method for deleted at
func (t *Timestamp) GetDeletedAt() *time.Time {
	return t.DeletedAt
}
