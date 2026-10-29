package icon

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"runtime"
	"strings"
	"sync"
)

type State string

const (
	StateVPN   State = "vpn"   // AWG UP - Green
	StateNoVPN State = "novpn" // AWG DOWN - Red
	StateBusy  State = "busy"  // Operation in progress - Orange
	StateError State = "error" // Router unreachable - Gray
)

var (
	ColorVPN   = color.RGBA{R: 34, G: 197, B: 94, A: 255}   // #22c55e (Green)
	ColorNoVPN = color.RGBA{R: 239, G: 68, B: 68, A: 255}   // #ef4444 (Red)
	ColorBusy  = color.RGBA{R: 245, G: 158, B: 11, A: 255}  // #f59e0b (Amber)
	ColorError = color.RGBA{R: 107, G: 114, B: 128, A: 255} // #6b7280 (Gray)
)

// GetBuiltinFlagPNG returns pre-rendered crisp PNG bytes for common country flags
func GetBuiltinFlagPNG(code string) []byte {
	code = strings.ToUpper(strings.TrimSpace(code))
	switch code {
	case "RU":
		return GenerateTricolorPNG(
			color.RGBA{255, 255, 255, 255}, // White
			color.RGBA{0, 57, 166, 255},    // Russian Blue (#0039A6)
			color.RGBA{213, 43, 30, 255},   // Russian Red (#D52B1E)
		)
	case "AT":
		return GenerateTricolorPNG(
			color.RGBA{237, 41, 57, 255},   // Austrian Red (#ED2939)
			color.RGBA{255, 255, 255, 255}, // White
			color.RGBA{237, 41, 57, 255},   // Austrian Red (#ED2939)
		)
	case "NL":
		return GenerateTricolorPNG(
			color.RGBA{174, 28, 40, 255},   // Vermilion
			color.RGBA{255, 255, 255, 255}, // White
			color.RGBA{33, 70, 139, 255},   // Cobalt Blue
		)
	case "DE":
		return GenerateTricolorPNG(
			color.RGBA{0, 0, 0, 255},       // Black
			color.RGBA{221, 0, 0, 255},     // Red
			color.RGBA{255, 206, 0, 255},   // Gold
		)
	case "FR":
		return GenerateVerticalTricolorPNG(
			color.RGBA{0, 38, 84, 255},     // Blue
			color.RGBA{255, 255, 255, 255}, // White
			color.RGBA{237, 41, 57, 255},   // Red
		)
	case "IT":
		return GenerateVerticalTricolorPNG(
			color.RGBA{0, 146, 70, 255},    // Green
			color.RGBA{255, 255, 255, 255}, // White
			color.RGBA{206, 43, 55, 255},   // Red
		)
	case "PL":
		return GenerateBicolorPNG(
			color.RGBA{255, 255, 255, 255}, // White
			color.RGBA{220, 20, 60, 255},   // Crimson Red
		)
	case "UA":
		return GenerateBicolorPNG(
			color.RGBA{0, 91, 187, 255},    // Blue
			color.RGBA{255, 213, 0, 255},   // Yellow
		)
	default:
		return nil
	}
}

// GenerateTricolorPNG creates a crisp 64x64 horizontal tricolor PNG in memory
func GenerateTricolorPNG(c1, c2, c3 color.RGBA) []byte {
	const size = 64
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	h1 := 21
	h2 := 43
	for y := 0; y < size; y++ {
		var c color.RGBA
		if y < h1 {
			c = c1
		} else if y < h2 {
			c = c2
		} else {
			c = c3
		}
		for x := 0; x < size; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	pngBytes, _ := ImageToPNG(img)
	return pngBytes
}

// GenerateVerticalTricolorPNG creates a crisp 64x64 vertical tricolor PNG in memory
func GenerateVerticalTricolorPNG(c1, c2, c3 color.RGBA) []byte {
	const size = 64
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	w1 := 21
	w2 := 43
	for x := 0; x < size; x++ {
		var c color.RGBA
		if x < w1 {
			c = c1
		} else if x < w2 {
			c = c2
		} else {
			c = c3
		}
		for y := 0; y < size; y++ {
			img.SetRGBA(x, y, c)
		}
	}
	pngBytes, _ := ImageToPNG(img)
	return pngBytes
}

// GenerateBicolorPNG creates a crisp 64x64 horizontal bicolor PNG in memory
func GenerateBicolorPNG(c1, c2 color.RGBA) []byte {
	const size = 64
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	hHalf := 32
	for y := 0; y < size; y++ {
		var c color.RGBA
		if y < hHalf {
			c = c1
		} else {
			c = c2
		}
		for x := 0; x < size; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	pngBytes, _ := ImageToPNG(img)
	return pngBytes
}

func maskCircle(img *image.RGBA, size int, radius float64) {
	cx, cy := float64(size)/2.0, float64(size)/2.0
	r2 := radius * radius
	rOuter := radius + 0.6
	rOuter2 := rOuter * rOuter

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			dist2 := dx*dx + dy*dy

			if dist2 > rOuter2 {
				img.SetRGBA(x, y, color.RGBA{0, 0, 0, 0})
			} else if dist2 > r2 {
				cur := img.RGBAAt(x, y)
				edgeFrac := 1.0 - (math.Sqrt(dist2) - radius)
				if edgeFrac < 0 {
					edgeFrac = 0
				}
				img.SetRGBA(x, y, color.RGBA{
					R: cur.R,
					G: cur.G,
					B: cur.B,
					A: uint8(float64(cur.A) * edgeFrac),
				})
			}
		}
	}
}

func drawCircleBorder(img *image.RGBA, size int, radius float64) {
	cx, cy := float64(size)/2.0, float64(size)/2.0
	for angle := 0.0; angle < 2*math.Pi; angle += 0.012 {
		px := int(math.Round(cx + radius*math.Cos(angle)))
		py := int(math.Round(cy + radius*math.Sin(angle)))
		if px >= 0 && px < size && py >= 0 && py < size {
			img.SetRGBA(px, py, color.RGBA{0, 0, 0, 140})
		}
		px2 := int(math.Round(cx + (radius-1.0)*math.Cos(angle)))
		py2 := int(math.Round(cy + (radius-1.0)*math.Sin(angle)))
		if px2 >= 0 && px2 < size && py2 >= 0 && py2 < size {
			img.SetRGBA(px2, py2, color.RGBA{255, 255, 255, 65})
		}
	}
}

// GenerateIcon creates a 64x64 RGBA image with the status ring/shield and optional embedded country flag
func GenerateIcon(state State, flagPNG []byte) image.Image {
	const size = 64
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	var statusColor color.RGBA
	switch state {
	case StateVPN:
		statusColor = ColorVPN
	case StateNoVPN:
		statusColor = ColorNoVPN
	case StateBusy:
		statusColor = ColorBusy
	default:
		statusColor = ColorError
	}

	// Try to decode flag image if provided
	var flagImg image.Image
	if len(flagPNG) > 0 {
		if dec, err := png.Decode(bytes.NewReader(flagPNG)); err == nil {
			flagImg = dec
		}
	}

	if flagImg != nil {
		// Fill the FULL 64x64 square canvas
		scaleDraw(img, image.Rect(0, 0, size, size), flagImg)

		// Trendy circular flag badge (matching Zoom icon)
		const radius = 30.5
		maskCircle(img, size, radius)

		// Crisp circular outline for high contrast on all taskbars
		drawCircleBorder(img, size, radius)
	} else {
		// Default shield / badge icon
		drawShield(img, size, statusColor)
	}

	return img
}

func scaleDraw(dst draw.Image, r image.Rectangle, src image.Image) {
	srcBounds := src.Bounds()
	sw := srcBounds.Dx()
	sh := srcBounds.Dy()
	dw := r.Dx()
	dh := r.Dy()

	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			sx := srcBounds.Min.X + (x*sw)/dw
			sy := srcBounds.Min.Y + (y*sh)/dh
			dst.Set(r.Min.X+x, r.Min.Y+y, src.At(sx, sy))
		}
	}
}

func drawCircle(img *image.RGBA, cx, cy, r int, c color.RGBA) {
	r2 := r * r
	for y := -r; y <= r; y++ {
		for x := -r; x <= r; x++ {
			if x*x+y*y <= r2 {
				img.SetRGBA(cx+x, cy+y, c)
			}
		}
	}
}

func drawCircleOutline(img *image.RGBA, cx, cy, r int, c color.RGBA) {
	for angle := 0.0; angle < 2*math.Pi; angle += 0.05 {
		x := cx + int(math.Round(float64(r)*math.Cos(angle)))
		y := cy + int(math.Round(float64(r)*math.Sin(angle)))
		img.SetRGBA(x, y, c)
	}
}

func drawRectOutline(img *image.RGBA, x, y, w, h int, c color.RGBA) {
	for i := 0; i < w; i++ {
		img.SetRGBA(x+i, y, c)
		img.SetRGBA(x+i, y+h-1, c)
	}
	for j := 0; j < h; j++ {
		img.SetRGBA(x, y+j, c)
		img.SetRGBA(x+w-1, y+j, c)
	}
}

func drawShield(img *image.RGBA, size int, c color.RGBA) {
	s := float64(size)
	cx := s * 0.5
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			nx := (float64(x) - cx) / (s * 0.45)
			ny := float64(y) / s
			if ny >= 0.1 && ny <= 0.85 {
				// parabolic shield envelope
				widthFactor := 1.0
				if ny > 0.4 {
					widthFactor = 1.0 - math.Pow((ny-0.4)/0.45, 1.8)
				}
				if math.Abs(nx) <= widthFactor {
					img.SetRGBA(x, y, c)
				}
			}
		}
	}
}

// ImageToPNG converts an image.Image to PNG bytes
func ImageToPNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ImageToICO creates a valid Windows .ico file with embedded PNG data
func ImageToICO(img image.Image) ([]byte, error) {
	pngData, err := ImageToPNG(img)
	if err != nil {
		return nil, err
	}

	b := img.Bounds()
	w := uint8(b.Dx())
	h := uint8(b.Dy())
	if b.Dx() >= 256 {
		w = 0
	}
	if b.Dy() >= 256 {
		h = 0
	}

	var buf bytes.Buffer
	// ICONDIR header
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0)) // reserved
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1)) // type 1 = icon
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1)) // count = 1

	// ICONDIRENTRY (16 bytes)
	_ = buf.WriteByte(w)
	_ = buf.WriteByte(h)
	_ = buf.WriteByte(0)                                             // color count
	_ = buf.WriteByte(0)                                             // reserved
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))           // color planes
	_ = binary.Write(&buf, binary.LittleEndian, uint16(32))          // bits per pixel
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(pngData))) // bytes in resource
	_ = binary.Write(&buf, binary.LittleEndian, uint32(22))          // offset to image data (6+16=22)

	// Image data (PNG bytes)
	buf.Write(pngData)

	return buf.Bytes(), nil
}

// GetPlatformIcon returns ICO bytes for Windows and PNG bytes for macOS/Linux
func GetPlatformIcon(state State, flagPNG []byte) ([]byte, error) {
	img := GenerateIcon(state, flagPNG)
	if runtime.GOOS == "windows" {
		return ImageToICO(img)
	}
	return ImageToPNG(img)
}

const NumSpinnerFrames = 16

var (
	spinnerFrames   [NumSpinnerFrames][]byte
	spinnerInitOnce sync.Once
)

// GenerateSpinnerImage renders one frame of an antialiased, modern rotating gradient spinner.
func GenerateSpinnerImage(frame, totalFrames int) image.Image {
	const S = 64
	img := image.NewRGBA(image.Rect(0, 0, S, S))
	cx, cy := 32.0, 32.0
	rMid := 22.0
	halfThick := 5.0

	// leadAngle starts at top (12 o'clock) and rotates clockwise
	leadAngle := float64(frame)*(2*math.Pi/float64(totalFrames)) - math.Pi/2
	arcLen := 1.65 * math.Pi // ~297 degrees arc

	for y := 0; y < S; y++ {
		for x := 0; x < S; x++ {
			dx := float64(x) - cx + 0.5
			dy := float64(y) - cy + 0.5
			dist := math.Hypot(dx, dy)

			distFromMid := math.Abs(dist - rMid)
			if distFromMid > halfThick+1.2 {
				continue
			}

			coverage := 1.0
			if distFromMid > halfThick-0.6 {
				coverage = (halfThick + 1.2 - distFromMid) / 1.8
				if coverage < 0 {
					coverage = 0
				}
				if coverage > 1 {
					coverage = 1
				}
			}

			angle := math.Atan2(dy, dx)
			relAngle := leadAngle - angle
			for relAngle < 0 {
				relAngle += 2 * math.Pi
			}
			for relAngle >= 2*math.Pi {
				relAngle -= 2 * math.Pi
			}

			if relAngle > arcLen {
				continue
			}

			t := 1.0 - (relAngle / arcLen)
			alpha := float64(coverage) * math.Pow(t, 0.75)

			// Glowing modern gradient:
			// Head: Vibrant Cyan (#00F0FF, RGB: 0, 240, 255)
			// Mid: Electric Blue (#3B82F6, RGB: 59, 130, 246)
			// Tail: Violet / Indigo (#818CF8, RGB: 129, 140, 248)
			var red, green, blue uint8
			if t > 0.5 {
				k := (t - 0.5) * 2.0
				red = uint8(0*k + 59*(1-k))
				green = uint8(240*k + 130*(1-k))
				blue = uint8(255*k + 246*(1-k))
			} else {
				k := t * 2.0
				red = uint8(59*k + 129*(1-k))
				green = uint8(130*k + 140*(1-k))
				blue = uint8(246*k + 248*(1-k))
			}
			a := uint8(alpha * 255)

			img.SetRGBA(x, y, color.RGBA{R: red, G: green, B: blue, A: a})
		}
	}

	// Rounded cap at the leading head
	headX := cx + rMid*math.Cos(leadAngle)
	headY := cy + rMid*math.Sin(leadAngle)
	for y := int(headY - halfThick - 2); y <= int(headY + halfThick + 2); y++ {
		for x := int(headX - halfThick - 2); x <= int(headX + halfThick + 2); x++ {
			if x < 0 || x >= S || y < 0 || y >= S {
				continue
			}
			d := math.Hypot(float64(x)+0.5-headX, float64(y)+0.5-headY)
			if d <= halfThick+1.0 {
				aa := 1.0
				if d > halfThick-0.6 {
					aa = (halfThick + 1.0 - d) / 1.6
				}
				orig := img.RGBAAt(x, y)
				newA := uint8(math.Max(float64(orig.A), aa*255))
				img.SetRGBA(x, y, color.RGBA{R: 0, G: 240, B: 255, A: newA})
			}
		}
	}

	return img
}

func initSpinnerFrames() {
	spinnerInitOnce.Do(func() {
		for f := 0; f < NumSpinnerFrames; f++ {
			img := GenerateSpinnerImage(f, NumSpinnerFrames)
			var data []byte
			if runtime.GOOS == "windows" {
				data, _ = ImageToICO(img)
			} else {
				data, _ = ImageToPNG(img)
			}
			spinnerFrames[f] = data
		}
	})
}

// GetSpinnerFrame returns pre-rendered icon bytes for the animated tray spinner.
func GetSpinnerFrame(frame int) []byte {
	initSpinnerFrames()
	idx := frame % NumSpinnerFrames
	if idx < 0 {
		idx += NumSpinnerFrames
	}
	return spinnerFrames[idx]
}
