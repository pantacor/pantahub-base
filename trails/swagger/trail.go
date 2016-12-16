/* 
 * PANTAHUB Core API - Trails
 *
 * Join the Federation of Humans and Things, with cloud and devices
 *
 * OpenAPI spec version: 1.0.0
 * Contact: asac129@gmail.com
 * Generated by: https://github.com/swagger-api/swagger-codegen.git
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package swagger

type Object map[string]interface{}

type Trail struct {

	// prn resource id for the owner of this trail
	Owner string `json:"owner,omitempty"`

	// prn resource id for the device of this trail
	Device string `json:"device,omitempty"`

	// the factory state setting for this device trail
	FactoryState Object `json:"factory-state,omitempty"`

	// time of when device was last time in sync with trail goal
	LastInSync string `json:"last-in-sync,omitempty"`
}
