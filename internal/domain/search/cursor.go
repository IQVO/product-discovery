package search

import (
	"encoding/base64"
	"encoding/json"
	"errors"
)

// Mode is the matching strategy a page was produced with. A search starts in
// fts (or browse without text); when the full-text query finds no row at all
// the engine falls back to trgm, and every later page stays in that mode.
type Mode string

// The three matching strategies.
const (
	ModeBrowse Mode = "browse"
	ModeFTS    Mode = "fts"
	ModeTrgm   Mode = "trgm"
)

// Cursor is the keyset position after the last hit of a page: the sort key
// values of that hit plus its catalogItemId as the tiebreak. It is opaque to
// clients (base64url JSON).
type Cursor struct {
	Sort  Sort    `json:"s"`
	Mode  Mode    `json:"m"`
	Rank  float64 `json:"r,omitempty"`
	Price int64   `json:"p,omitempty"`
	Title string  `json:"t,omitempty"`
	ID    string  `json:"i"`
}

// ErrBadCursor is returned for a cursor that does not decode.
var ErrBadCursor = errors.New("search: bad cursor")

// Encode returns the opaque string form of c.
func (c Cursor) Encode() string {
	b, _ := json.Marshal(c) // a struct of plain fields cannot fail to marshal
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeCursor parses the string Encode produced.
func DecodeCursor(s string) (Cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, ErrBadCursor
	}
	var c Cursor
	if err := json.Unmarshal(raw, &c); err != nil || c.ID == "" {
		return Cursor{}, ErrBadCursor
	}
	switch c.Mode {
	case ModeBrowse, ModeFTS, ModeTrgm:
	default:
		return Cursor{}, ErrBadCursor
	}
	return c, nil
}
