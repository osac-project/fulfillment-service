/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package templating

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	tmpl "text/template"

	"github.com/google/uuid"
	"golang.org/x/exp/slices"
)

// EngineBuilder contains the data and logic needed to create templates. Don't create objects of this type directly, use
// the NewEngine function instead.
type EngineBuilder struct {
	logger    *slog.Logger
	fsys      []fs.FS
	dir       string
	functions map[string]any
}

// Engine is a template engine based on template.Template with some additional functions. Don't create objects of this
// type directly, use the NewConsole function instead.
type Engine struct {
	logger   *slog.Logger
	fsys     []fs.FS
	dir      string
	names    []string
	template *tmpl.Template
}

// NewEngine creates a builder that can the be used to create a template engine.
func NewEngine() *EngineBuilder {
	return &EngineBuilder{}
}

// SetLogger sets the logger that the engine will use to write messages to the log. This is mandatory.
func (b *EngineBuilder) SetLogger(value *slog.Logger) *EngineBuilder {
	b.logger = value
	return b
}

// AddFS adds one or more filesystems that will be used to read the templates. At least one filesystem must be added
// before building the engine.
func (b *EngineBuilder) AddFS(values ...fs.FS) *EngineBuilder {
	b.fsys = append(b.fsys, values...)
	return b
}

// SetDir instructs the engine to load load the templates only from the given directory of the filesystem. This is
// optional and the default is to load all the templates.
func (b *EngineBuilder) SetDir(value string) *EngineBuilder {
	b.dir = value
	return b
}

// AddFunction adds a custom function that will be available to the templates. This is optional. The function can be
// used in templates with the given name.
func (b *EngineBuilder) AddFunction(name string, function any) *EngineBuilder {
	if b.functions == nil {
		b.functions = map[string]any{}
	}
	b.functions[name] = function
	return b
}

// AddFunctions adds multiple custom functions that will be available to the templates. This is optional. The functions
// can be used in templates with the names specified in the map keys.
func (b *EngineBuilder) AddFunctions(functions tmpl.FuncMap) *EngineBuilder {
	if b.functions == nil {
		b.functions = maps.Clone(functions)
	} else {
		maps.Copy(b.functions, functions)
	}
	return b
}

// Build uses the configuration stored in the builder to create a new engine.
func (b *EngineBuilder) Build() (result *Engine, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}

	// We need to create the engine early because the some of the functions need the pointer:
	e := &Engine{
		logger:   b.logger,
		fsys:     b.fsys,
		dir:      b.dir,
		template: tmpl.New(""),
	}

	// Register custom functions first:
	if b.functions != nil {
		e.template.Funcs(b.functions)
	}

	// Register the built-in functions:
	e.template.Funcs(map[string]any{
		"base64":  e.base64Func,
		"data":    e.dataFunc,
		"execute": e.executeFunc,
		"json":    e.jsonFunc,
		"uuid":    e.uuidFunc,
	})

	// Find and parse the template files from all filesystems:
	for _, filesystem := range b.fsys {
		var fsys fs.FS = filesystem
		if b.dir != "" {
			fsys, err = fs.Sub(filesystem, b.dir)
			if err != nil {
				return
			}
		}
		err = e.loadTemplates(fsys)
		if err != nil {
			return
		}
	}

	// Return the object:
	result = e
	return
}

func (e *Engine) loadTemplates(fsys fs.FS) error {
	return fs.WalkDir(fsys, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		err = e.parseTemplate(fsys, name)
		if err != nil {
			return err
		}
		e.names = append(e.names, name)
		return nil
	})
}

func (e *Engine) parseTemplate(fsys fs.FS, name string) error {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return err
	}
	text := string(data)
	_, err = e.template.New(name).Parse(text)
	if err != nil {
		return err
	}
	e.logger.Debug(
		"Parsed template",
		slog.String("name", name),
		slog.String("text", text),
	)
	return nil
}

// Execute executes the template with the given name and passing the given input data. It writes the result to the given
// writer.
func (e *Engine) Execute(writer io.Writer, name string, data any) error {
	buffer := &bytes.Buffer{}
	err := e.template.ExecuteTemplate(buffer, name, data)
	if err != nil {
		return err
	}
	_, err = buffer.WriteTo(writer)
	if err != nil {
		return err
	}
	if e.logger.Enabled(context.TODO(), slog.LevelDebug) {
		e.logger.Debug(
			"Executed template",
			slog.String("name", name),
			slog.Any("data", data),
			slog.String("text", buffer.String()),
		)
	}
	return nil
}

// Names returns the names of the templates.
func (e *Engine) Names() []string {
	return slices.Clone(e.names)
}

// AddFS adds one or more filesystems to the engine and loads templates from them. This can be called after the engine
// has been created to add additional template sources.
func (e *Engine) AddFS(values ...fs.FS) error {
	for _, filesystem := range values {
		var fsys fs.FS = filesystem
		var err error
		if e.dir != "" {
			fsys, err = fs.Sub(filesystem, e.dir)
			if err != nil {
				return err
			}
		}
		err = e.loadTemplates(fsys)
		if err != nil {
			return err
		}
		e.fsys = append(e.fsys, filesystem)
	}
	return nil
}

// base64Func is a template function that encodes the given data using Base64 and returns the result as a string. If the
// data is an array of bytes it will be encoded directly. If the data is a string it will be converted to an array of
// bytes using the UTF-8 encoding. If the data implements the fmt.Stringer interface it will be converted to a string
// using the String method, and then to an array of bytes using the UTF-8 encoding. Any other kind of data will result
// in an error.
func (e *Engine) base64Func(value any) (result string, err error) {
	var data []byte
	switch typed := value.(type) {
	case []byte:
		data = typed
	case string:
		data = []byte(typed)
	case fmt.Stringer:
		data = []byte(typed.String())
	default:
		err = fmt.Errorf(
			"don't know how to encode value of type %T",
			value,
		)
		if err != nil {
			return
		}
	}
	result = base64.StdEncoding.EncodeToString(data)
	return
}

// executeFunc is a template function similar to template.ExecuteTemplate but it returns the result instead of writing
// it to the output. That is useful when some processing is needed after that, for example, to encode the result using
// Base64:
//
//	{{ execute "my.tmpl" . | base64 }}
func (e *Engine) executeFunc(name string, data any) (result string, err error) {
	buffer := &bytes.Buffer{}
	executed := e.template.Lookup(name)
	if executed == nil {
		err = fmt.Errorf("failed to find template '%s'", name)
		return
	}
	err = executed.Execute(buffer, data)
	if err != nil {
		return
	}
	result = buffer.String()
	return
}

// jsonFunc is a template function that encodes the given data as JSON. This can be used, for example, to encode as a
// JSON string the result of executing other function. For example, to create a JSON document with a 'content' field
// that contains the text of the 'my.tmpl' template:
//
//	"content": {{ execute "my.tmpl" . | json }}
//
// Note how that the value of that 'content' field doesn't need to sorrounded by quotes, because the 'json' function
// will generate a valid JSON string, including those quotes.
func (e *Engine) jsonFunc(data any) (result string, err error) {
	text, err := json.Marshal(data)
	if err != nil {
		return
	}
	result = string(text)
	return
}

// uuidFunc is a template function that generates a random UUID.
func (e *Engine) uuidFunc() string {
	return uuid.NewString()
}

// dataFunc is a template function that creates a map with the keys and values passed as parameters. The parameters
// should be a set of name/value pairs: values witn even indexes should be the names and values with odd indexes the
// values. For example, the following template:
//
//	{{ range $name, $value := data "X" 123 "Y 456 }}
//	{{ $name }}: {{ $value }}
//	{{ end }}
//
// Generates the following text:
//
//	X: 123
//	Y: 456
//
// This is specially useful to pass multiple named parameters to other templates. For example, a template that two
// values named `Name` and `Age` can be executed like this:
//
//	{{ execute "my-template.yaml" (data "Name" "Joe" "Age" 52) }}
//
// If the number of arguments isn't even, or if any of the names isn't a string an error will be returned.
func (e *Engine) dataFunc(args ...any) (result map[string]any, err error) {
	if len(args)%2 != 0 {
		err = fmt.Errorf(
			"number of arguments should be even, but it is %d",
			len(args),
		)
		return
	}
	result = map[string]any{}
	for i := 0; i < len(args)/2; i++ {
		key := args[2*i]
		name, ok := key.(string)
		if !ok {
			err = fmt.Errorf(
				"argument %d should be a string, but it is of type %T",
				i, key,
			)
			return
		}
		value := args[2*i+1]
		result[name] = value
	}
	return
}
