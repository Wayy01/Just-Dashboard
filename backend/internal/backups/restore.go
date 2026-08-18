package backups

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type RestoreResult struct {
	RunID       int64    `json:"runId"`
	Destination string   `json:"destination"`
	Entries     int      `json:"entries"`
	Bytes       int64    `json:"bytes"`
	Skipped     []string `json:"skipped,omitempty"`
}

// Restore unpacks a completed run's artifact into a destination directory.
// It never restores in place over the original paths by default: the caller
// names an explicit destination, so recovering a single file does not require
// overwriting a live tree.
func (r *Runner) Restore(ctx context.Context, runID int64, destination string) (*RestoreResult, error) {
	run, err := r.store.Run(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Status != StatusSuccess || run.Artifact == "" {
		return nil, fmt.Errorf("run %d has no artifact to restore", runID)
	}
	job, err := r.store.Get(ctx, run.JobID)
	if err != nil {
		return nil, err
	}
	dest, err := filepath.Abs(filepath.Clean(destination))
	if err != nil {
		return nil, err
	}
	if dest == "/" {
		return nil, fmt.Errorf("refusing to restore directly over /")
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return nil, err
	}

	archivePath := run.Artifact
	if job.TargetKind != TargetLocal {
		secrets, err := r.store.Secrets(ctx, job.ID)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(r.stage, 0o700); err != nil {
			return nil, err
		}
		archivePath = filepath.Join(r.stage, "restore-"+filepath.Base(run.Artifact))
		// The staged copy is removed after extraction so a restore does not
		// leave a second full copy of the backup on disk.
		defer os.Remove(archivePath)
		if err := downloadObject(ctx, job, secrets, run.Artifact, archivePath); err != nil {
			return nil, err
		}
	}
	return extractArchive(ctx, archivePath, dest, runID)
}

func extractArchive(ctx context.Context, archivePath, dest string, runID int64) (*RestoreResult, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	res := &RestoreResult{RunID: runID, Destination: dest, Skipped: []string{}}
	tr := tar.NewReader(gz)
	for {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		hdr, err := tr.Next()
		if err == io.EOF {
			return res, nil
		}
		if err != nil {
			return res, err
		}
		target, err := safeJoin(dest, hdr.Name)
		if err != nil {
			// A tampered or hand-built archive could carry ../ entries; the
			// restore refuses them rather than writing outside the target.
			res.Skipped = append(res.Skipped, hdr.Name)
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode).Perm()); err != nil {
				return res, err
			}
		case tar.TypeSymlink:
			if _, err := safeJoin(dest, filepath.Join(filepath.Dir(hdr.Name), hdr.Linkname)); err != nil {
				res.Skipped = append(res.Skipped, hdr.Name)
				continue
			}
			os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return res, err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return res, err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode).Perm())
			if err != nil {
				return res, err
			}
			n, err := io.Copy(out, tr)
			out.Close()
			if err != nil {
				return res, err
			}
			res.Bytes += n
		default:
			continue
		}
		res.Entries++
	}
}

func safeJoin(dest, name string) (string, error) {
	cleaned := filepath.Clean(filepath.Join(dest, name))
	if cleaned != dest && !strings.HasPrefix(cleaned, dest+string(os.PathSeparator)) {
		return "", fmt.Errorf("entry %q escapes the destination", name)
	}
	return cleaned, nil
}

// ListArchive shows what a run contains without extracting it, so an operator
// can confirm a backup holds what they expect before restoring anything.
func (r *Runner) ListArchive(ctx context.Context, runID int64, limit int) ([]ArchiveEntry, error) {
	run, err := r.store.Run(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.Status != StatusSuccess || run.Artifact == "" {
		return nil, fmt.Errorf("run %d has no artifact", runID)
	}
	job, err := r.store.Get(ctx, run.JobID)
	if err != nil {
		return nil, err
	}
	path := run.Artifact
	if job.TargetKind != TargetLocal {
		secrets, err := r.store.Secrets(ctx, job.ID)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(r.stage, 0o700); err != nil {
			return nil, err
		}
		path = filepath.Join(r.stage, "list-"+filepath.Base(run.Artifact))
		defer os.Remove(path)
		if err := downloadObject(ctx, job, secrets, run.Artifact, path); err != nil {
			return nil, err
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	if limit <= 0 || limit > 20000 {
		limit = 2000
	}
	out := []ArchiveEntry{}
	tr := tar.NewReader(gz)
	for len(out) < limit {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return out, err
		}
		out = append(out, ArchiveEntry{
			Name:  hdr.Name,
			Size:  hdr.Size,
			Mode:  fmt.Sprintf("%04o", os.FileMode(hdr.Mode).Perm()),
			IsDir: hdr.Typeflag == tar.TypeDir,
		})
	}
	return out, nil
}

type ArchiveEntry struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Mode  string `json:"mode"`
	IsDir bool   `json:"isDir"`
}
