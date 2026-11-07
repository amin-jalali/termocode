package grpcreq

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

// maxReplies caps how many replies of a server stream are kept.
const maxReplies = 1000

// Result is one finished call.
type Result struct {
	Method   string // /package.Service/Method
	Requests []string
	Header   metadata.MD
	Replies  []string // JSON, one per reply
	Dropped  int      // replies past maxReplies
	Trailer  metadata.MD
	Code     codes.Code
	Message  string // status message ("" for OK)
	Elapsed  time.Duration
}

// Call sends the JSON messages reqs to method md over cc and reads the
// replies. A unary or server-streaming method takes at most one message
// (none = an empty message). An error is returned only when the call
// could not be started (bad JSON, too many messages); a failed call is
// in Result.Code and Result.Message.
func Call(ctx context.Context, cc grpc.ClientConnInterface, files *protoregistry.Files, md protoreflect.MethodDescriptor, reqs []string, meta metadata.MD) (Result, error) {
	types := dynamicpb.NewTypes(files)
	full := "/" + string(md.Parent().FullName()) + "/" + string(md.Name())
	res := Result{Method: full}
	if len(reqs) > 1 && !md.IsStreamingClient() {
		return res, fmt.Errorf("grpc: %s takes one message, the body has %d (split by ===)", md.Name(), len(reqs))
	}
	if len(reqs) == 0 && !md.IsStreamingClient() {
		reqs = []string{"{}"}
	}
	in := protojson.UnmarshalOptions{Resolver: types}
	var msgs []proto.Message
	for i, r := range reqs {
		m := dynamicpb.NewMessage(md.Input())
		if strings.TrimSpace(r) != "" {
			if err := in.Unmarshal([]byte(r), m); err != nil {
				return res, fmt.Errorf("grpc: message %d is not a valid %s: %w", i+1, md.Input().FullName(), err)
			}
		}
		msgs = append(msgs, m)
	}
	out := protojson.MarshalOptions{Multiline: true, Indent: "  ", Resolver: types}
	for _, m := range msgs {
		b, err := out.Marshal(m)
		if err != nil {
			return res, fmt.Errorf("grpc: %w", err)
		}
		res.Requests = append(res.Requests, string(b))
	}
	if len(meta) > 0 {
		ctx = metadata.NewOutgoingContext(ctx, meta)
	}
	start := time.Now()
	desc := &grpc.StreamDesc{StreamName: string(md.Name()), ServerStreams: md.IsStreamingServer(), ClientStreams: md.IsStreamingClient()}
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	st, err := cc.NewStream(sctx, desc, full)
	if err != nil {
		s := status.Convert(err)
		res.Code, res.Message = s.Code(), s.Message()
		res.Elapsed = time.Since(start)
		return res, nil
	}
	for _, m := range msgs {
		if err := st.SendMsg(m); err != nil {
			break // io.EOF: the server ended the call; RecvMsg has the status
		}
	}
	_ = st.CloseSend()
	for {
		reply := dynamicpb.NewMessage(md.Output())
		err := st.RecvMsg(reply)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			s := status.Convert(err)
			res.Code, res.Message = s.Code(), s.Message()
			break
		}
		if len(res.Replies) < maxReplies {
			b, merr := out.Marshal(reply)
			if merr != nil {
				b = []byte(fmt.Sprintf("(could not show the reply as JSON: %v)", merr))
			}
			res.Replies = append(res.Replies, string(b))
		} else {
			res.Dropped++
		}
		if !md.IsStreamingServer() {
			break
		}
	}
	if h, err := st.Header(); err == nil {
		res.Header = h
	}
	res.Trailer = st.Trailer()
	res.Elapsed = time.Since(start)
	return res, nil
}

// Text is the call as a read-only page: requests, header, replies,
// status and trailer.
func (r Result) Text(target Target) string {
	var b strings.Builder
	fmt.Fprintf(&b, "gRPC %s%s\n", target.String(), r.Method)
	for i, q := range r.Requests {
		fmt.Fprintf(&b, "\n→ request %d\n%s\n", i+1, indent(q))
	}
	if len(r.Header) > 0 {
		fmt.Fprintf(&b, "\n← header\n%s", mdText(r.Header))
	}
	for i, q := range r.Replies {
		fmt.Fprintf(&b, "\n← reply %d\n%s\n", i+1, indent(q))
	}
	if r.Dropped > 0 {
		fmt.Fprintf(&b, "\n… %d more replies not shown\n", r.Dropped)
	}
	fmt.Fprintf(&b, "\nstatus %s", r.Code.String())
	if r.Message != "" {
		fmt.Fprintf(&b, ": %s", r.Message)
	}
	fmt.Fprintf(&b, "  (%d ms)\n", r.Elapsed.Milliseconds())
	if len(r.Trailer) > 0 {
		fmt.Fprintf(&b, "\n← trailer\n%s", mdText(r.Trailer))
	}
	return b.String()
}

func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = "    " + l
	}
	return strings.Join(lines, "\n")
}

// mdText lists metadata sorted by key; binary (-bin) values are shown
// by size only.
func mdText(md metadata.MD) string {
	keys := make([]string, 0, len(md))
	for k := range md {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		for _, v := range md[k] {
			if strings.HasSuffix(k, "-bin") {
				v = fmt.Sprintf("(%d bytes)", len(v))
			}
			fmt.Fprintf(&b, "    %s: %s\n", k, v)
		}
	}
	return b.String()
}

// Metadata turns request headers into gRPC metadata: keys lower case;
// Content-Type, TE, Host and Connection are left to the transport.
func Metadata(headers [][2]string) metadata.MD {
	md := metadata.MD{}
	for _, h := range headers {
		k := strings.ToLower(strings.TrimSpace(h[0]))
		switch k {
		case "", "content-type", "te", "host", "connection", "user-agent":
			continue
		}
		md.Append(k, h[1])
	}
	return md
}

// Describe lists the services in files (only service when set) with
// their methods and the fields of each request and reply message.
func Describe(files *protoregistry.Files, service string) (string, error) {
	names := Services(files)
	if service != "" {
		names = []string{service}
	}
	if len(names) == 0 {
		return "", errors.New("grpc: no services")
	}
	var b strings.Builder
	seen := map[protoreflect.FullName]bool{}
	var msgs []protoreflect.MessageDescriptor
	for _, n := range names {
		d, err := files.FindDescriptorByName(protoreflect.FullName(n))
		if err != nil {
			return "", fmt.Errorf("grpc: service %q not found%s", n, knownServices(files))
		}
		sd, ok := d.(protoreflect.ServiceDescriptor)
		if !ok {
			return "", fmt.Errorf("grpc: %q is not a service", n)
		}
		fmt.Fprintf(&b, "service %s {\n", sd.FullName())
		for i := 0; i < sd.Methods().Len(); i++ {
			m := sd.Methods().Get(i)
			fmt.Fprintf(&b, "  rpc %s(%s%s) returns (%s%s);\n", m.Name(), streamWord(m.IsStreamingClient()), m.Input().FullName(), streamWord(m.IsStreamingServer()), m.Output().FullName())
			for _, md := range []protoreflect.MessageDescriptor{m.Input(), m.Output()} {
				if !seen[md.FullName()] {
					seen[md.FullName()] = true
					msgs = append(msgs, md)
				}
			}
		}
		b.WriteString("}\n\n")
	}
	for _, md := range msgs {
		writeMessage(&b, md)
	}
	return strings.TrimRight(b.String(), "\n") + "\n", nil
}

func streamWord(on bool) string {
	if on {
		return "stream "
	}
	return ""
}

func writeMessage(b *strings.Builder, md protoreflect.MessageDescriptor) {
	fmt.Fprintf(b, "message %s {\n", md.FullName())
	fs := md.Fields()
	for i := 0; i < fs.Len(); i++ {
		f := fs.Get(i)
		fmt.Fprintf(b, "  %s%s %s = %d;\n", fieldLabel(f), fieldType(f), f.Name(), f.Number())
	}
	b.WriteString("}\n\n")
}

func fieldLabel(f protoreflect.FieldDescriptor) string {
	switch {
	case f.IsMap():
		return ""
	case f.IsList():
		return "repeated "
	case f.HasOptionalKeyword():
		return "optional "
	}
	return ""
}

func fieldType(f protoreflect.FieldDescriptor) string {
	if f.IsMap() {
		return "map<" + fieldType(f.MapKey()) + ", " + fieldType(f.MapValue()) + ">"
	}
	switch f.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		return string(f.Message().FullName())
	case protoreflect.EnumKind:
		return string(f.Enum().FullName())
	}
	return f.Kind().String()
}
