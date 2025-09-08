package builder

import (
	"fmt"

	"github.com/tinyrange/tinyrange/pkg/common"
	"github.com/tinyrange/tinyrange/pkg/filesystem"
	"github.com/tinyrange/tinyrange/pkg/hash"
	pb "github.com/tinyrange/tinyrange/pkg/proto"
)

// ensure definitions implement hash.ProtoMarshaler where needed
var (
	_ hash.ProtoMarshaler = (*buildFsDefinition)(nil)
	_ hash.ProtoMarshaler = (*buildVmDefinition)(nil)
	_ hash.ProtoMarshaler = (*fetchHttpBuildDefinition)(nil)
	_ hash.ProtoMarshaler = (*fetchOciImageDefinition)(nil)
	_ hash.ProtoMarshaler = (*constantHashDefinition)(nil)
	_ hash.ProtoMarshaler = (*starBuildDefinition)(nil)
)

// directiveToProto converts a common.Directive to its protobuf representation.
func directiveToProto(db *hash.DefinitionDatabase, d common.Directive) (*pb.Directive, error) {
	switch v := d.(type) {
	case common.DirectiveRunCommand:
		return &pb.Directive{Directive: &pb.Directive_RunCommand{RunCommand: &pb.DirectiveRunCommand{Command: v.Command, Raw: v.Raw}}}, nil
	case common.DirectiveStartServiceCommand:
		return &pb.Directive{Directive: &pb.Directive_StartServiceCommand{StartServiceCommand: &pb.DirectiveStartServiceCommand{Command: v.Command}}}, nil
	case common.DirectiveRunStarlarkScript:
		return &pb.Directive{Directive: &pb.Directive_RunStarlarkScript{RunStarlarkScript: &pb.DirectiveRunStarlarkScript{Script: v.Script}}}, nil
	case common.DirectiveAddFile:
		var ref *pb.BuildDefinitionRef
		if v.Definition != nil {
			h, err := db.HashDefinition(v.Definition)
			if err != nil {
				return nil, err
			}
			ref = &pb.BuildDefinitionRef{Hash: h.String()}
		}
		return &pb.Directive{Directive: &pb.Directive_AddFile{AddFile: &pb.DirectiveAddFile{Filename: v.Filename, Definition: ref, Contents: v.Contents, Executable: v.Executable}}}, nil
	case common.DirectiveLocalFile:
		return &pb.Directive{Directive: &pb.Directive_LocalFile{LocalFile: &pb.DirectiveLocalFile{Filename: v.Filename, HostFilename: v.HostFilename}}}, nil
	case common.DirectiveLocalDirectory:
		return &pb.Directive{Directive: &pb.Directive_LocalDirectory{LocalDirectory: &pb.DirectiveLocalDirectory{HostDirectory: v.HostDirectory, GuestDirectory: v.GuestDirectory}}}, nil
	case common.DirectiveArchive:
		h, err := db.HashDefinition(v.Definition)
		if err != nil {
			return nil, err
		}
		return &pb.Directive{Directive: &pb.Directive_Archive{Archive: &pb.DirectiveArchive{Definition: &pb.BuildDefinitionRef{Hash: h.String()}, Target: v.Target, Archive2: v.Archive2}}}, nil
	case common.DirectiveExportPort:
		return &pb.Directive{Directive: &pb.Directive_ExportPort{ExportPort: &pb.DirectiveExportPort{Name: v.Name, ListenAddress: v.ListenAddress, Port: int32(v.Port)}}}, nil
	case common.DirectiveEnvironment:
		return &pb.Directive{Directive: &pb.Directive_Environment{Environment: &pb.DirectiveEnvironment{Variables: v.Variables}}}, nil
	case common.DirectiveBuiltin:
		return &pb.Directive{Directive: &pb.Directive_Builtin{Builtin: &pb.DirectiveBuiltin{Name: v.Name, Architecture: v.Architecture, GuestFilename: v.GuestFilename}}}, nil
	case common.DirectiveList:
		var items []*pb.Directive
		for _, it := range v.Items {
			pd, err := directiveToProto(db, it)
			if err != nil {
				return nil, err
			}
			items = append(items, pd)
		}
		return &pb.Directive{Directive: &pb.Directive_List{List: &pb.DirectiveList{Items: items}}}, nil
	case common.DirectiveAddPackage:
		return &pb.Directive{Directive: &pb.Directive_AddPackage{AddPackage: &pb.DirectiveAddPackage{PackageQuery: v.Name.String()}}}, nil
	case common.DirectiveInteraction:
		return &pb.Directive{Directive: &pb.Directive_Interaction{Interaction: &pb.DirectiveInteraction{Interaction: v.Interaction}}}, nil
	case common.DirectiveDefaultInteractive:
		return &pb.Directive{Directive: &pb.Directive_DefaultInteractive{DefaultInteractive: &pb.DirectiveDefaultInteractive{InteractiveCommand: v.InteractiveCommand}}}, nil
	case common.DirectiveMountHostDirectory:
		return &pb.Directive{Directive: &pb.Directive_MountHostDirectory{MountHostDirectory: &pb.DirectiveMountHostDirectory{HostDirectory: v.HostDirectory, GuestDirectory: v.GuestDirectory, Port: int32(v.Port), Writable: v.Writable}}}, nil
	case common.DirectiveKernel:
		var kref, iref *pb.BuildDefinitionRef
		if v.Kernel != nil {
			h, err := db.HashDefinition(v.Kernel)
			if err != nil {
				return nil, err
			}
			kref = &pb.BuildDefinitionRef{Hash: h.String()}
		}
		if v.Initramfs != nil {
			h, err := db.HashDefinition(v.Initramfs)
			if err != nil {
				return nil, err
			}
			iref = &pb.BuildDefinitionRef{Hash: h.String()}
		}
		return &pb.Directive{Directive: &pb.Directive_Kernel{Kernel: &pb.DirectiveKernel{Kernel: kref, Initramfs: iref}}}, nil
	case common.DirectiveAddInitScript:
		return &pb.Directive{Directive: &pb.Directive_AddInitScript{AddInitScript: &pb.DirectiveAddInitScript{GuestFilename: v.GuestFilename}}}, nil
	case common.DirectiveAddVolume:
		return &pb.Directive{Directive: &pb.Directive_AddVolume{AddVolume: &pb.DirectiveAddVolume{VolumeName: v.VolumeName, GuestPath: v.GuestPath, MinimumSizeMb: uint64(v.MinimumSizeMB), Persist: v.Persist}}}, nil
	default:
		// Some directives are also BuildDefinitions (e.g., *planDefinition).
		// For hashing, represent them as a definition reference inside an
		// Archive directive (definition field only). This mirrors legacy
		// behavior where definitions inside directive lists hashed by ref.
		if bd, ok := d.(common.BuildDefinition); ok {
			h, err := db.HashDefinition(bd)
			if err != nil {
				return nil, err
			}
			return &pb.Directive{
				Directive: &pb.Directive_Reference{
					Reference: &pb.DirectiveReference{
						Definition: &pb.BuildDefinitionRef{Hash: h.String()},
					},
				},
			}, nil
		}
		return nil, fmt.Errorf("directive to proto not implemented: %T", d)
	}
}

func (def *buildFsDefinition) ToProto(db *hash.DefinitionDatabase) (*pb.BuildDefinition, error) {
	var dirs []*pb.Directive
	for _, d := range def.params.Directives {
		pd, err := directiveToProto(db, d)
		if err != nil {
			return nil, err
		}
		dirs = append(dirs, pd)
	}
	return &pb.BuildDefinition{Definition: &pb.BuildDefinition_BuildFs{BuildFs: &pb.BuildFsParameters{
		Directives: dirs,
		Kind:       def.params.Kind,
	}}}, nil
}

func (def *buildVmDefinition) ToProto(db *hash.DefinitionDatabase) (*pb.BuildDefinition, error) {
	p := &pb.BuildVmParameters{
		OutputFile:       def.params.OutputFile,
		Architecture:     def.params.Architecture,
		RootArchitecture: def.params.RootArchitecture,
		CpuCores:         int32(def.params.CpuCores),
		MemoryMb:         int32(def.params.MemoryMB),
		AutoScale:        def.params.AutoScale,
		StorageSize:      int32(def.params.StorageSize),
		Interaction:      def.params.Interaction,
		HistoryKey:       def.params.HistoryKey,
		Debug:            def.params.Debug,
	}

	// Kernel reference
	if def.params.Kernel != nil {
		h, err := db.HashDefinition(def.params.Kernel)
		if err != nil {
			return nil, err
		}
		p.Kernel = &pb.BuildDefinitionRef{Hash: h.String()}
	}
	if def.params.InitRamFs != nil {
		h, err := db.HashDefinition(def.params.InitRamFs)
		if err != nil {
			return nil, err
		}
		p.InitRamFs = &pb.BuildDefinitionRef{Hash: h.String()}
	}

	// Directives (not needed by current tests; include conversion for completeness if present)
	if len(def.params.Directives) > 0 {
		for _, d := range def.params.Directives {
			pd, err := directiveToProto(db, d)
			if err != nil {
				return nil, err
			}
			p.Directives = append(p.Directives, pd)
		}
	}

	return &pb.BuildDefinition{Definition: &pb.BuildDefinition_BuildVm{BuildVm: p}}, nil
}

func (def *fetchHttpBuildDefinition) ToProto(db *hash.DefinitionDatabase) (*pb.BuildDefinition, error) {
	return &pb.BuildDefinition{Definition: &pb.BuildDefinition_FetchHttp{FetchHttp: &pb.FetchHttpParameters{
		Url:        def.params.Url,
		ExpireTime: def.params.ExpireTime,
		Headers:    def.params.Headers,
	}}}, nil
}

func (def *fetchOciImageDefinition) ToProto(db *hash.DefinitionDatabase) (*pb.BuildDefinition, error) {
	return &pb.BuildDefinition{Definition: &pb.BuildDefinition_FetchOciImage{FetchOciImage: &pb.FetchOciImageParameters{
		Registry:     def.params.Registry,
		Image:        def.params.Image,
		Tag:          def.params.Tag,
		Architecture: def.params.Architecture,
	}}}, nil
}

func (def *constantHashDefinition) ToProto(db *hash.DefinitionDatabase) (*pb.BuildDefinition, error) {
	return &pb.BuildDefinition{Definition: &pb.BuildDefinition_ConstantHash{ConstantHash: &pb.ConstantHashParameters{Hash: def.params.Hash}}}, nil
}

// Additional definitions

func (def *buildEmulatorDefinition) ToProto(db *hash.DefinitionDatabase) (*pb.BuildDefinition, error) {
	p := &pb.BuildEmulatorParameters{OutputFile: def.params.OutputFile, ScriptFilename: def.params.ScriptFilename, CreateName: def.params.CreateName}
	for _, d := range def.params.Directives {
		pd, err := directiveToProto(db, d)
		if err != nil {
			return nil, err
		}
		p.Directives = append(p.Directives, pd)
	}
	return &pb.BuildDefinition{Definition: &pb.BuildDefinition_BuildEmulator{BuildEmulator: p}}, nil
}

func (def *decompressFileBuildDefinition) ToProto(db *hash.DefinitionDatabase) (*pb.BuildDefinition, error) {
	if def.params.Base == nil {
		return nil, fmt.Errorf("decompress base is nil")
	}
	h, err := db.HashDefinition(def.params.Base)
	if err != nil {
		return nil, err
	}
	return &pb.BuildDefinition{Definition: &pb.BuildDefinition_DecompressFile{DecompressFile: &pb.DecompressFileParameters{Base: &pb.BuildDefinitionRef{Hash: h.String()}, Kind: def.params.Kind}}}, nil
}

func (def *registryRequestDefinition) ToProto(db *hash.DefinitionDatabase) (*pb.BuildDefinition, error) {
	return &pb.BuildDefinition{Definition: &pb.BuildDefinition_RegistryRequest{RegistryRequest: &pb.RegistryRequestParameters{Url: def.params.Url, ExpireTime: def.params.ExpireTime, Accept: def.params.Accept}}}, nil
}

func (def *fetchCvmfsDefinition) ToProto(db *hash.DefinitionDatabase) (*pb.BuildDefinition, error) {
	return &pb.BuildDefinition{Definition: &pb.BuildDefinition_FetchCvmfs{FetchCvmfs: &pb.FetchCVMFSParameters{Mirror: def.params.Mirror, Repo: def.params.Repo, Path: def.params.Path}}}, nil
}

func (def *readOciImageDefinition) ToProto(db *hash.DefinitionDatabase) (*pb.BuildDefinition, error) {
	if def.params.Base == nil {
		return nil, fmt.Errorf("read oci image base is nil")
	}
	h, err := db.HashDefinition(def.params.Base)
	if err != nil {
		return nil, err
	}
	return &pb.BuildDefinition{Definition: &pb.BuildDefinition_ReadOciImage{ReadOciImage: &pb.ReadOciImageParameters{Base: &pb.BuildDefinitionRef{Hash: h.String()}}}}, nil
}

func (def *extractFileDefinition) ToProto(db *hash.DefinitionDatabase) (*pb.BuildDefinition, error) {
	if def.params.Base == nil {
		return nil, fmt.Errorf("extract file base is nil")
	}
	h, err := db.HashDefinition(def.params.Base)
	if err != nil {
		return nil, err
	}
	return &pb.BuildDefinition{Definition: &pb.BuildDefinition_ExtractFile{ExtractFile: &pb.ExtractFileParameters{Base: &pb.BuildDefinitionRef{Hash: h.String()}, Name: def.params.Name}}}, nil
}

func (def *planDefinition) ToProto(db *hash.DefinitionDatabase) (*pb.BuildDefinition, error) {
	p := &pb.PlanParameters{Builder: def.params.Builder, Architecture: string(def.params.Architecture)}
	// Convert search list
	for _, q := range def.params.Search {
		p.Search = append(p.Search, &pb.PackageQuery{MatchDirect: q.MatchDirect, Name: q.Name, MatchPartialName: q.MatchPartialName, Version: q.Version, Tags: q.Tags})
	}
	p.TagList = append(p.TagList, def.params.TagList...)
	return &pb.BuildDefinition{Definition: &pb.BuildDefinition_Plan{Plan: p}}, nil
}

func (def *readArchiveBuildDefinition) ToProto(db *hash.DefinitionDatabase) (*pb.BuildDefinition, error) {
	if def.params.Base == nil {
		return nil, fmt.Errorf("read archive base is nil")
	}
	h, err := db.HashDefinition(def.params.Base)
	if err != nil {
		return nil, err
	}
	return &pb.BuildDefinition{Definition: &pb.BuildDefinition_ReadArchive{ReadArchive: &pb.ReadArchiveParameters{Base: &pb.BuildDefinitionRef{Hash: h.String()}, Kind: def.params.Kind, StripComponents: int32(def.params.StripComponents)}}}, nil
}

func (def *readArchive2BuildDefinition) ToProto(db *hash.DefinitionDatabase) (*pb.BuildDefinition, error) {
	// Represent identically to ReadArchive; distinction is in Go type, not proto.
	if def.params.Base == nil {
		return nil, fmt.Errorf("read archive2 base is nil")
	}
	h, err := db.HashDefinition(def.params.Base)
	if err != nil {
		return nil, err
	}
	return &pb.BuildDefinition{Definition: &pb.BuildDefinition_ReadArchive{ReadArchive: &pb.ReadArchiveParameters{Base: &pb.BuildDefinitionRef{Hash: h.String()}, Kind: def.params.Kind, StripComponents: int32(def.params.StripComponents)}}}, nil
}

func (def *fetchOciImageDefinitionV2) ToProto(db *hash.DefinitionDatabase) (*pb.BuildDefinition, error) {
	return &pb.BuildDefinition{Definition: &pb.BuildDefinition_FetchOciImage{FetchOciImage: &pb.FetchOciImageParameters{Registry: def.params.Registry, Image: def.params.Image, Tag: def.params.Tag, Architecture: def.params.Architecture}}}, nil
}

// Convert hash.SerializableValue recursively into pb.SerializableValue
func toProtoValue(db *hash.DefinitionDatabase, v hash.SerializableValue) (*pb.SerializableValue, error) {
	switch x := v.(type) {
	case hash.SerializableString:
		return &pb.SerializableValue{Kind: &pb.SerializableValue_StringValue{StringValue: string(x)}}, nil
	case hash.SerializableBool:
		return &pb.SerializableValue{Kind: &pb.SerializableValue_BoolValue{BoolValue: bool(x)}}, nil
	case hash.SerializableList:
		var items []*pb.SerializableValue
		for _, it := range x {
			pv, err := toProtoValue(db, it)
			if err != nil {
				return nil, err
			}
			items = append(items, pv)
		}
		return &pb.SerializableValue{Kind: &pb.SerializableValue_ListValue{ListValue: &pb.SerializableList{Items: items}}}, nil
	default:
		// BuildDefinition reference
		if bd, ok := v.(common.BuildDefinition); ok {
			h, err := db.HashDefinition(bd)
			if err != nil {
				return nil, err
			}
			return &pb.SerializableValue{Kind: &pb.SerializableValue_DefinitionRef{DefinitionRef: &pb.BuildDefinitionRef{Hash: h.String()}}}, nil
		}
		// filesystem.ChildSource
		if cs, ok := v.(filesystem.ChildSource); ok {
			var child pb.ChildSource
			if bd, ok := cs.Source.(common.BuildDefinition); ok {
				h, err := db.HashDefinition(bd)
				if err != nil {
					return nil, err
				}
				child.SourceDef = &pb.BuildDefinitionRef{Hash: h.String()}
			} else if cs.Source != nil {
				pv, err := toProtoValue(db, cs.Source)
				if err != nil {
					return nil, err
				}
				child.SourceVal = pv
			}
			child.Name = cs.Name
			return &pb.SerializableValue{Kind: &pb.SerializableValue_ChildSource{ChildSource: &child}}, nil
		}
		return nil, fmt.Errorf("toProtoValue: unsupported type %T", v)
	}
}

// Star definitions: encode script filename, builder name, and arguments.
func (def *starBuildDefinition) ToProto(db *hash.DefinitionDatabase) (*pb.BuildDefinition, error) {
	p := &pb.StarParameters{ScriptFilename: def.params.ScriptFilename, BuilderName: def.params.BuilderName}
	for _, a := range def.params.Arguments {
		pv, err := toProtoValue(db, a)
		if err != nil {
			return nil, err
		}
		p.Arguments = append(p.Arguments, pv)
	}
	return &pb.BuildDefinition{Definition: &pb.BuildDefinition_Star{Star: p}}, nil
}

// Inverse conversions (protobuf -> params)

func fromProtoValue(db *hash.DefinitionDatabase, v *pb.SerializableValue) (hash.SerializableValue, error) {
	switch k := v.GetKind().(type) {
	case *pb.SerializableValue_StringValue:
		return hash.SerializableString(k.StringValue), nil
	case *pb.SerializableValue_BoolValue:
		return hash.SerializableBool(k.BoolValue), nil
	case *pb.SerializableValue_ListValue:
		var lst hash.SerializableList
		for _, it := range k.ListValue.GetItems() {
			cv, err := fromProtoValue(db, it)
			if err != nil {
				return nil, err
			}
			lst = append(lst, cv)
		}
		return lst, nil
	case *pb.SerializableValue_DefinitionRef:
		def, err := db.GetDefinitionByHash(hash.Hash(k.DefinitionRef.GetHash()))
		if err != nil {
			return nil, err
		}
		return def, nil
	case *pb.SerializableValue_ChildSource:
		var src hash.SerializableValue
		if k.ChildSource.GetSourceDef() != nil {
			def, err := db.GetDefinitionByHash(hash.Hash(k.ChildSource.GetSourceDef().GetHash()))
			if err != nil {
				return nil, err
			}
			src = def
		} else if k.ChildSource.GetSourceVal() != nil {
			val, err := fromProtoValue(db, k.ChildSource.GetSourceVal())
			if err != nil {
				return nil, err
			}
			src = val
		}
		return filesystem.ChildSource{Source: src, Name: k.ChildSource.GetName()}, nil
	default:
		return nil, fmt.Errorf("fromProtoValue: unsupported kind %T", v.GetKind())
	}
}

func fromProtoDirective(db *hash.DefinitionDatabase, d *pb.Directive) (common.Directive, error) {
	switch v := d.GetDirective().(type) {
	case *pb.Directive_RunCommand:
		rc := v.RunCommand
		return common.DirectiveRunCommand{Command: rc.GetCommand(), Raw: rc.GetRaw()}, nil
	case *pb.Directive_StartServiceCommand:
		return common.DirectiveStartServiceCommand{Command: v.StartServiceCommand.GetCommand()}, nil
	case *pb.Directive_RunStarlarkScript:
		return common.DirectiveRunStarlarkScript{Script: v.RunStarlarkScript.GetScript()}, nil
	case *pb.Directive_AddFile:
		var def common.BuildDefinition
		if v.AddFile.GetDefinition() != nil {
			d2, err := db.GetDefinitionByHash(hash.Hash(v.AddFile.GetDefinition().GetHash()))
			if err != nil {
				return nil, err
			}
			var ok bool
			def, ok = d2.(common.BuildDefinition)
			if !ok {
				return nil, fmt.Errorf("hash %s is not BuildDefinition", v.AddFile.GetDefinition().GetHash())
			}
		}
		return common.DirectiveAddFile{Filename: v.AddFile.GetFilename(), Definition: def, Contents: v.AddFile.GetContents(), Executable: v.AddFile.GetExecutable()}, nil
	case *pb.Directive_LocalFile:
		return common.DirectiveLocalFile{Filename: v.LocalFile.GetFilename(), HostFilename: v.LocalFile.GetHostFilename()}, nil
	case *pb.Directive_LocalDirectory:
		return common.DirectiveLocalDirectory{HostDirectory: v.LocalDirectory.GetHostDirectory(), GuestDirectory: v.LocalDirectory.GetGuestDirectory()}, nil
	case *pb.Directive_Archive:
		var def common.BuildDefinition
		if v.Archive.GetDefinition() != nil {
			d2, err := db.GetDefinitionByHash(hash.Hash(v.Archive.GetDefinition().GetHash()))
			if err != nil {
				return nil, err
			}
			var ok bool
			def, ok = d2.(common.BuildDefinition)
			if !ok {
				return nil, fmt.Errorf("hash %s is not BuildDefinition", v.Archive.GetDefinition().GetHash())
			}
		}
		return common.DirectiveArchive{Definition: def, Target: v.Archive.GetTarget(), Archive2: v.Archive.GetArchive2()}, nil
	case *pb.Directive_ExportPort:
		ep := v.ExportPort
		return common.DirectiveExportPort{Name: ep.GetName(), ListenAddress: ep.GetListenAddress(), Port: int(ep.GetPort())}, nil
	case *pb.Directive_Environment:
		return common.DirectiveEnvironment{Variables: append([]string(nil), v.Environment.GetVariables()...)}, nil
	case *pb.Directive_Builtin:
		b := v.Builtin
		return common.DirectiveBuiltin{Name: b.GetName(), Architecture: b.GetArchitecture(), GuestFilename: b.GetGuestFilename()}, nil
	case *pb.Directive_List:
		var items []common.Directive
		for _, it := range v.List.GetItems() {
			cd, err := fromProtoDirective(db, it)
			if err != nil {
				return nil, err
			}
			items = append(items, cd)
		}
		return common.DirectiveList{Items: items}, nil
	case *pb.Directive_AddPackage:
		pq, err := common.ParsePackageQuery(v.AddPackage.GetPackageQuery())
		if err != nil {
			return nil, err
		}
		return common.DirectiveAddPackage{Name: pq}, nil
	case *pb.Directive_Interaction:
		return common.DirectiveInteraction{Interaction: v.Interaction.GetInteraction()}, nil
	case *pb.Directive_DefaultInteractive:
		return common.DirectiveDefaultInteractive{InteractiveCommand: append([]string(nil), v.DefaultInteractive.GetInteractiveCommand()...)}, nil
	case *pb.Directive_MountHostDirectory:
		m := v.MountHostDirectory
		return common.DirectiveMountHostDirectory{HostDirectory: m.GetHostDirectory(), GuestDirectory: m.GetGuestDirectory(), Port: int(m.GetPort()), Writable: m.GetWritable()}, nil
	case *pb.Directive_Kernel:
		var k, i common.BuildDefinition
		if v.Kernel.GetKernel() != nil {
			d2, err := db.GetDefinitionByHash(hash.Hash(v.Kernel.GetKernel().GetHash()))
			if err != nil {
				return nil, err
			}
			var ok bool
			k, ok = d2.(common.BuildDefinition)
			if !ok {
				return nil, fmt.Errorf("hash %s is not BuildDefinition", v.Kernel.GetKernel().GetHash())
			}
		}
		if v.Kernel.GetInitramfs() != nil {
			d2, err := db.GetDefinitionByHash(hash.Hash(v.Kernel.GetInitramfs().GetHash()))
			if err != nil {
				return nil, err
			}
			var ok bool
			i, ok = d2.(common.BuildDefinition)
			if !ok {
				return nil, fmt.Errorf("hash %s is not BuildDefinition", v.Kernel.GetInitramfs().GetHash())
			}
		}
		return common.DirectiveKernel{Kernel: k, Initramfs: i}, nil
	case *pb.Directive_AddInitScript:
		return common.DirectiveAddInitScript{GuestFilename: v.AddInitScript.GetGuestFilename()}, nil
	case *pb.Directive_AddVolume:
		a := v.AddVolume
		return common.DirectiveAddVolume{VolumeName: a.GetVolumeName(), GuestPath: a.GetGuestPath(), MinimumSizeMB: a.GetMinimumSizeMb(), Persist: a.GetPersist()}, nil
	case *pb.Directive_Reference:
		if v.Reference.GetDefinition() == nil {
			return nil, fmt.Errorf("reference directive missing definition")
		}
		d2, err := db.GetDefinitionByHash(hash.Hash(v.Reference.GetDefinition().GetHash()))
		if err != nil {
			return nil, err
		}
		// Ensure it's a BuildDefinition
		bd, ok := d2.(common.Directive)
		if !ok {
			return nil, fmt.Errorf("hash %s is not BuildDefinition", v.Reference.GetDefinition().GetHash())
		}
		return bd, nil
	default:
		return nil, fmt.Errorf("fromProtoDirective: unsupported %T", d.GetDirective())
	}
}

// Register proto unmarshallers for all definitions
func init() {
	hash.RegisterProto(&buildFsDefinition{}, (*pb.BuildFsParameters)(nil), func(db *hash.DefinitionDatabase, msg any) (hash.SerializableValue, error) {
		p := msg.(*pb.BuildFsParameters)
		var dirs []common.Directive
		for _, d := range p.GetDirectives() {
			cd, err := fromProtoDirective(db, d)
			if err != nil {
				return nil, err
			}
			dirs = append(dirs, cd)
		}
		return BuildFsParameters{Directives: dirs, Kind: p.GetKind()}, nil
	})

	hash.RegisterProto(&buildVmDefinition{}, (*pb.BuildVmParameters)(nil), func(db *hash.DefinitionDatabase, msg any) (hash.SerializableValue, error) {
		p := msg.(*pb.BuildVmParameters)
		var dirs []common.Directive
		for _, d := range p.GetDirectives() {
			cd, err := fromProtoDirective(db, d)
			if err != nil {
				return nil, err
			}
			dirs = append(dirs, cd)
		}
		var kernel, initramfs common.BuildDefinition
		if p.GetKernel() != nil {
			d2, err := db.GetDefinitionByHash(hash.Hash(p.GetKernel().GetHash()))
			if err != nil {
				return nil, err
			}
			var ok bool
			kernel, ok = d2.(common.BuildDefinition)
			if !ok {
				return nil, fmt.Errorf("kernel ref %s is not BuildDefinition", p.GetKernel().GetHash())
			}
		}
		if p.GetInitRamFs() != nil {
			d2, err := db.GetDefinitionByHash(hash.Hash(p.GetInitRamFs().GetHash()))
			if err != nil {
				return nil, err
			}
			var ok bool
			initramfs, ok = d2.(common.BuildDefinition)
			if !ok {
				return nil, fmt.Errorf("initramfs ref %s is not BuildDefinition", p.GetInitRamFs().GetHash())
			}
		}
		return BuildVmParameters{
			Directives:       dirs,
			OutputFile:       p.GetOutputFile(),
			Architecture:     p.GetArchitecture(),
			RootArchitecture: p.GetRootArchitecture(),
			Kernel:           kernel,
			InitRamFs:        initramfs,
			CpuCores:         int(p.GetCpuCores()),
			MemoryMB:         int(p.GetMemoryMb()),
			AutoScale:        p.GetAutoScale(),
			StorageSize:      int(p.GetStorageSize()),
			Interaction:      p.GetInteraction(),
			HistoryKey:       p.GetHistoryKey(),
			Debug:            p.GetDebug(),
		}, nil
	})

	hash.RegisterProto(&fetchHttpBuildDefinition{}, (*pb.FetchHttpParameters)(nil), func(db *hash.DefinitionDatabase, msg any) (hash.SerializableValue, error) {
		p := msg.(*pb.FetchHttpParameters)
		return FetchHttpParameters{Url: p.GetUrl(), ExpireTime: p.GetExpireTime(), Headers: p.GetHeaders()}, nil
	})

	hash.RegisterProto(&fetchOciImageDefinition{}, (*pb.FetchOciImageParameters)(nil), func(db *hash.DefinitionDatabase, msg any) (hash.SerializableValue, error) {
		p := msg.(*pb.FetchOciImageParameters)
		return FetchOciImageParameters{Registry: p.GetRegistry(), Image: p.GetImage(), Tag: p.GetTag(), Architecture: p.GetArchitecture()}, nil
	})

	hash.RegisterProto(&fetchCvmfsDefinition{}, (*pb.FetchCVMFSParameters)(nil), func(db *hash.DefinitionDatabase, msg any) (hash.SerializableValue, error) {
		p := msg.(*pb.FetchCVMFSParameters)
		return FetchCVMFSParameters{Mirror: p.GetMirror(), Repo: p.GetRepo(), Path: p.GetPath()}, nil
	})

	// Registry request (used internally by OCI fetcher)
	hash.RegisterProto(&registryRequestDefinition{}, (*pb.RegistryRequestParameters)(nil), func(db *hash.DefinitionDatabase, msg any) (hash.SerializableValue, error) {
		p := msg.(*pb.RegistryRequestParameters)
		return RegistryRequestParameters{Url: p.GetUrl(), ExpireTime: p.GetExpireTime(), Accept: p.GetAccept()}, nil
	})

	hash.RegisterProto(&decompressFileBuildDefinition{}, (*pb.DecompressFileParameters)(nil), func(db *hash.DefinitionDatabase, msg any) (hash.SerializableValue, error) {
		p := msg.(*pb.DecompressFileParameters)
		var base common.BuildDefinition
		if p.GetBase() != nil {
			d2, err := db.GetDefinitionByHash(hash.Hash(p.GetBase().GetHash()))
			if err != nil {
				return nil, err
			}
			var ok bool
			base, ok = d2.(common.BuildDefinition)
			if !ok {
				return nil, fmt.Errorf("base ref %s is not BuildDefinition", p.GetBase().GetHash())
			}
		}
		return DecompressFileParameters{Base: base, Kind: p.GetKind()}, nil
	})

	hash.RegisterProto(&readOciImageDefinition{}, (*pb.ReadOciImageParameters)(nil), func(db *hash.DefinitionDatabase, msg any) (hash.SerializableValue, error) {
		p := msg.(*pb.ReadOciImageParameters)
		var base common.BuildDefinition
		if p.GetBase() != nil {
			d2, err := db.GetDefinitionByHash(hash.Hash(p.GetBase().GetHash()))
			if err != nil {
				return nil, err
			}
			var ok bool
			base, ok = d2.(common.BuildDefinition)
			if !ok {
				return nil, fmt.Errorf("base ref %s is not BuildDefinition", p.GetBase().GetHash())
			}
		}
		return ReadOciImageParameters{Base: base}, nil
	})

	hash.RegisterProto(&extractFileDefinition{}, (*pb.ExtractFileParameters)(nil), func(db *hash.DefinitionDatabase, msg any) (hash.SerializableValue, error) {
		p := msg.(*pb.ExtractFileParameters)
		var base common.BuildDefinition
		if p.GetBase() != nil {
			d2, err := db.GetDefinitionByHash(hash.Hash(p.GetBase().GetHash()))
			if err != nil {
				return nil, err
			}
			var ok bool
			base, ok = d2.(common.BuildDefinition)
			if !ok {
				return nil, fmt.Errorf("base ref %s is not BuildDefinition", p.GetBase().GetHash())
			}
		}
		return ExtractFileParameters{Base: base, Name: p.GetName()}, nil
	})

	hash.RegisterProto(&planDefinition{}, (*pb.PlanParameters)(nil), func(db *hash.DefinitionDatabase, msg any) (hash.SerializableValue, error) {
		p := msg.(*pb.PlanParameters)
		var search []common.PackageQuery
		for _, q := range p.GetSearch() {
			search = append(search, common.PackageQuery{MatchDirect: q.GetMatchDirect(), Name: q.GetName(), MatchPartialName: q.GetMatchPartialName(), Version: q.GetVersion(), Tags: q.GetTags()})
		}
		return PlanParameters{Builder: p.GetBuilder(), Architecture: p.GetArchitecture(), Search: search, TagList: p.GetTagList()}, nil
	})

	hash.RegisterProto(&readArchiveBuildDefinition{}, (*pb.ReadArchiveParameters)(nil), func(db *hash.DefinitionDatabase, msg any) (hash.SerializableValue, error) {
		p := msg.(*pb.ReadArchiveParameters)
		var base common.BuildDefinition
		if p.GetBase() != nil {
			d2, err := db.GetDefinitionByHash(hash.Hash(p.GetBase().GetHash()))
			if err != nil {
				return nil, err
			}
			var ok bool
			base, ok = d2.(common.BuildDefinition)
			if !ok {
				return nil, fmt.Errorf("base ref %s is not BuildDefinition", p.GetBase().GetHash())
			}
		}
		return ReadArchiveParameters{Base: base, Kind: p.GetKind(), StripComponents: int(p.GetStripComponents())}, nil
	})

	hash.RegisterProto(&starBuildDefinition{}, (*pb.StarParameters)(nil), func(db *hash.DefinitionDatabase, msg any) (hash.SerializableValue, error) {
		p := msg.(*pb.StarParameters)
		var args []hash.SerializableValue
		for _, a := range p.GetArguments() {
			cv, err := fromProtoValue(db, a)
			if err != nil {
				return nil, err
			}
			args = append(args, cv)
		}
		return StarParameters{ScriptFilename: p.GetScriptFilename(), BuilderName: p.GetBuilderName(), Arguments: args}, nil
	})
}
