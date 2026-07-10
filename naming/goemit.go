package naming

// goemit.go holds Go source-emission helpers shared by every code target: rendering
// a source description as Go doc comments, and turning a type name into a safe,
// lowercase Go file name.

import (
	"strconv"
	"strings"
)

// Doc renders a description as Go doc-comment lines — one `// ` line per input line,
// tab-indented so it sits above a struct field or declaration (gofmt normalizes the
// indentation). An empty or whitespace-only description yields "", so callers emit no
// comment. This carries a schema's field/type descriptions into generated code the way
// proto/GraphQL comments flow into generated structs.
func Doc(desc string) string {
	if strings.TrimSpace(desc) == "" {
		return ""
	}
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(desc, "\n"), "\n") {
		b.WriteString("\t// ")
		b.WriteString(strings.TrimRight(line, " \t"))
		b.WriteByte('\n')
	}
	return b.String()
}

// GoFileName turns a type name into a lowercase snake_case Go file name
// ("VenueKind" -> "venue_kind.go", "Kind" -> "kind.go"). Generated Go files are always
// lowercase, never PascalCase. If the trailing word is one the Go toolchain reads as an
// implicit build constraint (a GOOS/GOARCH value, or "test"), guard — a caller-chosen
// word like "enum"/"schema"/"store" — is appended so the file is not accidentally
// build-constrained; pass "" to skip the guard. used de-duplicates names that snake to
// the same base (a numeric suffix is appended) and records the result.
func GoFileName(name, guard string, used map[string]bool) string {
	s := SnakeCase(name)
	if guard != "" && constrainedSuffix[s[strings.LastIndex(s, "_")+1:]] {
		s += "_" + guard
	}
	return Unique(s, used) + ".go"
}

// Unique returns base, or base with the smallest numeric suffix (base2, base3, …)
// not already in used, and records the result. Callers use it to keep generated
// identifiers, file names, or package names from colliding within a scope.
func Unique(base string, used map[string]bool) string {
	name := base
	for i := 2; used[name]; i++ {
		name = base + strconv.Itoa(i)
	}
	used[name] = true
	return name
}

// goKeywords are the Go reserved words. They cannot be used as generated
// identifiers — e.g. a schema element named "Type" must not become `package type`.
var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
}

// GoKeyword reports whether s is a Go reserved word (invalid as a bare identifier
// or package name).
func GoKeyword(s string) bool { return goKeywords[s] }

// constrainedSuffix lists the trailing filename words the Go toolchain treats as
// implicit build constraints (GOOS/GOARCH values) or as a test-file marker.
var constrainedSuffix = map[string]bool{
	"aix": true, "android": true, "darwin": true, "dragonfly": true, "freebsd": true,
	"hurd": true, "illumos": true, "ios": true, "js": true, "linux": true, "nacl": true,
	"netbsd": true, "openbsd": true, "plan9": true, "solaris": true, "wasip1": true,
	"windows": true, "zos": true,
	"386": true, "amd64": true, "amd64p32": true, "arm": true, "arm64": true,
	"arm64be": true, "armbe": true, "loong64": true, "mips": true, "mips64": true,
	"mips64le": true, "mips64p32": true, "mips64p32le": true, "mipsle": true, "ppc": true,
	"ppc64": true, "ppc64le": true, "riscv": true, "riscv64": true, "s390": true,
	"s390x": true, "sparc": true, "sparc64": true, "wasm": true,
	"test": true,
}
