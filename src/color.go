package main

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
)

type palColor struct {
	name    string
	r, g, b float64
}

var palette = []palColor{
	{"red", 200, 50, 50},
	{"orange", 220, 140, 40},
	{"yellow", 220, 210, 60},
	{"green", 70, 170, 80},
	{"teal", 40, 170, 170},
	{"blue", 60, 100, 210},
	{"purple", 150, 70, 190},
	{"pink", 220, 110, 170},
	{"brown", 130, 90, 60},
	{"black", 30, 30, 30},
	{"gray", 140, 140, 140},
	{"white", 235, 235, 235},
}

func dominantColor(data []byte) string {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ""
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return ""
	}
	stepX, stepY := w/48+1, h/48+1
	var sr, sg, sb, n float64
	var br, bg, bb, bestSat float64
	for y := b.Min.Y; y < b.Max.Y; y += stepY {
		for x := b.Min.X; x < b.Max.X; x += stepX {
			r16, g16, b16, a16 := img.At(x, y).RGBA()
			if a16 < 0x8000 {
				continue
			}
			r, g, bl := float64(r16>>8), float64(g16>>8), float64(b16>>8)
			sr += r
			sg += g
			sb += bl
			n++
			mx := math.Max(r, math.Max(g, bl))
			mn := math.Min(r, math.Min(g, bl))
			sat := mx - mn
			if sat > bestSat && mx > 30 && mx < 245 {
				bestSat, br, bg, bb = sat, r, g, bl
			}
		}
	}
	if n == 0 {
		return ""
	}
	if bestSat < 30 {
		br, bg, bb = sr/n, sg/n, sb/n
	}
	return nearestPalette(br, bg, bb)
}

func nearestPalette(r, g, b float64) string {
	best, bestD := "", math.MaxFloat64
	for _, p := range palette {
		d := (p.r-r)*(p.r-r) + (p.g-g)*(p.g-g) + (p.b-b)*(p.b-b)
		if d < bestD {
			bestD, best = d, p.name
		}
	}
	return best
}
