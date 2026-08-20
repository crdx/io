package tool

import (
	"encoding/json"
	"strings"

	"crdx.org/io/internal/strutil"
)

// DataType is the JSON Schema type of one value.
type DataType string

// The JSON Schema types a parameter may declare.
const (
	TypeObject  DataType = "object"
	TypeString  DataType = "string"
	TypeInteger DataType = "integer"
)

// Schema is a tool's parameters, in the order the tool declares them.
type Schema []Parameter

// Parameter is one argument a tool takes.
type Parameter struct {
	Name        string
	Type        DataType
	Description string

	isOptional bool
}

// Optional marks a parameter the model may leave out. An absent argument decodes as its zero value,
// which is what a call carrying none means, so a tool reads one the same either way.
func (self Parameter) Optional() Parameter {
	self.isOptional = true
	return self
}

// String declares a string parameter.
func String(name string, description string) Parameter {
	return Parameter{Name: name, Type: TypeString, Description: description}
}

// Integer declares an integer parameter.
func Integer(name string, description string) Parameter {
	return Parameter{Name: name, Type: TypeInteger, Description: description}
}

type property struct {
	Type        DataType `json:"type"`
	Description string   `json:"description"`
}

type object struct {
	Type                 DataType            `json:"type"`
	Properties           map[string]property `json:"properties"`
	Required             []string            `json:"required,omitempty"`
	AdditionalProperties bool                `json:"additionalProperties"`
}

// MarshalJSON renders the parameters as the JSON Schema object the endpoint expects.
func (self Schema) MarshalJSON() ([]byte, error) {
	renderedSchema := object{
		Type:       TypeObject,
		Properties: make(map[string]property, len(self)),
	}

	for _, parameter := range self {
		renderedSchema.Properties[parameter.Name] = property{
			Type:        parameter.Type,
			Description: parameter.Description,
		}

		if !parameter.isOptional {
			renderedSchema.Required = append(renderedSchema.Required, parameter.Name)
		}
	}

	return json.Marshal(renderedSchema)
}

// Definition is what a tool is, in a form no wire format's shape leaks into.
type Definition struct {
	Name        string
	Description string
	Schema      Schema
}

// Describe says what a tool is, for a provider to offer to the model however it offers things.
func Describe(subject Tool) Definition {
	return Definition{
		Name:        subject.Name(),
		Description: subject.Description(),
		Schema:      subject.Schema(),
	}
}

// DescribeUnparsedArguments reports the subject of arguments no call could be parsed from.
func DescribeUnparsedArguments(subject Tool, arguments string) string {
	var decoded map[string]json.RawMessage
	if json.Unmarshal([]byte(arguments), &decoded) != nil {
		return strutil.FirstLine(arguments)
	}

	var values []string

	for _, parameter := range subject.Schema() {
		raw, present := decoded[parameter.Name]
		if !present {
			continue
		}

		var text string
		if json.Unmarshal(raw, &text) != nil {
			text = string(raw)
		}

		if text = strings.TrimSpace(text); text != "" {
			values = append(values, text)
		}
	}

	if len(values) == 0 {
		return strutil.FirstLine(arguments)
	}

	return strutil.FirstLine(strings.Join(values, " "))
}
