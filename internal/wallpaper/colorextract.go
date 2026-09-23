package wallpaper

import (
	"fmt"
	"image"
	"image/color"
	"math"
)

// ExtractAccentColor extracts a vibrant accent color from an image.
// It downscales the image, runs k-means clustering to find dominant colors,
// then picks the most vibrant one suitable as a UI accent color.
func ExtractAccentColor(img image.Image) color.NRGBA {
	// Downscale to max 64x64 for speed
	pixels := samplePixels(img, 64)
	if len(pixels) == 0 {
		return color.NRGBA{R: 0x21, G: 0x96, B: 0xf3, A: 0xff} // fallback blue
	}

	// Run k-means with 5 clusters
	clusters := kmeans(pixels, 5, 20)

	// Pick the best accent color: most vibrant with sufficient lightness
	best := pickAccentColor(clusters)
	return best
}

type rgbPixel struct {
	r, g, b uint8
}

// samplePixels downscales the image and returns RGB pixel samples.
func samplePixels(img image.Image, maxDim int) []rgbPixel {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if w == 0 || h == 0 {
		return nil
	}

	// Calculate step to subsample to approximately maxDim x maxDim
	step := 1
	if w > maxDim {
		step = w / maxDim
	}
	if h/step > maxDim && h > maxDim {
		step = h / maxDim
	}
	if step < 1 {
		step = 1
	}

	var pixels []rgbPixel
	for y := bounds.Min.Y; y < bounds.Max.Y; y += step {
		for x := bounds.Min.X; x < bounds.Max.X; x += step {
			r, g, b, a := img.At(x, y).RGBA()
			if a < 0x8000 {
				continue // skip transparent pixels
			}
			// Convert from 16-bit premultiplied to 8-bit
			pixels = append(pixels, rgbPixel{
				r: uint8(r >> 8),
				g: uint8(g >> 8),
				b: uint8(b >> 8),
			})
		}
	}
	return pixels
}

// kmeans runs k-means clustering on RGB pixels and returns cluster centroids
// with their sizes (number of pixels in each cluster).
func kmeans(pixels []rgbPixel, k, maxIter int) []struct {
	center rgbPixel
	size   int
} {
	if len(pixels) < k {
		k = len(pixels)
	}
	if k == 0 {
		return nil
	}

	// Initialize centroids by picking evenly spaced pixels
	centroids := make([]rgbPixel, k)
	step := len(pixels) / k
	for i := 0; i < k; i++ {
		centroids[i] = pixels[i*step]
	}

	assignments := make([]int, len(pixels))

	for iter := 0; iter < maxIter; iter++ {
		changed := false

		// Assign each pixel to nearest centroid
		for i, p := range pixels {
			minDist := math.MaxFloat64
			best := 0
			for c := 0; c < k; c++ {
				d := colorDistSq(p, centroids[c])
				if d < minDist {
					minDist = d
					best = c
				}
			}
			if assignments[i] != best {
				assignments[i] = best
				changed = true
			}
		}

		if !changed {
			break
		}

		// Recompute centroids
		sums := make([][3]int64, k)
		counts := make([]int, k)
		for i, p := range pixels {
			c := assignments[i]
			sums[c][0] += int64(p.r)
			sums[c][1] += int64(p.g)
			sums[c][2] += int64(p.b)
			counts[c]++
		}
		for c := 0; c < k; c++ {
			if counts[c] == 0 {
				continue
			}
			centroids[c] = rgbPixel{
				r: uint8(sums[c][0] / int64(counts[c])),
				g: uint8(sums[c][1] / int64(counts[c])),
				b: uint8(sums[c][2] / int64(counts[c])),
			}
		}
	}

	// Build result with cluster sizes
	counts := make([]int, k)
	for _, a := range assignments {
		counts[a]++
	}

	result := make([]struct {
		center rgbPixel
		size   int
	}, k)
	for i := 0; i < k; i++ {
		result[i].center = centroids[i]
		result[i].size = counts[i]
	}
	return result
}

// colorDistSq returns the squared Euclidean distance between two RGB pixels.
func colorDistSq(a, b rgbPixel) float64 {
	dr := float64(a.r) - float64(b.r)
	dg := float64(a.g) - float64(b.g)
	db := float64(a.b) - float64(b.b)
	return dr*dr + dg*dg + db*db
}

// pickAccentColor selects the best accent color from clusters.
// Prefers vibrant colors with moderate lightness (not too dark, not too light).
func pickAccentColor(clusters []struct {
	center rgbPixel
	size   int
}) color.NRGBA {
	fallback := color.NRGBA{R: 0x21, G: 0x96, B: 0xf3, A: 0xff}
	if len(clusters) == 0 {
		return fallback
	}

	totalPixels := 0
	for _, c := range clusters {
		totalPixels += c.size
	}
	if totalPixels == 0 {
		return fallback
	}

	bestScore := -1.0
	bestColor := fallback

	for _, cluster := range clusters {
		c := cluster.center
		h, s, l := rgbToHSL(c.r, c.g, c.b)
		_ = h

		// Score components:
		// 1. Saturation: more saturated = better accent color (0-1)
		satScore := s

		// 2. Lightness: prefer mid-range (0.3-0.7), penalize extremes
		lightScore := 1.0 - math.Abs(l-0.5)*2
		if l < 0.15 || l > 0.85 {
			lightScore *= 0.1 // heavily penalize near-black/white
		}

		// 3. Population: slight bonus for larger clusters (they're more representative)
		popScore := float64(cluster.size) / float64(totalPixels)

		// Combined score: weight saturation highest
		score := satScore*0.6 + lightScore*0.3 + popScore*0.1

		if score > bestScore {
			bestScore = score
			bestColor = color.NRGBA{R: c.r, G: c.g, B: c.b, A: 0xff}
		}
	}

	// If best color is too desaturated (gray wallpaper), boost saturation
	_, s, l := rgbToHSL(bestColor.R, bestColor.G, bestColor.B)
	if s < 0.3 {
		bestColor = boostSaturation(bestColor, 0.5)
	}
	_ = l

	return bestColor
}

// rgbToHSL converts RGB to HSL color space.
func rgbToHSL(r, g, b uint8) (h, s, l float64) {
	rf := float64(r) / 255.0
	gf := float64(g) / 255.0
	bf := float64(b) / 255.0

	max := math.Max(rf, math.Max(gf, bf))
	min := math.Min(rf, math.Min(gf, bf))
	l = (max + min) / 2

	if max == min {
		return 0, 0, l
	}

	d := max - min
	if l > 0.5 {
		s = d / (2.0 - max - min)
	} else {
		s = d / (max + min)
	}

	switch max {
	case rf:
		h = (gf - bf) / d
		if gf < bf {
			h += 6
		}
	case gf:
		h = (bf-rf)/d + 2
	case bf:
		h = (rf-gf)/d + 4
	}
	h /= 6
	return
}

// hslToRGB converts HSL back to RGB.
func hslToRGB(h, s, l float64) (r, g, b uint8) {
	if s == 0 {
		v := uint8(l * 255)
		return v, v, v
	}

	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q

	hueToRGB := func(p, q, t float64) float64 {
		if t < 0 {
			t++
		}
		if t > 1 {
			t--
		}
		switch {
		case t < 1.0/6:
			return p + (q-p)*6*t
		case t < 1.0/2:
			return q
		case t < 2.0/3:
			return p + (q-p)*(2.0/3-t)*6
		default:
			return p
		}
	}

	r = uint8(math.Round(hueToRGB(p, q, h+1.0/3) * 255))
	g = uint8(math.Round(hueToRGB(p, q, h) * 255))
	b = uint8(math.Round(hueToRGB(p, q, h-1.0/3) * 255))
	return
}

// boostSaturation increases the saturation of a color to a minimum target.
func boostSaturation(c color.NRGBA, targetS float64) color.NRGBA {
	h, s, l := rgbToHSL(c.R, c.G, c.B)
	if s >= targetS {
		return c
	}
	r, g, b := hslToRGB(h, targetS, l)
	return color.NRGBA{R: r, G: g, B: b, A: c.A}
}

// ColorToHex converts a color.NRGBA to a hex string like "#rrggbb".
func ColorToHex(c color.NRGBA) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}
