package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SuperMarioYL/draftproof/internal/chain"
	"github.com/SuperMarioYL/draftproof/internal/store"
)

func TestEndToEndInitCaptureExportVerify(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	thesis := t.TempDir()
	outDir := t.TempDir()

	// 1. init generates keys + key card under $HOME/.draftproof
	var initOut bytes.Buffer
	initCmd := NewInitCmd()
	initCmd.SetOut(&initOut)
	if err := initCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(initOut.String(), "sha256:") {
		t.Fatalf("init must print the key card, got: %s", initOut.String())
	}
	if _, err := os.Stat(filepath.Join(home, ".draftproof", "keycard.txt")); err != nil {
		t.Fatal("keycard.txt not written")
	}
	keys, err := chain.LoadKeys(filepath.Join(home, ".draftproof"))
	if err != nil {
		t.Fatal(err)
	}

	// 2. capture saves directly through the watcher core (fsnotify covered
	// separately below) — including one duplicate save that must dedupe.
	st, err := store.Open(thesis)
	if err != nil {
		t.Fatal(err)
	}
	w, err := newWatcher(thesis, st, keys, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	doc := filepath.Join(thesis, "thesis.md")
	saves := []string{
		"# 绪论\n",
		"# 绪论\n\n研究背景……\n",
		"# 绪论\n\n研究背景……\n（未变化的重复保存）\n",
	}
	for _, content := range saves {
		if err := os.WriteFile(doc, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := w.capture(doc); err != nil {
			t.Fatal(err)
		}
	}
	// duplicate content: re-capture the same bytes — must not append
	if err := w.capture(doc); err != nil {
		t.Fatal(err)
	}
	all, err := st.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 snapshots (dedupe keeps identical content out), got %d", len(all))
	}

	// 3. log prints a timeline
	var logOut bytes.Buffer
	logCmd := NewLogCmd()
	logCmd.SetOut(&logOut)
	logCmd.SetArgs([]string{thesis})
	if err := logCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logOut.String(), "写作时间线") || !strings.Contains(logOut.String(), "thesis.md") {
		t.Fatalf("log output wrong: %s", logOut.String())
	}

	// 4. export writes .dpb + .html
	var exportOut bytes.Buffer
	exportCmd := NewExportCmd()
	exportCmd.SetOut(&exportOut)
	exportCmd.SetArgs([]string{thesis, "--out", outDir})
	if err := exportCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	var dpbPath string
	sawHTML := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".dpb") {
			dpbPath = filepath.Join(outDir, e.Name())
		}
		if strings.HasSuffix(e.Name(), ".html") {
			sawHTML = true
		}
	}
	if dpbPath == "" || !sawHTML {
		t.Fatalf("export must produce .dpb and .html, got %v", entries)
	}
	html, err := os.ReadFile(strings.Replace(dpbPath, ".dpb", ".html", 1))
	if err != nil || !bytes.Contains(html, []byte("写作过程回执")) {
		t.Fatalf("html receipt wrong: err=%v", err)
	}

	// 5. verify PASS (exit nil)
	var verifyOut bytes.Buffer
	verifyCmd := NewVerifyCmd()
	verifyCmd.SetOut(&verifyOut)
	verifyCmd.SetArgs([]string{dpbPath})
	if err := verifyCmd.Execute(); err != nil {
		t.Fatalf("clean receipt must verify, output: %s err: %v", verifyOut.String(), err)
	}
	if !strings.Contains(verifyOut.String(), "PASS") {
		t.Fatalf("verify output missing PASS: %s", verifyOut.String())
	}

	// 6. tamper one snapshot's content hash -> verify FAIL with breakpoint
	raw, err := os.ReadFile(dpbPath)
	if err != nil {
		t.Fatal(err)
	}
	receiptJSON := string(raw)
	idx := strings.Index(receiptJSON, `"content_hash": "`)
	tampered := receiptJSON[:idx+len(`"content_hash": "`)] + "deadbeef" +
		receiptJSON[idx+len(`"content_hash": "`)+8:]
	if tampered == receiptJSON {
		t.Fatal("tamper substitution failed")
	}
	if err := os.WriteFile(dpbPath, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}
	var failOut bytes.Buffer
	failCmd := NewVerifyCmd()
	failCmd.SetOut(&failOut)
	failCmd.SetArgs([]string{dpbPath})
	err = failCmd.Execute()
	if !errors.Is(err, ErrVerifyFailed) {
		t.Fatalf("tampered receipt must return ErrVerifyFailed, got %v (out: %s)", err, failOut.String())
	}
	if !strings.Contains(failOut.String(), "FAIL") || !strings.Contains(failOut.String(), "断点") &&
		!strings.Contains(failOut.String(), "快照 #") {
		t.Fatalf("FAIL output must locate the break: %s", failOut.String())
	}
}

func TestWatchCapturesRealSaves(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	thesis := t.TempDir()

	if err := NewInitCmd().Execute(); err != nil {
		t.Fatal(err)
	}
	keys, err := chain.LoadKeys(filepath.Join(home, ".draftproof"))
	if err != nil {
		t.Fatal(err)
	}

	watchCmd := NewWatchCmd()
	watchCmd.SetArgs([]string{thesis, "--debounce", "150ms"})
	// run in background; kill via timeout
	done := make(chan error, 1)
	go func() { done <- watchCmd.Execute() }()

	// wait for the watcher to come up, then save twice
	time.Sleep(400 * time.Millisecond)
	doc := filepath.Join(thesis, "chapter.md")
	if err := os.WriteFile(doc, []byte("第一版\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(450 * time.Millisecond)
	if err := os.WriteFile(doc, []byte("第一版\n第二段\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// wait for debounce + capture
	deadline := time.Now().Add(5 * time.Second)
	for {
		st, err := store.OpenExisting(thesis)
		if err == nil {
			all, err2 := st.LoadAll()
			if err2 == nil && len(all) >= 2 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("watch did not capture two saves in time (fsnotify integration)")
		}
		time.Sleep(100 * time.Millisecond)
	}

	// the snapshots must verify
	st, err := store.OpenExisting(thesis)
	if err != nil {
		t.Fatal(err)
	}
	all, err := st.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected exactly 2 snapshots, got %d", len(all))
	}
	for _, s := range all {
		if err := chain.VerifySnapshot(s, keys.Public); err != nil {
			t.Fatalf("captured snapshot seq %d does not verify: %v", s.Seq, err)
		}
		if s.Path != "chapter.md" {
			t.Fatalf("unexpected path %q", s.Path)
		}
	}
	// store writes themselves must not be captured (no .jsonl matches)
	if all[0].ContentHash == all[1].ContentHash {
		t.Fatal("two different saves must produce different content hashes")
	}
}

func TestParseSince(t *testing.T) {
	for _, ok := range []string{"2026-06-01", "2026-06-01 09:30", "2026-06-01 09:30:15"} {
		if _, err := parseSince(ok); err != nil {
			t.Fatalf("parseSince(%q) failed: %v", ok, err)
		}
	}
	if _, err := parseSince("June 1"); err == nil {
		t.Fatal("parseSince must reject non-ISO input")
	}
}
