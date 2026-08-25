package service

// materialize.go turns message, field, and enum descriptors into IR values.

import "google.golang.org/protobuf/reflect/protoreflect"

// message materializes a message descriptor into the IR, memoized.
//
// Memoizing is not just a speed concern: a recursive message would otherwise
// recurse forever, and messages are shared by identity downstream.
func (b *builder) message(md protoreflect.MessageDescriptor) *Message {
	name := string(md.FullName())
	if existing, ok := b.messages[name]; ok {
		return existing
	}

	msg := &Message{
		FullName:  name,
		Name:      string(md.Name()),
		Package:   string(md.ParentFile().Package()),
		WellKnown: WellKnownOf(name),
		Oneofs:    map[string][]string{},
	}
	// Registered before its fields are walked, so a self-reference resolves to
	// the same value rather than recursing.
	b.messages[name] = msg

	if rd := resourceDescriptor(md); rd != nil {
		msg.Resource = b.resource(rd, name)
	}

	fields := md.Fields()
	for i := range fields.Len() {
		msg.Fields = append(msg.Fields, b.field(fields.Get(i)))
	}

	oneofs := md.Oneofs()
	for i := range oneofs.Len() {
		od := oneofs.Get(i)
		// Skip the synthetic oneof proto3 generates for `optional`: it is a
		// presence mechanism, not a choice the author wrote.
		if od.IsSynthetic() {
			continue
		}
		members := od.Fields()
		names := make([]string, 0, members.Len())
		for j := range members.Len() {
			names = append(names, string(members.Get(j).Name()))
		}
		msg.Oneofs[string(od.Name())] = names
	}
	return msg
}

// field materializes one field descriptor.
func (b *builder) field(fd protoreflect.FieldDescriptor) *Field {
	field := &Field{
		Name:     string(fd.Name()),
		JSONName: fd.JSONName(),
		Number:   int32(fd.Number()),
		Kind:     kindOf(fd),
		Repeated: fd.Cardinality() == protoreflect.Repeated && !fd.IsMap(),
		Optional: fd.HasOptionalKeyword(),
		Behavior: fieldBehaviors(fd),
		Format:   fieldFormat(fd),
	}

	if ref := resourceReference(fd); ref != nil {
		if child := ref.GetChildType(); child != "" {
			field.RefType, field.RefChild = child, true
		} else {
			field.RefType = ref.GetType()
		}
	}

	switch {
	case fd.IsMap():
		field.Kind = KindMap
		field.MapKey = b.field(fd.MapKey())
		field.MapValue = b.field(fd.MapValue())

	case fd.Kind() == protoreflect.MessageKind || fd.Kind() == protoreflect.GroupKind:
		field.Message = string(fd.Message().FullName())
		field.WellKnown = WellKnownOf(field.Message)
		// A well-known type is a leaf, not a tree: descending into Timestamp's
		// seconds and nanos would expose fields protojson does not spell.
		if field.WellKnown == WKNone {
			b.message(fd.Message())
		}

	case fd.Kind() == protoreflect.EnumKind:
		field.Enum = string(fd.Enum().FullName())
		b.enum(fd.Enum())
	}
	return field
}

// enum materializes an enum descriptor, memoized.
func (b *builder) enum(ed protoreflect.EnumDescriptor) {
	name := string(ed.FullName())
	if _, ok := b.enums[name]; ok {
		return
	}

	values := ed.Values()
	enum := &Enum{
		FullName: name,
		Name:     string(ed.Name()),
		Package:  string(ed.ParentFile().Package()),
		Values:   make([]EnumValue, 0, values.Len()),
	}
	for i := range values.Len() {
		vd := values.Get(i)
		enum.Values = append(enum.Values, EnumValue{
			Name:   string(vd.Name()),
			Number: int32(vd.Number()),
		})
	}
	b.enums[name] = enum
}

// kindOf maps a proto kind to the IR's, at proto granularity.
//
// The four 64-bit widths stay distinct because protojson renders every one as a
// JSON string while the 32-bit widths stay numbers.
func kindOf(fd protoreflect.FieldDescriptor) Kind {
	switch fd.Kind() {
	case protoreflect.DoubleKind:
		return KindDouble
	case protoreflect.FloatKind:
		return KindFloat
	case protoreflect.Int32Kind:
		return KindInt32
	case protoreflect.Int64Kind:
		return KindInt64
	case protoreflect.Uint32Kind:
		return KindUint32
	case protoreflect.Uint64Kind:
		return KindUint64
	case protoreflect.Sint32Kind:
		return KindSint32
	case protoreflect.Sint64Kind:
		return KindSint64
	case protoreflect.Fixed32Kind:
		return KindFixed32
	case protoreflect.Fixed64Kind:
		return KindFixed64
	case protoreflect.Sfixed32Kind:
		return KindSfixed32
	case protoreflect.Sfixed64Kind:
		return KindSfixed64
	case protoreflect.BoolKind:
		return KindBool
	case protoreflect.StringKind:
		return KindString
	case protoreflect.BytesKind:
		return KindBytes
	case protoreflect.EnumKind:
		return KindEnum
	}
	return KindMessage
}
