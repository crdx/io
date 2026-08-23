package anthropic

import (
	"strings"
	"testing"

	"crdx.org/io/agent"
)

func TestASealedThoughtWithoutABlockStopIsNativeStateButNotCompletedPortableReasoning(t *testing.T) {
	var turn reply
	var outputs []agent.Output
	yield := func(output agent.Output) bool {
		outputs = append(outputs, output)
		return true
	}

	payloads := []string{
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Signed but not stopped."}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"seal-1"}}`,
	}
	for _, payload := range payloads {
		if done, err := turn.step(payload, yield); err != nil || done {
			t.Fatalf("step returned done %t and error %v", done, err)
		}
	}

	if len(outputs) != 1 || outputs[0].Kind != agent.ModelReasoningEvent ||
		outputs[0].Text != "Signed but not stopped." || outputs[0].Done {
		t.Errorf("unexpected portable outputs: %+v", outputs)
	}

	nativeState := string(turn.prose())
	if !strings.Contains(nativeState, `"thinking":"Signed but not stopped."`) ||
		!strings.Contains(nativeState, `"signature":"seal-1"`) {
		t.Errorf("sealed native state is incomplete: %s", nativeState)
	}
}
