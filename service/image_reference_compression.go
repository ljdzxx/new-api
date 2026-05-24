package service

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/setting/image_storage_setting"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

const ImageEditAutoCompressField = "auto_compress_reference_image"

type ImageReferenceCompressionResult struct {
	Data        []byte
	Filename    string
	ContentType string
	Compressed  bool
	OriginalLen int
	Quality     int
	Width       int
	Height      int
}

func ShouldAutoCompressImageEditReference(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	return value == "" || value == "true" || value == "1" || value == "yes" || value == "on"
}

func CompressImageEditReferenceIfNeeded(data []byte, filename string, contentType string, requestEnabled bool) (*ImageReferenceCompressionResult, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty reference image")
	}
	result := &ImageReferenceCompressionResult{
		Data:        data,
		Filename:    filename,
		ContentType: normalizeImageContentType(contentType, filename, data),
		OriginalLen: len(data),
	}
	setting := image_storage_setting.GetImageStorageSetting()
	if setting == nil || !setting.EditReferenceImageCompressionEnabled || !requestEnabled {
		return result, nil
	}
	thresholdBytes := setting.EditReferenceImageCompressThresholdBytes()
	if thresholdBytes <= 0 || int64(len(data)) <= thresholdBytes {
		return result, nil
	}

	img, _, err := decodeReferenceImage(data)
	if err != nil {
		return nil, fmt.Errorf("decode reference image failed: %w", err)
	}

	maxSide := setting.EditReferenceImageMaxSideValue()
	targetBytes := setting.EditReferenceImageTargetBytes()
	if targetBytes <= 0 {
		targetBytes = thresholdBytes
	}
	if targetBytes > thresholdBytes {
		targetBytes = thresholdBytes
	}
	minQuality := setting.EditReferenceImageMinJPEGQualityValue()
	scaled := resizeImageToMaxSide(img, maxSide)
	encoded, quality, err := encodeJPEGUnderTarget(scaled, targetBytes, minQuality)
	if err != nil {
		return nil, err
	}
	if len(encoded) >= len(data) {
		return result, nil
	}

	bounds := scaled.Bounds()
	result.Data = encoded
	result.Filename = replaceImageExtension(filename, ".jpg")
	result.ContentType = "image/jpeg"
	result.Compressed = true
	result.Quality = quality
	result.Width = bounds.Dx()
	result.Height = bounds.Dy()
	return result, nil
}

func decodeReferenceImage(data []byte) (image.Image, string, error) {
	reader := bytes.NewReader(data)
	img, format, err := image.Decode(reader)
	if err == nil {
		return img, format, nil
	}
	if _, seekErr := reader.Seek(0, io.SeekStart); seekErr != nil {
		return nil, "", seekErr
	}
	img, err = webp.Decode(reader)
	if err == nil {
		return img, "webp", nil
	}
	return nil, "", err
}

func resizeImageToMaxSide(img image.Image, maxSide int) image.Image {
	if maxSide <= 0 {
		return img
	}
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 || (width <= maxSide && height <= maxSide) {
		return img
	}
	scale := float64(maxSide) / float64(width)
	if height > width {
		scale = float64(maxSide) / float64(height)
	}
	newWidth := int(float64(width)*scale + 0.5)
	newHeight := int(float64(height)*scale + 0.5)
	if newWidth < 1 {
		newWidth = 1
	}
	if newHeight < 1 {
		newHeight = 1
	}
	return resizeImage(img, newWidth, newHeight)
}

func resizeImage(img image.Image, width int, height int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
	return dst
}

func encodeJPEGUnderTarget(img image.Image, targetBytes int64, minQuality int) ([]byte, int, error) {
	const maxQuality = 92
	current := img
	for attempts := 0; attempts < 10; attempts++ {
		encoded, quality, ok, err := findBestJPEGQuality(current, targetBytes, minQuality, maxQuality)
		if err != nil {
			return nil, 0, err
		}
		if ok {
			return encoded, quality, nil
		}

		bounds := current.Bounds()
		width := int(float64(bounds.Dx()) * 0.85)
		height := int(float64(bounds.Dy()) * 0.85)
		if width < 256 || height < 256 {
			fallback, fallbackErr := encodeJPEG(current, minQuality)
			if fallbackErr != nil {
				return nil, 0, fallbackErr
			}
			return fallback, minQuality, nil
		}
		current = resizeImage(current, width, height)
	}
	fallback, err := encodeJPEG(current, minQuality)
	return fallback, minQuality, err
}

func findBestJPEGQuality(img image.Image, targetBytes int64, minQuality int, maxQuality int) ([]byte, int, bool, error) {
	low := minQuality
	high := maxQuality
	var best []byte
	bestQuality := 0
	for low <= high {
		mid := (low + high) / 2
		encoded, err := encodeJPEG(img, mid)
		if err != nil {
			return nil, 0, false, err
		}
		if int64(len(encoded)) <= targetBytes {
			best = encoded
			bestQuality = mid
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	return best, bestQuality, best != nil, nil
}

func encodeJPEG(img image.Image, quality int) ([]byte, error) {
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("encode jpeg failed: %w", err)
	}
	return buffer.Bytes(), nil
}

func normalizeImageContentType(contentType string, filename string, data []byte) string {
	contentType = strings.TrimSpace(strings.Split(contentType, ";")[0])
	if strings.HasPrefix(contentType, "image/") {
		return contentType
	}
	if filename != "" {
		if ext := strings.ToLower(filepath.Ext(filename)); ext != "" {
			if mimeType := mime.TypeByExtension(ext); strings.HasPrefix(mimeType, "image/") {
				return strings.Split(mimeType, ";")[0]
			}
		}
	}
	if len(data) > 0 {
		detected := http.DetectContentType(data)
		if strings.HasPrefix(detected, "image/") {
			return detected
		}
	}
	return "image/png"
}

func replaceImageExtension(filename string, ext string) string {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return "image" + ext
	}
	current := filepath.Ext(filename)
	if current == "" {
		return filename + ext
	}
	return strings.TrimSuffix(filename, current) + ext
}
