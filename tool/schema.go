package tool

import "encoding/json"

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
	Name        string   // what the argument is called
	Type        DataType // what kind of value it takes
	Description string   // what the argument means

	optional bool // whether the argument may be absent
}

// Optional marks a parameter the model may leave out. An absent argument decodes as its zero value,
// which is what a call carrying none means, so a tool reads one the same either way.
func (self Parameter) Optional() Parameter {
	self.optional = true
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
	Type        DataType `json:"type"`        // what kind of value it takes
	Description string   `json:"description"` // what the value means
}

type object struct {
	Type                 DataType            `json:"type"`                 // the schema kind
	Properties           map[string]property `json:"properties"`           // the declared arguments
	Required             []string            `json:"required,omitempty"`   // the mandatory arguments
	AdditionalProperties bool                `json:"additionalProperties"` // whether undeclared arguments are accepted
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

		if !parameter.optional {
			renderedSchema.Required = append(renderedSchema.Required, parameter.Name)
		}
	}

	return json.Marshal(renderedSchema)
}

// Definition is what a tool is, in a form no wire format's shape leaks into.
type Definition struct {
	Name        string // what the tool is called
	Description string // what the tool does
	Schema      Schema // what arguments it takes
}

// Describe says what a tool is, for a provider to offer to the model however it offers things.
func Describe(subject Tool) Definition {
	return Definition{
		Name:        subject.Name(),
		Description: subject.Description(),
		Schema:      subject.Schema(),
	}
}
