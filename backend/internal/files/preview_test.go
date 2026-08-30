package files

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 1, A: 255})
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// The preview decides what a click on a row shows, and every one of these
// kinds renders a completely different surface. Getting one wrong is a JPEG in
// a code editor or a shell script the page refuses to display.
func TestPreviewKinds(t *testing.T) {
	root := t.TempDir()
	s := New([]string{root})

	write := func(name, content string) string {
		p := filepath.Join(root, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	write("nginx.conf", "server {\n  listen 80;\n}\n")
	write("noextension", "#!/bin/sh\necho hello\n")
	write("blob.dat", "text\x00with a NUL\n")
	writePNG(t, filepath.Join(root, "logo.png"), 13, 7)
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	write("sub/one", "a")

	cases := []struct {
		name string
		want string
	}{
		{"nginx.conf", KindText},
		{"noextension", KindText},
		{"blob.dat", KindBinary},
		{"logo.png", KindImage},
		{"sub", KindDir},
	}
	for _, c := range cases {
		p, err := s.Preview(filepath.Join(root, c.name))
		if err != nil {
			t.Fatalf("Preview(%s): %v", c.name, err)
		}
		if p.Kind != c.want {
			t.Errorf("Preview(%s).Kind = %q, want %q", c.name, p.Kind, c.want)
		}
	}

	// A file whose extension says nothing and whose bytes say text is text:
	// most of what an operator opens on a server has no extension at all.
	script, _ := s.Preview(filepath.Join(root, "noextension"))
	if !strings.Contains(script.Text, "echo hello") || !script.Editable {
		t.Errorf("an extensionless script should preview as editable text, got %+v", script)
	}
	conf, _ := s.Preview(filepath.Join(root, "nginx.conf"))
	if conf.Language != "ini" || conf.Lines != 3 {
		t.Errorf("nginx.conf previewed as %q with %d lines", conf.Language, conf.Lines)
	}
	img, _ := s.Preview(filepath.Join(root, "logo.png"))
	if img.Width != 13 || img.Height != 7 || img.Mime != "image/png" {
		t.Errorf("png dimensions = %dx%d mime %q", img.Width, img.Height, img.Mime)
	}
	dir, _ := s.Preview(filepath.Join(root, "sub"))
	if dir.ChildCount != 1 || dir.FileCount != 1 {
		t.Errorf("directory preview = %+v", dir)
	}
}

// A preview is a head, never the file: the whole point is that clicking a
// two-gigabyte log costs the same as clicking a config.
func TestPreviewTruncatesAndRefusesToOfferAnEdit(t *testing.T) {
	root := t.TempDir()
	s := New([]string{root})
	var big bytes.Buffer
	for i := 0; i < previewMaxLines*3; i++ {
		fmt.Fprintf(&big, "line %d\n", i)
	}
	path := filepath.Join(root, "app.log")
	if err := os.WriteFile(path, big.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := s.Preview(path)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Truncated || p.Lines != previewMaxLines {
		t.Fatalf("expected a %d-line truncated head, got %d lines truncated=%v",
			previewMaxLines, p.Lines, p.Truncated)
	}
	if int64(len(p.Text)) >= p.Size {
		t.Fatal("the preview carried the whole file")
	}

	huge := filepath.Join(root, "huge.log")
	f, err := os.Create(huge)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxEditBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	hp, err := s.Preview(huge)
	if err != nil {
		t.Fatal(err)
	}
	// A sparse file reads as NULs, so this one is binary — but the rule that
	// matters is the same either way: nothing the editor would refuse to open
	// may come back marked editable, or Save writes the head over the file.
	if hp.Editable {
		t.Fatal("a file past the editor's limit must not be offered as editable")
	}
}

// Peeking inside an archive is what stops "what is in this tarball" being
// answered by unpacking it somewhere and looking.
func TestPreviewPeeksInsideArchives(t *testing.T) {
	root := t.TempDir()
	s := New([]string{root})

	zipPath := filepath.Join(root, "release.zip")
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	for _, name := range []string{"app/main.go", "app/README.md"} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte("package main\n"))
	}
	zw.Close()
	zf.Close()

	tgzPath := filepath.Join(root, "backup.tar.gz")
	tf, err := os.Create(tgzPath)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(tf)
	tw := tar.NewWriter(gz)
	body := []byte("hello")
	tw.WriteHeader(&tar.Header{Name: "etc/hosts", Size: int64(len(body)), Mode: 0o644})
	tw.Write(body)
	tw.Close()
	gz.Close()
	tf.Close()

	for _, c := range []struct{ path, want string }{
		{zipPath, "app/main.go"},
		{tgzPath, "etc/hosts"},
	} {
		p, err := s.Preview(c.path)
		if err != nil {
			t.Fatalf("Preview(%s): %v", c.path, err)
		}
		if p.Kind != KindArchive {
			t.Fatalf("%s previewed as %q", c.path, p.Kind)
		}
		if len(p.Entries) == 0 || p.Entries[0].Name != c.want {
			t.Fatalf("%s listed %+v, want %q first", c.path, p.Entries, c.want)
		}
	}
}

// MediaType is a security boundary rather than a convenience: it decides what
// the raw route will hand back with a content type the browser acts on, on the
// same origin as the session that drives this host.
func TestMediaTypeIsAnAllowlist(t *testing.T) {
	for name, want := range map[string]string{
		"photo.JPG":     "image/jpeg",
		"clip.mp4":      "video/mp4",
		"manual.pdf":    "application/pdf",
		"icon.svg":      "image/svg+xml",
		"index.html":    "",
		"app.js":        "",
		"shell.php":     "",
		"payload.xhtml": "",
		"notes.txt":     "",
		"archive.zip":   "",
	} {
		if got := MediaType(name); got != want {
			t.Errorf("MediaType(%q) = %q, want %q", name, got, want)
		}
	}
}

// The two image formats the standard library cannot read are half of what a
// web root actually holds, so both are parsed here — and a header parser is
// exactly the kind of code that is wrong until something checks it.
func TestImageSizeReadsWebPAndSVG(t *testing.T) {
	root := t.TempDir()

	lossy := make([]byte, 30)
	copy(lossy[0:], "RIFF")
	copy(lossy[8:], "WEBPVP8 ")
	lossy[23], lossy[24], lossy[25] = 0x9d, 0x01, 0x2a
	binary.LittleEndian.PutUint16(lossy[26:], 640)
	binary.LittleEndian.PutUint16(lossy[28:], 480)
	lossyPath := filepath.Join(root, "lossy.webp")
	if err := os.WriteFile(lossyPath, lossy, 0o644); err != nil {
		t.Fatal(err)
	}
	if w, h := imageSize(lossyPath); w != 640 || h != 480 {
		t.Errorf("VP8 webp = %dx%d, want 640x480", w, h)
	}

	extended := make([]byte, 30)
	copy(extended[0:], "RIFF")
	copy(extended[8:], "WEBPVP8X")
	// 24-bit canvas width-1 and height-1, little-endian.
	extended[24], extended[25], extended[26] = 0x0f, 0x00, 0x00 // 15 → 16
	extended[27], extended[28], extended[29] = 0x1f, 0x00, 0x00 // 31 → 32
	extPath := filepath.Join(root, "ext.webp")
	if err := os.WriteFile(extPath, extended, 0o644); err != nil {
		t.Fatal(err)
	}
	if w, h := imageSize(extPath); w != 16 || h != 32 {
		t.Errorf("VP8X webp = %dx%d, want 16x32", w, h)
	}

	svgPath := filepath.Join(root, "icon.svg")
	// No width or height at all, which is how an icon written to scale with
	// its container is shipped: the viewBox is the only size there is.
	if err := os.WriteFile(svgPath,
		[]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 48"><path d="M0 0"/></svg>`),
		0o644); err != nil {
		t.Fatal(err)
	}
	if w, h := imageSize(svgPath); w != 24 || h != 48 {
		t.Errorf("svg viewBox = %dx%d, want 24x48", w, h)
	}
}

func TestUsageCountsATreeAndNamesTheHeaviestChild(t *testing.T) {
	root := t.TempDir()
	s := New([]string{root})
	if err := os.MkdirAll(filepath.Join(root, "big"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "small"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "big", "a"), bytes.Repeat([]byte("x"), 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "small", "b"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	u, err := s.Usage(context.Background(), root, 0)
	if err != nil {
		t.Fatal(err)
	}
	if u.Bytes != 4097 || u.Files != 2 || u.Dirs != 2 {
		t.Fatalf("usage = %+v, want 4097 bytes over 2 files in 2 dirs", u)
	}
	if len(u.Largest) == 0 || u.Largest[0].Name != "big" {
		t.Fatalf("largest = %+v, want big first", u.Largest)
	}
}

func TestChecksumMatchesTheKnownDigest(t *testing.T) {
	root := t.TempDir()
	s := New([]string{root})
	path := filepath.Join(root, "hello.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum, err := s.Checksum(context.Background(), path, "sha256")
	if err != nil {
		t.Fatal(err)
	}
	const want = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if sum.Sum != want {
		t.Fatalf("sha256 = %q, want %q", sum.Sum, want)
	}
	if _, err := s.Checksum(context.Background(), path, "crc32"); err == nil {
		t.Fatal("an unknown algorithm must be refused rather than silently substituted")
	}
}

func TestPreviewStaysInsideTheRoots(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New([]string{root})
	if _, err := s.Preview(secret); err == nil {
		t.Fatal("Preview must refuse a path outside the roots")
	}
	if _, err := s.Usage(context.Background(), outside, 0); err == nil {
		t.Fatal("Usage must refuse a path outside the roots")
	}
	if _, err := s.Checksum(context.Background(), secret, "sha256"); err == nil {
		t.Fatal("Checksum must refuse a path outside the roots")
	}
}
