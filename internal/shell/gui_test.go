package shell

import (
	"testing"

	"github.com/egoist/mygo"
)

func TestConfigureDevName(t *testing.T) {
	// A test binary is a plain, unpackaged development build.
	Configure("9.9.9-test")
	if got := mygo.App.Name(); got != "trae Dev" {
		t.Errorf("name %q, want %q", got, "trae Dev")
	}
	if got := mygo.App.Version(); got != "9.9.9-test" {
		t.Errorf("version %q", got)
	}
}

func TestStepTextSize(t *testing.T) {
	for _, c := range []struct{ cur, step, want int }{
		{100, 1, 110}, {110, 1, 125}, {150, 1, 150},
		{125, -1, 110}, {100, -1, 100},
		{150, 0, 100}, {105, 1, 110},
	} {
		if got := stepTextSize(c.cur, c.step); got != c.want {
			t.Errorf("stepTextSize(%d, %d) = %d, want %d", c.cur, c.step, got, c.want)
		}
	}
}
