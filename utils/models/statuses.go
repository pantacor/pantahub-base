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

// StatusType type of status
type StatusType string

// Status status for a collection
type Status struct {
	State   StatusType `json:"state,omitempty" bson:"state"`
	Message string     `json:"message,omitempty" bson:"message"`
}

// Statusable make something Status able
type Statusable interface {
	SetDone()
	SetNew()
	SetScheduled()
	SetFailed()
	SetCanceled()
	SetDeleted()
	SetPaused()
	SetDeleting()
	SetActive()
	SetNotificationSend()
	SetNotificationConfirmed()
	SetNotificationScheduled()
	SetDeprecated()
	SetSuperseded()
	SetQueued()
	SetUnQueued()
	SetSkipped()
	GetStatus() StatusType
	GetMessage() string
	IsFinal() bool
	IsValid() bool
}

const (
	// StatusDone the thing is done
	StatusDone = StatusType("done")

	// StatusNew the thing is just created
	StatusNew = StatusType("new")

	// StatusScheduled the thing is scheduled (has a date when it will start)
	StatusScheduled = StatusType("scheduled")

	// StatusNotificationScheduled schedule to send notification
	StatusNotificationScheduled = StatusType("notification_scheduled")

	// StatusNotificationSend waiting for user confirmation
	StatusNotificationSend = StatusType("notification_send")

	// StatusNotificationConfirmed waiting for user confirmation
	StatusNotificationConfirmed = StatusType("notification_confirmed")

	// StatusActive means the thing is active (has been consume)
	StatusActive = StatusType("active")

	// StatusFailed the thing failed and has an error
	StatusFailed = StatusType("failed")

	// StatusCanceled the thing has been canceled for some reason
	StatusCanceled = StatusType("canceled")

	// StatusPaused the thing has been paused for some reason
	StatusPaused = StatusType("paused")

	// StatusSkipped the thing has been skipped for some reason
	StatusSkipped = StatusType("skipped")

	// StatusDeleted the thing has been deleted for some reason
	StatusDeleted = StatusType("deleted")

	// StatusDeleting something that as been marked to be deleted
	StatusDeleting = StatusType("deleting")

	// StatusDeprecated something that is deprecaded can not be consumed
	StatusDeprecated = StatusType("deprecated")

	// StatusSuperseded something that as been superseded by a newer version
	StatusSuperseded = StatusType("superseded")
	StatusRetried    = StatusType("retried")

	// StatusQueued process being queue
	StatusQueued = StatusType("queued")

	// StatusUnQueued process being unqueue
	StatusUnQueued = StatusType("unqueued")

	// StatusAccepted accepted
	StatusAccepted = StatusType("accept")
)

// ValidStatusTypes valid types of permissions
var ValidStatusTypes = map[StatusType]StatusType{
	StatusDone:                  StatusDone,
	StatusAccepted:              StatusAccepted,
	StatusNew:                   StatusNew,
	StatusScheduled:             StatusScheduled,
	StatusFailed:                StatusFailed,
	StatusCanceled:              StatusCanceled,
	StatusDeleted:               StatusDeleted,
	StatusDeleting:              StatusDeleting,
	StatusActive:                StatusActive,
	StatusNotificationSend:      StatusNotificationSend,
	StatusNotificationConfirmed: StatusNotificationConfirmed,
	StatusNotificationScheduled: StatusNotificationScheduled,
	StatusDeprecated:            StatusDeprecated,
	StatusSuperseded:            StatusSuperseded,
	StatusQueued:                StatusQueued,
	StatusUnQueued:              StatusUnQueued,
}

// NewStatus create new status
func NewStatus(status string) *Status {
	return &Status{
		State: StatusType(status),
	}
}

// CreateStatus create new status
func CreateStatus(status StatusType) *Status {
	return &Status{
		State: status,
	}
}

// SetActive set status as active
func (s *Status) SetActive() {
	s.State = StatusActive
}

// SetDone set the status as done
func (s *Status) SetDone() {
	s.State = StatusDone
}

// SetDone set the status as done
func (s *Status) SetAccepted() {
	s.State = StatusDone
}

// SetNew set the status as New
func (s *Status) SetNew() {
	s.State = StatusNew
}

// SetScheduled set the status as Scheduled
func (s *Status) SetScheduled() {
	s.State = StatusScheduled
}

// SetFailed set the status as Failed
func (s *Status) SetFailed() {
	s.State = StatusFailed
}

// SetCanceled set the status as Canceled
func (s *Status) SetCanceled() {
	s.State = StatusCanceled
}

// SetDeleted set the status as Canceled
func (s *Status) SetDeleted() {
	s.State = StatusDeleted
}

// SetPaused set the status as paused
func (s *Status) SetPaused() {
	s.State = StatusPaused
}

// SetDeleting set element as deleting
func (s *Status) SetDeleting() {
	s.State = StatusDeleting
}

func (s *Status) SetNotificationSend() {
	s.State = StatusNotificationSend
}

func (s *Status) SetNotificationConfirmed() {
	s.State = StatusNotificationConfirmed
}

// SetNotificationScheduled set element as deleting
func (s *Status) SetNotificationScheduled() {
	s.State = StatusNotificationScheduled
}

func (s *Status) SetSuperseded() {
	s.State = StatusSuperseded
}

func (s *Status) SetDeprecated() {
	s.State = StatusDeprecated
}

// SetQueued set element as deleting
func (s *Status) SetQueued() {
	s.State = StatusQueued
}

// SetUnQueued set element as deleting
func (s *Status) SetUnQueued() {
	s.State = StatusUnQueued
}

// SetSkipped set element as deleting
func (s *Status) SetSkipped() {
	s.State = StatusSkipped
}

// GetStatus Get the status
func (s *Status) GetStatus() StatusType {
	return s.State
}

// GetMessage Get the message
func (s *Status) GetMessage() string {
	return s.Message
}

// IsFinal Status is final
func (s *Status) IsFinal() bool {
	return s.State == StatusDone ||
		s.State == StatusDeleted ||
		s.State == StatusCanceled
}

// IsValid permission is valid
func (s *Status) IsValid() bool {
	_, ok := ValidStatusTypes[s.State]
	return ok
}
