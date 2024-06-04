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
	"io"

	"github.com/alecthomas/participle"
	"github.com/kythe/llvmbzlgen/cmakelib/lexer"
)

// Parser parses CMake-style files following (most of) the grammar
// defined at https://cmake.org/cmake/help/v3.0/manual/cmake-language.7.html
type Parser struct {
	p *participle.Parser
}

// NewParser constructs a new parser for CMakeLists-style files.
func NewParser() *Parser {
	return &Parser{participle.MustBuild(&CMakeFile{}, participle.Lexer(lexer.New()))}
}

// Parse reads a CMakeLists.txt file from r and parses it into an AST.
func (p *Parser) Parse(r io.Reader) (*CMakeFile, error) {
	cmf := &CMakeFile{}
	return cmf, p.p.Parse(r, cmf)
}

// ParseString reads a CMakeLists.txt file from string s and parses it into an AST.
func (p *Parser) ParseString(s string) (*CMakeFile, error) {
	cmf := &CMakeFile{}
	return cmf, p.p.ParseString(s, cmf)
}

// ParseBytes reads a CMakeLists.txt file from byte slice b and parses it into an AST.
func (p *Parser) ParseBytes(b []byte) (*CMakeFile, error) {
	cmf := &CMakeFile{}
	return cmf, p.p.ParseBytes(b, cmf)
}

// String returns a string corresponding to the CMakeLists grammar.
func (p *Parser) String() string {
	return p.p.String()
}
