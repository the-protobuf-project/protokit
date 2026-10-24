package protokit

// discover.go finds the target of a reference that no table of the run matches.
// buf invokes a plugin once per input, so a module generated in another run —
// freebusy's protos in one, the RFC module's in the next — never sees the
// other's tables, and a reference across them has nothing to resolve against.
//
// Discovery looks the target up in files the run does not generate:
//
//  1. the request's own non-generated files — the import closure buf passes
//     with every request; and
//  2. reference images (Options.References): descriptor sets such as
//     `buf build buf.build/the-protobuf-project/rfc -o rfc.binpb` writes,
//     read only for lookup, never generated.
//
// Their resource tables are built with the run's own readers and layout and the
// same naming passes, so a table found this way has exactly the database,
// schema, table and column names it gets when its own module is generated. They
// are built once, on the first miss, and only resolution ever sees them.

import (
	"fmt"
	"os"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/the-protobuf-project/protokit/schema"
)

// discovery is the lookup-only side of one build.
type discovery struct {
	build func() []*schema.Database // run once, on first use
	done  bool
	dbs   []*schema.Database
	idx   map[*schema.Database]*tableIndex
}

// newDiscovery prepares discovery over p's non-generated files and the files of
// refs (already loaded; see loadReferences) that p does not carry.
func newDiscovery(p *protogen.Plugin, refs []*descriptorpb.FileDescriptorProto, readers []schema.FacetReader, layout schema.LayoutResolver) (*discovery, error) {
	var lookup *protogen.Plugin
	if len(refs) > 0 {
		req := &pluginpb.CodeGeneratorRequest{}
		for _, f := range refs {
			req.ProtoFile = append(req.ProtoFile, withGoPackage(f))
		}
		var err error
		if lookup, err = (protogen.Options{}).New(req); err != nil {
			return nil, fmt.Errorf("protokit: reference images: %w", err)
		}
	}
	return &discovery{build: func() []*schema.Database {
		byName := map[string]*schema.Database{}
		var order []*schema.Database
		add := func(ctx *buildCtx, f *protogen.File) {
			db, err := ctx.mergeFile(byName, f)
			if err != nil || db == nil || contains(order, db) {
				return // a file the run could not generate either is no target
			}
			order = append(order, db)
		}
		// The lookup build's own problems (deprecations, collisions) are not the
		// run's: its diagnostics are discarded.
		ctx := newBuildCtx(p, &diagnostics{}, readers, layout)
		for _, f := range p.Files {
			if !f.Generate {
				add(ctx, f)
			}
		}
		if lookup != nil {
			ctx := newBuildCtx(lookup, &diagnostics{}, readers, layout)
			for _, f := range lookup.Files {
				if _, inRun := p.FilesByPath[f.Desc.Path()]; !inRun {
					add(ctx, f)
				}
			}
		}
		for _, db := range order {
			if layout != nil && layout.DedupeSchemaTable() {
				deStutterTables(db)
			}
			qualifyModels(db, &diagnostics{})
		}
		return order
	}}, nil
}

// databases builds the discovered databases on first use and indexes them.
func (d *discovery) databases() ([]*schema.Database, map[*schema.Database]*tableIndex) {
	if d == nil {
		return nil, nil
	}
	if !d.done {
		d.done = true
		d.dbs = d.build()
		d.idx = make(map[*schema.Database]*tableIndex, len(d.dbs))
		for _, db := range d.dbs {
			d.idx[db] = indexTables(db)
		}
	}
	return d.dbs, d.idx
}

// loadReferences reads each reference image — a google.protobuf.FileDescriptorSet
// in binary form, which a buf image also is — and returns their files in
// dependency order, each path once (the first image to carry it wins).
func loadReferences(paths []string) ([]*descriptorpb.FileDescriptorProto, error) {
	var out []*descriptorpb.FileDescriptorProto
	seen := map[string]bool{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("protokit: reference image: %w", err)
		}
		set := &descriptorpb.FileDescriptorSet{}
		// A buf image's per-file metadata lands in unknown fields, which lookup
		// ignores; extensions in options are kept for the readers.
		if err := proto.Unmarshal(raw, set); err != nil {
			return nil, fmt.Errorf("protokit: reference image %s: %w", path, err)
		}
		for _, f := range set.GetFile() {
			if !seen[f.GetName()] {
				seen[f.GetName()] = true
				out = append(out, f)
			}
		}
	}
	return out, nil
}

// withGoPackage gives f a go_package when it has none. protogen needs one for
// every file, and a lookup build emits no Go, so any distinct path will do.
func withGoPackage(f *descriptorpb.FileDescriptorProto) *descriptorpb.FileDescriptorProto {
	if f.GetOptions().GetGoPackage() != "" {
		return f
	}
	f = proto.Clone(f).(*descriptorpb.FileDescriptorProto)
	if f.Options == nil {
		f.Options = &descriptorpb.FileOptions{}
	}
	f.Options.GoPackage = proto.String("protokit.invalid/lookup/" + f.GetName())
	return f
}
