package service

// annotations.go reads the google.api.* vocabulary off descriptors. It is the
// only file that touches proto extension types; everything downstream works
// against the IR.

import (
	"strings"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// httpRule returns the google.api.http rule on a method, or nil.
func httpRule(md protoreflect.MethodDescriptor) *annotations.HttpRule {
	opts, ok := md.Options().(*descriptorpb.MethodOptions)
	if !ok || opts == nil {
		return nil
	}
	if !proto.HasExtension(opts, annotations.E_Http) {
		return nil
	}
	rule, _ := proto.GetExtension(opts, annotations.E_Http).(*annotations.HttpRule)
	return rule
}

// methodSignatures returns google.api.method_signature, one entry per declared
// signature, each already split on commas.
func methodSignatures(md protoreflect.MethodDescriptor) [][]string {
	opts, ok := md.Options().(*descriptorpb.MethodOptions)
	if !ok || opts == nil || !proto.HasExtension(opts, annotations.E_MethodSignature) {
		return nil
	}
	raw, _ := proto.GetExtension(opts, annotations.E_MethodSignature).([]string)

	out := make([][]string, 0, len(raw))
	for _, sig := range raw {
		fields := splitTrim(sig, ",")
		if len(fields) > 0 {
			out = append(out, fields)
		}
	}
	return out
}

// defaultHost returns google.api.default_host on a service, or "".
func defaultHost(sd protoreflect.ServiceDescriptor) string {
	opts, ok := sd.Options().(*descriptorpb.ServiceOptions)
	if !ok || opts == nil || !proto.HasExtension(opts, annotations.E_DefaultHost) {
		return ""
	}
	host, _ := proto.GetExtension(opts, annotations.E_DefaultHost).(string)
	return host
}

// oauthScopes returns google.api.oauth_scopes on a service, split on commas.
//
// Its presence is what tells the OpenAPI target to emit a security scheme and
// to document 401 and 403 on every operation, so an empty result and a missing
// annotation are meaningfully different.
func oauthScopes(sd protoreflect.ServiceDescriptor) []string {
	opts, ok := sd.Options().(*descriptorpb.ServiceOptions)
	if !ok || opts == nil || !proto.HasExtension(opts, annotations.E_OauthScopes) {
		return nil
	}
	raw, _ := proto.GetExtension(opts, annotations.E_OauthScopes).(string)
	return splitTrim(raw, ",")
}

// resourceDescriptor returns google.api.resource on a message, or nil.
func resourceDescriptor(md protoreflect.MessageDescriptor) *annotations.ResourceDescriptor {
	opts, ok := md.Options().(*descriptorpb.MessageOptions)
	if !ok || opts == nil || !proto.HasExtension(opts, annotations.E_Resource) {
		return nil
	}
	rd, _ := proto.GetExtension(opts, annotations.E_Resource).(*annotations.ResourceDescriptor)
	return rd
}

// resourceReference returns google.api.resource_reference on a field, or nil.
func resourceReference(fd protoreflect.FieldDescriptor) *annotations.ResourceReference {
	opts, ok := fd.Options().(*descriptorpb.FieldOptions)
	if !ok || opts == nil || !proto.HasExtension(opts, annotations.E_ResourceReference) {
		return nil
	}
	ref, _ := proto.GetExtension(opts, annotations.E_ResourceReference).(*annotations.ResourceReference)
	return ref
}

// fieldBehaviors returns google.api.field_behavior on a field (AIP-203),
// translated into the IR's own enum.
func fieldBehaviors(fd protoreflect.FieldDescriptor) []Behavior {
	opts, ok := fd.Options().(*descriptorpb.FieldOptions)
	if !ok || opts == nil || !proto.HasExtension(opts, annotations.E_FieldBehavior) {
		return nil
	}
	raw, _ := proto.GetExtension(opts, annotations.E_FieldBehavior).([]annotations.FieldBehavior)

	out := make([]Behavior, 0, len(raw))
	for _, b := range raw {
		if translated, ok := behaviorByProto[b]; ok {
			out = append(out, translated)
		}
	}
	return out
}

// behaviorByProto maps the proto enum to the IR's. Translating rather than
// aliasing keeps the IR free of the annotation package, which is what lets a
// second annotation vocabulary be added later without changing these types.
var behaviorByProto = map[annotations.FieldBehavior]Behavior{
	annotations.FieldBehavior_OPTIONAL:          BehaviorOptional,
	annotations.FieldBehavior_REQUIRED:          BehaviorRequired,
	annotations.FieldBehavior_OUTPUT_ONLY:       BehaviorOutputOnly,
	annotations.FieldBehavior_INPUT_ONLY:        BehaviorInputOnly,
	annotations.FieldBehavior_IMMUTABLE:         BehaviorImmutable,
	annotations.FieldBehavior_UNORDERED_LIST:    BehaviorUnorderedList,
	annotations.FieldBehavior_NON_EMPTY_DEFAULT: BehaviorNonEmptyDefault,
	annotations.FieldBehavior_IDENTIFIER:        BehaviorIdentifier,
}

// fieldFormat returns google.api.field_info's format token, e.g. "UUID4".
func fieldFormat(fd protoreflect.FieldDescriptor) string {
	opts, ok := fd.Options().(*descriptorpb.FieldOptions)
	if !ok || opts == nil || !proto.HasExtension(opts, annotations.E_FieldInfo) {
		return ""
	}
	info, _ := proto.GetExtension(opts, annotations.E_FieldInfo).(*annotations.FieldInfo)
	if info == nil {
		return ""
	}
	return strings.TrimPrefix(info.GetFormat().String(), "FORMAT_")
}

// splitTrim splits and trims, dropping empties. A trailing separator in an
// annotation is common enough that it should not produce a phantom entry.
func splitTrim(raw, sep string) []string {
	parts := strings.Split(raw, sep)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
