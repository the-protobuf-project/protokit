package naming

import (
	"crypto/sha1"
	"encoding/hex"
)

// MaxIdentifier is the longest identifier Postgres keeps whole: a name, or an
// enum label, of more bytes is refused (labels) or silently cut (names).
const MaxIdentifier = 63

// FitIdentifier returns s when it fits in MaxIdentifier bytes, else its first
// bytes and a short hash of the whole ("…_1a2b3c4d"), so two long identifiers
// sharing a prefix stay distinct. Deterministic: every target that stores or
// matches the identifier derives the same one.
func FitIdentifier(s string) string {
	if len(s) <= MaxIdentifier {
		return s
	}
	sum := sha1.Sum([]byte(s))
	tag := "_" + hex.EncodeToString(sum[:])[:8]
	return s[:MaxIdentifier-len(tag)] + tag
}

// EnumLabel is the stored label of enum value value of enum enumName: the value
// with the enum's prefix stripped, SCREAMING_SNAKE, fitted to MaxIdentifier.
// Every target that writes or matches a stored label uses this one derivation.
func EnumLabel(enumName, value string) string {
	return FitIdentifier(ScreamingSnake(EnumValueName(enumName, value)))
}
