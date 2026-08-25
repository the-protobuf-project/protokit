package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

// musicPlugin builds a protogen.Plugin over the example's music protos.
//
// It shells out to buf rather than vendoring a descriptor set, so the fixture
// cannot drift from the protos the example actually serves — which is the whole
// point of testing against them rather than against a hand-built one.
func musicPlugin(t *testing.T) *protogen.Plugin {
	t.Helper()

	protoDir, err := filepath.Abs("../../examples/protobuf")
	if err != nil {
		t.Fatalf("resolve proto dir: %v", err)
	}
	if _, err := os.Stat(protoDir); err != nil {
		t.Skipf("example protos not present: %v", err)
	}
	if _, err := exec.LookPath("buf"); err != nil {
		t.Skip("buf is not installed")
	}

	out := filepath.Join(t.TempDir(), "music.binpb")
	cmd := exec.Command("buf", "build", "-o", out)
	cmd.Dir = protoDir
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("buf build: %v\n%s", err, combined)
	}

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read descriptor set: %v", err)
	}
	var set descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(raw, &set); err != nil {
		t.Fatalf("unmarshal descriptor set: %v", err)
	}

	// Only the module's own files are generated; googleapis is a dependency.
	var generate []string
	for _, file := range set.File {
		if strings.HasPrefix(file.GetName(), "music/") {
			generate = append(generate, file.GetName())
		}
	}

	plugin, err := protogen.Options{}.New(&pluginpb.CodeGeneratorRequest{
		FileToGenerate: generate,
		ProtoFile:      set.File,
	})
	if err != nil {
		t.Fatalf("protogen: %v", err)
	}
	return plugin
}

// buildMusic builds the IR for the example protos.
func buildMusic(t *testing.T) *IR {
	t.Helper()
	ir, err := Build(musicPlugin(t), Options{Domain: "music.example.com"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return ir
}

// findMethod looks a method up by its unqualified name.
func findMethod(t *testing.T, ir *IR, name string) *Method {
	t.Helper()
	for _, svc := range ir.Services {
		for _, method := range svc.Methods {
			if method.Name == name {
				return method
			}
		}
	}
	t.Fatalf("no method named %q", name)
	return nil
}

func TestBuildFindsBothServices(t *testing.T) {
	ir := buildMusic(t)

	var names []string
	for _, svc := range ir.Services {
		names = append(names, svc.FullName)
	}
	want := []string{"music.v1.ArtistService", "music.v1.TrackService"}
	for _, expected := range want {
		if !contains(names, expected) {
			t.Errorf("services = %v, want one named %q", names, expected)
		}
	}
}

func TestServiceCarriesHostAndScopes(t *testing.T) {
	ir := buildMusic(t)
	svc := ir.Services[0]

	if svc.Host != "music.example.com" {
		t.Errorf("Host = %q, want %q", svc.Host, "music.example.com")
	}
	// Scope presence is what makes the OpenAPI target document 401 and 403.
	if len(svc.Scopes) != 1 {
		t.Errorf("Scopes = %v, want exactly one", svc.Scopes)
	}
}

func TestMethodsAreClassifiedByAIPPattern(t *testing.T) {
	ir := buildMusic(t)

	cases := map[string]MethodPattern{
		"GetArtist":    PatternGet,
		"ListArtists":  PatternList,
		"CreateArtist": PatternCreate,
		"UpdateArtist": PatternUpdate,
		"DeleteArtist": PatternDelete,
		"GetTrack":     PatternGet,
		"ListTracks":   PatternList,
		"CreateTrack":  PatternCreate,
		"UpdateTrack":  PatternUpdate,
		"DeleteTrack":  PatternDelete,
		// A custom verb makes it AIP-136 regardless of the name.
		"WithdrawTrack": PatternCustom,
		"WatchTracks":   PatternCustom,
	}
	for name, want := range cases {
		if got := findMethod(t, ir, name).Pattern; got != want {
			t.Errorf("%s pattern = %v, want %v", name, got, want)
		}
	}
}

func TestMutatingIsDerivedFromThePattern(t *testing.T) {
	ir := buildMusic(t)

	for _, name := range []string{"CreateArtist", "UpdateArtist", "DeleteArtist", "WithdrawTrack"} {
		if !findMethod(t, ir, name).Mutating {
			t.Errorf("%s should be mutating", name)
		}
	}
	for _, name := range []string{"GetArtist", "ListArtists", "GetTrack"} {
		if findMethod(t, ir, name).Mutating {
			t.Errorf("%s should not be mutating", name)
		}
	}
	// A custom method bound only to GET is read-only, which is the one case
	// where the author has said so unambiguously.
	if findMethod(t, ir, "WatchTracks").Mutating {
		t.Error("WatchTracks is a GET custom method and should not be mutating")
	}
}

func contains(haystack []string, needle string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}
