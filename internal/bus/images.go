package bus

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // sizes, as DecodeConfig reads them
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
)

// A message can carry pictures. Each is a file in the state directory,
// named by what is in it (so the same picture is kept once), and the
// message keeps only a reference to it. An agent opens one by its path.

// Image is a picture on a message.
type Image struct {
	ID   string `json:"id"` // the SHA-256 of its bytes, in hex
	Mime string `json:"mime"`
	Size int    `json:"size"`
	W    int    `json:"w,omitempty"` // when it can be read cheaply (not webp)
	H    int    `json:"h,omitempty"`
}

// Limits on what a message carries.
const (
	MaxImageBytes = 10 << 20
	MaxImages     = 6
)

// imageTypes are what is kept, by what the bytes are (sniffed, never by
// the name or the type the sender gave), with the file's extension.
var imageTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

var imageIDRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

func imageDir() string { return filepath.Join(dir(), "images") }

// StoreImage keeps a picture and returns its reference. The type comes
// from the bytes: a file that is not a png, jpeg, gif or webp is refused,
// whatever it is called (HEIC is not read: send a JPEG), and so is one
// over MaxImageBytes.
func StoreImage(data []byte) (Image, error) {
	if len(data) == 0 {
		return Image{}, errors.New("an empty image")
	}
	if len(data) > MaxImageBytes {
		return Image{}, fmt.Errorf("an image of %d MB is over the %d MB limit", len(data)>>20, MaxImageBytes>>20)
	}
	mime := http.DetectContentType(data)
	ext, ok := imageTypes[mime]
	if !ok {
		return Image{}, errors.New("not an image mad keeps: send a png, jpeg, gif or webp (a HEIC photo as a JPEG)")
	}
	sum := sha256.Sum256(data)
	im := Image{ID: hex.EncodeToString(sum[:]), Mime: mime, Size: len(data)}
	if c, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		im.W, im.H = c.Width, c.Height
	}
	path := filepath.Join(imageDir(), im.ID+ext)
	if _, err := os.Stat(path); err == nil {
		return im, nil // kept already
	}
	if err := os.MkdirAll(imageDir(), 0o755); err != nil {
		return Image{}, err
	}
	return im, writeAtomic(path, data)
}

// StoreImages keeps pictures for one message: none is kept unless all are
// good, at most MaxImages.
func StoreImages(datas [][]byte) ([]Image, error) {
	if len(datas) > MaxImages {
		return nil, fmt.Errorf("at most %d images in a message", MaxImages)
	}
	var out []Image
	for i, d := range datas {
		im, err := StoreImage(d)
		if err != nil {
			return nil, fmt.Errorf("image %d: %w", i+1, err)
		}
		out = append(out, im)
	}
	return out, nil
}

// ImagePath is where image id is kept, and its type; ok is false for an id
// that is not a reference or a picture not there. The id is checked, so it
// can never name another file.
func ImagePath(id string) (path, mime string, ok bool) {
	if !imageIDRe.MatchString(id) {
		return "", "", false
	}
	for mime, ext := range imageTypes {
		p := filepath.Join(imageDir(), id+ext)
		if _, err := os.Stat(p); err == nil {
			return p, mime, true
		}
	}
	return "", "", false
}

// Path is where the picture's file is, for an agent to open; "" if it is
// gone.
func (im Image) Path() string {
	p, _, _ := ImagePath(im.ID)
	return p
}
