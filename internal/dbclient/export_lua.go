package dbclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// Lua custom extractors (DBX-36). A script in .termocode/extractors/NAME.lua
// is the export format "lua:NAME". It may define:
//
//	extension = ".ndjson"      -- the file ending (default ".txt")
//	function header(cols) end  -- cols: list of column names; returns text
//	function row(r, i) end     -- r.name or r[1]; NULL is nil; returns text
//	function footer(n) end     -- n rows were written; returns text
//
// The script runs in a sandbox: only the base, table, string and math
// libraries, no files, no processes, no load or require, and each call stops
// after luaCallLimit.

// FormatLuaPrefix starts the ExportFormat of a Lua extractor.
const FormatLuaPrefix = "lua:"

// luaCallLimit is how long one call into a script may run.
var luaCallLimit = 2 * time.Second

// ErrLuaTooLong is returned when a script call runs past luaCallLimit.
var ErrLuaTooLong = errors.New("the Lua extractor ran too long and was stopped")

func newLuaState() *lua.LState {
	L := lua.NewState(lua.Options{SkipOpenLibs: true, RegistryMaxSize: 1 << 20})
	for _, lib := range []struct {
		name string
		fn   lua.LGFunction
	}{
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
	} {
		L.Push(L.NewFunction(lib.fn))
		L.Push(lua.LString(lib.name))
		L.Call(1, 0)
	}
	for _, name := range []string{"dofile", "loadfile", "load", "loadstring", "require", "module", "collectgarbage", "getfenv", "setfenv"} {
		L.SetGlobal(name, lua.LNil)
	}
	return L
}

// loadLua compiles and runs the script's top level.
func loadLua(src string) (*lua.LState, error) {
	if strings.TrimSpace(src) == "" {
		return nil, errors.New("the Lua extractor has no script")
	}
	L := newLuaState()
	ctx, cancel := context.WithTimeout(context.Background(), luaCallLimit)
	defer cancel()
	L.SetContext(ctx)
	err := L.DoString(src)
	L.RemoveContext()
	if err != nil {
		L.Close()
		return nil, fmt.Errorf("Lua extractor: %w", luaErr(err, ctx))
	}
	return L, nil
}

func luaErr(err error, ctx context.Context) error {
	if ctx.Err() != nil {
		return ErrLuaTooLong
	}
	msg := err.Error()
	if i := strings.Index(msg, "\nstack traceback"); i >= 0 {
		msg = msg[:i]
	}
	return errors.New(strings.TrimSpace(msg))
}

// LuaExtension is the file ending the script asks for (".txt" if none).
func LuaExtension(src string) string {
	L, err := loadLua(src)
	if err != nil {
		return ".txt"
	}
	defer L.Close()
	if s, ok := L.GetGlobal("extension").(lua.LString); ok {
		e := string(s)
		if e != "" && !strings.ContainsAny(e, `/\`) {
			if !strings.HasPrefix(e, ".") {
				e = "." + e
			}
			return e
		}
	}
	return ".txt"
}

// FindLuaExtractors lists the *.lua scripts of dir as export formats.
func FindLuaExtractors(dir string) []ExportFormatInfo {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []ExportFormatInfo
	for _, e := range ents {
		name, ok := strings.CutSuffix(e.Name(), ".lua")
		if !ok || e.IsDir() || name == "" {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, ExportFormatInfo{Format: ExportFormat(FormatLuaPrefix + name), Title: "Lua: " + name, Ext: LuaExtension(string(src))})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Format < out[j].Format })
	return out
}

type luaWriter struct {
	w     io.Writer
	L     *lua.LState
	cols  []string
	n     int
	hdr   lua.LValue
	rowFn lua.LValue
	ftr   lua.LValue
}

func newLuaWriter(w io.Writer, src string) (RowWriter, error) {
	L, err := loadLua(src)
	if err != nil {
		return nil, err
	}
	return &luaWriter{w: w, L: L, hdr: L.GetGlobal("header"), rowFn: L.GetGlobal("row"), ftr: L.GetGlobal("footer")}, nil
}

// call runs fn with args and writes a string result.
func (l *luaWriter) call(fn lua.LValue, args ...lua.LValue) error {
	if fn.Type() != lua.LTFunction {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), luaCallLimit)
	defer cancel()
	l.L.SetContext(ctx)
	defer l.L.RemoveContext()
	err := l.L.CallByParam(lua.P{Fn: fn, NRet: 1, Protect: true}, args...)
	if err != nil {
		return fmt.Errorf("Lua extractor: %w", luaErr(err, ctx))
	}
	ret := l.L.Get(-1)
	l.L.Pop(1)
	switch v := ret.(type) {
	case lua.LString:
		_, err = io.WriteString(l.w, string(v))
		return err
	case lua.LNumber:
		_, err = io.WriteString(l.w, v.String())
		return err
	}
	return nil
}

func (l *luaWriter) Columns(cols []Column) error {
	t := l.L.NewTable()
	l.cols = make([]string, len(cols))
	for i, c := range cols {
		l.cols[i] = c.Name
		t.Append(lua.LString(c.Name))
	}
	return l.call(l.hdr, t)
}

func (l *luaWriter) Row(row []Cell) error {
	t := l.L.NewTable()
	for i, c := range row {
		if c.Null {
			continue
		}
		var v lua.LValue = lua.LString(c.Text)
		t.RawSetInt(i+1, v)
		if i < len(l.cols) {
			t.RawSetString(l.cols[i], v)
		}
	}
	l.n++
	return l.call(l.rowFn, t, lua.LNumber(l.n))
}

func (l *luaWriter) End() error {
	defer l.L.Close()
	return l.call(l.ftr, lua.LNumber(l.n))
}
