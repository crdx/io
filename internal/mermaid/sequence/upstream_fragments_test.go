package sequence

import (
	"strings"
	"testing"
)

func fragmentKinds(sd *SequenceDiagram) []FragmentType {
	var out []FragmentType
	for _, ev := range sd.Events {
		if ev.Kind == EventFragmentStart {
			out = append(out, ev.Fragment.Type)
		}
	}
	return out
}

func TestParseFragments(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantMessages  int
		wantFragments []FragmentType
		wantLabels    []string
	}{
		{
			name:          "loop with label",
			input:         "sequenceDiagram\n A->>B: x\n loop every minute\n  A->>B: y\n end",
			wantMessages:  2,
			wantFragments: []FragmentType{FragmentLoop},
			wantLabels:    []string{"every minute"},
		},
		{
			name:          "opt without label",
			input:         "sequenceDiagram\n A->>B: x\n opt\n  A->>B: y\n end",
			wantMessages:  2,
			wantFragments: []FragmentType{FragmentOpt},
			wantLabels:    []string{""},
		},
		{
			name:          "nested loop then opt",
			input:         "sequenceDiagram\n loop retry\n  A->>B: try\n  opt failed\n   B-->>A: err\n  end\n end",
			wantMessages:  2,
			wantFragments: []FragmentType{FragmentLoop, FragmentOpt},
			wantLabels:    []string{"retry", "failed"},
		},
		{
			name:          "sequential fragments",
			input:         "sequenceDiagram\n loop a\n  A->>B: 1\n end\n opt b\n  A->>B: 2\n end",
			wantMessages:  2,
			wantFragments: []FragmentType{FragmentLoop, FragmentOpt},
			wantLabels:    []string{"a", "b"},
		},
		{
			name:          "empty fragment",
			input:         "sequenceDiagram\n A->>B: x\n loop nothing\n end",
			wantMessages:  1,
			wantFragments: []FragmentType{FragmentLoop},
			wantLabels:    []string{"nothing"},
		},
		{
			name:          "quoted participant named loop is a message",
			input:         "sequenceDiagram\n \"loop\"->>B: hi",
			wantMessages:  1,
			wantFragments: nil,
			wantLabels:    nil,
		},
		{
			name:          "message label containing keyword",
			input:         "sequenceDiagram\n A->>B: start the loop now",
			wantMessages:  1,
			wantFragments: nil,
			wantLabels:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sd, err := Parse(tt.input)
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}

			if len(sd.Messages) != tt.wantMessages {
				t.Errorf("Messages: got %d, want %d", len(sd.Messages), tt.wantMessages)
			}

			gotFrags := fragmentKinds(sd)
			if len(gotFrags) != len(tt.wantFragments) {
				t.Fatalf("fragments: got %v, want %v", gotFrags, tt.wantFragments)
			}
			for i, f := range tt.wantFragments {
				if gotFrags[i] != f {
					t.Errorf("fragment %d: got %v, want %v", i, gotFrags[i], f)
				}
			}

			starts, ends := 0, 0
			for _, ev := range sd.Events {
				switch ev.Kind {
				case EventFragmentStart:
					starts++
				case EventFragmentEnd:
					ends++
				case EventMessage, EventFragmentDivider, EventNote:
				}
			}
			if starts != ends {
				t.Errorf("unbalanced fragments: %d starts, %d ends", starts, ends)
			}

			var gotLabels []string
			for _, ev := range sd.Events {
				if ev.Kind == EventFragmentStart {
					gotLabels = append(gotLabels, ev.Fragment.Label)
				}
			}
			for i, want := range tt.wantLabels {
				if gotLabels[i] != want {
					t.Errorf("label %d: got %q, want %q", i, gotLabels[i], want)
				}
			}
		})
	}
}

func TestParseFragmentErrors(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:    "unclosed loop",
			input:   "sequenceDiagram\n A->>B: x\n loop forever\n  A->>B: y",
			wantErr: "unclosed",
		},
		{
			name:    "end without opener",
			input:   "sequenceDiagram\n A->>B: x\n end",
			wantErr: "without a matching fragment opener",
		},
		{
			name:    "unclosed nested opt",
			input:   "sequenceDiagram\n loop a\n  opt b\n   A->>B: y\n end",
			wantErr: "unclosed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.input)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestRenderFragmentSmoke(t *testing.T) {
	input := "sequenceDiagram\n A->>B: x\n loop retry\n  A->>B: y\n  opt maybe\n   B-->>A: z\n  end\n end"

	for _, ascii := range []bool{false, true} {
		sd, err := Parse(input)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		out, err := Render(sd, upstreamTestConfig(ascii))
		if err != nil {
			t.Fatalf("render (ascii=%v): %v", ascii, err)
		}
		for _, want := range []string{"[loop retry]", "[opt maybe]"} {
			if !strings.Contains(out, want) {
				t.Errorf("ascii=%v: output missing %q\n%s", ascii, want, out)
			}
		}
	}
}

func TestKeywordLineIsNeverAMessage(t *testing.T) {
	sd, err := Parse("sequenceDiagram\n alt ok\n A->>B: m\n else fall back -> retry: yes\n A->>B: n\n end")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(sd.Participants) != 2 {
		t.Fatalf("expected 2 participants, got %d", len(sd.Participants))
	}
	if _, err := Parse("sequenceDiagram\n loop->>B: hi"); err == nil {
		t.Fatal("expected unclosed-fragment error for keyword line, got none")
	}
}
