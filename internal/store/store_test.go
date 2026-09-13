package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SuperMarioYL/draftproof/internal/chain"
)

func mustKeys(t *testing.T) *chain.AuthorKeys {
	t.Helper()
	k, _, err := chain.LoadOrCreateKeys(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestOpenCreatesStoreAndAppendLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, DirName, LogName)); err != nil {
		t.Fatal("chain.jsonl should exist after Open")
	}

	k := mustKeys(t)
	base := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	var prevHash string
	want := 3
	for i := 1; i <= want; i++ {
		snap := chain.Snapshot{
			DocID:       chain.DocID("thesis.md"),
			Path:        "thesis.md",
			Seq:         uint64(i),
			SavedAt:     base.Add(time.Duration(i) * time.Minute),
			MonoNanos:   int64(i) * int64(time.Minute),
			PrevHash:    prevHash,
			ContentHash: chain.HashBytes([]byte{byte(i)}),
			Size:        int64(10 * i),
		}
		if err := k.SignSnapshot(&snap); err != nil {
			t.Fatal(err)
		}
		if err := s.Append(snap); err != nil {
			t.Fatal(err)
		}
		h, err := snap.RecordHash()
		if err != nil {
			t.Fatal(err)
		}
		prevHash = h
	}

	loaded, err := s.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != want {
		t.Fatalf("expected %d snapshots, got %d", want, len(loaded))
	}
	for i, snap := range loaded {
		if snap.Seq != uint64(i+1) {
			t.Fatalf("append order lost: position %d has seq %d", i, snap.Seq)
		}
		if err := chain.VerifySnapshot(snap, k.Public); err != nil {
			t.Fatalf("roundtripped snapshot %d fails verification: %v", i+1, err)
		}
	}
}

func TestHeadsAndByDoc(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	k := mustKeys(t)
	at := time.Date(2026, 6, 2, 20, 0, 0, 0, time.UTC)

	appendSnap := func(docPath string, seq uint64, prev string, content string) {
		t.Helper()
		snap := chain.Snapshot{
			DocID: chain.DocID(docPath), Path: docPath, Seq: seq, SavedAt: at,
			PrevHash: prev, ContentHash: chain.HashBytes([]byte(content)), Size: 5,
		}
		if err := k.SignSnapshot(&snap); err != nil {
			t.Fatal(err)
		}
		if err := s.Append(snap); err != nil {
			t.Fatal(err)
		}
	}

	appendSnap("thesis.md", 1, chain.GenesisPrevHash, "a")
	appendSnap("thesis.md", 2, "ignored-prev", "b")
	appendSnap("notes.md", 1, chain.GenesisPrevHash, "x")

	all, err := s.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	heads, err := Heads(all)
	if err != nil {
		t.Fatal(err)
	}
	if len(heads) != 2 {
		t.Fatalf("expected 2 doc heads, got %d", len(heads))
	}
	thesis := heads[chain.DocID("thesis.md")]
	if thesis.LastSeq != 2 || thesis.LastContentHash != chain.HashBytes([]byte("b")) {
		t.Fatalf("thesis head wrong: %+v", thesis)
	}
	if thesis.LastRecordHash == "" {
		t.Fatal("head must carry the last record hash for chaining")
	}

	grouped := ByDoc(all)
	if len(grouped[chain.DocID("thesis.md")]) != 2 || len(grouped[chain.DocID("notes.md")]) != 1 {
		t.Fatalf("ByDoc grouping wrong: %+v", grouped)
	}
}

func TestWalkWatchableSkipsHiddenDirs(t *testing.T) {
	root := t.TempDir()
	for _, sub := range []string{"chapters", ".git", ".draftproof", filepath.Join("chapters", "intro")} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dirs, err := WalkWatchable(root)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, d := range dirs {
		seen[filepath.Base(d)] = true
	}
	if seen[".git"] || seen[".draftproof"] {
		t.Fatalf("hidden dirs must be skipped, got %v", dirs)
	}
	if !seen["chapters"] || !seen["intro"] {
		t.Fatalf("normal subdirs must be watched, got %v", dirs)
	}
}
