package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/reflect/protoregistry"

	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/httpclient/grpcreq"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// gRPC requests in .http files (API-19). The send key on
//
//	### say hello
//	# @proto ./proto/greeter.proto
//	GRPC grpc://localhost:50051/helloworld.Greeter/SayHello
//	authorization: Bearer {{token}}
//
//	{"name": "ann"}
//
// sends the JSON body (several messages split by `===` lines for a
// client stream) and shows the call as a read-only page: every request,
// the reply header, every reply, the status and the trailer. Without
// # @proto the server's reflection service describes the method.
// HTTP: Describe gRPC Services shows the services of the request at the
// cursor.

// isGRPCRequest: method GRPC, or a grpc:// / grpcs:// URL.
func isGRPCRequest(r httpclient.Request) bool {
	if strings.EqualFold(r.Method, "GRPC") {
		return true
	}
	u := strings.ToLower(strings.TrimSpace(r.URL))
	return strings.HasPrefix(u, "grpc://") || strings.HasPrefix(u, "grpcs://")
}

func httpGRPCPaletteItems() []picker.Item {
	return []picker.Item{{ID: "http-grpc-describe", Title: "HTTP: Describe gRPC Services"}}
}

// httpGRPCPlan is what one gRPC send (or describe) does.
type httpGRPCPlan struct {
	target   grpcreq.Target
	protos   []string // .proto files (absolute); none = server reflection
	dirs     []string // import folders
	meta     metadata.MD
	messages []string
	timeout  time.Duration
}

// httpGRPCList splits a directive value on commas and spaces and makes
// each path absolute against dir.
func httpGRPCList(v, dir string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		if !filepath.IsAbs(f) {
			f = filepath.Join(dir, f)
		}
		out = append(out, filepath.Clean(f))
	}
	return out
}

// httpGRPCPlanFor reads the plan from a resolved request of file.
func httpGRPCPlanFor(rr httpclient.Resolved, file string, timeout time.Duration) (httpGRPCPlan, error) {
	t, err := grpcreq.ParseTarget(rr.URL)
	if err != nil {
		return httpGRPCPlan{}, err
	}
	dir := filepath.Dir(file)
	p := httpGRPCPlan{
		target:   t,
		protos:   httpGRPCList(rr.Directives["proto"], dir),
		dirs:     httpGRPCList(rr.Directives["proto-path"], dir),
		messages: httpclient.WSMessages(rr.Body),
		timeout:  timeout,
	}
	hs := make([][2]string, 0, len(rr.Headers))
	for _, h := range rr.Headers {
		hs = append(hs, [2]string{h.Name, h.Value})
	}
	p.meta = grpcreq.Metadata(hs)
	if p.timeout <= 0 {
		p.timeout = 30 * time.Second
	}
	return p, nil
}

// httpGRPCFiles loads the descriptors: the @proto files, or the
// server's reflection (cc may be nil only with @proto files).
func httpGRPCFiles(ctx context.Context, cc grpc.ClientConnInterface, plan httpGRPCPlan) (*protoregistry.Files, string, error) {
	if len(plan.protos) > 0 {
		files, err := grpcreq.LoadProtos(plan.protos, plan.dirs)
		names := make([]string, len(plan.protos))
		for i, p := range plan.protos {
			names[i] = filepath.Base(p)
		}
		return files, strings.Join(names, ", "), err
	}
	meta := plan.meta
	withMeta := func(c context.Context) context.Context {
		if len(meta) == 0 {
			return c
		}
		return metadata.NewOutgoingContext(c, meta)
	}
	r := grpcreq.NewReflector(cc, withMeta)
	if plan.target.Service != "" {
		files, err := r.Files(ctx, plan.target.Service)
		return files, "server reflection", err
	}
	files, err := r.Files(ctx)
	return files, "server reflection", err
}

// httpGRPCCall runs plan and returns the page text. err is set when the
// call could not start; a failed status is on the page.
func httpGRPCCall(ctx context.Context, plan httpGRPCPlan, insecureTLS bool, opts ...grpc.DialOption) (string, error) {
	if plan.target.Method == "" {
		return "", fmt.Errorf("grpc: put the method in the URL (grpc://host:port/package.Service/Method)")
	}
	ctx, cancel := context.WithTimeout(ctx, plan.timeout)
	defer cancel()
	cc, err := grpcreq.Dial(plan.target, insecureTLS, opts...)
	if err != nil {
		return "", err
	}
	defer func() { _ = cc.Close() }()
	files, _, err := httpGRPCFiles(ctx, cc, plan)
	if err != nil {
		return "", err
	}
	md, err := grpcreq.FindMethod(files, plan.target.Service, plan.target.Method)
	if err != nil {
		return "", err
	}
	res, err := grpcreq.Call(ctx, cc, files, md, plan.messages, plan.meta)
	if err != nil {
		return "", err
	}
	return res.Text(plan.target), nil
}

// httpGRPCDescribe lists the services of plan's server (only its
// service when the URL names one). With @proto files no connection is
// made.
func httpGRPCDescribe(ctx context.Context, plan httpGRPCPlan, insecureTLS bool, opts ...grpc.DialOption) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, plan.timeout)
	defer cancel()
	var cc grpc.ClientConnInterface
	if len(plan.protos) == 0 {
		conn, err := grpcreq.Dial(plan.target, insecureTLS, opts...)
		if err != nil {
			return "", err
		}
		defer func() { _ = conn.Close() }()
		cc = conn
	}
	files, from, err := httpGRPCFiles(ctx, cc, plan)
	if err != nil {
		return "", err
	}
	text, err := grpcreq.Describe(files, plan.target.Service)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("// gRPC services of %s (from %s)\n\n%s", plan.target.String(), from, text), nil
}

// httpGRPCDoneMsg is a finished gRPC call or describe.
type httpGRPCDoneMsg struct {
	title string
	text  string
	err   error
}

// httpGRPCPrepare resolves req and reads its plan; warn is a toast when
// that fails.
func (m *Model) httpGRPCPrepare(file string, vars []httpclient.Var, req httpclient.Request) (httpGRPCPlan, []string, tea.Cmd) {
	rv := httpResolver(file, vars, "")
	rv.Responses = m.httpS().chainSource(file)
	rr, un := rv.Resolve(req)
	if len(un) > 0 {
		return httpGRPCPlan{}, nil, m.pushToastDetail(toast.Warn, "gRPC: unknown variable {{"+un[0].Name+"}}", "Define it in http-client.env.json or with @name = value")
	}
	plan, err := httpGRPCPlanFor(rr, file, loadHTTPSettings().timeout())
	if err != nil {
		return httpGRPCPlan{}, nil, m.pushToastDetail(toast.Warn, err.Error(), "")
	}
	return plan, append([]string(nil), rr.Secrets...), nil
}

// httpGRPCRun runs job off the UI loop and hides secrets in its text.
func (m *Model) httpGRPCRun(title, note string, secrets []string, job func() (string, error)) tea.Cmd {
	m.httpS().grpcRunning = true
	run := func() tea.Msg {
		text, err := job()
		for _, v := range secrets {
			if v != "" {
				text = strings.ReplaceAll(text, v, httpclient.Redacted)
				if err != nil {
					err = fmt.Errorf("%s", strings.ReplaceAll(err.Error(), v, httpclient.Redacted))
				}
			}
		}
		return httpGRPCDoneMsg{title: title, text: text, err: err}
	}
	return tea.Batch(m.pushToastDetail(toast.Info, note, ""), run)
}

// httpGRPCSend sends the gRPC request req of file.
func (m *Model) httpGRPCSend(file string, vars []httpclient.Var, req httpclient.Request) tea.Cmd {
	if m.httpS().grpcRunning {
		return m.pushToastDetail(toast.Warn, "A gRPC call is still running", "It ends with its reply or the HTTP timeout")
	}
	plan, secrets, warn := m.httpGRPCPrepare(file, vars, req)
	if warn != nil {
		return warn
	}
	if plan.target.Method == "" {
		return m.pushToastDetail(toast.Warn, "gRPC: the URL names no method", "Write grpc://host:port/package.Service/Method, or use HTTP: Describe gRPC Services")
	}
	insecureTLS := loadHTTPSettings().tlsInsecure()
	return m.httpGRPCRun("gRPC: "+req.Label(), "gRPC: calling "+plan.target.Service+"/"+plan.target.Method, secrets, func() (string, error) {
		return httpGRPCCall(context.Background(), plan, insecureTLS)
	})
}

// dispatchHTTPGRPC runs an "http-grpc-…" palette id. ok=false: not ours.
func (m *Model) dispatchHTTPGRPC(id string) (tea.Cmd, bool) {
	if id != "http-grpc-describe" {
		return nil, false
	}
	if m.httpS().grpcRunning {
		return m.pushToastDetail(toast.Warn, "A gRPC call is still running", "Try again when it ends"), true
	}
	file, f, req, err := m.httpRequestFromBuffer(0, 0)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "HTTP: "+err.Error(), ""), true
	}
	if !isGRPCRequest(*req) {
		return m.pushToastDetail(toast.Warn, "The request at the cursor is not a gRPC request",
			"Use GRPC as the method: GRPC grpc://host:port/package.Service/Method"), true
	}
	plan, secrets, warn := m.httpGRPCPrepare(file, f.Vars, *req)
	if warn != nil {
		return warn, true
	}
	insecureTLS := loadHTTPSettings().tlsInsecure()
	return m.httpGRPCRun("gRPC services: "+plan.target.Addr, "gRPC: reading the services of "+plan.target.String(), secrets, func() (string, error) {
		return httpGRPCDescribe(context.Background(), plan, insecureTLS)
	}), true
}

// onHTTPGRPCDone shows the page.
func (m *Model) onHTTPGRPCDone(msg httpGRPCDoneMsg) tea.Cmd {
	m.httpS().grpcRunning = false
	if msg.err != nil {
		return m.pushToastDetail(toast.Errr, msg.title+" failed", msg.err.Error())
	}
	m.ensureEditorWindowCurrent()
	if err := m.openReadOnlyPage(msg.title, msg.text, "", "text"); err != nil {
		return m.pushToastDetail(toast.Errr, "Could not open the gRPC page", err.Error())
	}
	m.focus = FocusEditor
	return nil
}
