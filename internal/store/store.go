// Package store persists the append-only snapshot log. Everything DraftProof
// records lives in <watched-dir>/.draftproof/chain.jsonl: one JSON snapshot
// per line, in append order, interleaved across documents (each document
// threads its own chain through prev_hash). Records are only ever appended;
// rewriting, inserting or deleting lines is detectable by chain validation.
package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/SuperMarioYL/draftproof/internal/chain"
)

// DirName is the store directory created inside the watched folder.
const DirName = ".draftproof"

// LogName is the append-only snapshot log file inside the store directory.
const LogName = "chain.jsonl"

// Store is an opened snapshot log.
type Store struct {
	dir     string
	logPath string
}

// DocState is the current chain head of one document, used by watch to build
// the next snapshot and to dedupe identical content.
type DocState struct {
	Path            string
	LastSeq         uint64
	LastContentHash string
	LastRecordHash  string
}

// Open prepares the store inside watchDir, creating directories on first use.
func Open(watchDir string) (*Store, error) {
	dir := filepath.Join(watchDir, DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{dir: dir, logPath: filepath.Join(dir, LogName)}
	if _, err := os.Stat(s.logPath); os.IsNotExist(err) {
		if err := os.WriteFile(s.logPath, nil, 0o600); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// OpenExisting opens the store without creating anything; it fails when the
// directory has never been watched.
func OpenExisting(watchDir string) (*Store, error) {
	s := &Store{dir: filepath.Join(watchDir, DirName), logPath: filepath.Join(watchDir, DirName, LogName)}
	if _, err := os.Stat(s.logPath); err != nil {
		return nil, fmt.Errorf("%s 下没有存证库（.draftproof/%s 不存在）——先运行 draftproof watch %s",
			watchDir, LogName, watchDir)
	}
	return s, nil
}

// Dir returns the store directory path.
func (s *Store) Dir() string { return s.dir }

// Append writes one snapshot as a single JSON line and fsyncs. It never
// rewrites earlier lines.
func (s *Store) Append(snap chain.Snapshot) error {
	line, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

// LoadAll returns every snapshot in append order.
func (s *Store) LoadAll() ([]chain.Snapshot, error) {
	f, err := os.Open(s.logPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []chain.Snapshot
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		text := sc.Bytes()
		if len(text) == 0 {
			continue
		}
		var snap chain.Snapshot
		if err := json.Unmarshal(text, &snap); err != nil {
			return nil, fmt.Errorf("chain.jsonl 第 %d 行损坏: %w", line, err)
		}
		out = append(out, snap)
	}
	return out, sc.Err()
}

// ByDoc groups snapshots by document id, preserving append order.
func ByDoc(snapshots []chain.Snapshot) map[string][]chain.Snapshot {
	grouped := make(map[string][]chain.Snapshot)
	for _, s := range snapshots {
		grouped[s.DocID] = append(grouped[s.DocID], s)
	}
	return grouped
}

// Heads computes the current chain head per document from a full load.
func Heads(snapshots []chain.Snapshot) (map[string]DocState, error) {
	heads := make(map[string]DocState)
	for _, s := range snapshots {
		h, err := s.RecordHash()
		if err != nil {
			return nil, err
		}
		heads[s.DocID] = DocState{
			Path:            s.Path,
			LastSeq:         s.Seq,
			LastContentHash: s.ContentHash,
			LastRecordHash:  h,
		}
	}
	return heads, nil
}

// WalkWatchable lists directories under root that should be watched:
// everything except hidden directories (which includes the store itself).
func WalkWatchable(root string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && isHidden(d.Name()) {
			return filepath.SkipDir
		}
		dirs = append(dirs, path)
		return nil
	})
	return dirs, err
}

func isHidden(name string) bool {
	return len(name) > 1 && name[0] == '.'
}
