package chain

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestKeys(t *testing.T) *AuthorKeys {
	t.Helper()
	k, created, err := LoadOrCreateKeys(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected fresh key creation")
	}
	return k
}

func snap(seq uint64, at time.Time, prev, content string) Snapshot {
	return Snapshot{
		DocID:       DocID("thesis.md"),
		Path:        "thesis.md",
		Seq:         seq,
		SavedAt:     at,
		MonoNanos:   int64(seq) * int64(time.Second),
		PrevHash:    prev,
		ContentHash: content,
		Size:        100 * int64(seq),
	}
}

func buildChain(t *testing.T, k *AuthorKeys, n int) []Snapshot {
	t.Helper()
	base := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	var out []Snapshot
	var prevHash string
	for i := 1; i <= n; i++ {
		s := snap(uint64(i), base.Add(time.Duration(i)*time.Hour), prevHash, HashBytes([]byte{byte(i)}))
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

func TestKeysRoundtripAndFingerprint(t *testing.T) {
	dir := t.TempDir()
	k1, _, err := LoadOrCreateKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	k2, created, err := LoadOrCreateKeys(dir)
	if err != nil || created {
		t.Fatalf("reload: err=%v created=%v", err, created)
	}
	if Fingerprint(k1.Public) != Fingerprint(k2.Public) {
		t.Fatal("fingerprint changed across reload")
	}
	if len(Fingerprint(k1.Public)) != 64 {
		t.Fatalf("fingerprint should be 64 hex chars, got %d", len(Fingerprint(k1.Public)))
	}
	card := KeyCard(k1.Public)
	if !strings.Contains(card, "sha256:"+Fingerprint(k1.Public)) {
		t.Fatal("key card missing fingerprint")
	}
	if !strings.Contains(card, "导师") {
		t.Fatal("key card must tell the author to pre-register with an advisor")
	}
}

func TestSignVerifyTamperEveryField(t *testing.T) {
	k := newTestKeys(t)
	s := snap(1, time.Now().UTC(), GenesisPrevHash, "aa")
	if err := k.SignSnapshot(&s); err != nil {
		t.Fatal(err)
	}
	if err := VerifySnapshot(s, k.Public); err != nil {
		t.Fatalf("clean snapshot should verify: %v", err)
	}
	tampered := []func(Snapshot) Snapshot{
		func(x Snapshot) Snapshot { x.ContentHash = strings.Repeat("0", 64); return x },
		func(x Snapshot) Snapshot { x.SavedAt = x.SavedAt.Add(time.Hour); return x },
		func(x Snapshot) Snapshot { x.Seq++; return x },
		func(x Snapshot) Snapshot { x.Size += 1; return x },
		func(x Snapshot) Snapshot { x.Path = "other.md"; return x },
		func(x Snapshot) Snapshot { x.PrevHash = strings.Repeat("f", 64); return x },
		func(x Snapshot) Snapshot { x.MonoNanos += 1; return x },
	}
	for i, f := range tampered {
		if err := VerifySnapshot(f(s), k.Public); err == nil {
			t.Fatalf("tamper case %d should fail verification", i)
		}
	}
	// wrong key
	other := newTestKeys(t)
	if err := VerifySnapshot(s, other.Public); err == nil {
		t.Fatal("signature from another key must not verify")
	}
}

func TestValidateChainCleanAndTampered(t *testing.T) {
	k := newTestKeys(t)
	chainSnaps := buildChain(t, k, 4)

	if problems := ValidateChain(chainSnaps, k.Public, true); len(problems) != 0 {
		t.Fatalf("clean chain has problems: %v", problems)
	}

	// Edit the middle record's content hash: its own sig breaks and the
	// next link breaks.
	bad := make([]Snapshot, len(chainSnaps))
	copy(bad, chainSnaps)
	bad[1].ContentHash = strings.Repeat("0", 64)
	problems := ValidateChain(bad, k.Public, true)
	if len(problems) < 2 {
		t.Fatalf("expected sig break at #2 and chain break at #3, got %v", problems)
	}
	if problems[0].Index != 1 {
		t.Fatalf("first problem should point at index 1, got %d", problems[0].Index)
	}
	if !strings.Contains(problems[0].Reason, "签名无效") {
		t.Fatalf("problem 0 should be a signature failure: %v", problems[0])
	}

	// Delete the middle record: seq gap at #3 (now index 1).
	del := append([]Snapshot{}, chainSnaps[0], chainSnaps[2], chainSnaps[3])
	problems = ValidateChain(del, k.Public, true)
	if len(problems) == 0 || problems[0].Index != 1 {
		t.Fatalf("deletion should break at index 1, got %v", problems)
	}
	if !strings.Contains(problems[0].Reason, "seq 不连续") {
		t.Fatalf("deletion should surface as seq gap, got %v", problems[0])
	}

	// Reorder: seq 2 and 3 swapped.
	rot := append([]Snapshot{}, chainSnaps[0], chainSnaps[2], chainSnaps[1], chainSnaps[3])
	if problems := ValidateChain(rot, k.Public, true); len(problems) == 0 {
		t.Fatal("reordering must be detected")
	}

	// Truncated sub-chain (as produced by --since): valid when fullChain=false.
	sub := chainSnaps[2:]
	if problems := ValidateChain(sub, k.Public, false); len(problems) != 0 {
		t.Fatalf("contiguous sub-chain should verify: %v", problems)
	}
	// The same sub-chain presented as a full chain must fail.
	if problems := ValidateChain(sub, k.Public, true); len(problems) == 0 {
		t.Fatal("sub-chain presented as full chain must fail genesis check")
	}
}

func TestClockRollbackWarning(t *testing.T) {
	k := newTestKeys(t)
	base := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	// Wall clock jumps back 2h while the monotonic counter advances.
	s1 := snap(1, base, GenesisPrevHash, "a")
	s2 := snap(2, base.Add(2*time.Hour), "", "b")
	s2.SavedAt = base.Add(-2 * time.Hour) // rolled back
	for _, s := range []Snapshot{s1, s2} {
		if err := k.SignSnapshot(&s); err != nil {
			t.Fatal(err)
		}
	}
	warns := ClockRollbacks([]Snapshot{s1, s2})
	if len(warns) != 1 || warns[0].Index != 1 {
		t.Fatalf("expected one rollback warning at index 1, got %v", warns)
	}
	if warns := ClockRollbacks([]Snapshot{s1}); len(warns) != 0 {
		t.Fatalf("single snapshot cannot roll back, got %v", warns)
	}
}

func TestDocIDStable(t *testing.T) {
	if DocID("chapters/intro.md") != DocID("chapters/intro.md") {
		t.Fatal("doc id must be stable")
	}
	if DocID("chapters/intro.md") == DocID("chapters/intro.tex") {
		t.Fatal("different paths must produce different doc ids")
	}
	// Native path separators must normalize to the same slash form.
	if DocID("a/b.md") != DocID(filepath.Join("a", "b.md")) {
		t.Fatal("doc id must be OS-path independent")
	}
}
