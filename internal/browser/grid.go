package browser

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
)

// OverlayGrid draws a subtle 100px coordinate grid and axis labels onto a base64 JPEG
// in memory so vision models can read exact (x, y) pixel coordinates on <canvas>
// without mutating the live page DOM or the user's screencast.
func OverlayGrid(b64JPEG string, cssWidth, cssHeight int) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(b64JPEG)
	if err != nil {
		return b64JPEG, err
	}
	src, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		return b64JPEG, err
	}

	bounds := src.Bounds()
	dst := image.NewRGBA(bounds)
	draw.Draw(dst, bounds, src, bounds.Min, draw.Src)

	imgW := bounds.Dx()
	imgH := bounds.Dy()
	if cssWidth <= 0 {
		cssWidth = imgW
	}
	if cssHeight <= 0 {
		cssHeight = imgH
	}
	scaleX := float64(imgW) / float64(cssWidth)
	scaleY := float64(imgH) / float64(cssHeight)

	lineCol := color.RGBA{R: 59, G: 130, B: 246, A: 85}
	majorCol := color.RGBA{R: 59, G: 130, B: 246, A: 145}
	badgeBg := color.RGBA{R: 15, G: 23, B: 42, A: 205}
	badgeFg := color.RGBA{R: 255, G: 255, B: 255, A: 240}

	blendPixel := func(x, y int, c color.RGBA) {
		if x < 0 || y < 0 || x >= imgW || y >= imgH {
			return
		}
		cur := dst.RGBAAt(x, y)
		a := uint32(c.A)
		inv := 255 - a
		dst.SetRGBA(x, y, color.RGBA{
			R: uint8((uint32(cur.R)*inv + uint32(c.R)*a) / 255),
			G: uint8((uint32(cur.G)*inv + uint32(c.G)*a) / 255),
			B: uint8((uint32(cur.B)*inv + uint32(c.B)*a) / 255),
			A: 255,
		})
	}

	// Draw vertical grid lines every 100 CSS px
	for cx := 100; cx < cssWidth; cx += 100 {
		px := int(float64(cx) * scaleX)
		col := lineCol
		if cx%200 == 0 {
			col = majorCol
		}
		for y := 0; y < imgH; y++ {
			blendPixel(px, y, col)
		}
		drawLabel(dst, px+3, 4, fmt.Sprintf("%d", cx), badgeBg, badgeFg)
	}

	// Draw horizontal grid lines every 100 CSS px
	for cy := 100; cy < cssHeight; cy += 100 {
		py := int(float64(cy) * scaleY)
		col := lineCol
		if cy%200 == 0 {
			col = majorCol
		}
		for x := 0; x < imgW; x++ {
			blendPixel(x, py, col)
		}
		drawLabel(dst, 4, py+3, fmt.Sprintf("%d", cy), badgeBg, badgeFg)
	}

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 82}); err != nil {
		return b64JPEG, err
	}
	return base64.StdEncoding.EncodeToString(out.Bytes()), nil
}

// Tiny 3x5 bitmap font for digits 0-9
var digitGlyphs = map[rune][5]uint8{
	'0': {0b111, 0b101, 0b101, 0b101, 0b111},
	'1': {0b010, 0b110, 0b010, 0b010, 0b111},
	'2': {0b111, 0b001, 0b111, 0b100, 0b111},
	'3': {0b111, 0b001, 0b111, 0b001, 0b111},
	'4': {0b101, 0b101, 0b111, 0b001, 0b001},
	'5': {0b111, 0b100, 0b111, 0b001, 0b111},
	'6': {0b111, 0b100, 0b111, 0b101, 0b111},
	'7': {0b111, 0b001, 0b001, 0b001, 0b001},
	'8': {0b111, 0b101, 0b111, 0b101, 0b111},
	'9': {0b111, 0b101, 0b111, 0b001, 0b111},
}

func drawLabel(dst *image.RGBA, x, y int, text string, bg, fg color.RGBA) {
	b := dst.Bounds()
	w := len(text)*4 + 3
	h := 9
	for dy := 0; dy < h; dy++ {
		for dx := 0; dx < w; dx++ {
			px, py := x+dx, y+dy
			if px < b.Min.X || py < b.Min.Y || px >= b.Max.X || py >= b.Max.Y {
				continue
			}
			cur := dst.RGBAAt(px, py)
			a := uint32(bg.A)
			inv := 255 - a
			dst.SetRGBA(px, py, color.RGBA{
				R: uint8((uint32(cur.R)*inv + uint32(bg.R)*a) / 255),
				G: uint8((uint32(cur.G)*inv + uint32(bg.G)*a) / 255),
				B: uint8((uint32(cur.B)*inv + uint32(bg.B)*a) / 255),
				A: 255,
			})
		}
	}
	cx := x + 2
	cy := y + 2
	for _, ch := range text {
		glyph, ok := digitGlyphs[ch]
		if ok {
			for row := 0; row < 5; row++ {
				for col := 0; col < 3; col++ {
					if (glyph[row] & (1 << (2 - col))) != 0 {
						px, py := cx+col, cy+row
						if px >= b.Min.X && py >= b.Min.Y && px < b.Max.X && py < b.Max.Y {
							dst.SetRGBA(px, py, fg)
						}
					}
				}
			}
		}
		cx += 4
	}
}
