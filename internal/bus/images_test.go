package bus

import (
	"bytes"
	"fmt"
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

func setMtime(t *testing.T, id string, ago time.Duration) {
	t.Helper()
	p, _, ok := ImagePath(id)
	if !ok {
		t.Fatalf("no file for %s", id)
	}
	at := time.Now().Add(-ago)
	if err := os.Chtimes(p, at, at); err != nil {
		t.Fatal(err)
	}
}

// Pictures no kept message names are removed, unless they are young (a post
// in flight); the ones a message names stay, archived channels' too.
func TestCollectImages(t *testing.T) {
	setup(t)
	mustCreate(t, "keep", idBuild)
	keep, _ := StoreImage(testPNG(t, 4, 4, color.RGBA{1, 0, 0, 255}))
	orphan, _ := StoreImage(testPNG(t, 4, 4, color.RGBA{2, 0, 0, 255}))
	fresh, _ := StoreImage(testPNG(t, 4, 4, color.RGBA{3, 0, 0, 255}))
	if _, err := PostImages("keep", me, "@builder this one", 0, []Image{keep}); err != nil {
		t.Fatal(err)
	}
	setMtime(t, keep.ID, time.Hour)
	setMtime(t, orphan.ID, time.Hour)
	// fresh: stored just now, named by no message yet.
	n, err := CollectImages(ImageGCAge)
	if err != nil || n != 1 {
		t.Fatalf("collected %d, %v; want the orphan alone", n, err)
	}
	for id, want := range map[string]bool{keep.ID: true, orphan.ID: false, fresh.ID: true} {
		if _, _, ok := ImagePath(id); ok != want {
			t.Errorf("%s kept = %v, want %v", id[:8], ok, want)
		}
	}
	// Named by a message in an archived channel: kept.
	mustCreate(t, "old", idBuild)
	oldPic, _ := StoreImage(testPNG(t, 4, 4, color.RGBA{4, 0, 0, 255}))
	if _, err := PostImages("old", me, "@builder before it closed", 0, []Image{oldPic}); err != nil {
		t.Fatal(err)
	}
	if err := Archive("old"); err != nil {
		t.Fatal(err)
	}
	setMtime(t, oldPic.ID, time.Hour)
	if n, _ := CollectImages(ImageGCAge); n != 0 {
		t.Errorf("collected %d: an archived channel's picture is still named", n)
	}
	// At age 0 only the unnamed one goes: fresh.
	if n, err := CollectImages(0); err != nil || n != 1 {
		t.Errorf("at age 0: %d, %v; want the unnamed one alone", n, err)
	}
	if _, _, ok := ImagePath(fresh.ID); ok {
		t.Error("the unnamed picture is still there")
	}
}

// A post names pictures that are there: one collected meanwhile is an
// error, not a message with a broken picture.
func TestPostImagesGone(t *testing.T) {
	setup(t)
	mustCreate(t, "pics", idBuild)
	im, _ := StoreImage(testPNG(t, 4, 4, color.RGBA{5, 0, 0, 255}))
	setMtime(t, im.ID, time.Hour)
	if _, err := CollectImages(ImageGCAge); err != nil {
		t.Fatal(err)
	}
	if _, err := PostImages("pics", me, "@builder a picture that is gone", 0, []Image{im}); err == nil || !strings.Contains(err.Error(), "gone") {
		t.Errorf("post with a collected picture: %v", err)
	}
}

// The person's target is checked before anything is kept.
func TestCheckReply(t *testing.T) {
	setup(t)
	mustCreate(t, "here", idBuild)
	if err := CheckReply("here"); err != nil {
		t.Errorf("a channel: %v", err)
	}
	if err := CheckReply("nowhere"); err == nil {
		t.Error("a channel that is not there")
	}
	if err := CheckReply(DMChannel(idArch, idBuild)); err != ErrTheirDM {
		t.Errorf("a DM of two agents: %v", err)
	}
	if err := CheckReply(DMChannel(You, idBuild)); err != nil {
		t.Errorf("your DM with an agent, not made yet: %v", err)
	}
	if err := CheckReply(DMChannel(You, "dddd4444-0000-0000-0000-000000000009")); err == nil {
		t.Error("a DM with no such agent")
	}
	if err := CheckDM(me, idRun); err == nil {
		t.Error("a DM with an agent in a run")
	}
	if err := CheckDM(me, ""); err == nil {
		t.Error("a DM with no one")
	}
}

// A log cut down runs the collection.
func TestCompactionCollects(t *testing.T) {
	setup(t)
	maxLines, keepLines = 20, 5
	t.Cleanup(func() { maxLines, keepLines = 2000, 500 })
	called := 0
	old := afterCompact
	afterCompact = func() { called++ }
	t.Cleanup(func() { afterCompact = old })
	mustCreate(t, "busy", idBuild)
	for i := 0; i <= maxLines; i++ {
		mustPost(t, "busy", me, fmt.Sprintf("message number %d here", i))
	}
	if called == 0 {
		t.Error("cutting a log did not ask for the collection")
	}
}

// A sweep does not hold the bus: a post completes while one waits after its
// snapshot, and what the post named, stored meanwhile, survives it.
func TestCollectImagesDoesNotBlockPosts(t *testing.T) {
	setup(t)
	mustCreate(t, "live", idBuild)
	orphan, _ := StoreImage(testPNG(t, 4, 4, color.RGBA{6, 0, 0, 255}))
	again, _ := StoreImage(testPNG(t, 4, 4, color.RGBA{7, 0, 0, 255}))
	setMtime(t, orphan.ID, time.Hour)
	setMtime(t, again.ID, time.Hour)

	snapshotted, release := make(chan struct{}), make(chan struct{})
	sweepSnapshotted = func() {
		close(snapshotted)
		<-release
	}
	t.Cleanup(func() { sweepSnapshotted = nil })
	done := make(chan int, 1)
	go func() {
		n, _ := CollectImages(ImageGCAge)
		done <- n
	}()
	<-snapshotted

	// The sweep is held after its snapshot: a post goes through (it would
	// wait for the lock if the sweep had it), naming a picture the snapshot
	// did not know, one stored again by this very post.
	posted := make(chan error, 1)
	go func() {
		im, err := StoreImage(testPNG(t, 4, 4, color.RGBA{7, 0, 0, 255})) // the old picture, sent again
		if err == nil {
			_, err = PostImages("live", me, "@builder sent again", 0, []Image{im})
		}
		posted <- err
	}()
	select {
	case err := <-posted:
		if err != nil {
			t.Fatalf("post during a sweep: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a post waited for the sweep")
	}
	close(release)
	if n := <-done; n != 1 {
		t.Errorf("the sweep removed %d; want the orphan alone", n)
	}
	if _, _, ok := ImagePath(orphan.ID); ok {
		t.Error("the orphan is still there")
	}
	if _, _, ok := ImagePath(again.ID); !ok {
		t.Error("the picture the post named was swept away")
	}
	msgs, _, _ := Read("live", 0, 10)
	if len(msgs) != 1 || len(msgs[0].Images) != 1 || msgs[0].Images[0].Path() == "" {
		t.Errorf("the message or its picture is broken: %+v", msgs)
	}
}
