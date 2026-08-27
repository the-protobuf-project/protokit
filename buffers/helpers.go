package buffers

// helpers.go holds the two small normalizations the walk applies to values that
// arrive from an [AnnotationReader].
//
// Both live here rather than on the plugin's side of the seam so that every
// vocabulary normalizes identically. A reader's job is to spell its own options
// in this package's types; it is not also to remember that an empty name means
// "derive one" and that a negative bound means nothing.

// orDefault returns got, or fallback when got is empty.
func orDefault(got, fallback string) string {
	if got == "" {
		return fallback
	}
	return got
}

// nonNegative clamps a bound to zero. The bounds are int32 because AIP-141
// forbids unsigned types in an API surface, so a negative value is expressible
// and means nothing.
func nonNegative(n int32) uint32 {
	if n < 0 {
		return 0
	}
	return uint32(n)
}
