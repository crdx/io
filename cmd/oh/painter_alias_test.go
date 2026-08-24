package main

import (
	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/call"
	"crdx.org/io/cmd/oh/output"
	"crdx.org/io/cmd/oh/painter"
)

type Painter = painter.Painter

func newTestPainter(screen *output.Screen, isRunning bool) *Painter {
	return painter.New(screen, isRunning, nil, "")
}

func describeHarnessCall(harness *Harness, event agent.Event) agent.FallbackRendering {
	return call.Describe(event, harness.agent.Tool, harness.workspaceDir)
}

func describeBareCall(workspaceDir string, event agent.Event) agent.FallbackRendering {
	return call.Describe(event, nil, workspaceDir)
}
