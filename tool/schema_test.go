package tool_test

import (
	"encoding/json"
	"testing"

	"crdx.org/io/tool"
)

// Which arguments are required falls out of the parameters themselves, so an optional one is
// declared as a property the model may leave out rather than listed twice.
func TestOptionalParametersAreLeftOutOfRequired(t *testing.T) {
	schema := tool.Schema{
		tool.String("path", "path to the file"),
		tool.Integer("offset", "first line to return").Optional(),
	}

	rendered, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := `{"type":"object","properties":{` +
		`"offset":{"type":"integer","description":"first line to return"},` +
		`"path":{"type":"string","description":"path to the file"}},` +
		`"required":["path"],"additionalProperties":false}`

	if string(rendered) != expected {
		t.Errorf("expected %s, got %s", expected, rendered)
	}
}

// A tool taking nothing at all still renders an object, since the endpoint reads the properties
// rather than the absence of them.
func TestASchemaWithNoParametersIsStillAnObject(t *testing.T) {
	rendered, err := json.Marshal(tool.Schema{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := `{"type":"object","properties":{},"additionalProperties":false}`

	if string(rendered) != expected {
		t.Errorf("expected %s, got %s", expected, rendered)
	}
}
