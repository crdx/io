package jobs

import "sync"

const spoolBytes = 256 << 10

type spool struct {
	mutex        sync.Mutex
	contents     []byte
	droppedBytes int
}

func (self *spool) Write(data []byte) (int, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.contents = append(self.contents, data...)

	if excess := len(self.contents) - spoolBytes; excess > 0 {
		self.contents = self.contents[excess:]
		self.droppedBytes += excess
	}

	return len(data), nil
}

func (self *spool) String() string {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return string(self.contents)
}

func (self *spool) DroppedBytes() int {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.droppedBytes
}
