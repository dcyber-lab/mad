package bus

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	im := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			im.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// A picture is kept by what is in it: the type is read from the bytes,
// never from a name, a file that is no picture is refused, and the same
// picture is one file.
func TestStoreImage(t *testing.T) {
	setup(t)
	data := testPNG(t, 40, 30, color.RGBA{200, 30, 30, 255})
	im, err := StoreImage(data)
	if err != nil {
		t.Fatal(err)
	}
	if im.Mime != "image/png" || im.W != 40 || im.H != 30 || im.Size != len(data) || len(im.ID) != 64 {
		t.Errorf("image %+v", im)
	}
	path, mime, ok := ImagePath(im.ID)
	if !ok || mime != "image/png" || filepath.Ext(path) != ".png" {
		t.Fatalf("path %q %q %v", path, mime, ok)
	}
	if b, _ := os.ReadFile(path); !bytes.Equal(b, data) {
		t.Error("the file differs from what was stored")
	}
	// The same bytes again: the same reference, one file.
	again, err := StoreImage(data)
	if err != nil || again.ID != im.ID {
		t.Errorf("again: %+v, %v", again, err)
	}
	if files, _ := os.ReadDir(imageDir()); len(files) != 1 {
		t.Errorf("%d files for one picture", len(files))
	}

	// Sniffed: a text file is no picture, whatever it is called; a script
	// is not one; HEIC (ftyp heic) is not read.
	for name, bad := range map[string][]byte{
		"a text file called .png": []byte("hello, I am not a picture"),
		"html":                    []byte("<html><script>alert(1)</script></html>"),
		"svg":                     []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>1</script></svg>`),
		"heic":                    append([]byte{0, 0, 0, 24}, []byte("ftypheic")...),
		"empty":                   nil,
	} {
		if _, err := StoreImage(bad); err == nil {
			t.Errorf("%s was kept", name)
		}
	}
	// A jpeg and a gif are kept as what they are.
	jpg := append([]byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 16, 'J', 'F', 'I', 'F', 0}, make([]byte, 64)...)
	if j, err := StoreImage(jpg); err != nil || j.Mime != "image/jpeg" {
		t.Errorf("jpeg: %+v, %v", j, err)
	}
	if g, err := StoreImage(append([]byte("GIF89a"), make([]byte, 32)...)); err != nil || g.Mime != "image/gif" {
		t.Errorf("gif: %+v, %v", g, err)
	}

	// The size cap, and a reference that names another file.
	big := append(testPNG(t, 2, 2, color.RGBA{}), make([]byte, MaxImageBytes)...)
	if _, err := StoreImage(big); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("over the cap: %v", err)
	}
	for _, id := range []string{"../../etc/passwd", "", im.ID[:63], strings.ToUpper(im.ID), im.ID + "/x"} {
		if _, _, ok := ImagePath(id); ok {
			t.Errorf("%q is a reference", id)
		}
	}
	if _, err := StoreImages(make([][]byte, MaxImages+1)); err == nil {
		t.Error("too many images")
	}
}

// A message carries pictures, may be pictures alone, and an agent's digest
// names them by their files.
func TestPostImages(t *testing.T) {
	setup(t)
	mustCreate(t, "pics", idBuild)
	im, err := StoreImage(testPNG(t, 8, 8, color.RGBA{0, 0, 255, 255}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PostImages("pics", me, "", 0, nil); err == nil {
		t.Error("nothing posted with no text and no pictures")
	}
	res, err := PostImages("pics", me, "@builder look at this", 0, []Image{im})
	if err != nil || len(res.Woke) != 1 {
		t.Fatalf("post: %+v, %v", res, err)
	}
	// Pictures alone are a message (it wakes no one: no mention).
	if _, err := ReplyImages("pics", "", 0, []Image{im}); err != nil {
		t.Fatalf("pictures alone: %v", err)
	}
	msgs, _, err := Read("pics", 0, 10)
	if err != nil || len(msgs) < 2 {
		t.Fatalf("read: %v %v", msgs, err)
	}
	if len(msgs[0].Images) != 1 || msgs[0].Images[0].ID != im.ID {
		t.Errorf("the message keeps %+v", msgs[0].Images)
	}
	text, _, ok, err := Compose(idBuild, time.Date(2026, 10, 30, 14, 5, 0, 0, time.UTC))
	if err != nil || !ok {
		t.Fatalf("compose: %v %v", ok, err)
	}
	path := im.Path()
	if path == "" || !strings.Contains(text, path) || !strings.Contains(text, "file-read tool") {
		t.Errorf("the digest does not name the picture's file %q:\n%s", path, text)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("the named file is not readable: %v", err)
	}
}
