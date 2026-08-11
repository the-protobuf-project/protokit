package golden

// golden.go drives a case through a registry of targets and diffs (or, under
// -update, rewrites) the output against the committed golden tree.

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/the-protobuf-project/protokit"
	"github.com/the-protobuf-project/protokit/schema"
)

// Update, set via the -update test flag, makes RunCase rewrite the golden files
// with current output instead of comparing against them. Defined here so every
// generator module's golden tests share the one flag.
var Update = flag.Bool("update", false, "rewrite golden files with current output")

// RunCase compiles dir's protos in-process and, for each target, checks the
// generated output against dir/golden/<target>/ byte-for-byte (or rewrites it
// under -update). registry supplies the targets to run; defaultTargets is used
// when the case has no "targets" file.
//
// Deprecated: use [RunPluginCase], which takes a [protokit.Plugin] (facet readers
// plus a layout resolver) instead of a [schema.Backend]. RunCase adapts the
// backend internally and behaves identically; it will be removed one major after
// the Backend SPI.
func RunCase(t *testing.T, dir string, registry map[string]schema.Target, defaultTargets []string, newBackend func(caseDir string) schema.Backend) {
	RunPluginCase(t, dir, defaultTargets, func(caseDir string) protokit.Plugin {
		reader, layout := schema.AdaptBackend(newBackend(caseDir))
		return protokit.Plugin{
			Registry: registry,
			Readers:  []protokit.FacetReader{reader},
			Layout:   layout,
		}
	})
}

// RunPluginCase compiles dir's protos in-process and, for each target, checks the
// generated output against dir/golden/<target>/ byte-for-byte (or rewrites it
// under -update).
//
// newPlugin builds the generator's [protokit.Plugin] for this case, receiving the
// case directory so the generator can read any per-case config it owns (its own
// layout YAML, a "stores" marker, …). The harness stays generator-neutral: it
// knows nothing of orm.yaml/web3.yaml, stores, go_module, or otel — those live
// entirely in the generator's factory.
func RunPluginCase(t *testing.T, dir string, defaultTargets []string, newPlugin func(caseDir string) protokit.Plugin) {
	req := BuildRequest(t, dir)
	pl := newPlugin(dir)
	registry := pl.Registry

	for _, target := range CaseTargets(t, dir, defaultTargets) {
		if _, ok := registry[target]; !ok {
			// A target this module doesn't ship — its goldens live in the module
			// that owns it (e.g. solidity in web3). Skip rather than fail.
			continue
		}
		t.Run(target, func(t *testing.T) {
			files := runTarget(t, req, target, pl)
			goldenDir := filepath.Join(dir, "golden", target)

			if *Update {
				writeGolden(t, goldenDir, files)
				return
			}
			compareGolden(t, goldenDir, files)
		})
	}
}

// CaseTargets returns the backends a case runs: the comma-separated content of
// its optional "targets" file, defaulting to defaults.
func CaseTargets(t *testing.T, dir string, defaults []string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "targets"))
	if err != nil {
		if os.IsNotExist(err) {
			return defaults
		}
		t.Fatalf("read targets: %v", err)
	}
	var out []string
	for _, s := range strings.Split(strings.TrimSpace(string(b)), ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// runTarget executes one target through the real plugin entry point (RunPlugin)
// and returns the generated files as path → content. Everything
// generator-specific (grouping config, stores, go module, otel) is baked into pl
// by the caller's factory, so protokit needs only the target name.
func runTarget(t *testing.T, req *pluginpb.CodeGeneratorRequest, target string, pl protokit.Plugin) map[string]string {
	t.Helper()

	p, err := protogen.Options{}.New(req)
	if err != nil {
		t.Fatalf("protogen: %v", err)
	}
	opts := protokit.Options{Target: target}
	if err := protokit.RunPlugin(p, opts, pl); err != nil {
		t.Fatalf("generate %s: %v", target, err)
	}
	resp := p.Response()
	if resp.GetError() != "" {
		t.Fatalf("response error: %s", resp.GetError())
	}

	files := map[string]string{}
	for _, f := range resp.GetFile() {
		files[f.GetName()] = f.GetContent()
	}
	if len(files) == 0 {
		t.Fatalf("target %s produced no files", target)
	}
	return files
}

// writeGolden replaces goldenDir with the current output.
func writeGolden(t *testing.T, goldenDir string, files map[string]string) {
	t.Helper()
	if err := os.RemoveAll(goldenDir); err != nil {
		t.Fatalf("clean golden: %v", err)
	}
	for name, content := range files {
		path := filepath.Join(goldenDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	t.Logf("updated %d golden files in %s", len(files), goldenDir)
}

// compareGolden diffs current output against the committed golden tree.
func compareGolden(t *testing.T, goldenDir string, files map[string]string) {
	t.Helper()

	want := map[string]string{}
	err := filepath.WalkDir(goldenDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(goldenDir, path)
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		want[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("read golden tree %s (run with -update to create): %v", goldenDir, err)
	}

	for name, got := range files {
		w, ok := want[name]
		if !ok {
			t.Errorf("unexpected output file %s (run with -update if intentional)", name)
			continue
		}
		if got != w {
			t.Errorf("output mismatch for %s (run with -update if intentional):\n%s",
				name, firstDiff(w, got))
		}
		delete(want, name)
	}
	for name := range want {
		t.Errorf("missing output file %s (golden exists, nothing generated)", name)
	}
}

// firstDiff renders the first differing line with one line of context.
func firstDiff(want, got string) string {
	wl, gl := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(wl) || i < len(gl); i++ {
		w, g := line(wl, i), line(gl, i)
		if w != g {
			return fmt.Sprintf("line %d:\n  want: %q\n  got:  %q", i+1, w, g)
		}
	}
	return "contents differ"
}

func line(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return "<EOF>"
}
