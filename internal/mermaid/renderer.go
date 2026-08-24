// Package mermaid renders a terminal-native subset of Mermaid diagrams.
//
// The renderer is derived from mermaid-ascii 1.5.0, Copyright (c) 2023 Alexander Grooff, under the
// MIT licence in LICENSE. It supports flowcharts, sequence diagrams, and entity-relationship
// diagrams.
package mermaid

import (
	"fmt"
	"strings"

	"crdx.org/io/internal/mermaid/diagram"
	"crdx.org/io/internal/mermaid/er"
	"crdx.org/io/internal/mermaid/sequence"
)

const (
	boxBorderPadding = 1
	paddingBetweenX  = 5
	paddingBetweenY  = 5
)

// Render turns Mermaid source into Unicode rows.
func Render(source string) (string, error) {
	if err := validateSourceLimits(source); err != nil {
		return "", err
	}

	config := diagram.DefaultConfig()
	source, title := diagram.StripFrontmatter(source)

	renderer := rendererFor(source)
	if err := renderer.Parse(source); err != nil {
		return "", fmt.Errorf("parse %s diagram: %w", renderer.Type(), err)
	}

	output, err := renderer.Render(config)
	if err != nil {
		return "", fmt.Errorf("render %s diagram: %w", renderer.Type(), err)
	}

	if title != "" {
		output = title + "\n\n" + output
	}

	return strings.TrimRight(output, "\n"), nil
}

func rendererFor(source string) diagram.Diagram {
	source = strings.TrimSpace(source)
	if sequence.IsSequenceDiagram(source) {
		return &sequenceRenderer{}
	}
	if er.IsErDiagram(source) {
		return &entityRelationshipRenderer{}
	}
	return &flowchartRenderer{}
}

type sequenceRenderer struct {
	parsed *sequence.SequenceDiagram
}

func (self *sequenceRenderer) Parse(source string) error {
	parsed, err := sequence.Parse(source)
	if err != nil {
		return err
	}
	if err := validateSequenceLimits(parsed); err != nil {
		return err
	}
	self.parsed = parsed
	return nil
}

func (self *sequenceRenderer) Render(config *diagram.Config) (string, error) {
	return sequence.Render(self.parsed, config)
}

func (self *sequenceRenderer) Type() string {
	return "sequence"
}

type flowchartRenderer struct {
	properties *graphProperties
}

func (self *flowchartRenderer) Parse(source string) error {
	properties, err := mermaidFileToMap(source)
	if err != nil {
		return err
	}
	if err := validateFlowchartLimits(properties); err != nil {
		return err
	}
	self.properties = properties
	return nil
}

func (self *flowchartRenderer) Render(config *diagram.Config) (string, error) {
	self.properties.boxBorderPadding = config.BoxBorderPadding
	self.properties.paddingX = config.PaddingBetweenX
	self.properties.paddingY = config.PaddingBetweenY
	self.properties.styleType = config.StyleType
	self.properties.useAscii = config.UseAscii
	return drawMap(self.properties)
}

func (self *flowchartRenderer) Type() string {
	return "flowchart"
}

type entityRelationshipRenderer struct {
	parsed *er.ErDiagram
}

func (self *entityRelationshipRenderer) Parse(source string) error {
	parsed, err := er.Parse(source)
	if err != nil {
		return err
	}
	if err := validateEntityRelationshipLimits(parsed); err != nil {
		return err
	}
	self.parsed = parsed
	return nil
}

func (self *entityRelationshipRenderer) Render(config *diagram.Config) (string, error) {
	return er.Render(self.parsed, config.UseAscii), nil
}

func (self *entityRelationshipRenderer) Type() string {
	return "entity-relationship"
}
