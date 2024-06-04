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

import (
	"github.com/alecthomas/participle/lexer"
)

// CMakeFile represents the root of a CMakeLists.txt AST and corresponds to:
// https://cmake.org/cmake/help/v3.0/manual/cmake-language.7.html#source-files
type CMakeFile struct {
	Commands []CommandInvocation `( ( Space | Newline )* @@ ( Space | Newline )* )*`
}

// CommandInvocation is a top-level CMake command.
// https://cmake.org/cmake/help/v3.0/manual/cmake-language.7.html#command-invocations
type CommandInvocation struct {
	Pos lexer.Position

	Name      string       `Space* @Identifier  Space*`
	Arguments ArgumentList `@@`
}

// ArgumentList is a parentheses-enclosed separated list of arguments.
// It broadly corresponds to the arguments and separated_argument productions from:
// https://cmake.org/cmake/help/v3.0/manual/cmake-language.7.html#command-invocations
type ArgumentList struct {
	Values []Argument `"(" @@? ((( Space | Newline )+ @@? ) | @@ )* ")"`
}

// Argument is a union-production for each of the CMake argument kinds.
// See: https://cmake.org/cmake/help/v3.0/manual/cmake-language.7.html#command-arguments
type Argument struct {
	Pos lexer.Position

	ArgumentList     *ArgumentList     `@@`
	QuotedArgument   *QuotedArgument   `| @@`
	UnquotedArgument *UnquotedArgument `| @@`
	BracketArgument  *BracketArgument  `| @@`
}

// BracketArgument is a [=*[<text>]=*]-enclosed argument corresponding to:
// https://cmake.org/cmake/help/v3.0/manual/cmake-language.7.html#bracket-argument
type BracketArgument struct {
	Text string `@BracketContent`
}

// QuotedArgument is a simple quoted string, corresponding to:
// https://cmake.org/cmake/help/v3.0/manual/cmake-language.7.html#quoted-argument
type QuotedArgument struct {
	Elements []QuotedElement `"\"" ( @@ )* "\""`
}

// QuotedElement is either a string of quoted text or a variable reference.
type QuotedElement struct {
	Ref  *VariableReference `@@`
	Text string             `| @( Quoted | EscapeSequence | VarClose )+`
}

// UnquotedArgument is CMake's standed unquoted command argument:
// https://cmake.org/cmake/help/v3.0/manual/cmake-language.7.html#unquoted-argument
// Note: The unquoted_legacy production mentioned above is *not* supported.
type UnquotedArgument struct {
	Elements []UnquotedElement `@@ ( @@ )*`
}

// UnquotedElement is either a run of unquoted text or a variable reference.
type UnquotedElement struct {
	Ref  *VariableReference `@@`
	Text string             `| @( Identifier | Unquoted | EscapeSequence | VarClose )+`
}

// VariableReference is a possibly-nested CMake ${}-enclosed variable reference:
// https://cmake.org/cmake/help/v3.0/manual/cmake-language.7.html#variable-references
// In order to capture the nature of these, they are embedded in the grammar rather than
// being handled during evaluation.
type VariableReference struct {
	Pos lexer.Position

	Domain   VarDomain         `@VarOpen`
	Elements []VariableElement `@@ ( @@ )* "}"`
}

// VariableElement is either a run of text corresponding the a variable name
// or a nested VariableReference.
type VariableElement struct {
	Text string             `@( Identifier | Unquoted | Quoted )?`
	Ref  *VariableReference `( @@ )?`
}
