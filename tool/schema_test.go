package tool_test

import (
	"encoding/json"
	"testing"

	"crdx.org/io/tool"
)

func TestOptionalParametersAreLeftOutOfRequired(t *testing.T) {
	schema := tool.Schema{
		tool.String("path", "path to the file"),
		tool.Integer("offset", "first line to return").Optional(),
	}

	schemaJSON, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedJSON := `{"type":"object","properties":{` +
		`"offset":{"type":"integer","description":"first line to return"},` +
		`"path":{"type":"string","description":"path to the file"}},` +
		`"required":["path"],"additionalProperties":false}`

	if string(schemaJSON) != expectedJSON {
		t.Errorf("expected %s, got %s", expectedJSON, schemaJSON)
	}
}

func TestASchemaWithNoParametersIsStillAnObject(t *testing.T) {
	schemaJSON, err := json.Marshal(tool.Schema{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedJSON := `{"type":"object","properties":{},"additionalProperties":false}`

	if string(schemaJSON) != expectedJSON {
		t.Errorf("expected %s, got %s", expectedJSON, schemaJSON)
	}
}
