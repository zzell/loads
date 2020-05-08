// Copyright 2015 go-swagger maintainers
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package loads

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"

	"github.com/go-openapi/analysis"
	"github.com/go-openapi/spec"
	"github.com/go-openapi/swag"
)

// JSONDoc loads a json document from either a file or a remote url
func JSONDoc(path string) (json.RawMessage, error) {
	data, err := swag.LoadFromFileOrHTTP(path)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// DocLoader represents a doc loader type
type DocLoader func(string) (json.RawMessage, error)

// DocMatcher represents a predicate to check if a loader matches
type DocMatcher func(string) bool

type XX struct {
	loaders *loader
	raw     []byte
	uid     string
}

// todo: uuid should be generated here
// it is used for reference
func New(raw []byte, uuid string) XX {
	var matcherFn = func(_ string) bool {
		return true
	}

	var loaderFn = func(_ string) (json.RawMessage, error) {
		r := raw
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) > 0 {
			if trimmed[0] != '{' && trimmed[0] != '[' {
				yml, err := swag.BytesToYAMLDoc(trimmed)
				if err != nil {
					return nil, err
				}
				d, err := swag.YAMLToJSON(yml)
				if err != nil {
					return nil, err
				}
				r = d
			}
		}

		return r, nil
	}

	return XX{
		loaders: &loader{
			Match: matcherFn,
			Fn:    loaderFn,
		},
		raw: raw,
		uid: uuid,
	}
}

// var (
// 	loaders       *loader
// 	defaultLoader *loader
// )

func init() {
	// defaultLoader = &loader{Match: func(_ string) bool { return true }, Fn: JSONDoc}
	// loaders = defaultLoader
	// spec.PathLoader = loaders.Fn
	// AddLoader(swag.YAMLMatcher, swag.YAMLDoc)

	gob.Register(map[string]interface{}{})
	gob.Register([]interface{}{})
	//gob.Register(spec.Refable{})
}

// // AddLoader for a document
// func AddLoader(predicate DocMatcher, load DocLoader) {
// 	prev := loaders
// 	loaders = &loader{
// 		Match: predicate,
// 		Fn:    load,
// 		Next:  prev,
// 	}
// 	spec.PathLoader = loaders.Fn
// }

type loader struct {
	Fn    DocLoader
	Match DocMatcher
	Next  *loader
}

// JSONSpec loads a spec from a json document
func JSONSpec(path string) (*Document, error) {
	data, err := JSONDoc(path)
	if err != nil {
		return nil, err
	}
	// convert to json
	return Analyzed(data, "")
}

// Document represents a swagger spec document
type Document struct {
	// specAnalyzer
	Analyzer     *analysis.Spec
	spec         *spec.Swagger
	specFilePath string
	origSpec     *spec.Swagger
	schema       *spec.Schema
	raw          json.RawMessage
}

// Embedded returns a Document based on embedded specs. No analysis is required
func Embedded(orig, flat json.RawMessage) (*Document, error) {
	var origSpec, flatSpec spec.Swagger
	if err := json.Unmarshal(orig, &origSpec); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(flat, &flatSpec); err != nil {
		return nil, err
	}
	return &Document{
		raw:      orig,
		origSpec: &origSpec,
		spec:     &flatSpec,
	}, nil
}

// Spec loads a new spec document
func (x XX) Spec() (*Document, error) {
	var lastErr error
	b, err2 := x.loaders.Fn(x.uid)
	if err2 != nil {
		lastErr = err2
		return nil, lastErr
	}
	doc, err3 := Analyzed(b, "")
	if err3 != nil {
		return nil, err3
	}
	if doc != nil {
		doc.specFilePath = x.uid
	}
	return doc, nil
}

// Analyzed creates a new analyzed spec document
func Analyzed(data json.RawMessage, version string) (*Document, error) {
	if version == "" {
		version = "2.0"
	}
	if version != "2.0" {
		return nil, fmt.Errorf("spec version %q is not supported", version)
	}

	raw := data
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 {
		if trimmed[0] != '{' && trimmed[0] != '[' {
			yml, err := swag.BytesToYAMLDoc(trimmed)
			if err != nil {
				return nil, fmt.Errorf("analyzed: %v", err)
			}
			d, err := swag.YAMLToJSON(yml)
			if err != nil {
				return nil, fmt.Errorf("analyzed: %v", err)
			}
			raw = d
		}
	}

	swspec := new(spec.Swagger)
	if err := json.Unmarshal(raw, swspec); err != nil {
		return nil, err
	}

	origsqspec, err := cloneSpec(swspec)
	if err != nil {
		return nil, err
	}

	d := &Document{
		Analyzer: analysis.New(swspec),
		schema:   spec.MustLoadSwagger20Schema(),
		spec:     swspec,
		raw:      raw,
		origSpec: origsqspec,
	}
	return d, nil
}

// Expanded expands the ref fields in the spec document and returns a new spec document
func (d *Document) Expanded(options ...*spec.ExpandOptions) (*Document, error) {
	swspec := new(spec.Swagger)
	if err := json.Unmarshal(d.raw, swspec); err != nil {
		return nil, err
	}

	var expandOptions *spec.ExpandOptions
	if len(options) > 0 {
		expandOptions = options[0]
	} else {
		expandOptions = &spec.ExpandOptions{
			RelativeBase: d.specFilePath,
		}
	}

	if err := spec.ExpandSpec(swspec, expandOptions); err != nil {
		return nil, err
	}

	dd := &Document{
		Analyzer:     analysis.New(swspec),
		spec:         swspec,
		specFilePath: d.specFilePath,
		schema:       spec.MustLoadSwagger20Schema(),
		raw:          d.raw,
		origSpec:     d.origSpec,
	}
	return dd, nil
}

// BasePath the base path for this spec
func (d *Document) BasePath() string {
	return d.spec.BasePath
}

// Version returns the version of this spec
func (d *Document) Version() string {
	return d.spec.Swagger
}

// Schema returns the swagger 2.0 schema
func (d *Document) Schema() *spec.Schema {
	return d.schema
}

// Spec returns the swagger spec object model
func (d *Document) Spec() *spec.Swagger {
	return d.spec
}

// Host returns the host for the API
func (d *Document) Host() string {
	return d.spec.Host
}

// Raw returns the raw swagger spec as json bytes
func (d *Document) Raw() json.RawMessage {
	return d.raw
}

// OrigSpec yields the original spec
func (d *Document) OrigSpec() *spec.Swagger {
	return d.origSpec
}

// ResetDefinitions gives a shallow copy with the models reset
func (d *Document) ResetDefinitions() *Document {
	defs := make(map[string]spec.Schema, len(d.origSpec.Definitions))
	for k, v := range d.origSpec.Definitions {
		defs[k] = v
	}

	d.spec.Definitions = defs
	return d
}

// Pristine creates a new pristine document instance based on the input data
func (d *Document) Pristine() *Document {
	dd, _ := Analyzed(d.Raw(), d.Version())
	return dd
}

// SpecFilePath returns the file path of the spec if one is defined
func (d *Document) SpecFilePath() string {
	return d.specFilePath
}

func cloneSpec(src *spec.Swagger) (*spec.Swagger, error) {
	var b bytes.Buffer
	if err := gob.NewEncoder(&b).Encode(src); err != nil {
		return nil, err
	}

	var dst spec.Swagger
	if err := gob.NewDecoder(&b).Decode(&dst); err != nil {
		return nil, err
	}
	return &dst, nil
}
