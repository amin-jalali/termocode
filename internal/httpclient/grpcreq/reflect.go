package grpcreq

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	rv1 "google.golang.org/grpc/reflection/grpc_reflection_v1"
	rv1alpha "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// maxReflectFiles caps how many files one reflection fetch takes.
const maxReflectFiles = 500

// ErrNoReflection means the server does not offer server reflection.
var ErrNoReflection = errors.New("grpc: the server has no reflection service; add the .proto file with # @proto")

// reflectAsk is one reflection question: list the services, or the file
// that defines a symbol, or a file by name.
type reflectAsk struct {
	list     bool
	symbol   string
	filename string
}

// reflectAnswer is the version-free answer.
type reflectAnswer struct {
	services []string
	files    [][]byte
}

// reflectStream asks questions on one reflection stream (v1, or v1alpha
// for older servers).
type reflectStream interface {
	ask(reflectAsk) (reflectAnswer, error)
	close()
}

type v1Stream struct {
	s      grpc.BidiStreamingClient[rv1.ServerReflectionRequest, rv1.ServerReflectionResponse]
	cancel context.CancelFunc
}

func (v *v1Stream) ask(q reflectAsk) (reflectAnswer, error) {
	req := &rv1.ServerReflectionRequest{}
	switch {
	case q.list:
		req.MessageRequest = &rv1.ServerReflectionRequest_ListServices{ListServices: "*"}
	case q.symbol != "":
		req.MessageRequest = &rv1.ServerReflectionRequest_FileContainingSymbol{FileContainingSymbol: q.symbol}
	default:
		req.MessageRequest = &rv1.ServerReflectionRequest_FileByFilename{FileByFilename: q.filename}
	}
	// Send says io.EOF when the server already ended the stream (say,
	// Unimplemented); the real status comes from Recv.
	if err := v.s.Send(req); err != nil && !errors.Is(err, io.EOF) {
		return reflectAnswer{}, err
	}
	resp, err := v.s.Recv()
	if err != nil {
		return reflectAnswer{}, err
	}
	if e := resp.GetErrorResponse(); e != nil {
		return reflectAnswer{}, status.Error(codes.Code(e.GetErrorCode()), e.GetErrorMessage())
	}
	var a reflectAnswer
	for _, s := range resp.GetListServicesResponse().GetService() {
		a.services = append(a.services, s.GetName())
	}
	a.files = resp.GetFileDescriptorResponse().GetFileDescriptorProto()
	return a, nil
}

func (v *v1Stream) close() { _ = v.s.CloseSend(); v.cancel() }

type v1alphaStream struct {
	s      grpc.BidiStreamingClient[rv1alpha.ServerReflectionRequest, rv1alpha.ServerReflectionResponse]
	cancel context.CancelFunc
}

func (v *v1alphaStream) ask(q reflectAsk) (reflectAnswer, error) {
	req := &rv1alpha.ServerReflectionRequest{}
	switch {
	case q.list:
		req.MessageRequest = &rv1alpha.ServerReflectionRequest_ListServices{ListServices: "*"}
	case q.symbol != "":
		req.MessageRequest = &rv1alpha.ServerReflectionRequest_FileContainingSymbol{FileContainingSymbol: q.symbol}
	default:
		req.MessageRequest = &rv1alpha.ServerReflectionRequest_FileByFilename{FileByFilename: q.filename}
	}
	// Send says io.EOF when the server already ended the stream (say,
	// Unimplemented); the real status comes from Recv.
	if err := v.s.Send(req); err != nil && !errors.Is(err, io.EOF) {
		return reflectAnswer{}, err
	}
	resp, err := v.s.Recv()
	if err != nil {
		return reflectAnswer{}, err
	}
	if e := resp.GetErrorResponse(); e != nil {
		return reflectAnswer{}, status.Error(codes.Code(e.GetErrorCode()), e.GetErrorMessage())
	}
	var a reflectAnswer
	for _, s := range resp.GetListServicesResponse().GetService() {
		a.services = append(a.services, s.GetName())
	}
	a.files = resp.GetFileDescriptorResponse().GetFileDescriptorProto()
	return a, nil
}

func (v *v1alphaStream) close() { _ = v.s.CloseSend(); v.cancel() }

// Reflector asks a server about its services over server reflection.
type Reflector struct {
	cc grpc.ClientConnInterface
	md func(context.Context) context.Context // adds the request metadata
}

// NewReflector uses cc; withMeta (may be nil) adds metadata (auth
// headers) to the reflection calls.
func NewReflector(cc grpc.ClientConnInterface, withMeta func(context.Context) context.Context) *Reflector {
	return &Reflector{cc: cc, md: withMeta}
}

// open starts a v1 stream and checks it with q; on Unimplemented it
// falls back to v1alpha. It returns the stream and the answer to q.
func (r *Reflector) open(ctx context.Context, q reflectAsk) (reflectStream, reflectAnswer, error) {
	if r.md != nil {
		ctx = r.md(ctx)
	}
	sctx, cancel := context.WithCancel(ctx)
	s1, err := rv1.NewServerReflectionClient(r.cc).ServerReflectionInfo(sctx)
	if err == nil {
		st := &v1Stream{s: s1, cancel: cancel}
		a, err := st.ask(q)
		if err == nil {
			return st, a, nil
		}
		st.close()
		if status.Code(err) != codes.Unimplemented {
			return nil, reflectAnswer{}, err
		}
	} else {
		cancel()
		if status.Code(err) != codes.Unimplemented {
			return nil, reflectAnswer{}, err
		}
	}
	sctx, cancel = context.WithCancel(ctx)
	s0, err := rv1alpha.NewServerReflectionClient(r.cc).ServerReflectionInfo(sctx)
	if err != nil {
		cancel()
		return nil, reflectAnswer{}, reflectErr(err)
	}
	st := &v1alphaStream{s: s0, cancel: cancel}
	a, err := st.ask(q)
	if err != nil {
		st.close()
		return nil, reflectAnswer{}, reflectErr(err)
	}
	return st, a, nil
}

func reflectErr(err error) error {
	if status.Code(err) == codes.Unimplemented {
		return ErrNoReflection
	}
	return err
}

// Services lists the services the server offers, sorted; the reflection
// service itself is left out.
func (r *Reflector) Services(ctx context.Context) ([]string, error) {
	st, a, err := r.open(ctx, reflectAsk{list: true})
	if err != nil {
		return nil, err
	}
	st.close()
	var out []string
	for _, s := range a.services {
		if s == "grpc.reflection.v1.ServerReflection" || s == "grpc.reflection.v1alpha.ServerReflection" {
			continue
		}
		out = append(out, s)
	}
	sort.Strings(out)
	return out, nil
}

// Files fetches the files that define symbols (full service or message
// names) and every file they import. With no symbols it lists the
// services first and fetches them all.
func (r *Reflector) Files(ctx context.Context, symbols ...string) (*protoregistry.Files, error) {
	if len(symbols) == 0 {
		svcs, err := r.Services(ctx)
		if err != nil {
			return nil, err
		}
		if len(svcs) == 0 {
			return nil, errors.New("grpc: the server lists no services")
		}
		symbols = svcs
	}
	st, a, err := r.open(ctx, reflectAsk{symbol: symbols[0]})
	if err != nil {
		return nil, fmt.Errorf("grpc: %s: %w", symbols[0], err)
	}
	defer st.close()
	got := map[string]*descriptorpb.FileDescriptorProto{}
	var order []*descriptorpb.FileDescriptorProto
	add := func(raw [][]byte) error {
		for _, b := range raw {
			fd := &descriptorpb.FileDescriptorProto{}
			if err := proto.Unmarshal(b, fd); err != nil {
				return fmt.Errorf("grpc: bad descriptor from the server: %w", err)
			}
			if _, ok := got[fd.GetName()]; ok {
				continue
			}
			if len(got) >= maxReflectFiles {
				return fmt.Errorf("grpc: the server sent more than %d files", maxReflectFiles)
			}
			got[fd.GetName()] = fd
			order = append(order, fd)
		}
		return nil
	}
	if err := add(a.files); err != nil {
		return nil, err
	}
	for _, sym := range symbols[1:] {
		a, err := st.ask(reflectAsk{symbol: sym})
		if err != nil {
			return nil, fmt.Errorf("grpc: %s: %w", sym, err)
		}
		if err := add(a.files); err != nil {
			return nil, err
		}
	}
	// Fetch imports the server did not send along.
	for i := 0; i < len(order); i++ {
		for _, dep := range order[i].GetDependency() {
			if _, ok := got[dep]; ok {
				continue
			}
			if fd, err := protoregistry.GlobalFiles.FindFileByPath(dep); err == nil {
				l := &protoLoader{byName: got}
				l.addBuiltin(fd)
				order = append(order, l.order...)
				continue
			}
			a, err := st.ask(reflectAsk{filename: dep})
			if err != nil {
				return nil, fmt.Errorf("grpc: %s: %w", dep, err)
			}
			if err := add(a.files); err != nil {
				return nil, err
			}
		}
	}
	return FilesFromProtos(order)
}
