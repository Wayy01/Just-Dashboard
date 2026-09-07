package files

import (
	"archive/tar"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractRejectsMaliciousArchivePathsBeforeOutsideWrite(t *testing.T) {
	sandbox := t.TempDir()
	root := filepath.Join(sandbox, "allowed")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(root, "malicious.tar")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(file)
	body := []byte("must-not-escape")
	if err := writer.WriteHeader(&tar.Header{Name: "../../outside/evil", Mode: 0o600, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	service := New([]string{root})
	if _, err := service.Extract(archivePath, filepath.Join(root, "destination")); err == nil {
		t.Fatal("archive traversal was accepted")
	}
	if _, err := os.Stat(filepath.Join(sandbox, "outside", "evil")); !os.IsNotExist(err) {
		t.Fatalf("malicious archive wrote outside its destination: %v", err)
	}
}

func TestExtractRejectsAbsoluteArchiveSymlinkTargetBeforeFollowupWrite(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "malicious-link.tar")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(file)
	if err := writer.WriteHeader(&tar.Header{Name: "link", Linkname: "/etc", Typeflag: tar.TypeSymlink, Mode: 0o777}); err != nil {
		t.Fatal(err)
	}
	body := []byte("must-not-follow")
	if err := writer.WriteHeader(&tar.Header{Name: "link/cron.d/evil", Mode: 0o600, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	service := New([]string{root})
	if _, err := service.Extract(archivePath, filepath.Join(root, "destination")); err == nil {
		t.Fatal("absolute archive symlink target was accepted")
	}
	if _, err := os.Lstat(filepath.Join(root, "destination", "link")); !os.IsNotExist(err) {
		t.Fatalf("malicious archive symlink was created: %v", err)
	}
}
