package source

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

// Caps.
const (
	// MaxItemBytes caps one item's JSON (compact form).
	MaxItemBytes = 64 << 10
	// DefaultMaxItems is the item cap when a step sets none.
	DefaultMaxItems = 100
	// HardMaxItems is the highest max_items a step may ask for.
	HardMaxItems = 1000
	// maxKeyLen is the longest key kept as written; a longer one is hashed.
	maxKeyLen = 128
)

// Item is one item of a source.
type Item struct {
	// Key is stable for the same item across runs. It is the value at
	// the step's key path when that is a short plain string or number
	// (an issue id, a PR number, a file path), else "sha256:" and the
	// first 16 bytes of the SHA-256 of the item's canonical JSON (object
	// keys sorted), so formatting changes do not change it.
	Key string `json:"key"`
	// Data is the item's JSON, compact. Untrusted.
	Data json.RawMessage `json:"data"`
}

// Result is what an adapter returns.
type Result struct {
	Items []Item `json:"items"`
	// Truncated is set when the source had more items than max_items;
	// the first max_items are kept.
	Truncated bool `json:"truncated,omitempty"`
	// Total is how many items the source had before the cap.
	Total int `json:"total"`
}

// Shape are the fields every adapter shares: where the items are in the
// answer, how to key them, and how many to keep.
type Shape struct {
	// Items is the path of the list in the answer: "$", "$.a.b",
	// "$.a[*].b", "$.a[0]". "" means "$". A path without [*] that ends
	// on a list gives its elements; on any other value, that one value.
	// A missing path gives no items (never an error).
	Items string
	// Key is the path of the key inside one item, like "$.id"; "" uses
	// the content hash.
	Key string
	// MaxItems caps the items (0: DefaultMaxItems; above HardMaxItems
	// is an error).
	MaxItems int
}

// ErrTooBig is returned for an item over MaxItemBytes.
var ErrTooBig = errors.New("item too big")

// maxItems returns the cap, or an error for a bad one.
func (s Shape) maxItems() (int, error) {
	switch {
	case s.MaxItems < 0:
		return 0, fmt.Errorf("max_items must be 0 or more, not %d", s.MaxItems)
	case s.MaxItems == 0:
		return DefaultMaxItems, nil
	case s.MaxItems > HardMaxItems:
		return 0, fmt.Errorf("max_items %d is over the limit of %d", s.MaxItems, HardMaxItems)
	}
	return s.MaxItems, nil
}

// check validates the paths and the cap before any work is done.
func (s Shape) check() error {
	if _, err := parsePath(s.Items); err != nil {
		return fmt.Errorf("items: %w", err)
	}
	if _, err := parsePath(s.Key); err != nil {
		return fmt.Errorf("key: %w", err)
	}
	_, err := s.maxItems()
	return err
}

// collector builds a Result under the caps.
type collector struct {
	shape Shape
	max   int
	key   []seg
	res   Result
}

func newCollector(s Shape) (*collector, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	max, _ := s.maxItems()
	key, _ := parsePath(s.Key)
	return &collector{shape: s, max: max, key: key, res: Result{Items: []Item{}}}, nil
}

// addDoc selects the items in one JSON document and adds them.
func (c *collector) addDoc(doc []byte) error {
	vals, err := Select(doc, c.shape.Items)
	if err != nil {
		return err
	}
	for _, v := range vals {
		if err := c.add(v, ""); err != nil {
			return err
		}
	}
	return nil
}

// add adds one item; key "" derives it from the key path or the hash.
func (c *collector) add(raw json.RawMessage, key string) error {
	c.res.Total++
	if len(c.res.Items) >= c.max {
		c.res.Truncated = true
		return nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return fmt.Errorf("item %d is not valid JSON", c.res.Total)
	}
	if buf.Len() > MaxItemBytes {
		return fmt.Errorf("%w: item %d is %d KB, over the %d KB cap", ErrTooBig, c.res.Total, (buf.Len()+1023)/1024, MaxItemBytes>>10)
	}
	data := json.RawMessage(buf.Bytes())
	if key == "" {
		key = c.keyOf(data)
	} else {
		key = cleanKey(key, data)
	}
	c.res.Items = append(c.res.Items, Item{Key: key, Data: data})
	return nil
}

func (c *collector) keyOf(data json.RawMessage) string {
	if len(c.key) > 0 {
		vals, err := selectSegs(data, c.key)
		if err == nil && len(vals) == 1 {
			var s string
			if json.Unmarshal(vals[0], &s) == nil {
				return cleanKey(s, data)
			}
			var n json.Number
			d := json.NewDecoder(bytes.NewReader(vals[0]))
			d.UseNumber()
			if d.Decode(&n) == nil {
				return cleanKey(n.String(), data)
			}
		}
	}
	return hashKey(data)
}

var plainKeyRe = regexp.MustCompile(`^[A-Za-z0-9._:/#@+=-]+$`)

// cleanKey keeps a short plain key; anything else (spaces, new lines,
// control or very long text from an outside system) becomes the hash.
func cleanKey(k string, data json.RawMessage) string {
	if len(k) <= maxKeyLen && plainKeyRe.MatchString(k) {
		return k
	}
	return hashKey(data)
}

// hashKey is the content key: the SHA-256 of the canonical JSON.
func hashKey(data json.RawMessage) string {
	canon := data
	var v any
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if d.Decode(&v) == nil {
		if b, err := json.Marshal(v); err == nil {
			canon = b
		}
	}
	sum := sha256.Sum256(canon)
	return "sha256:" + hex.EncodeToString(sum[:16])
}
