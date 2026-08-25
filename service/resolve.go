package service

// resolve.go walks field paths and materializes messages into the IR.

import (
	"fmt"
	"strings"
)

// resolveFieldPath walks a dotted proto field path from a message.
//
// Every step but the last must be a singular message: `book.author.name` needs
// `book` and `author` to be messages, and a repeated one has no single value to
// descend into.
func (b *builder) resolveFieldPath(msg *Message, parts []string) (*FieldPath, error) {
	if msg == nil {
		return nil, fmt.Errorf("no message to resolve against")
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty field path")
	}

	fields := make([]*Field, 0, len(parts))
	current := msg

	for i, name := range parts {
		if current == nil {
			return nil, fmt.Errorf("%q is not a message, so %q cannot be reached through it",
				strings.Join(parts[:i], "."), strings.Join(parts, "."))
		}
		field := current.Field(name)
		if field == nil {
			return nil, fmt.Errorf("message %s has no field %q", current.FullName, name)
		}
		fields = append(fields, field)

		if i == len(parts)-1 {
			break
		}
		if field.Repeated {
			return nil, fmt.Errorf("field %q is repeated, so %q cannot be reached through it",
				name, strings.Join(parts, "."))
		}
		current = b.messages[field.Message]
	}
	return b.fieldPath(fields), nil
}

// fieldPath builds a FieldPath from resolved fields, computing both spellings.
func (b *builder) fieldPath(fields []*Field) *FieldPath {
	proto := make([]string, len(fields))
	json := make([]string, len(fields))
	for i, field := range fields {
		proto[i] = field.Name
		json[i] = field.JSONName
	}
	return &FieldPath{
		Proto:  proto,
		JSON:   strings.Join(json, "."),
		Fields: fields,
	}
}
