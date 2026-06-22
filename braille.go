package main

import (
	"math"
	"strings"
)

// brailleDot maps a sub-cell coordinate (column 0..1, row 0..3) to its bit
// within a Unicode braille cell (base U+2800). The dot numbering is:
//
//	1 4      0x01 0x08
//	2 5  ->  0x02 0x10
//	3 6      0x04 0x20
//	7 8      0x40 0x80
var brailleDot = [2][4]byte{
	{0x01, 0x02, 0x04, 0x40}, // left column  (dots 1,2,3,7)
	{0x08, 0x10, 0x20, 0x80}, // right column (dots 4,5,6,8)
}

// brailleCanvas is a dot grid rendered with Unicode braille characters. It is
// wCells×hCells character cells, giving (wCells*2)×(hCells*4) addressable dots.
type brailleCanvas struct {
	wCells, hCells int
	cells          []byte // one braille bitmask per character cell
}

func newBrailleCanvas(wCells, hCells int) *brailleCanvas {
	if wCells < 1 {
		wCells = 1
	}
	if hCells < 1 {
		hCells = 1
	}
	return &brailleCanvas{wCells: wCells, hCells: hCells, cells: make([]byte, wCells*hCells)}
}

func (c *brailleCanvas) dotW() int { return c.wCells * 2 }
func (c *brailleCanvas) dotH() int { return c.hCells * 4 }

// set turns on the dot at pixel (x,y); (0,0) is top-left. Out-of-range is ignored.
func (c *brailleCanvas) set(x, y int) {
	if x < 0 || y < 0 || x >= c.dotW() || y >= c.dotH() {
		return
	}
	c.cells[(y/4)*c.wCells+(x/2)] |= brailleDot[x%2][y%4]
}

// plotLine draws a connected line of values (oldest→newest) with the newest
// aligned to the right edge — one dot column per value — scaled so that `max`
// reaches the top of the canvas. Columns with no data (buffer not yet full)
// are left blank on the left.
func (c *brailleCanvas) plotLine(values []float64, max float64) {
	n := len(values)
	if n == 0 || max <= 0 {
		return
	}
	dw, dh := c.dotW(), c.dotH()
	yFor := func(v float64) int {
		r := v / max
		if r < 0 {
			r = 0
		}
		if r > 1 {
			r = 1
		}
		return int(math.Round((1 - r) * float64(dh-1)))
	}
	prevY, hasPrev := 0, false
	for col := 0; col < dw; col++ {
		idx := n - dw + col // newest value at the rightmost column
		if idx < 0 {
			hasPrev = false
			continue
		}
		y := yFor(values[idx])
		if hasPrev {
			// Connect to the previous point with a thin line: split the vertical
			// step across the two columns at the midpoint, so a steep change
			// renders as a diagonal rather than a solid full-height vertical bar
			// (which read as a "fill", especially on a plateau at the peak).
			mid := (prevY + y) / 2
			c.vspan(col-1, prevY, mid)
			c.vspan(col, mid, y)
		} else {
			c.set(col, y)
		}
		prevY, hasPrev = y, true
	}
}

// vspan sets every dot in column x between rows a and b (inclusive).
func (c *brailleCanvas) vspan(x, a, b int) {
	if a > b {
		a, b = b, a
	}
	for y := a; y <= b; y++ {
		c.set(x, y)
	}
}

// plotArea draws the same line as plotLine but fills every dot from the value
// down to the baseline, producing a filled "mountain" chart.
func (c *brailleCanvas) plotArea(values []float64, max float64) {
	n := len(values)
	if n == 0 || max <= 0 {
		return
	}
	dw, dh := c.dotW(), c.dotH()
	for col := 0; col < dw; col++ {
		idx := n - dw + col
		if idx < 0 {
			continue
		}
		r := values[idx] / max
		if r < 0 {
			r = 0
		}
		if r > 1 {
			r = 1
		}
		top := int(math.Round((1 - r) * float64(dh-1)))
		for y := top; y < dh; y++ {
			c.set(col, y)
		}
	}
}

// rows renders the canvas to hCells strings, each wCells braille runes wide.
func (c *brailleCanvas) rows() []string {
	out := make([]string, c.hCells)
	var b strings.Builder
	for cy := 0; cy < c.hCells; cy++ {
		b.Reset()
		for cx := 0; cx < c.wCells; cx++ {
			b.WriteRune(rune(0x2800 + int(c.cells[cy*c.wCells+cx])))
		}
		out[cy] = b.String()
	}
	return out
}
