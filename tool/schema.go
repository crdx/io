package tool

import "encoding/json"

// DataType is the JSON Schema type of one value.
type DataType string

// The JSON Schema types a parameter may declare.
const (
	TypeObject DataType = "object"
	TypeString DataType = "string"
)

// Schema is a tool's parameters, in the order the tool declares them.
type Schema []Parameter

// Parameter is one argument a tool takes.
type Parameter struct {
	Name        string
	Type        DataType
	Description string
}

// String declares a string parameter.
func String(name string, description string) Parameter {
	return Parameter{Name: name, Type: TypeString, Description: description}
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
	rendered := object{
		Type:       TypeObject,
		Properties: make(map[string]property, len(self)),
	}

	for _, parameter := range self {
		rendered.Properties[parameter.Name] = property{
			Type:        parameter.Type,
			Description: parameter.Description,
		}

		rendered.Required = append(rendered.Required, parameter.Name)
	}

	return json.Marshal(rendered)
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
