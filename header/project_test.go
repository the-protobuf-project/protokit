package header

import (
	"strings"
	"testing"
)

// Render prefixes every line with a comment marker, so a newline reaching the
// credit would escape the comment and emit an unparseable file.
func TestSetProjectCollapsesLineBreaks(t *testing.T) {
	original := project
	t.Cleanup(func() { project = original })

	for _, credit := range []string{
		"repo — https://example.com\nrm -rf /",
		"repo — https://example.com\r\nsecond line",
		"repo — https://example.com\rsecond line",
	} {
		SetProject(credit)
		out := Render("//", Info{PluginVersion: "v1"})

		for _, line := range strings.Split(out, "\n") {
			if line != "" && !strings.HasPrefix(line, "//") {
				t.Errorf("credit %q produced an uncommented line: %q", credit, line)
			}
		}
	}
}

func TestSetProjectKeepsAnOrdinaryCreditIntact(t *testing.T) {
	original := project
	t.Cleanup(func() { project = original })

	SetProject("grpc-gateway-rs — https://github.com/the-protobuf-project/grpc-gateway-rs")
	if !strings.Contains(Render("//", Info{}), "grpc-gateway-rs — https://github.com") {
		t.Error("an ordinary credit should survive unchanged")
	}
}
