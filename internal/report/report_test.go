package report

import (
	"crypto/ed25519"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/SuperMarioYL/draftproof/internal/chain"
)

func testKeys(t *testing.T) *chain.AuthorKeys {
	t.Helper()
	k, _, err := chain.LoadOrCreateKeys(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// fakeWritingSim produces a signed chain that looks like two writing
// sessions on two days.
func fakeWritingSim(t *testing.T, k *chain.AuthorKeys, saves int) []chain.Snapshot {
	t.Helper()
	base := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	var out []chain.Snapshot
	var prevHash string
	size := int64(200)
	for i := 0; i < saves; i++ {
		// session 1: minutes 0..60 (saves 0..3), 40h idle, session 2
		at := base.Add(time.Duration(i) * 20 * time.Minute)
		if i >= 4 {
			at = base.Add(44*time.Hour + time.Duration(i-4)*25*time.Minute)
		}
		size += 350
		if i == 5 { // a rewrite that shrinks the file
			size -= 500
		}
		s := chain.Snapshot{
			DocID: chain.DocID("thesis.md"), Path: "thesis.md",
			Seq: uint64(i + 1), SavedAt: at,
			MonoNanos:   int64(i) * int64(20*time.Minute),
			PrevHash:    prevHash,
			ContentHash: chain.HashBytes([]byte{byte(i), 'x'}),
			Size:        size,
		}
		if err := k.SignSnapshot(&s); err != nil {
			t.Fatal(err)
		}
		h, err := s.RecordHash()
		if err != nil {
			t.Fatal(err)
		}
		prevHash = h
		out = append(out, s)
	}
	return out
}

func TestBuildAndVerifyCleanReceipt(t *testing.T) {
	k := testKeys(t)
	snaps := fakeWritingSim(t, k, 8)
	r, err := BuildReceipt(snaps, k, time.Time{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Since != "" || len(r.Snapshots) != 8 {
		t.Fatalf("full export wrong: since=%q snaps=%d", r.Since, len(r.Snapshots))
	}
	if r.Stats.SessionCount != 2 {
		t.Fatalf("expected 2 sessions (40h gap + mono run), got %d", r.Stats.SessionCount)
	}
	if r.Author.Fingerprint != chain.Fingerprint(k.Public) {
		t.Fatal("author fingerprint mismatch")
	}
	res := VerifyReceipt(r)
	if !res.OK {
		t.Fatalf("clean receipt must verify: %+v", res)
	}
	if !res.FullChain || res.SnapshotCount != 8 || res.ChainLinks != 7 {
		t.Fatalf("result accounting wrong: %+v", res)
	}
}

func TestVerifyDetectsTampering(t *testing.T) {
	k := testKeys(t)
	snaps := fakeWritingSim(t, k, 6)
	r, err := BuildReceipt(snaps, k, time.Time{}, "")
	if err != nil {
		t.Fatal(err)
	}

	// 1. tamper a snapshot content hash -> its sig + next chain link break
	bad := *r
	bad.Snapshots = append([]chain.Snapshot(nil), r.Snapshots...)
	bad.Snapshots[2].ContentHash = strings.Repeat("0", 64)
	res := VerifyReceipt(&bad)
	if res.OK {
		t.Fatal("tampered content hash must fail")
	}
	if len(res.ChainProblems) == 0 || res.ChainProblems[0].Index != 2 {
		t.Fatalf("breakpoint should be at index 2, got %+v", res.ChainProblems)
	}

	// 2. tamper a timestamp -> signature breaks
	bad = *r
	bad.Snapshots = append([]chain.Snapshot(nil), r.Snapshots...)
	bad.Snapshots[1].SavedAt = bad.Snapshots[1].SavedAt.Add(time.Hour)
	res = VerifyReceipt(&bad)
	if res.OK {
		t.Fatal("tampered timestamp must fail")
	}

	// 3. tamper stats (not a snapshot) -> receipt envelope sig / stats check fails
	bad = *r
	bad.Stats.SessionCount = 99
	res = VerifyReceipt(&bad)
	if res.OK || res.ReceiptSigOK || res.StatsConsistent {
		t.Fatalf("tampered stats must fail: %+v", res)
	}

	// 4. delete a middle snapshot -> seq gap
	bad = *r
	bad.Snapshots = append([]chain.Snapshot(nil), r.Snapshots[0], r.Snapshots[2], r.Snapshots[3], r.Snapshots[4], r.Snapshots[5])
	res = VerifyReceipt(&bad)
	if res.OK {
		t.Fatal("deleted snapshot must fail")
	}

	// 5. swap the author public key (forged receipt signed by another key)
	other := testKeys(t)
	forged, err := BuildReceipt(snaps, other, time.Time{}, "")
	if err == nil {
		// BuildReceipt refuses to sign snapshots made with another key, so
		// simulate a raw forgery by editing the envelope.
		forged.Author = r.Author
		res = VerifyReceipt(forged)
		if res.OK {
			t.Fatal("receipt signed by a different key must fail")
		}
	}
}

func TestSinceFilterKeepsContiguousRun(t *testing.T) {
	k := testKeys(t)
	snaps := fakeWritingSim(t, k, 8)
	cut := snaps[3].SavedAt // first snapshot of session 2's day
	r, err := BuildReceipt(snaps, k, cut, "2026-06-02")
	if err != nil {
		t.Fatal(err)
	}
	if r.Since != "2026-06-02" || len(r.Snapshots) != 5 {
		t.Fatalf("since filter wrong: since=%q kept=%d", r.Since, len(r.Snapshots))
	}
	if r.Snapshots[0].Seq != 4 {
		t.Fatalf("sub-chain must start at seq 4, got %d", r.Snapshots[0].Seq)
	}
	res := VerifyReceipt(r)
	if !res.OK {
		t.Fatalf("contiguous sub-chain must verify: %+v", res)
	}
	if res.FullChain {
		t.Fatal("since-exported receipt is not a full chain")
	}
}

func TestBuildReceiptRejectsForeignSnapshots(t *testing.T) {
	k := testKeys(t)
	other := testKeys(t)
	snaps := fakeWritingSim(t, other, 3)
	if _, err := BuildReceipt(snaps, k, time.Time{}, ""); err == nil {
		t.Fatal("export must refuse snapshots signed by a different key")
	}
}

func TestWriteLoadRoundtrip(t *testing.T) {
	k := testKeys(t)
	snaps := fakeWritingSim(t, k, 4)
	r, err := BuildReceipt(snaps, k, time.Time{}, "")
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/receipt-test.dpb"
	if err := r.WriteFile(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadReceipt(path)
	if err != nil {
		t.Fatal(err)
	}
	if VerifyReceipt(loaded).OK != true {
		t.Fatal("written + loaded receipt must still verify")
	}
	// corrupt the file: flip one hex char of a content hash via string replace
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(raw), r.Snapshots[1].ContentHash[:8], r.Snapshots[1].ContentHash[:7]+"f", 1)
	if tampered == string(raw) {
		t.Fatal("test tamper did not change the file")
	}
	if err := os.WriteFile(path, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}
	bad, err := LoadReceipt(path)
	if err != nil {
		t.Fatal(err)
	}
	if VerifyReceipt(bad).OK {
		t.Fatal("corrupted file must not verify")
	}
}

func TestRenderHTML(t *testing.T) {
	k := testKeys(t)
	snaps := fakeWritingSim(t, k, 6)
	r, err := BuildReceipt(snaps, k, time.Time{}, "")
	if err != nil {
		t.Fatal(err)
	}
	html, err := RenderHTMLString(r, "0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	markers := []string{
		"DraftProof 写作过程回执",
		"thesis.md",
		"sha256:" + r.Author.Fingerprint,
		"draftproof verify",
		"信任边界",
		"<svg",
		"写作会话",
	}
	for _, m := range markers {
		if !strings.Contains(html, m) {
			t.Fatalf("HTML missing marker %q", m)
		}
	}
	if strings.Contains(html, "<script") {
		t.Fatal("receipt HTML must not contain scripts")
	}
	// no raw draft content can leak: content only ever appears as hashes
	if strings.Contains(html, "第一章") {
		t.Fatal("unexpected content leak")
	}
}

func TestSessionsRespectGapAndMonoReset(t *testing.T) {
	base := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	snaps := []chain.Snapshot{
		{Seq: 1, SavedAt: base, MonoNanos: int64(time.Minute)},
		{Seq: 2, SavedAt: base.Add(10 * time.Minute), MonoNanos: int64(2 * time.Minute)},  // same session
		{Seq: 3, SavedAt: base.Add(50 * time.Minute), MonoNanos: int64(3 * time.Minute)},  // >30min gap -> new
		{Seq: 4, SavedAt: base.Add(55 * time.Minute), MonoNanos: int64(30 * time.Second)}, // mono reset -> new
	}
	sessions := AnalyzeSessions(snaps)
	if len(sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %+v", sessions)
	}
	if sessions[0].Snapshots != 2 || sessions[1].Snapshots != 1 || sessions[2].Snapshots != 1 {
		t.Fatalf("session grouping wrong: %+v", sessions)
	}
}

// resign re-signs the envelope after a test mutates a field, simulating what the
// author's key can do. v0.2.0's verifier trusted the doc block without checking
// it against the snapshots, so a re-signed over-claiming receipt still passed.
func resign(t *testing.T, r *Receipt, k *chain.AuthorKeys) {
	t.Helper()
	payload, err := r.signedPayload()
	if err != nil {
		t.Fatal(err)
	}
	r.ReceiptSig = hex.EncodeToString(ed25519.Sign(k.Private, payload))
}

// Regression: VerifyReceipt never compared the receipt's doc block with the
// snapshots it encloses, so a receipt could claim any snapshot count and time
// span — and an empty receipt printed PASS with "-1/-1" chain links.
func TestVerifyRejectsInconsistentDocBlock(t *testing.T) {
	k := testKeys(t)
	snaps := fakeWritingSim(t, k, 6)
	r, err := BuildReceipt(snaps, k, time.Time{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyReceipt(r).OK {
		t.Fatal("clean receipt must still verify")
	}

	bad := *r
	bad.Doc.SnapshotCount = 99
	bad.Doc.FirstSavedAt = bad.Doc.FirstSavedAt.Add(-90 * 24 * time.Hour)
	resign(t, &bad, k)
	if res := VerifyReceipt(&bad); res.OK {
		t.Fatalf("doc block that does not follow from the snapshots must fail: %+v", res)
	}

	bad = *r
	bad.Doc.Path = "another-thesis.docx"
	resign(t, &bad, k)
	if res := VerifyReceipt(&bad); res.OK {
		t.Fatal("doc path swapped for a path no snapshot records must fail")
	}
}

func TestVerifyRejectsEmptyReceipt(t *testing.T) {
	k := testKeys(t)
	snaps := fakeWritingSim(t, k, 3)
	r, err := BuildReceipt(snaps, k, time.Time{}, "")
	if err != nil {
		t.Fatal(err)
	}
	empty := *r
	empty.Snapshots = nil
	empty.Sessions = nil
	empty.Stats = ComputeStats(nil, nil)
	empty.Doc = DocInfo{}
	resign(t, &empty, k)
	res := VerifyReceipt(&empty)
	if res.OK {
		t.Fatal("a receipt with no snapshots must never PASS")
	}
	if res.ChainLinks < 0 {
		t.Fatalf("chain link count must not go negative, got %d", res.ChainLinks)
	}
}

// Regression: BuildReceipt signed receipts over structurally broken chains (a
// deleted middle record), producing a .dpb that verify is guaranteed to reject.
func TestBuildReceiptRejectsBrokenChain(t *testing.T) {
	k := testKeys(t)
	snaps := fakeWritingSim(t, k, 4)

	gapped := []chain.Snapshot{snaps[0], snaps[2], snaps[3]}
	if _, err := BuildReceipt(gapped, k, time.Time{}, ""); err == nil {
		t.Fatal("export must refuse a chain with a missing record")
	}

	// A non-contiguous record set must also be refused when it starts mid-chain
	// (--since export of the same broken store).
	if _, err := BuildReceipt(gapped, k, gapped[0].SavedAt, "since"); err == nil {
		t.Fatal("sub-chain export must refuse a broken chain too")
	}

	// The contiguous run still exports.
	if _, err := BuildReceipt(snaps, k, time.Time{}, ""); err != nil {
		t.Fatalf("clean chain must still export: %v", err)
	}
}

// Regression: --since earlier than the first save exported the whole chain but
// labelled it a sub-chain, which disabled the genesis assertion in verify.
func TestSinceBeforeFirstSnapshotIsFullChain(t *testing.T) {
	k := testKeys(t)
	snaps := fakeWritingSim(t, k, 4)
	r, err := BuildReceipt(snaps, k, snaps[0].SavedAt.Add(-time.Hour), "2020-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Snapshots) != len(snaps) {
		t.Fatalf("expected the whole chain, got %d of %d", len(r.Snapshots), len(snaps))
	}
	if r.Since != "" {
		t.Fatalf("a filter that excludes nothing must not label the receipt a sub-chain, since=%q", r.Since)
	}
	if res := VerifyReceipt(r); !res.OK || !res.FullChain {
		t.Fatalf("complete export must verify as a full chain: %+v", res)
	}
}

// Regression: a --since receipt counted the first selected snapshot's whole size
// as bytes written inside the window (1600 B baseline + 350 B growth was
// reported as 1950 B added).
func TestSinceReceiptDoesNotCountBaselineAsAdded(t *testing.T) {
	k := testKeys(t)
	snaps := fakeWritingSim(t, k, 5) // sizes 550,900,1250,1600,1950
	cut := snaps[3].SavedAt
	r, err := BuildReceipt(snaps, k, cut, "2026-06-02")
	if err != nil {
		t.Fatal(err)
	}
	baseline := r.Snapshots[0].Size
	growth := r.Snapshots[len(r.Snapshots)-1].Size - baseline
	if r.Stats.TotalBytesAdded != growth {
		t.Fatalf("window growth must be %d B, got %d B (baseline %d B must not count)",
			growth, r.Stats.TotalBytesAdded, baseline)
	}
	if !VerifyReceipt(r).OK {
		t.Fatal("sub-chain receipt must still verify")
	}
	html, err := RenderHTMLString(r, "0.3.0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "—") {
		t.Fatal("HTML first row must mark the sub-chain baseline instead of a size delta")
	}
}

// Compatibility: receipts exported by v0.2.0 (receipt schema @1) credited the
// sub-chain baseline to Stats.TotalBytesAdded. They must keep verifying as PASS,
// because a false FAIL accuses the author of tampering.
func TestVerifyAcceptsV020StatsForSubChain(t *testing.T) {
	k := testKeys(t)
	snaps := fakeWritingSim(t, k, 5)
	r, err := BuildReceipt(snaps, k, snaps[3].SavedAt, "2026-06-02")
	if err != nil {
		t.Fatal(err)
	}
	legacy := *r
	legacy.Stats.TotalBytesAdded += legacy.Snapshots[0].Size // the v0.2.0 rule
	resign(t, &legacy, k)
	if res := VerifyReceipt(&legacy); !res.OK {
		t.Fatalf("a v0.2.0-accounted sub-chain receipt must still verify: %+v", res)
	}
	// An arbitrary third total is still rejected.
	bad := *r
	bad.Stats.TotalBytesAdded += 12345
	resign(t, &bad, k)
	if VerifyReceipt(&bad).OK {
		t.Fatal("a stats total matching no known accounting rule must fail")
	}
}
