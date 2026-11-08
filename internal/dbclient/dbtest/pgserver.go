package dbtest

import (
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgproto3"
)

// PGServer is a fake PostgreSQL server for tunnel tests: it speaks the
// wire protocol (the TLS ask refused, a cleartext password login, the
// simple and the extended query flow) to a real driver. Every statement
// that reads answers Rows (default: name, kind = each of Tables, "table");
// the others answer their command tag. It counts the logins and the
// round trips the client waited for (a Query or a Sync each). Nothing
// listens on a port: Forward hands out one end of a pipe.
type PGServer struct {
	Password string   // "": any login works
	Tables   []string // the default answer's rows
	// Rows answers a read (nil: the Tables answer).
	Rows func(query string) (cols []string, rows [][]string)

	logins atomic.Int32
	rounds atomic.Int32
	open   atomic.Int32

	mu    sync.Mutex
	stmts []string
	conns map[net.Conn]bool
}

// Open is how many client connections the server still serves.
func (p *PGServer) Open() int { return int(p.open.Load()) }

// WaitOpen waits up to 2 s until at most n client connections are open
// and returns the count.
func (p *PGServer) WaitOpen(n int) int {
	deadline := time.Now().Add(2 * time.Second)
	for p.Open() > n && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	return p.Open()
}

// DropAll closes every client connection (the server restarts).
func (p *PGServer) DropAll() {
	p.mu.Lock()
	conns := p.conns
	p.conns = nil
	p.mu.Unlock()
	for c := range conns {
		_ = c.Close()
	}
}

// Logins is how many logins the server accepted.
func (p *PGServer) Logins() int { return int(p.logins.Load()) }

// Rounds is how many round trips clients waited for.
func (p *PGServer) Rounds() int { return int(p.rounds.Load()) }

// Statements are the statements run, in order.
func (p *PGServer) Statements() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.stmts...)
}

// Conn is a new client connection to the server.
func (p *PGServer) Conn() net.Conn {
	a, b := BufferedPipe()
	go p.Serve(b)
	return a
}

var pgParam = regexp.MustCompile(`\$(\d+)`)

func (p *PGServer) answer(q string) ([]string, [][]string, bool) {
	w := strings.ToUpper(strings.TrimSpace(q))
	reads := false
	for _, k := range []string{"SELECT", "WITH", "SHOW", "TABLE", "VALUES"} {
		if strings.HasPrefix(w, k) {
			reads = true
		}
	}
	if !reads {
		return nil, nil, false
	}
	if p.Rows != nil {
		c, r := p.Rows(q)
		return c, r, true
	}
	var rows [][]string
	for _, t := range p.Tables {
		rows = append(rows, []string{t, "table"})
	}
	return []string{"name", "kind"}, rows, true
}

func (p *PGServer) record(q string) {
	p.mu.Lock()
	p.stmts = append(p.stmts, strings.TrimSpace(q))
	p.mu.Unlock()
}

func rowDesc(cols []string) *pgproto3.RowDescription {
	d := &pgproto3.RowDescription{}
	for _, c := range cols {
		d.Fields = append(d.Fields, pgproto3.FieldDescription{Name: []byte(c), DataTypeOID: 25, DataTypeSize: -1, TypeModifier: -1})
	}
	return d
}

// send sends the result of q (rows or its tag).
func (p *PGServer) send(be *pgproto3.Backend, q string, tx *byte, simple bool) {
	p.record(q)
	w := strings.ToUpper(strings.TrimSpace(q))
	switch {
	case strings.HasPrefix(w, "BEGIN"), strings.HasPrefix(w, "START"):
		*tx = 'T'
	case strings.HasPrefix(w, "ROLLBACK"), strings.HasPrefix(w, "COMMIT"):
		*tx = 'I'
	}
	cols, rows, reads := p.answer(q)
	if !reads {
		tag, _, _ := strings.Cut(w, " ")
		be.Send(&pgproto3.CommandComplete{CommandTag: []byte(tag)})
		return
	}
	if simple {
		be.Send(rowDesc(cols))
	}
	for _, r := range rows {
		vals := make([][]byte, len(r))
		for i, v := range r {
			vals[i] = []byte(v)
		}
		be.Send(&pgproto3.DataRow{Values: vals})
	}
	be.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT " + strconv.Itoa(len(rows)))})
}

// Serve speaks to one client on c until it leaves.
func (p *PGServer) Serve(c net.Conn) {
	p.open.Add(1)
	defer p.open.Add(-1)
	p.mu.Lock()
	if p.conns == nil {
		p.conns = map[net.Conn]bool{}
	}
	p.conns[c] = true
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.conns, c)
		p.mu.Unlock()
		_ = c.Close()
	}()
	be := pgproto3.NewBackend(c, c)
	for {
		msg, err := be.ReceiveStartupMessage()
		if err != nil {
			return
		}
		switch msg.(type) {
		case *pgproto3.SSLRequest, *pgproto3.GSSEncRequest:
			if _, err := c.Write([]byte{'N'}); err != nil {
				return
			}
			continue
		case *pgproto3.StartupMessage:
		default:
			return
		}
		break
	}
	if p.Password != "" {
		be.Send(&pgproto3.AuthenticationCleartextPassword{})
		if be.Flush() != nil {
			return
		}
		if be.SetAuthType(pgproto3.AuthTypeCleartextPassword) != nil {
			return
		}
		msg, err := be.Receive()
		pw, ok := msg.(*pgproto3.PasswordMessage)
		if err != nil || !ok || pw.Password != p.Password {
			be.Send(&pgproto3.ErrorResponse{Severity: "FATAL", Code: "28P01", Message: "password authentication failed"})
			_ = be.Flush()
			return
		}
	}
	p.logins.Add(1)
	be.Send(&pgproto3.AuthenticationOk{})
	be.Send(&pgproto3.ParameterStatus{Name: "server_version", Value: "16.4"})
	be.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
	be.Send(&pgproto3.ParameterStatus{Name: "standard_conforming_strings", Value: "on"})
	be.Send(&pgproto3.BackendKeyData{ProcessID: 1, SecretKey: []byte{0, 0, 0, 2}})
	be.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	if be.Flush() != nil {
		return
	}
	tx := byte('I')
	prepared, portals := map[string]string{}, map[string]string{}
	for {
		msg, err := be.Receive()
		if err != nil {
			return
		}
		switch m := msg.(type) {
		case *pgproto3.Query:
			p.rounds.Add(1)
			sent := false
			for _, q := range strings.Split(m.String, ";") {
				if strings.TrimSpace(q) == "" || strings.HasPrefix(strings.TrimSpace(q), "--") {
					continue
				}
				sent = true
				p.send(be, q, &tx, true)
			}
			if !sent {
				be.Send(&pgproto3.EmptyQueryResponse{})
			}
			be.Send(&pgproto3.ReadyForQuery{TxStatus: tx})
			if be.Flush() != nil {
				return
			}
		case *pgproto3.Parse:
			prepared[m.Name] = m.Query
			be.Send(&pgproto3.ParseComplete{})
		case *pgproto3.Describe:
			q := portals[m.Name]
			if m.ObjectType == 'S' {
				q = prepared[m.Name]
				n := 0
				for _, g := range pgParam.FindAllStringSubmatch(q, -1) {
					if k, _ := strconv.Atoi(g[1]); k > n {
						n = k
					}
				}
				oids := make([]uint32, n)
				for i := range oids {
					oids[i] = 25
				}
				be.Send(&pgproto3.ParameterDescription{ParameterOIDs: oids})
			}
			if cols, _, reads := p.answer(q); reads {
				be.Send(rowDesc(cols))
			} else {
				be.Send(&pgproto3.NoData{})
			}
		case *pgproto3.Bind:
			portals[m.DestinationPortal] = prepared[m.PreparedStatement]
			be.Send(&pgproto3.BindComplete{})
		case *pgproto3.Execute:
			p.send(be, portals[m.Portal], &tx, false)
		case *pgproto3.Close:
			be.Send(&pgproto3.CloseComplete{})
		case *pgproto3.Sync:
			p.rounds.Add(1)
			be.Send(&pgproto3.ReadyForQuery{TxStatus: tx})
			if be.Flush() != nil {
				return
			}
		case *pgproto3.Flush:
			if be.Flush() != nil {
				return
			}
		case *pgproto3.Terminate:
			return
		}
	}
}
