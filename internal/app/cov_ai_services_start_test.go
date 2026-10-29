package app

import (
	"strings"
	"testing"
)

// Bug: when the terminal tab cannot open (no editor, the panel cannot
// open), the start failed but the model was told "started …". The
// model must get the error.
func TestCovAIServicesStartFailureIsAnError(t *testing.T) {
	e := covSvcSetup(t)
	if _, err := e.run(t, "services_list", nil); err != nil {
		t.Fatal(err)
	}
	for _, a := range []map[string]any{{"name": "api"}, {"all": true}, {"name": "web"}} {
		out, err := e.run(t, "services_start", a)
		if err == nil || !strings.Contains(err.Error(), "could not open") || out != "" {
			t.Errorf("start %v without a terminal: %q %v", a, out, err)
		}
	}
	if len(e.m.services.busy) != 0 {
		t.Errorf("rows left busy: %v", e.m.services.busy)
	}
}
