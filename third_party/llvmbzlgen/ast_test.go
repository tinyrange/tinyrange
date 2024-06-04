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
	"reflect"
	"testing"

	"github.com/alecthomas/participle"
	plex "github.com/alecthomas/participle/lexer"
	"github.com/google/go-cmp/cmp"

	"github.com/kythe/llvmbzlgen/cmakelib/lexer"
)

var positionType = reflect.TypeOf(plex.Position{})

func ignorePosition() cmp.Option {
	return cmp.FilterPath(func(p cmp.Path) bool {
		f, ok := p.Last().(cmp.StructField)
		if !ok {
			return false
		}
		return f.Name() == "Pos" && f.Type() == positionType
	}, cmp.Ignore())
}

func parseVariableReference(input string) (*VariableReference, error) {
	ref := &VariableReference{}
	parser := participle.MustBuild(ref, participle.Lexer(lexer.New()))
	if err := parser.ParseString(input, ref); err != nil {
		return nil, err
	}
	return ref, nil
}

func parseUnquotedArgument(input string) (*UnquotedArgument, error) {
	arg := &UnquotedArgument{}
	parser := participle.MustBuild(arg, participle.Lexer(lexer.New()))
	if err := parser.ParseString(input, arg); err != nil {
		return nil, err
	}
	return arg, nil
}

func parseBracketArgument(input string) (*BracketArgument, error) {
	arg := &BracketArgument{}
	parser := participle.MustBuild(arg, participle.Lexer(lexer.New()))
	if err := parser.ParseString(input, arg); err != nil {
		return nil, err
	}
	return arg, nil
}

func parseQuotedArgument(input string) (*QuotedArgument, error) {
	arg := &QuotedArgument{}
	parser := participle.MustBuild(arg, participle.Lexer(lexer.New()))
	if err := parser.ParseString(input, arg); err != nil {
		return nil, err
	}
	return arg, nil
}

func parseArgumentList(input string) (*ArgumentList, error) {
	arg := &ArgumentList{}
	parser := participle.MustBuild(arg, participle.Lexer(lexer.New()))
	if err := parser.ParseString(input, arg); err != nil {
		return nil, err
	}
	return arg, nil
}

func parseCMakeFile(input string) (*CMakeFile, error) {
	file, err := NewParser().ParseString(input)
	if err != nil {
		return nil, err
	}
	return file, nil
}

func TestVariableReferences(t *testing.T) {
	varRef := VariableReference{Elements: []VariableElement{{Text: "VAR"}}}
	tests := map[string]VariableReference{
		`${VAR}`:                       varRef,
		`$ENV{VAR}`:                    {Domain: DomainEnv, Elements: varRef.Elements},
		`${${VAR}}`:                    {Elements: []VariableElement{{Ref: &varRef}}},
		`${pre_${VAR}_in_${VAR}_post}`: {Elements: []VariableElement{{"pre_", &varRef}, {"_in_", &varRef}, {Text: "_post"}}},
		`${${VAR}_in_${VAR}}`:          {Elements: []VariableElement{{Ref: &varRef}, {"_in_", &varRef}}},
	}
	for input, expected := range tests {
		root, err := parseVariableReference(input)
		if err != nil {
			t.Errorf("Error parsing %#v: %s", input, err)
		} else if diff := cmp.Diff(*root, expected, ignorePosition()); diff != "" {
			t.Errorf("Unexpected parse %#v:\n%s", input, diff)
		}
	}

}

func TestUnquotedArgument(t *testing.T) {
	tests := map[string]UnquotedArgument{
		`NoSpace`:            {Elements: []UnquotedElement{{Text: "NoSpace"}}},
		`Escaped\ Space`:     {Elements: []UnquotedElement{{Text: `Escaped\ Space`}}},
		`Escaped\;Semicolon`: {Elements: []UnquotedElement{{Text: `Escaped\;Semicolon`}}},
		`${VAR}`:             {Elements: []UnquotedElement{{Ref: &VariableReference{Elements: []VariableElement{{Text: "VAR"}}}}}},
		`$ENV`:               {Elements: []UnquotedElement{{Text: "$ENV"}}},
		`Nested${VAR}Reference`: {Elements: []UnquotedElement{
			{Text: "Nested"},
			{Ref: &VariableReference{Elements: []VariableElement{{Text: "VAR"}}}},
			{Text: "Reference"},
		}},
		// This is divided during evaluation, but is still a single argument.
		`This;Divides;Into;Five;Arguments`: {Elements: []UnquotedElement{{Text: "This;Divides;Into;Five;Arguments"}}},
	}
	for input, expected := range tests {
		root, err := parseUnquotedArgument(input)
		if err != nil {
			t.Errorf("Error parsing %#v: %s", input, err)
		} else if diff := cmp.Diff(*root, expected, ignorePosition()); diff != "" {
			t.Errorf("Unexpected parse %#v:\n%s", input, diff)
		}
	}
}

type binder map[string]string

func (b binder) Get(key string) string {
	val, ok := b[key]
	if ok {
		return val
	}
	return ""
}

func (b binder) GetCache(key string) string {
	return b.Get(key)
}

func (b binder) GetEnv(key string) string {
	return b.Get(key)
}

func TestUnquotedEvaluation(t *testing.T) {
	tests := map[string][]string{
		`NoSpace`:                          {"NoSpace"},
		`Escaped\ Space`:                   {`Escaped Space`},
		`Escaped\;Semicolon`:               {`Escaped;Semicolon`},
		`${VAR}`:                           {"VAR"},
		`${LIST}`:                          {"A", "List", "Of", "Items"},
		`$ENV`:                             {"$ENV"},
		`Nested${VAR}Reference`:            {"NestedVARReference"},
		`Mixed${LIST}And${ESCAPED}Var`:     {"MixedA", "List", "Of", "ItemsAndEscaped;SemicolonVar"},
		`This;Divides;Into;Five;Arguments`: {"This", "Divides", "Into", "Five", "Arguments"},
		`Escaped\${VAR}Ref`:                {"Escaped${VAR}Ref"},
		`;LeadingSemicolon`:                {"", "LeadingSemicolon"},
	}
	vars := binder{
		"VAR":     "VAR",
		"LIST":    "A;List;Of;Items",
		"ESCAPED": `Escaped\;Semicolon`,
	}
	for input, expected := range tests {
		root, err := parseUnquotedArgument(input)
		if err != nil {
			t.Errorf("Error parsing %#v: %s", input, err)
		} else if diff := cmp.Diff(root.Eval(vars), expected); diff != "" {
			t.Errorf("Unexpected evaluation %#v:\n%s", input, diff)
		}
	}
}

func TestBracketArgument(t *testing.T) {
	tests := map[string]string{
		`[[]]`:                         ``,                   // Empty
		`[=[]=]`:                       ``,                   // Empty, non-empty delimiter.
		`[=[${var}]=]`:                 `${var}`,             // Unevaluated variable reference.
		`[===[content\n]]]=]]==]]===]`: `content\n]]]=]]==]`, // Unmatched delimiters.
	}
	for input, expected := range tests {
		root, err := parseBracketArgument(input)
		if err != nil {
			t.Errorf("Error parsing %#v: %s", input, err)
		} else if diff := cmp.Diff(root.Text, expected, ignorePosition()); diff != "" {
			t.Errorf("Unexpected parse %#v:\n%s", input, diff)
		}
	}
}

func TestQuotedArgument(t *testing.T) {
	tests := map[string]QuotedArgument{
		`""`:               {Elements: []QuotedElement{{Text: ""}}},             // Empty.
		`"\n"`:             {Elements: []QuotedElement{{Text: `\n`}}},           // Newline.
		`"\\n"`:            {Elements: []QuotedElement{{Text: `\\n`}}},          // Escaped newline.
		"\"\n\"":           {Elements: []QuotedElement{{Text: "\n"}}},           // Literal newline.
		`"regular text"`:   {Elements: []QuotedElement{{Text: "regular text"}}}, // Boring regular text.
		"\"cont\\\ninue\"": {Elements: []QuotedElement{{Text: "continue"}}},     // Escaped continuation.
		`"ident"`:          {Elements: []QuotedElement{{Text: `ident`}}},        // Thing that could be an identifier.
		`"\${var}"`:        {Elements: []QuotedElement{{Text: `\${var}`}}},      // Escaped variable reference.
		`"$ENV"`:           {Elements: []QuotedElement{{Text: "$ENV"}}},         // String that looks like a varible reference.
		// Variable reference.
		`"${var}"`: {Elements: []QuotedElement{{Ref: &VariableReference{Elements: []VariableElement{{Text: "var"}}}}}},
		// Environment variable reference.
		`"$ENV{var}"`: {Elements: []QuotedElement{
			{Ref: &VariableReference{Domain: DomainEnv, Elements: []VariableElement{{Text: "var"}}}},
		}},
		`"Nested${var}Reference"`: {Elements: []QuotedElement{
			{Text: "Nested"},
			{Ref: &VariableReference{Elements: []VariableElement{{Text: "var"}}}},
			{Text: "Reference"},
		}},
	}
	for input, expected := range tests {
		root, err := parseQuotedArgument(input)
		if err != nil {
			t.Errorf("Error parsing %#v: %s", input, err)
		} else if diff := cmp.Diff(*root, expected, ignorePosition()); diff != "" {
			t.Errorf("Unexpected parse %#v:\n%s", input, diff)
		}
	}
}

func TestArgumentList(t *testing.T) {
	tests := map[string]ArgumentList{
		`()`:             {},
		`(  )`:           {},
		"(\n \n )":       {},
		"(#comment\n)":   {},
		"(#[[comment]])": {},
		`(())`:           {Values: []Argument{{ArgumentList: &ArgumentList{}}}},
		`(A()B)`: {Values: []Argument{
			{UnquotedArgument: &UnquotedArgument{Elements: []UnquotedElement{{Text: "A"}}}},
			{ArgumentList: &ArgumentList{}},
			{UnquotedArgument: &UnquotedArgument{Elements: []UnquotedElement{{Text: "B"}}}},
		}},
		`(A (B AND NOT(C OR D)))`: {Values: []Argument{
			{UnquotedArgument: &UnquotedArgument{Elements: []UnquotedElement{{Text: "A"}}}},
			{ArgumentList: &ArgumentList{Values: []Argument{
				{UnquotedArgument: &UnquotedArgument{Elements: []UnquotedElement{{Text: "B"}}}},
				{UnquotedArgument: &UnquotedArgument{Elements: []UnquotedElement{{Text: "AND"}}}},
				{UnquotedArgument: &UnquotedArgument{Elements: []UnquotedElement{{Text: "NOT"}}}},
				{ArgumentList: &ArgumentList{Values: []Argument{
					{UnquotedArgument: &UnquotedArgument{Elements: []UnquotedElement{{Text: "C"}}}},
					{UnquotedArgument: &UnquotedArgument{Elements: []UnquotedElement{{Text: "OR"}}}},
					{UnquotedArgument: &UnquotedArgument{Elements: []UnquotedElement{{Text: "D"}}}},
				}}},
			}}},
		},
		},
	}
	for input, expected := range tests {
		root, err := parseArgumentList(input)
		if err != nil {
			t.Errorf("Error parsing %#v: %s", input, err)
		} else if diff := cmp.Diff(*root, expected, ignorePosition()); diff != "" {
			t.Errorf("Unexpected parse %#v:\n%s", input, diff)
		}
	}
}

func TestCMakeFile(t *testing.T) {
	tests := map[string]CMakeFile{
		"directive(\nCOMMAND   )\n": {
			[]CommandInvocation{{Name: "directive", Arguments: ArgumentList{Values: []Argument{
				{UnquotedArgument: &UnquotedArgument{Elements: []UnquotedElement{{Text: "COMMAND"}}}},
			}}}},
		},
		"directive(\nCOMMAND\n\n  )\n": {
			[]CommandInvocation{{Name: "directive", Arguments: ArgumentList{Values: []Argument{
				{UnquotedArgument: &UnquotedArgument{Elements: []UnquotedElement{{Text: "COMMAND"}}}},
			}}}},
		},
		`directive(1234 Unquoted;List Nested${VAR}Ref "Quoted${VAR}Ref")`: {
			[]CommandInvocation{{Name: "directive", Arguments: ArgumentList{Values: []Argument{
				{UnquotedArgument: &UnquotedArgument{Elements: []UnquotedElement{{Text: "1234"}}}},
				{UnquotedArgument: &UnquotedArgument{Elements: []UnquotedElement{{Text: "Unquoted;List"}}}},
				{UnquotedArgument: &UnquotedArgument{Elements: []UnquotedElement{
					{Text: "Nested"},
					{Ref: &VariableReference{Elements: []VariableElement{{Text: "VAR"}}}},
					{Text: "Ref"},
				}}},
				{QuotedArgument: &QuotedArgument{Elements: []QuotedElement{
					{Text: "Quoted"},
					{Ref: &VariableReference{Elements: []VariableElement{{Text: "VAR"}}}},
					{Text: "Ref"},
				}}},
			}}}},
		},
		`directive(terrible"cho#ces"tail)`: {
			[]CommandInvocation{{Name: "directive", Arguments: ArgumentList{Values: []Argument{
				{UnquotedArgument: &UnquotedArgument{Elements: []UnquotedElement{{Text: "terrible"}}}},
				{QuotedArgument: &QuotedArgument{Elements: []QuotedElement{{Text: "cho#ces"}}}},
				{UnquotedArgument: &UnquotedArgument{Elements: []UnquotedElement{{Text: "tail"}}}},
			}}}},
		},
		`set(LLVM_RUNTIME_OUTPUT_INTDIR ${CMAKE_CURRENT_BINARY_DIR}/${CMAKE_CFG_INTDIR}/bin)`: {
			[]CommandInvocation{{Name: "set", Arguments: ArgumentList{Values: []Argument{
				{UnquotedArgument: &UnquotedArgument{Elements: []UnquotedElement{{Text: "LLVM_RUNTIME_OUTPUT_INTDIR"}}}},
				{UnquotedArgument: &UnquotedArgument{Elements: []UnquotedElement{
					{Ref: &VariableReference{Elements: []VariableElement{{Text: "CMAKE_CURRENT_BINARY_DIR"}}}},
					{Text: "/"},
					{Ref: &VariableReference{Elements: []VariableElement{{Text: "CMAKE_CFG_INTDIR"}}}},
					{Text: "/bin"},
				}}},
			}}}},
		},
	}
	for input, expected := range tests {
		root, err := parseCMakeFile(input)
		if err != nil {
			t.Errorf("Error parsing %#v: %s", input, err)
		} else if diff := cmp.Diff(*root, expected, ignorePosition()); diff != "" {
			t.Errorf("Unexpected parse %#v:\n%s", input, diff)
		}
	}
}
