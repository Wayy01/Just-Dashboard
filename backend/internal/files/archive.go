package files

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Wayy01/Just-Dashboard/backend/internal/safepath"
)

type ArchiveFormat string

const (
	FormatTarGz ArchiveFormat = "tar.gz"
	FormatZip   ArchiveFormat = "zip"
)

// Compress writes an archive of the given paths to w. Streaming rather than
// building the archive on disk first means a multi-gigabyte directory download
// costs no temporary space.
func (s *Service) Compress(w io.Writer, base string, paths []string, format ArchiveFormat) error {
	baseDir, err := s.Resolve(base)
	if err != nil {
		return err
	}
	resolved := make([]string, 0, len(paths))
	for _, p := range paths {
		full, err := s.Resolve(p)
		if err != nil {
			return err
		}
		resolved = append(resolved, full)
	}
	switch format {
	case FormatZip:
		return writeZip(w, baseDir, resolved)
	default:
		return writeTarGz(w, baseDir, resolved)
	}
}

func writeTarGz(w io.Writer, baseDir string, paths []string) error {
	gz := gzip.NewWriter(w)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	for _, root := range paths {
		if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			rel, err := filepath.Rel(baseDir, path)
			if err != nil {
				return err
			}
			link := ""
			if info.Mode()&os.ModeSymlink != 0 {
				link, _ = os.Readlink(path)
			}
			hdr, err := tar.FileInfoHeader(info, link)
			if err != nil {
				return err
			}
			hdr.Name = filepath.ToSlash(rel)
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			f, err := os.Open(path)
			if err != nil {
				return nil
			}
			defer f.Close()
			_, err = io.Copy(tw, f)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

func writeZip(w io.Writer, baseDir string, paths []string) error {
	zw := zip.NewWriter(w)
	defer zw.Close()

	for _, root := range paths {
		if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || !info.Mode().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(baseDir, path)
			if err != nil {
				return err
			}
			hdr, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			hdr.Name = filepath.ToSlash(rel)
			hdr.Method = zip.Deflate
			out, err := zw.CreateHeader(hdr)
			if err != nil {
				return err
			}
			f, err := os.Open(path)
			if err != nil {
				return nil
			}
			defer f.Close()
			_, err = io.Copy(out, f)
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

// Extract unpacks an archive into dest. Entry names are checked against the
// destination before anything is written: a crafted archive containing
// "../../etc/cron.d/evil" is the classic path-traversal write primitive, and
// refusing it is not optional.
func (s *Service) Extract(archivePath, dest string) ([]string, error) {
	src, err := s.Resolve(archivePath)
	if err != nil {
		return nil, err
	}
	target, err := s.Resolve(dest)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return nil, err
	}
	lower := strings.ToLower(src)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(src, target)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTar(src, target, true)
	case strings.HasSuffix(lower, ".tar"):
		return extractTar(src, target, false)
	default:
		return nil, fmt.Errorf("unsupported archive format: %s", filepath.Base(src))
	}
}

func extractTar(src, dest string, compressed bool) ([]string, error) {
	f, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var reader io.Reader = f
	if compressed {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		reader = gz
	}
	written := []string{}
	tr := tar.NewReader(reader)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return written, nil
		}
		if err != nil {
			return written, err
		}
		path, err := safepath.Join(dest, hdr.Name)
		if err != nil {
			return written, err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := safepath.Mkdir(path, os.FileMode(hdr.Mode).Perm()); err != nil {
				return written, err
			}
		case tar.TypeSymlink:
			// A symlink whose target escapes the destination turns into an
			// arbitrary write the next time anything follows it.
			if err := safepath.CheckLinkTarget(dest, hdr.Name, hdr.Linkname); err != nil {
				return written, err
			}
			if err := safepath.Symlink(hdr.Linkname, path); err != nil {
				return written, err
			}
		case tar.TypeReg:
			if err := safepath.MkdirParents(path); err != nil {
				return written, err
			}
			out, err := safepath.Create(path, os.FileMode(hdr.Mode).Perm())
			if err != nil {
				return written, err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return written, err
			}
			out.Close()
		default:
			continue
		}
		written = append(written, path)
	}
}

func extractZip(src, dest string) ([]string, error) {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	written := []string{}
	for _, entry := range zr.File {
		path, err := safepath.Join(dest, entry.Name)
		if err != nil {
			return written, err
		}
		if entry.FileInfo().IsDir() {
			if err := safepath.Mkdir(path, entry.Mode().Perm()); err != nil {
				return written, err
			}
			written = append(written, path)
			continue
		}
		if err := safepath.MkdirParents(path); err != nil {
			return written, err
		}
		rc, err := entry.Open()
		if err != nil {
			return written, err
		}
		out, err := safepath.Create(path, entry.Mode().Perm())
		if err != nil {
			rc.Close()
			return written, err
		}
		_, copyErr := io.Copy(out, rc)
		out.Close()
		rc.Close()
		if copyErr != nil {
			return written, copyErr
		}
		written = append(written, path)
	}
	return written, nil
}
