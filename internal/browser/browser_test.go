package browser

import "testing"

func TestOpenReportsWhenNoBrowserLauncherExists(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := Open("https://example.test"); err == nil {
		t.Fatal("expected the missing browser launcher to be reported")
	}
}
