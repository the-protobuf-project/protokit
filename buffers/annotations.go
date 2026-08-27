package buffers

// annotations.go is the seam between this IR and the annotation vocabulary a
// generator owns.
//
// protokit reads AIP and nothing else — see boundary_test.go, which fails the
// build if any package here imports a plugin's generated stubs. That rule is why
// the walk cannot call `proto.GetExtension` for a `buffers.v1` option the way it
// did when this IR lived inside the plugin: the option types are declared in the
// plugin's module, and importing them would invert the dependency the engine
// exists to keep pointed one way.
//
// So the values arrive through an [AnnotationReader] the plugin registers, in the
// same shape [schema.StructureReader] uses for the schema IR: a descriptor in, a
// plain struct out. The structs below are deliberately *not* the plugin's option
// messages. They are this package's own types, they hold Go builtins and this
// package's enums, and a plugin maps its proto enums onto them on its side of the
// seam. A target downstream renders a Layout without ever learning which option
// carried it.
//
// Every method must return a usable zero value when the option is absent, which
// is what makes [NoAnnotations] a complete implementation rather than a stub: a
// .proto that declares none of these options still builds a schema, and that is
// the common robotics input rather than an edge case.

import "google.golang.org/protobuf/reflect/protoreflect"

// FileAnnotations is the file-level vocabulary the walk reads.
//
// Every name here has a derived default and none of them is required; the empty
// value means "derive it", not "emit nothing". Namespace falls back to the proto
// package, ROSPackage to the package with dots replaced, JVMPackage to the
// java_package option, and CapnpID to a hash of the file path.
type FileAnnotations struct {
	// Namespace overrides the emitted namespace. Empty derives it from the proto
	// package.
	Namespace string
	// ROSPackage overrides the ROS 2 package name.
	ROSPackage string
	// JVMPackage overrides the JVM package for the Wire target.
	JVMPackage string

	// CapnpID is a declared Cap'n Proto file ID, or 0 to derive one from the path.
	// It is int64 rather than uint64 because AIP-141 forbids unsigned types in an
	// API surface, so the vocabulary that carries it cannot spell the top bit;
	// resolveCapnpID puts that bit back.
	CapnpID int64

	// Identifier is the FlatBuffers file_identifier: exactly four bytes, or empty
	// to emit none. A wrong length is reported rather than passed to flatc.
	Identifier string
	// Extension is the FlatBuffers file_extension.
	Extension string
	// Includes are extra .fbs includes to emit verbatim.
	Includes []string
}

// MessageAnnotations is the message-level vocabulary the walk reads.
type MessageAnnotations struct {
	// Layout selects the packed or evolvable record shape. LayoutUnspecified
	// leaves the choice to resolveLayouts, which is the only safe default: see
	// layout.go for why a packed layout is never inferred.
	Layout Layout
	// CapnpID is a declared Cap'n Proto type ID, or 0 to derive one.
	CapnpID int64
	// ROSName overrides the emitted ROS type name.
	ROSName string
	// Targets restricts the message to the named targets; empty means all.
	Targets []string
	// Skip excludes the message from every target. It still consumes its slots.
	Skip bool
	// OriginalOrder emits FlatBuffers fields in declaration order.
	OriginalOrder bool
	// FBSRoot marks the message as the .fbs root_type.
	FBSRoot bool
}

// FieldAnnotations is the field-level vocabulary the walk reads.
type FieldAnnotations struct {
	// Ordinal pins the field to a target slot. Zero means unpinned — derived from
	// the ledger and the field number instead — which is why a pin of 0 cannot be
	// expressed and does not need to be: slot 0 is what the first field gets
	// anyway.
	Ordinal int32
	// Skip excludes the field. It still consumes its ordinal, because releasing
	// the slot would renumber every field after it.
	Skip bool
	// Key marks the FlatBuffers sort key.
	Key bool
	// Shared requests string sharing in the emitted buffer.
	Shared bool

	// MaxLen and FixedLen bound a string or repeated field for ROS. They are int32
	// for the same AIP-141 reason CapnpID is: unsigned is not expressible in the
	// vocabulary, so a negative value is, and it means nothing. The walk clamps
	// rather than the plugin, so every vocabulary clamps identically.
	MaxLen   int32
	FixedLen int32

	// ROSDefault is the default value literal for a ROS message field.
	ROSDefault string
	// CapnpGroup places the field in a named Cap'n Proto group.
	CapnpGroup string
	// Targets restricts the field to the named targets; empty means all.
	Targets []string
}

// EnumAnnotations is the enum-level vocabulary the walk reads.
type EnumAnnotations struct {
	// Underlying is the declared integer width. A width too narrow for a declared
	// value is reported rather than truncated.
	Underlying IntWidth
	// BitFlags emits the enum as FlatBuffers bit_flags.
	BitFlags bool
	// Skip excludes the enum from every target.
	Skip bool
}

// EnumValueAnnotations is the enum-value vocabulary the walk reads.
type EnumValueAnnotations struct {
	// Ordinal pins the value to a slot; zero means unpinned.
	Ordinal int32
	// Skip excludes the value, which still consumes its ordinal.
	Skip bool
}

// OneofAnnotations is the oneof-level vocabulary the walk reads.
type OneofAnnotations struct {
	// UnionName overrides the generated union type name. Empty derives it from the
	// parent message and the oneof name.
	UnionName string
	// Skip excludes the union.
	Skip bool
}

// ServiceAnnotations is the service-level vocabulary the walk reads.
//
// CapnpInterface and ROSService are pointers because both have a *computed*
// default that a declaration overrides in either direction — an empty service
// emits no Cap'n Proto interface unless one is asked for, and a service of pure
// publications emits no ROS service unless one is. A plain bool could not tell
// "not declared" from "declared false", and the second is a meaningful thing to
// say about both.
type ServiceAnnotations struct {
	// CapnpID is a declared Cap'n Proto interface ID, or 0 to derive one.
	CapnpID int64
	// Targets restricts the service to the named targets; empty means all.
	Targets []string
	// Skip excludes the service from every target.
	Skip bool
	// CapnpInterface forces an interface on or off; nil computes it.
	CapnpInterface *bool
	// ROSService forces a .srv on or off; nil computes it.
	ROSService *bool
}

// MethodAnnotations is the method-level vocabulary the walk reads.
type MethodAnnotations struct {
	// Ordinal pins the Cap'n Proto method ordinal; zero means unpinned.
	Ordinal int32
	// ROSName overrides the emitted ROS service or action name.
	ROSName string
	// Targets restricts the method to the named targets; empty means all.
	Targets []string
	// Skip excludes the method, which still consumes its ordinal.
	Skip bool
	// Transport declares how the method's messages move. TransportUnspecified
	// derives it from the streaming keywords.
	Transport Transport
	// Topic overrides the published topic name; empty derives it from the method.
	Topic string
}

// AnnotationReader carries a generator's own option vocabulary into this IR.
//
// It is the buffers-IR counterpart of [schema.StructureReader], and the contract
// is the same one: a descriptor in, a plain neutral struct out, a usable zero
// value whenever the option is absent, and no error return — a vocabulary that
// cannot be read is a vocabulary the plugin should not have registered, and the
// walk has nothing useful to do with the failure at the node where it surfaces.
//
// Implement it in the repository that owns the vocabulary. `buffers.v1` is the
// reference implementation; nothing here privileges it.
type AnnotationReader interface {
	ReadFile(protoreflect.FileDescriptor) FileAnnotations
	ReadMessage(protoreflect.MessageDescriptor) MessageAnnotations
	ReadField(protoreflect.FieldDescriptor) FieldAnnotations
	ReadEnum(protoreflect.EnumDescriptor) EnumAnnotations
	ReadEnumValue(protoreflect.EnumValueDescriptor) EnumValueAnnotations
	ReadOneof(protoreflect.OneofDescriptor) OneofAnnotations
	ReadService(protoreflect.ServiceDescriptor) ServiceAnnotations
	ReadMethod(protoreflect.MethodDescriptor) MethodAnnotations
}

// NoAnnotations reads nothing, which is a complete and useful behaviour rather
// than a placeholder: it is what [Build] uses when a caller registers no reader,
// and it produces the schema a plain .proto with no vocabulary at all describes.
// Every name is derived, every slot comes from the field number and the ledger,
// and every layout is evolvable.
type NoAnnotations struct{}

func (NoAnnotations) ReadFile(protoreflect.FileDescriptor) FileAnnotations { return FileAnnotations{} }
func (NoAnnotations) ReadMessage(protoreflect.MessageDescriptor) MessageAnnotations {
	return MessageAnnotations{}
}
func (NoAnnotations) ReadField(protoreflect.FieldDescriptor) FieldAnnotations {
	return FieldAnnotations{}
}
func (NoAnnotations) ReadEnum(protoreflect.EnumDescriptor) EnumAnnotations { return EnumAnnotations{} }
func (NoAnnotations) ReadEnumValue(protoreflect.EnumValueDescriptor) EnumValueAnnotations {
	return EnumValueAnnotations{}
}
func (NoAnnotations) ReadOneof(protoreflect.OneofDescriptor) OneofAnnotations {
	return OneofAnnotations{}
}
func (NoAnnotations) ReadService(protoreflect.ServiceDescriptor) ServiceAnnotations {
	return ServiceAnnotations{}
}
func (NoAnnotations) ReadMethod(protoreflect.MethodDescriptor) MethodAnnotations {
	return MethodAnnotations{}
}

// Vocabulary spells a plugin's option names for the diagnostics that tell someone
// how to fix their .proto.
//
// A hint is only useful if it names the thing to type, and protokit owns no
// annotation module, so it cannot know what that is. Six hints in this package
// would otherwise have to hardcode one vocabulary's spelling — which is the same
// mistake as importing it, minus the compile error. So the spelling arrives here,
// alongside the values.
//
// Every field is optional. An empty one falls back to a neutral description of
// the option's *effect*, which reads correctly but tells the reader less:
//
//	FieldOrdinal: "(buffers.v1.field).ordinal"
//	  → "remove one of the (buffers.v1.field).ordinal pins"
//	FieldOrdinal: ""
//	  → "remove one of the ordinal pins"
type Vocabulary struct {
	// FieldOrdinal spells the option that pins a field to a slot.
	FieldOrdinal string
	// FieldFixedLen spells the option that fixes a repeated field's length.
	FieldFixedLen string
	// EnumUnderlying spells the option that declares an enum's integer width.
	EnumUnderlying string
	// MethodSkip spells the option that excludes a method.
	MethodSkip string
	// FileROSPackage spells the option that overrides a file's ROS package.
	FileROSPackage string
}
