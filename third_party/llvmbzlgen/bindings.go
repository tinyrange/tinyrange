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

// Bindings provides the AST with variable bindings in the current scope.
// All interface functions are expected to return either the value currently
// bound to the provided name or an empty string.
type Bindings interface {
	Get(string) string      // Returns the named CMake variable or the empty string.
	GetCache(string) string // Returns the named CMake variable from the cache.
	GetEnv(string) string   // Returns the named Environment variable.
}
