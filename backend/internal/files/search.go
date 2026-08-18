package files

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type SearchHit struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	IsDir   bool   `json:"isDir"`
	Line    int    `json:"line,omitempty"`
	Snippet string `json:"snippet,omitempty"`
	Size    int64  `json:"size"`
}

type SearchOptions struct {
	Root       string
	Query      string
	Content    bool
	Regex      bool
	IgnoreCase bool
	MaxDepth   int
	Limit      int
	// MaxFileSize caps which files are opened for a content search. Grepping
	// a database dump or a video is never what the operator meant and would
	// stall the request for minutes.
	MaxFileSize int64
}

var skipDirs = map[string]bool{
	"node_modules": true, ".git": true, ".svn": true, "vendor": true,
	"__pycache__": true, ".cache": true, ".next": true,
	"proc": true, "sys": true, "dev": true, "run": true,
}

func (s *Service) Search(ctx context.Context, opts SearchOptions) ([]SearchHit, error) {
	root, err := s.Resolve(opts.Root)
	if err != nil {
		return nil, err
	}
	if opts.Limit <= 0 || opts.Limit > 2000 {
		opts.Limit = 500
	}
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = 12
	}
	if opts.MaxFileSize <= 0 {
		opts.MaxFileSize = 4 << 20
	}

	var match func(string) bool
	switch {
	case opts.Regex:
		expr := opts.Query
		if opts.IgnoreCase {
			expr = "(?i)" + expr
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			return nil, err
		}
		match = re.MatchString
	case opts.IgnoreCase:
		needle := strings.ToLower(opts.Query)
		match = func(s string) bool { return strings.Contains(strings.ToLower(s), needle) }
	default:
		match = func(s string) bool { return strings.Contains(s, opts.Query) }
	}

	baseDepth := strings.Count(root, string(os.PathSeparator))
	hits := []SearchHit{}
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return nil
		}
		if len(hits) >= opts.Limit {
			return filepath.SkipAll
		}
		if d.IsDir() {
			if path != root && (skipDirs[d.Name()] ||
				strings.Count(path, string(os.PathSeparator))-baseDepth > opts.MaxDepth) {
				return filepath.SkipDir
			}
			if path != root && match(d.Name()) && !opts.Content {
				hits = append(hits, SearchHit{Path: path, Name: d.Name(), IsDir: true})
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		// A symlink is followed by neither the name nor content search; the
		// target is visited on its own if it lives under the root.
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if !opts.Content {
			if match(d.Name()) {
				hits = append(hits, SearchHit{Path: path, Name: d.Name(), Size: info.Size()})
			}
			return nil
		}
		if info.Size() > opts.MaxFileSize || info.Size() == 0 {
			return nil
		}
		if hit, ok := grepFile(path, info.Size(), match); ok {
			hit.Name = d.Name()
			hits = append(hits, hit)
		}
		return nil
	})
	if err != nil && err != filepath.SkipAll && ctx.Err() == nil {
		return hits, err
	}
	return hits, nil
}

func grepFile(path string, size int64, match func(string) bool) (SearchHit, bool) {
	f, err := os.Open(path)
	if err != nil {
		return SearchHit{}, false
	}
	defer f.Close()

	head := make([]byte, 512)
	n, _ := f.Read(head)
	if looksBinary(head[:n]) {
		return SearchHit{}, false
	}
	f.Seek(0, 0)

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		if match(line) {
			snippet := strings.TrimSpace(line)
			if len(snippet) > 240 {
				snippet = snippet[:240] + "…"
			}
			return SearchHit{Path: path, Line: lineNo, Snippet: snippet, Size: size}, true
		}
	}
	return SearchHit{}, false
}
