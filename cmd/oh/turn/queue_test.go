package turn

import "testing"

func TestLatestReplacementWinsAndKeepsModeChange(t *testing.T) {
	var queue Queue
	queue.Replace("first")
	queue.MarkModeChange()
	queue.Replace("second")

	pending := queue.Peek()
	if !pending.Replacement || !pending.ModeChange || pending.Message != "second" {
		t.Errorf("got %+v", pending)
	}
}

func TestReplacementTakesPriorityAndConsumesTheQueue(t *testing.T) {
	var queue Queue
	queue.MarkModeChange()
	queue.Replace("continue")

	kind, message := queue.Take()
	if kind != Replacement || message != "continue" {
		t.Errorf("got %v %q", kind, message)
	}
	if !queue.Empty() {
		t.Errorf("queue was not consumed: %+v", queue.Peek())
	}
}

func TestModeChangeIsTakenWithoutAReplacement(t *testing.T) {
	var queue Queue
	queue.MarkModeChange()

	kind, message := queue.Take()
	if kind != ModeChange || message != "" {
		t.Errorf("got %v %q", kind, message)
	}
}

func TestClearDropsEveryQueuedAction(t *testing.T) {
	var queue Queue
	queue.Replace("later")
	queue.MarkModeChange()
	queue.Clear()

	kind, message := queue.Take()
	if kind != None || message != "" || !queue.Empty() {
		t.Errorf("got %v %q %+v", kind, message, queue.Peek())
	}
}
