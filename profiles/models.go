// Copyright 2020  Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.
//

package profiles

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Profile : Public information for one account
type Profile struct {
	ID    primitive.ObjectID `json:"-" bson:"_id"`
	Prn   string             `json:"-" bson:"prn"`
	Nick  string             `json:"nick" bson:"-"`
	Email string             `json:"email" bson:"-"`

	*UpdateableProfile `json:",inline" bson:",inline"`

	Public  bool `json:"-" bson:"public"`
	Garbage bool `json:"-" bson:"garbage"`

	TimeCreated  time.Time `json:"time-created,omit" bson:"time-created"`
	TimeModified time.Time `json:"time-modified,omit" bson:"time-modified"`
}

// UpdateableProfile updateable part of a Profile
type UpdateableProfile struct {
	FullName string `json:"fullName" bson:"full-name"`
	Bio      string `json:"bio" bson:"bio"`
	Picture  string `json:"picture" bson:"picture"`
	Website  string `json:"website" bson:"website"`
	Location string `json:"location" bson:"location"`
	Github   string `json:"github" bson:"github"`
	Gitlab   string `json:"gitlab" bson:"gitlab"`
	Company  string `json:"company" bson:"company"`
	Twitter  string `json:"twitter" bson:"twitter"`
}
