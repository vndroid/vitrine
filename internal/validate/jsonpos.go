package validate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// kind of a JSON value
type kind int

const (
	kNull kind = iota
	kBool
	kNumber
	kString
	kArray
	kObject
)

func (k kind) String() string {
	return [...]string{"null", "boolean", "number", "string", "array", "object"}[k]
}

// node is a JSON value with its position in the source.
type node struct {
	kind   kind
	offset int64 // of the value
	value  any   // bool, json.Number, string for scalars
	keys   []string
	fields map[string]*node
	keyOff map[string]int64 // offset of each key
	items  []*node
}

// parse reads a JSON value (comments already blanked) and records the
// offsets of all values and keys.
func parse(src []byte) (*node, error) {
	dec := json.NewDecoder(bytes.NewReader(src))
	dec.UseNumber()
	p := &parser{dec: dec, src: src}
	n, err := p.value()
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, &json.SyntaxError{Offset: dec.InputOffset()}
	}
	return n, nil
}

type parser struct {
	dec *json.Decoder
	src []byte
}

// start returns the offset of the next token, skipping what lies between
// the previous token and it.
func (p *parser) start() int64 {
	off := p.dec.InputOffset()
	for off < int64(len(p.src)) {
		switch p.src[off] {
		case ' ', '\t', '\r', '\n', ',', ':':
			off++
			continue
		}
		break
	}
	return off
}

func (p *parser) value() (*node, error) {
	off := p.start()
	tok, err := p.dec.Token()
	if err != nil {
		return nil, err
	}
	n := &node{offset: off}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			n.kind = kObject
			n.fields = map[string]*node{}
			n.keyOff = map[string]int64{}
			for p.dec.More() {
				koff := p.start()
				ktok, err := p.dec.Token()
				if err != nil {
					return nil, err
				}
				key, _ := ktok.(string)
				child, err := p.value()
				if err != nil {
					return nil, err
				}
				if _, dup := n.fields[key]; !dup {
					n.keys = append(n.keys, key)
				}
				n.fields[key] = child
				n.keyOff[key] = koff
			}
		case '[':
			n.kind = kArray
			for p.dec.More() {
				child, err := p.value()
				if err != nil {
					return nil, err
				}
				n.items = append(n.items, child)
			}
		default:
			return nil, fmt.Errorf("unexpected %v", t)
		}
		if _, err := p.dec.Token(); err != nil { // closing delimiter
			return nil, err
		}
	case bool:
		n.kind, n.value = kBool, t
	case json.Number:
		n.kind, n.value = kNumber, t
	case string:
		n.kind, n.value = kString, t
	case nil:
		n.kind = kNull
	}
	return n, nil
}

// get returns the node at a dot separated key path.
func (n *node) get(path string) *node {
	cur := n
	for _, key := range splitPath(path) {
		if cur == nil || cur.kind != kObject {
			return nil
		}
		cur = cur.fields[key]
	}
	return cur
}

func splitPath(path string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(path); i++ {
		if i == len(path) || path[i] == '.' {
			if i > start {
				out = append(out, path[start:i])
			}
			start = i + 1
		}
	}
	return out
}
