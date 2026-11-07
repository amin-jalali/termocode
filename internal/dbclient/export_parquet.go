package dbclient

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
)

// FormatParquet is the Apache Parquet file format: columns, a footer with
// the schema, readable by data tools. Every column is an optional UTF-8
// string (lossless for any database type); NULL stays NULL.
const FormatParquet ExportFormat = "parquet"

// parquetRowGroupRows is how many rows are kept in memory before they are
// written as one row group, so a large export never holds the whole result.
const parquetRowGroupRows = 65536

// Parquet enum values (parquet.thrift).
const (
	pqTypeByteArray   = 6
	pqRepOptional     = 1
	pqConvertedUTF8   = 0
	pqEncodingPlain   = 0
	pqEncodingRLE     = 3
	pqCodecNone       = 0
	pqPageData        = 0
	pqFileVersion     = 1
	pqCreatedBy       = "termocode"
	pqMagic           = "PAR1"
	pqThriftI32       = 5
	pqThriftI64       = 6
	pqThriftBinary    = 8
	pqThriftList      = 9
	pqThriftStruct    = 12
	pqThriftStructEnd = 0
)

type pqColumn struct {
	defs   []bool // true = a value, false = NULL
	values bytes.Buffer
}

type pqChunk struct {
	offset, size, values int64
}

type pqGroup struct {
	chunks []pqChunk
	rows   int64
	bytes  int64
}

// parquetWriter buffers up to parquetRowGroupRows rows per column, then
// writes them as a row group; End writes the footer.
type parquetWriter struct {
	w      io.Writer
	off    int64
	names  []string
	cols   []pqColumn
	rows   int64
	total  int64
	groups []pqGroup
	err    error
}

func (p *parquetWriter) write(b []byte) error {
	if p.err != nil {
		return p.err
	}
	n, err := p.w.Write(b)
	p.off += int64(n)
	p.err = err
	return err
}

// parquetNames makes every column name unique and non-empty.
func parquetNames(cols []Column) []string {
	seen := map[string]bool{}
	out := make([]string, len(cols))
	for i, c := range cols {
		name := c.Name
		if name == "" {
			name = "column_" + strconv.Itoa(i+1)
		}
		base := name
		for n := 2; seen[name]; n++ {
			name = base + "_" + strconv.Itoa(n)
		}
		seen[name] = true
		out[i] = name
	}
	return out
}

func (p *parquetWriter) Columns(cols []Column) error {
	p.names = parquetNames(cols)
	p.cols = make([]pqColumn, len(cols))
	return p.write([]byte(pqMagic))
}

func (p *parquetWriter) Row(row []Cell) error {
	if p.err != nil {
		return p.err
	}
	for i := range p.cols {
		c := &p.cols[i]
		if i >= len(row) || row[i].Null {
			c.defs = append(c.defs, false)
			continue
		}
		c.defs = append(c.defs, true)
		var n [4]byte
		binary.LittleEndian.PutUint32(n[:], uint32(len(row[i].Text)))
		c.values.Write(n[:])
		c.values.WriteString(row[i].Text)
	}
	p.rows++
	if p.rows >= parquetRowGroupRows {
		return p.flush()
	}
	return nil
}

// flush writes the buffered rows as one row group: one data page per column.
func (p *parquetWriter) flush() error {
	if p.rows == 0 || p.err != nil {
		return p.err
	}
	g := pqGroup{rows: p.rows}
	for i := range p.cols {
		c := &p.cols[i]
		levels := pqDefLevels(c.defs)
		body := make([]byte, 0, 4+len(levels)+c.values.Len())
		body = binary.LittleEndian.AppendUint32(body, uint32(len(levels)))
		body = append(body, levels...)
		body = append(body, c.values.Bytes()...)
		if len(body) > 1<<31-1 {
			p.err = fmt.Errorf("column %s is too large for one Parquet page", p.names[i])
			return p.err
		}
		var h thriftBuf
		h.i32(1, pqPageData)
		h.i32(2, int32(len(body)))
		h.i32(3, int32(len(body)))
		h.structBegin(5) // data_page_header
		h.i32(1, int32(len(c.defs)))
		h.i32(2, pqEncodingPlain)
		h.i32(3, pqEncodingRLE)
		h.i32(4, pqEncodingRLE)
		h.structEnd()
		h.end()
		ch := pqChunk{offset: p.off, values: int64(len(c.defs))}
		if p.write(h.b) != nil || p.write(body) != nil {
			return p.err
		}
		ch.size = p.off - ch.offset
		g.chunks = append(g.chunks, ch)
		g.bytes += ch.size
		c.defs = c.defs[:0]
		c.values.Reset()
	}
	p.groups = append(p.groups, g)
	p.total += p.rows
	p.rows = 0
	return nil
}

func (p *parquetWriter) End() error {
	if p.names == nil {
		if err := p.Columns(nil); err != nil {
			return err
		}
	}
	if err := p.flush(); err != nil {
		return err
	}
	var f thriftBuf
	f.i32(1, pqFileVersion)
	f.listBegin(2, pqThriftStruct, len(p.names)+1)
	f.elemStruct()
	f.str(4, "schema")
	f.i32(5, int32(len(p.names)))
	f.elemEnd()
	for _, name := range p.names {
		f.elemStruct()
		f.i32(1, pqTypeByteArray)
		f.i32(3, pqRepOptional)
		f.str(4, name)
		f.i32(6, pqConvertedUTF8)
		f.elemEnd()
	}
	f.i64(3, p.total)
	f.listBegin(4, pqThriftStruct, len(p.groups))
	for _, g := range p.groups {
		f.elemStruct()
		f.listBegin(1, pqThriftStruct, len(g.chunks))
		for i, ch := range g.chunks {
			f.elemStruct()
			f.i64(2, ch.offset)
			f.structBegin(3) // meta_data
			f.i32(1, pqTypeByteArray)
			f.listBegin(2, pqThriftI32, 2)
			f.zigzag(pqEncodingPlain)
			f.zigzag(pqEncodingRLE)
			f.listBegin(3, pqThriftBinary, 1)
			f.bin(p.names[i])
			f.i32(4, pqCodecNone)
			f.i64(5, ch.values)
			f.i64(6, ch.size)
			f.i64(7, ch.size)
			f.i64(9, ch.offset)
			f.structEnd()
			f.elemEnd()
		}
		f.i64(2, g.bytes)
		f.i64(3, g.rows)
		f.elemEnd()
	}
	f.str(6, pqCreatedBy)
	f.end()
	if p.write(f.b) != nil {
		return p.err
	}
	return p.write(append(binary.LittleEndian.AppendUint32(nil, uint32(len(f.b))), pqMagic...))
}

// pqDefLevels encodes definition levels (bit width 1) as RLE runs of the
// RLE / bit-packing hybrid.
func pqDefLevels(defs []bool) []byte {
	var out []byte
	for i := 0; i < len(defs); {
		j := i
		for j < len(defs) && defs[j] == defs[i] {
			j++
		}
		out = binary.AppendUvarint(out, uint64(j-i)<<1)
		if defs[i] {
			out = append(out, 1)
		} else {
			out = append(out, 0)
		}
		i = j
	}
	return out
}

// thriftBuf writes the Thrift compact protocol, just what Parquet needs.
// Field ids are kept per struct level so the deltas stay right.
type thriftBuf struct {
	b    []byte
	last []int16
	cur  int16
}

func (t *thriftBuf) field(id int16, typ byte) {
	if d := id - t.cur; d > 0 && d <= 15 {
		t.b = append(t.b, byte(d)<<4|typ)
	} else {
		t.b = append(t.b, typ)
		t.zigzag(int64(id))
	}
	t.cur = id
}

func (t *thriftBuf) zigzag(v int64) {
	t.b = binary.AppendUvarint(t.b, uint64((v<<1)^(v>>63)))
}

func (t *thriftBuf) i32(id int16, v int32) { t.field(id, pqThriftI32); t.zigzag(int64(v)) }
func (t *thriftBuf) i64(id int16, v int64) { t.field(id, pqThriftI64); t.zigzag(v) }
func (t *thriftBuf) str(id int16, s string) {
	t.field(id, pqThriftBinary)
	t.bin(s)
}
func (t *thriftBuf) bin(s string) {
	t.b = binary.AppendUvarint(t.b, uint64(len(s)))
	t.b = append(t.b, s...)
}

func (t *thriftBuf) listBegin(id int16, elem byte, n int) {
	t.field(id, pqThriftList)
	if n < 15 {
		t.b = append(t.b, byte(n)<<4|elem)
		return
	}
	t.b = append(t.b, 0xf0|elem)
	t.b = binary.AppendUvarint(t.b, uint64(n))
}

func (t *thriftBuf) push()    { t.last = append(t.last, t.cur); t.cur = 0 }
func (t *thriftBuf) pop()     { t.cur = t.last[len(t.last)-1]; t.last = t.last[:len(t.last)-1] }
func (t *thriftBuf) end()     { t.b = append(t.b, pqThriftStructEnd) }
func (t *thriftBuf) elemEnd() { t.end(); t.pop() }

// elemStruct starts a struct that is a list element (no field header).
func (t *thriftBuf) elemStruct() { t.push() }

// structBegin starts a struct field; structEnd closes it.
func (t *thriftBuf) structBegin(id int16) { t.field(id, pqThriftStruct); t.push() }
func (t *thriftBuf) structEnd()           { t.end(); t.pop() }
