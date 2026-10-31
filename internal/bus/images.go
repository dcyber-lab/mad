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
	"strings"
	"time"
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
	mime, err := CheckImage(data)
	if err != nil {
		return Image{}, err
	}
	ext := imageTypes[mime]
	sum := sha256.Sum256(data)
	im := Image{ID: hex.EncodeToString(sum[:]), Mime: mime, Size: len(data)}
	if c, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		im.W, im.H = c.Width, c.Height
	}
	path := filepath.Join(imageDir(), im.ID+ext)
	if _, err := os.Stat(path); err == nil {
		// Kept already: touched, so that CollectImages leaves it, as a
		// message is about to name it.
		now := time.Now()
		_ = os.Chtimes(path, now, now)
		return im, nil
	}
	if err := os.MkdirAll(imageDir(), 0o755); err != nil {
		return Image{}, err
	}
	return im, writeAtomic(path, data)
}

// CheckImage says what data is, or why it is not kept, without keeping it.
func CheckImage(data []byte) (mime string, err error) {
	if len(data) == 0 {
		return "", errors.New("an empty image")
	}
	if len(data) > MaxImageBytes {
		return "", fmt.Errorf("an image of %d MB is over the %d MB limit", len(data)>>20, MaxImageBytes>>20)
	}
	mime = http.DetectContentType(data)
	if _, ok := imageTypes[mime]; !ok {
		return "", errors.New("not an image mad keeps: send a png, jpeg, gif or webp (a HEIC photo as a JPEG)")
	}
	return mime, nil
}

// StoreImages keeps pictures for one message: none is kept unless all are
// good, at most MaxImages.
func StoreImages(datas [][]byte) ([]Image, error) {
	if len(datas) > MaxImages {
		return nil, TooManyImages()
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

// TooManyImages is the error for more pictures than a message takes.
func TooManyImages() error {
	return fmt.Errorf("at most %d images in a message", MaxImages)
}

// ImageGCAge is how old an unreferenced picture must be to be removed: a
// post in flight has stored its pictures and not yet named them.
const ImageGCAge = 10 * time.Minute

// afterCompact runs when a channel's log was cut: the pictures of what was
// dropped are no one's. Tests replace it.
var afterCompact = func() {
	go func() { _, _ = CollectImages(ImageGCAge) }()
}

// sweepSnapshotted, when set, is called by CollectImages after it took the
// set of named pictures and let go of the bus lock; tests hold the sweep
// there.
var sweepSnapshotted func()

// CollectImages removes the pictures no kept message names (archived
// channels' included), unless they are younger than minAge, and says how
// many it removed. It runs when the deck starts, and when a log is cut.
// The bus lock is held only to read the logs for the names; the files are
// looked at and removed without it, each one's age looked at again just
// before it goes, so a post is never held up by a sweep. A picture named
// by a post made after the snapshot is young (stored for it), or touched
// when stored again, and post checks its pictures are still there.
func CollectImages(minAge time.Duration) (int, error) {
	root := imageDir()
	files, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) || len(files) == 0 {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	named := map[string]bool{}
	err = locked(func() error {
		cl, err := loadChannels()
		if err != nil {
			return err
		}
		for _, c := range cl.Channels {
			msgs, err := readMsgs(c.ID)
			if err != nil {
				return err // a log unread: remove nothing
			}
			for _, m := range msgs {
				for _, im := range m.Images {
					named[im.ID] = true
				}
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if sweepSnapshotted != nil {
		sweepSnapshotted()
	}
	removed := 0
	for _, f := range files {
		name := f.Name()
		id := strings.TrimSuffix(name, filepath.Ext(name))
		if f.IsDir() || named[id] || !imageIDRe.MatchString(id) {
			continue
		}
		path := filepath.Join(root, name)
		// Its age now, not as the directory was listed: it may have been
		// stored again meanwhile.
		if info, err := os.Stat(path); err != nil || time.Since(info.ModTime()) < minAge {
			continue
		}
		if os.Remove(path) == nil {
			removed++
		}
	}
	return removed, nil
}
