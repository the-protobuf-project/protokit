package buffers

// extension.go is the one place that reaches into proto's extension machinery.
//
// It is exported because an [AnnotationReader] needs exactly this and nothing
// else: a reader's whole job is to pull its own options off a descriptor and
// spell them in this package's types, and the awkward part of that job is not the
// spelling but proto's two ways of saying "absent". Every vocabulary would
// otherwise write these same twelve lines, and a vocabulary that writes them
// slightly differently gets a typed nil where it expected a nil interface.
//
// The walk uses them for the google.api.* annotations it reads directly, which is
// the same shape from the same machinery.

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Extension reads a message-valued extension, returning the zero value when the
// option is absent.
//
// proto.GetExtension panics on a nil message and returns the extension's zero
// value — a typed nil pointer — when the option is absent, neither of which a
// caller wants to think about. This funnels both into "nil means not declared".
func Extension[T proto.Message](opts proto.Message, xt protoreflect.ExtensionType) T {
	var zero T
	if opts == nil || !opts.ProtoReflect().IsValid() {
		return zero
	}
	if !proto.HasExtension(opts, xt) {
		return zero
	}
	got, ok := proto.GetExtension(opts, xt).(T)
	if !ok {
		return zero
	}
	return got
}

// ExtensionSlice reads a repeated extension, which proto.GetExtension returns as
// a slice rather than a message.
func ExtensionSlice[T any](opts proto.Message, xt protoreflect.ExtensionType) []T {
	if opts == nil || !opts.ProtoReflect().IsValid() || !proto.HasExtension(opts, xt) {
		return nil
	}
	got, _ := proto.GetExtension(opts, xt).([]T)
	return got
}
