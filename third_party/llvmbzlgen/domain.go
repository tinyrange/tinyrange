/*
 * Copyright 2019 The Kythe Authors. All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package ast

import "fmt"

// Constants defining the recognized valid variable domains.
const (
	DomainDefault VarDomain = iota // The default (anonymous) domain.
	DomainCache                    // CACHE variable references.
	DomainEnv                      // ENV variable references.
	DomainMake                     // Make variables.
)

// VarDomain represents one of CMake's variable scopes.
type VarDomain int

// Capture translates a sequence of values into the appropriate variable domain.
func (d *VarDomain) Capture(values []string) error {
	if len(values) > 1 {
		return fmt.Errorf("invalid Domain values: %v", values)
	}
	value := values[0]
	if len(value) > 0 && value[0] == '$' {
		value = value[1 : len(value)-1]
	}
	switch value {
	case "":
		*d = DomainDefault
	case "CACHE":
		*d = DomainCache
	case "ENV":
		*d = DomainEnv
	default:
		return fmt.Errorf("invalid Domain: %s", value)
	}
	return nil
}

// String implements fmt.Stringer for VarDomain and returns a user-readable string for the domain.
func (d VarDomain) String() string {
	switch d {
	case DomainDefault:
		return "(default)"
	case DomainEnv:
		return "ENV"
	case DomainCache:
		return "CACHE"
	case DomainMake:
		return "(make)"
	default:
		panic("invalid domain")

	}
}
