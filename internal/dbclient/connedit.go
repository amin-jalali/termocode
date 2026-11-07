package dbclient

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/amin-jalali/termocode/internal/jsonc"
)

// Connection management on the user's list of connections. Each
// function returns a new slice and never changes the one it gets.

func connIndex(cs []Conn, id string) int {
	for i := range cs {
		if cs[i].ID == id {
			return i
		}
	}
	return -1
}

// cloneConn copies a connection so the copy shares no pointer or map.
func cloneConn(c Conn) Conn {
	if c.SSL != nil {
		s := *c.SSL
		c.SSL = &s
	}
	if c.SSH != nil {
		s := *c.SSH
		s.Jump = append([]string(nil), s.Jump...)
		c.SSH = &s
	}
	if c.Kube != nil {
		k := *c.Kube
		c.Kube = &k
	}
	if c.Options != nil {
		o := *c.Options
		c.Options = &o
	}
	if c.Params != nil {
		p := make(map[string]string, len(c.Params))
		for k, v := range c.Params {
			p[k] = v
		}
		c.Params = p
	}
	return c
}

func cloneConns(cs []Conn) []Conn {
	out := make([]Conn, len(cs))
	for i, c := range cs {
		out[i] = cloneConn(c)
	}
	return out
}

// freeID returns base+"-copy", then base+"-copy-2", … the first id not
// used in cs.
func freeID(cs []Conn, base string) string {
	id := base + "-copy"
	for n := 2; connIndex(cs, id) >= 0; n++ {
		id = base + "-copy-" + strconv.Itoa(n)
	}
	return id
}

// DuplicateConn puts a copy of id right after it, with a free id and a
// "(copy)" name. It returns the new list and the new id.
func DuplicateConn(cs []Conn, id string) ([]Conn, string, error) {
	i := connIndex(cs, id)
	if i < 0 {
		return nil, "", fmt.Errorf("unknown connection %q", id)
	}
	c := cloneConn(cs[i])
	c.ID = freeID(cs, id)
	name := c.Name
	if name == "" {
		name = id
	}
	c.Name = name + " (copy)"
	out := make([]Conn, 0, len(cs)+1)
	out = append(out, cloneConns(cs[:i+1])...)
	out = append(out, c)
	out = append(out, cloneConns(cs[i+1:])...)
	return out, c.ID, nil
}

// RenameConn changes the id of a connection. The new id must be valid
// and free.
func RenameConn(cs []Conn, oldID, newID string) ([]Conn, error) {
	i := connIndex(cs, oldID)
	if i < 0 {
		return nil, fmt.Errorf("unknown connection %q", oldID)
	}
	if !idPattern.MatchString(newID) {
		return nil, fmt.Errorf("invalid connection id")
	}
	if newID != oldID && connIndex(cs, newID) >= 0 {
		return nil, fmt.Errorf("connection id %q is already used", newID)
	}
	out := cloneConns(cs)
	out[i].ID = newID
	return out, nil
}

// DeleteConn removes a connection.
func DeleteConn(cs []Conn, id string) ([]Conn, error) {
	i := connIndex(cs, id)
	if i < 0 {
		return nil, fmt.Errorf("unknown connection %q", id)
	}
	out := cloneConns(cs[:i])
	return append(out, cloneConns(cs[i+1:])...), nil
}

// CopySSL gives connection to the SSL settings of connection from.
func CopySSL(cs []Conn, from, to string) ([]Conn, error) {
	fi, ti := connIndex(cs, from), connIndex(cs, to)
	if fi < 0 || ti < 0 {
		return nil, fmt.Errorf("unknown connection")
	}
	if cs[fi].SSL == nil {
		return nil, fmt.Errorf("connection %q has no SSL settings", from)
	}
	out := cloneConns(cs)
	s := *cs[fi].SSL
	out[ti].SSL = &s
	return out, nil
}

// dropLiteralSecrets removes a literal password and the password of the
// URL; secret references stay.
func dropLiteralSecrets(c Conn) Conn {
	if literalPassword(c.Password) {
		c.Password = ""
	}
	if urlHasPassword(c.URL) {
		if u, err := url.Parse(c.URL); err == nil {
			u.User = url.User(u.User.Username())
			c.URL = u.String()
		}
	}
	return c
}

// ConnJSON is one connection as indented JSON for the clipboard. A
// literal password is never in it.
func ConnJSON(c Conn) (string, error) {
	c = dropLiteralSecrets(cloneConn(c))
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ParseConnJSON reads one pasted connection. A taken id gets a free one,
// a literal password is dropped, and the result must pass Validate.
func ParseConnJSON(s string, cs []Conn) (Conn, error) {
	var c Conn
	if err := json.Unmarshal(jsonc.Strip([]byte(s)), &c); err != nil {
		return Conn{}, fmt.Errorf("not a connection: %v", err)
	}
	c = dropLiteralSecrets(c)
	c.Driver = normDriver(c.Driver)
	if c.Port == 0 {
		c.Port = defaultPort(c.Driver)
	}
	if c.ID == "" {
		c.ID = "connection"
	}
	if connIndex(cs, c.ID) >= 0 {
		c.ID = freeID(cs, c.ID)
	}
	c.Source = SourceUser
	if err := Validate(c); err != nil {
		return Conn{}, err
	}
	return c, nil
}
