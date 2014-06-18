package supersizeme

import (
	"bytes"
	"image"
	"image/jpeg"
	"io"

	"github.com/disintegration/imaging"
)

func CenterCrop(img *io.Reader, width, height int) ([]byte, error) {
	m, _, err := image.Decode(*img)
	if err != nil {
		return nil, err
	}

	imgW := float64(m.Bounds().Max.X - m.Bounds().Min.X)
	imgH := float64(m.Bounds().Max.Y - m.Bounds().Min.Y)
	targetW := float64(width)
	targetH := float64(height)

	var scale float64

	if imgW*targetH > targetW*imgH {
		scale = targetH / imgH
	} else {
		scale = targetW / imgW
	}

	m = imaging.Resize(m, int(imgW*scale), int(imgH*scale), imaging.Lanczos)
	m = imaging.CropCenter(m, width, height)

	buf := new(bytes.Buffer)
	jpeg.Encode(buf, m, &jpeg.Options{Quality: 95})

	return buf.Bytes(), nil
}
