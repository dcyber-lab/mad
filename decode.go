package jqgo

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Decoder reads a stream of whitespace-separated JSON values, the input
// format jq accepts. Objects become map[string]any, integers that fit
// become int and other numbers float64.
type Decoder struct {
	r    *bufio.Reader
	line int
	col  int
	buf  []byte

	started bool
}

// NewDecoder returns a Decoder reading from r.
func NewDecoder(r io.Reader) *Decoder {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReaderSize(r, 64*1024)
	}
	return &Decoder{r: br, line: 1}
}

// SyntaxError reports malformed JSON input.
type SyntaxError struct {
	Msg          string
	Line, Column int
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("%s at line %d, column %d", e.Msg, e.Line, e.Column)
}

// Decode returns the next value, or io.EOF when the stream is exhausted.
func (d *Decoder) Decode() (any, error) {
	if !d.started {
		d.started = true
		if b, err := d.r.Peek(3); err == nil && string(b) == "\ufeff" {
			d.r.Discard(3)
		}
	}
	c, err := d.skipSpace()
	if err != nil {
		return nil, err
	}
	return d.value(c)
}

func parseJSON(b []byte) (any, error) {
	d := NewDecoder(bytes.NewReader(b))
	v, err := d.Decode()
	if err != nil {
		if err == io.EOF {
			return nil, &SyntaxError{Msg: "Expected JSON value", Line: d.line, Column: d.col}
		}
		return nil, err
	}
	if _, err := d.skipSpace(); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, d.errorf("Unexpected extra JSON values")
	}
	return v, nil
}

func (d *Decoder) errorf(format string, args ...any) error {
	return &SyntaxError{Msg: fmt.Sprintf(format, args...), Line: d.line, Column: d.col}
}

func (d *Decoder) read() (byte, error) {
	c, err := d.r.ReadByte()
	if err != nil {
		return 0, err
	}
	if c == '\n' {
		d.line++
		d.col = 0
	} else {
		d.col++
	}
	return c, nil
}

func (d *Decoder) unread() {
	_ = d.r.UnreadByte()
	d.col--
}

func (d *Decoder) skipSpace() (byte, error) {
	for {
		c, err := d.read()
		if err != nil {
			return 0, err
		}
		switch c {
		case ' ', '\t', '\n', '\r':
			continue
		}
		return c, nil
	}
}

func (d *Decoder) need() (byte, error) {
	c, err := d.skipSpace()
	if err == io.EOF {
		return 0, d.errorf("Unfinished JSON term at EOF")
	}
	return c, err
}

func (d *Decoder) value(c byte) (any, error) {
	switch c {
	case '{':
		return d.object()
	case '[':
		return d.array()
	case '"':
		return d.str()
	case 't':
		return true, d.literal("rue")
	case 'f':
		return false, d.literal("alse")
	case 'n':
		c, err := d.read()
		if err == nil && c == 'a' {
			return nan, d.literal("n")
		}
		if err == nil {
			d.unread()
		}
		return nil, d.literal("ull")
	case 'N':
		return nan, d.literal("aN")
	case 'I':
		return math.Inf(1), d.literal("nfinity")
	case '-':
		if b, _ := d.r.Peek(1); len(b) == 1 {
			switch b[0] {
			case 'I':
				d.read()
				return math.Inf(-1), d.literal("nfinity")
			case 'N', 'n':
				d.read()
				if b[0] == 'N' {
					return nan, d.literal("aN")
				}
				return nan, d.literal("an")
			}
		}
		return d.number(c)
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return d.number(c)
	}
	return nil, d.errorf("Invalid literal %q", rune(c))
}

func (d *Decoder) literal(rest string) error {
	for i := 0; i < len(rest); i++ {
		c, err := d.read()
		if err != nil || c != rest[i] {
			return d.errorf("Invalid literal")
		}
	}
	return d.checkDelim()
}

// checkDelim makes sure a scalar is not glued to the next token, e.g. "truex".
func (d *Decoder) checkDelim() error {
	c, err := d.r.ReadByte()
	if err != nil {
		return nil
	}
	_ = d.r.UnreadByte()
	switch c {
	case ' ', '\t', '\n', '\r', ',', ']', '}', ':', '[', '{', '"':
		return nil
	}
	return d.errorf("Invalid literal")
}

func (d *Decoder) number(first byte) (any, error) {
	d.buf = append(d.buf[:0], first)
	isInt := true
	for {
		c, err := d.r.ReadByte()
		if err != nil {
			break
		}
		if (c >= '0' && c <= '9') || c == '-' || c == '+' {
			d.buf = append(d.buf, c)
			d.col++
			continue
		}
		if c == '.' || c == 'e' || c == 'E' {
			isInt = false
			d.buf = append(d.buf, c)
			d.col++
			continue
		}
		_ = d.r.UnreadByte()
		break
	}
	s := string(d.buf)
	if !validNumber(s) {
		return nil, d.errorf("Invalid numeric literal")
	}
	if isInt {
		if i, err := strconv.ParseInt(s, 10, 64); err == nil && int64(int(i)) == i {
			return int(i), d.checkDelim()
		}
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return nil, d.errorf("Invalid numeric literal")
	}
	return f, d.checkDelim()
}

// validNumber checks JSON number grammar (strconv is more permissive).
func validNumber(s string) bool {
	i := 0
	if i < len(s) && s[i] == '-' {
		i++
	}
	if i >= len(s) {
		return false
	}
	if s[i] == '0' {
		i++
	} else if s[i] >= '1' && s[i] <= '9' {
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
	} else {
		return false
	}
	if i < len(s) && s[i] == '.' {
		i++
		start := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == start {
			return false
		}
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		start := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == start {
			return false
		}
	}
	return i == len(s)
}

func (d *Decoder) str() (string, error) {
	var sb strings.Builder
	for {
		c, err := d.r.ReadByte()
		if err != nil {
			return "", d.errorf("Unfinished string at EOF")
		}
		d.col++
		switch {
		case c == '"':
			s := sb.String()
			if !utf8.ValidString(s) {
				s = strings.ToValidUTF8(s, "�")
			}
			return s, nil
		case c == '\\':
			if err := d.escape(&sb); err != nil {
				return "", err
			}
		case c == '\n':
			d.line++
			d.col = 0
			sb.WriteByte(c)
		default:
			sb.WriteByte(c)
		}
	}
}

func (d *Decoder) escape(sb *strings.Builder) error {
	c, err := d.read()
	if err != nil {
		return d.errorf("Unfinished string at EOF")
	}
	switch c {
	case '"', '\\', '/':
		sb.WriteByte(c)
	case 'b':
		sb.WriteByte('\b')
	case 'f':
		sb.WriteByte('\f')
	case 'n':
		sb.WriteByte('\n')
	case 'r':
		sb.WriteByte('\r')
	case 't':
		sb.WriteByte('\t')
	case 'u':
		r, err := d.hex4()
		if err != nil {
			return err
		}
		if utf16.IsSurrogate(r) {
			// Try to pair with a following \uXXXX low surrogate.
			if b, _ := d.r.Peek(2); len(b) == 2 && b[0] == '\\' && b[1] == 'u' {
				d.read()
				d.read()
				r2, err := d.hex4()
				if err != nil {
					return err
				}
				if dec := utf16.DecodeRune(r, r2); dec != utf8.RuneError {
					sb.WriteRune(dec)
					return nil
				}
				sb.WriteRune(utf8.RuneError)
				if utf16.IsSurrogate(r2) {
					sb.WriteRune(utf8.RuneError)
				} else {
					sb.WriteRune(r2)
				}
				return nil
			}
			r = utf8.RuneError
		}
		sb.WriteRune(r)
	default:
		return d.errorf("Invalid escape")
	}
	return nil
}

func (d *Decoder) hex4() (rune, error) {
	var r rune
	for i := 0; i < 4; i++ {
		c, err := d.read()
		if err != nil {
			return 0, d.errorf("Invalid \\uXXXX escape")
		}
		switch {
		case c >= '0' && c <= '9':
			r = r<<4 | rune(c-'0')
		case c >= 'a' && c <= 'f':
			r = r<<4 | rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			r = r<<4 | rune(c-'A'+10)
		default:
			return 0, d.errorf("Invalid \\uXXXX escape")
		}
	}
	return r, nil
}

func (d *Decoder) array() (any, error) {
	arr := []any{}
	c, err := d.need()
	if err != nil {
		return nil, err
	}
	if c == ']' {
		return arr, nil
	}
	for {
		v, err := d.value(c)
		if err != nil {
			return nil, err
		}
		arr = append(arr, v)
		c, err = d.need()
		if err != nil {
			return nil, err
		}
		if c == ']' {
			return arr, nil
		}
		if c != ',' {
			return nil, d.errorf("Expected separator between values")
		}
		if c, err = d.need(); err != nil {
			return nil, err
		}
	}
}

func (d *Decoder) object() (any, error) {
	obj := map[string]any{}
	c, err := d.need()
	if err != nil {
		return nil, err
	}
	if c == '}' {
		return obj, nil
	}
	for {
		if c != '"' {
			return nil, d.errorf("Object keys must be strings")
		}
		k, err := d.str()
		if err != nil {
			return nil, err
		}
		if c, err = d.need(); err != nil {
			return nil, err
		}
		if c != ':' {
			return nil, d.errorf("Objects must consist of key:value pairs")
		}
		if c, err = d.need(); err != nil {
			return nil, err
		}
		v, err := d.value(c)
		if err != nil {
			return nil, err
		}
		obj[k] = v
		if c, err = d.need(); err != nil {
			return nil, err
		}
		if c == '}' {
			return obj, nil
		}
		if c != ',' {
			return nil, d.errorf("Expected separator between values")
		}
		if c, err = d.need(); err != nil {
			return nil, err
		}
	}
}
