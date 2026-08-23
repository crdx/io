package contextUsage_test

import (
	"testing"

	"crdx.org/io/cmd/oh/segment"
	"crdx.org/io/cmd/oh/segment/contextUsage"
	"crdx.org/io/cmd/oh/style"
)

type noOptions struct{}

func (noOptions) Read(any) error { return nil }

func render(t *testing.T, usedTokens int, totalTokens int) string {
	t.Helper()

	built, err := contextUsage.New(func() (int, int) {
		return usedTokens, totalTokens
	})(noOptions{})
	if err != nil {
		t.Fatal(err)
	}

	return style.Plain(built.Render(segment.Context{}))
}

func TestContextUsageIsDerivedWhenRendered(t *testing.T) {
	usedTokens := 32_000
	built, err := contextUsage.New(func() (int, int) {
		return usedTokens, 274_000
	})(noOptions{})
	if err != nil {
		t.Fatal(err)
	}

	usedTokens = 64_000
	if got := style.Plain(built.Render(segment.Context{})); got != "23% 64Kt/274Kt" {
		t.Errorf("got %q", got)
	}
}

func TestContextUsageShowsEveryKnownPart(t *testing.T) {
	for name, test := range map[string]struct {
		usedTokens  int
		totalTokens int
		want        string
	}{
		"neither":    {want: "?% ?/?"},
		"total only": {totalTokens: 200_000, want: "?% ?/200Kt"},
		"used only":  {usedTokens: 5000, want: "?% 5Kt/?"},
		"both":       {usedTokens: 5000, totalTokens: 200_000, want: "3% 5Kt/200Kt"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := render(t, test.usedTokens, test.totalTokens); got != test.want {
				t.Errorf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestContextUsageRoundsAndCapsItsPercentage(t *testing.T) {
	if got := render(t, 5000, 200_000); got != "3% 5Kt/200Kt" {
		t.Errorf("got %q", got)
	}
	if got := render(t, 250_000, 200_000); got != "100% 250Kt/200Kt" {
		t.Errorf("got %q", got)
	}
}
