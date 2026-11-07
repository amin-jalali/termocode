package grpcreq

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"

	// The well-known types, so `import "google/protobuf/…"` resolves
	// without the files on disk.
	_ "google.golang.org/protobuf/types/known/anypb"
	_ "google.golang.org/protobuf/types/known/apipb"
	_ "google.golang.org/protobuf/types/known/durationpb"
	_ "google.golang.org/protobuf/types/known/emptypb"
	_ "google.golang.org/protobuf/types/known/fieldmaskpb"
	_ "google.golang.org/protobuf/types/known/sourcecontextpb"
	_ "google.golang.org/protobuf/types/known/structpb"
	_ "google.golang.org/protobuf/types/known/timestamppb"
	_ "google.golang.org/protobuf/types/known/typepb"
	_ "google.golang.org/protobuf/types/known/wrapperspb"
)

// maxProtoFiles caps how many files one load reads (imports included).
const maxProtoFiles = 500

// maxProtoBytes caps the size of one .proto file.
const maxProtoBytes = 4 << 20

// readFileFunc reads one file; tests swap it.
type readFileFunc func(path string) ([]byte, error)

// LoadProtos parses the .proto files at paths and every file they import
// and returns them as one registry. An import is looked up in
// importDirs in order, then next to the file that imports it; the
// well-known google/protobuf/*.proto files are built in.
func LoadProtos(paths, importDirs []string) (*protoregistry.Files, error) {
	return loadProtos(paths, importDirs, os.ReadFile)
}

func loadProtos(paths, importDirs []string, read readFileFunc) (*protoregistry.Files, error) {
	if len(paths) == 0 {
		return nil, errors.New("grpc: no .proto file given")
	}
	l := &protoLoader{read: read, importDirs: importDirs, byName: map[string]*descriptorpb.FileDescriptorProto{}}
	for _, p := range paths {
		name := l.nameFor(p)
		if err := l.loadPath(name, p); err != nil {
			return nil, err
		}
	}
	files, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: l.order})
	if err != nil {
		return nil, fmt.Errorf("grpc: %w", err)
	}
	return files, nil
}

type protoLoader struct {
	read       readFileFunc
	importDirs []string
	byName     map[string]*descriptorpb.FileDescriptorProto
	order      []*descriptorpb.FileDescriptorProto
}

// nameFor is the import name of a file given by path: its path under the
// first import dir that holds it, else its base name.
func (l *protoLoader) nameFor(path string) string {
	for _, d := range l.importDirs {
		if rel, err := filepath.Rel(d, path); err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.Base(path)
}

func (l *protoLoader) loadPath(name, path string) error {
	if _, ok := l.byName[name]; ok {
		return nil
	}
	if len(l.byName) >= maxProtoFiles {
		return fmt.Errorf("grpc: more than %d .proto files", maxProtoFiles)
	}
	b, err := l.read(path)
	if err != nil {
		return fmt.Errorf("grpc: %w", err)
	}
	return l.loadBytes(name, path, b)
}

func (l *protoLoader) loadBytes(name, path string, b []byte) error {
	if len(b) > maxProtoBytes {
		return fmt.Errorf("grpc: %s is larger than %d MB", path, maxProtoBytes>>20)
	}
	fd, err := ParseProto(name, string(b))
	if err != nil {
		return fmt.Errorf("grpc: %w", err)
	}
	l.byName[name] = fd
	l.order = append(l.order, fd)
	dir := filepath.Dir(path)
	for _, dep := range fd.GetDependency() {
		if err := l.loadImport(dep, dir); err != nil {
			return err
		}
	}
	return nil
}

func (l *protoLoader) loadImport(name, fromDir string) error {
	if _, ok := l.byName[name]; ok {
		return nil
	}
	if strings.Contains(filepath.ToSlash(name), "..") {
		return fmt.Errorf("grpc: import %q leaves the import folders", name)
	}
	dirs := append(append([]string(nil), l.importDirs...), fromDir)
	for _, d := range dirs {
		p := filepath.Join(d, filepath.FromSlash(name))
		if b, err := l.read(p); err == nil {
			if len(l.byName) >= maxProtoFiles {
				return fmt.Errorf("grpc: more than %d .proto files", maxProtoFiles)
			}
			return l.loadBytes(name, p, b)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("grpc: %w", err)
		}
	}
	if fd, err := protoregistry.GlobalFiles.FindFileByPath(name); err == nil {
		l.addBuiltin(fd)
		return nil
	}
	return fmt.Errorf("grpc: import %q not found (add its folder with # @proto-path)", name)
}

// addBuiltin adds a compiled-in file and its imports.
func (l *protoLoader) addBuiltin(fd protoreflect.FileDescriptor) {
	if _, ok := l.byName[fd.Path()]; ok {
		return
	}
	imps := fd.Imports()
	for i := 0; i < imps.Len(); i++ {
		l.addBuiltin(imps.Get(i).FileDescriptor)
	}
	p := protodesc.ToFileDescriptorProto(fd)
	l.byName[fd.Path()] = p
	l.order = append(l.order, p)
}

// FilesFromProtos builds a registry from file descriptors (as sent by
// server reflection).
func FilesFromProtos(fds []*descriptorpb.FileDescriptorProto) (*protoregistry.Files, error) {
	files, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{File: fds})
	if err != nil {
		return nil, fmt.Errorf("grpc: %w", err)
	}
	return files, nil
}

// FindMethod looks up service/method in files.
func FindMethod(files *protoregistry.Files, service, method string) (protoreflect.MethodDescriptor, error) {
	d, err := files.FindDescriptorByName(protoreflect.FullName(service))
	if err != nil {
		return nil, fmt.Errorf("grpc: service %q not found%s", service, knownServices(files))
	}
	sd, ok := d.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, fmt.Errorf("grpc: %q is not a service", service)
	}
	md := sd.Methods().ByName(protoreflect.Name(method))
	if md == nil {
		var names []string
		for i := 0; i < sd.Methods().Len(); i++ {
			names = append(names, string(sd.Methods().Get(i).Name()))
		}
		return nil, fmt.Errorf("grpc: %s has no method %q (it has %s)", service, method, strings.Join(names, ", "))
	}
	return md, nil
}

func knownServices(files *protoregistry.Files) string {
	names := Services(files)
	if len(names) == 0 {
		return ""
	}
	if len(names) > 8 {
		names = append(names[:8], "…")
	}
	return " (known: " + strings.Join(names, ", ") + ")"
}

// Services lists the full names of the services in files, sorted.
func Services(files *protoregistry.Files) []string {
	var out []string
	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		for i := 0; i < fd.Services().Len(); i++ {
			out = append(out, string(fd.Services().Get(i).FullName()))
		}
		return true
	})
	sort.Strings(out)
	return out
}
