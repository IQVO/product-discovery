package search

import (
	"strconv"
)

// DefaultBandEdges are the fixed price-band edges in minor units (decision
// H6): <2500, 2500-5000, 5000-10000, 10000-25000, 25000+. A band includes its
// lower edge and excludes its upper one.
var DefaultBandEdges = []int64{2500, 5000, 10000, 25000}

// Band is one price-band facet value: [Lower, Upper) in minor units; Upper
// is 0 for the open last band.
type Band struct {
	Lower int64
	Upper int64
}

// Label is the facet value of the band: "<2500", "2500-4999", "25000+".
func (b Band) Label() string {
	if b.Upper == 0 {
		return strconv.FormatInt(b.Lower, 10) + "+"
	}
	if b.Lower == 0 {
		return "<" + strconv.FormatInt(b.Upper, 10)
	}
	return strconv.FormatInt(b.Lower, 10) + "-" + strconv.FormatInt(b.Upper-1, 10)
}

// Bands builds the bands from ascending positive edges. Edges that are not
// strictly ascending and positive are ignored from the first violation on.
func Bands(edges []int64) []Band {
	var out []Band
	var lower int64
	for _, e := range edges {
		if e <= lower {
			break
		}
		out = append(out, Band{Lower: lower, Upper: e})
		lower = e
	}
	return append(out, Band{Lower: lower})
}

// BandOf returns the band of price among bands: the first band whose upper
// edge is above the price, else the open last band, so every price (even a
// negative one) has a band.
func BandOf(bands []Band, price int64) Band {
	for _, b := range bands[:len(bands)-1] {
		if price < b.Upper {
			return b
		}
	}
	return bands[len(bands)-1]
}
