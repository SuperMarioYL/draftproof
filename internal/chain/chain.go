// Package chain implements DraftProof's core primitive: the signed snapshot
// hash chain. Every editor save becomes a Snapshot whose fields are signed
// with the author's ed25519 key and chained to the previous snapshot by
// sha256, so any later insert, delete, reorder or edit of a stored record
// breaks the chain at a verifiable breakpoint.
//
// Trust model (kept honest): timestamps are local wall-clock plus a
// process-monotonic counter. This cannot stop an author who regenerates the
// key and rebuilds the whole chain; the documented mitigation is that init
// exports a key card whose fingerprint the author is told to pre-register
// with an advisor before writing.
package chain

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SchemaVersion is the snapshot/receipt format version emitted by v0.1.
const SchemaVersion = 1

// GenesisPrevHash marks the first snapshot of a document chain.
const GenesisPrevHash = ""

// Snapshot is one signed save event. Field order is canonical encoding
// order — never reorder fields, signatures cover the JSON encoding of
// everything except Sig.
type Snapshot struct {
	DocID       string    `json:"doc_id"`
	Path        string    `json:"path"`
	Seq         uint64    `json:"seq"`
	SavedAt     time.Time `json:"saved_at"`     // wall clock, RFC3339
	MonoNanos   int64     `json:"mono_nanos"`   // ns since the capturing watcher started
	PrevHash    string    `json:"prev_hash"`    // sha256 of the previous snapshot record (hex), "" for genesis
	ContentHash string    `json:"content_hash"` // sha256 of the file bytes (hex); content is never stored
	Size        int64     `json:"size"`
	AppHint     string    `json:"app_hint,omitempty"` // reserved: which app saved; v0.1 records none
	Sig         string    `json:"sig"`                // ed25519 over signedPayload, hex
}

// signedSnapshot mirrors Snapshot without Sig; it defines the bytes covered
// by the snapshot signature.
type signedSnapshot struct {
	DocID       string    `json:"doc_id"`
	Path        string    `json:"path"`
	Seq         uint64    `json:"seq"`
	SavedAt     time.Time `json:"saved_at"`
	MonoNanos   int64     `json:"mono_nanos"`
	PrevHash    string    `json:"prev_hash"`
	ContentHash string    `json:"content_hash"`
	Size        int64     `json:"size"`
	AppHint     string    `json:"app_hint,omitempty"`
}

func (s Snapshot) signedPayload() ([]byte, error) {
	return json.Marshal(signedSnapshot{
		DocID: s.DocID, Path: s.Path, Seq: s.Seq, SavedAt: s.SavedAt,
		MonoNanos: s.MonoNanos, PrevHash: s.PrevHash, ContentHash: s.ContentHash,
		Size: s.Size, AppHint: s.AppHint,
	})
}

// RecordHash is the chain hash of a snapshot record. It covers the full
// record including the signature, so tampering with any byte of a stored
// record breaks the next snapshot's prev_hash link.
func (s Snapshot) RecordHash() (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return HashBytes(b), nil
}

// HashBytes returns the hex sha256 of b.
func HashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// DocID derives the stable document identity from the path relative to the
// watched directory (slash-separated). Renaming a file starts a new chain.
func DocID(relPath string) string {
	sum := sha256.Sum256([]byte(filepath.ToSlash(relPath)))
	return hex.EncodeToString(sum[:16])
}

// ---------------------------------------------------------------------------
// Author keys

// AuthorKeys is the single-author ed25519 key pair (v0.1 is one machine,
// one author). Keys live in the user's home .draftproof directory, never
// inside the watched folder.
type AuthorKeys struct {
	Private ed25519.PrivateKey
	Public  ed25519.PublicKey
}

// DefaultKeyDir returns the key directory (~/.draftproof).
func DefaultKeyDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".draftproof"), nil
}

// LoadKeys reads an existing key pair from dir.
func LoadKeys(dir string) (*AuthorKeys, error) {
	privB, err := os.ReadFile(filepath.Join(dir, "author.key"))
	if err != nil {
		return nil, err
	}
	priv, err := decodeKey(privB)
	if err != nil {
		return nil, fmt.Errorf("author.key 无效: %w", err)
	}
	return &AuthorKeys{Private: priv, Public: priv.Public().(ed25519.PublicKey)}, nil
}

// LoadOrCreateKeys returns the key pair in dir, generating and persisting a
// new one (plus a printable key card) when absent. The bool reports whether
// a new key was created.
func LoadOrCreateKeys(dir string) (*AuthorKeys, bool, error) {
	if k, err := LoadKeys(dir); err == nil {
		return k, false, nil
	}
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, false, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, false, err
	}
	if err := os.WriteFile(filepath.Join(dir, "author.key"), []byte(encodeKey(priv)), 0o600); err != nil {
		return nil, false, err
	}
	if err := os.WriteFile(filepath.Join(dir, "author.pub"), []byte(encodeKey(pub)), 0o644); err != nil {
		return nil, false, err
	}
	return &AuthorKeys{Private: priv, Public: pub}, true, nil
}

func encodeKey(k []byte) string { return hex.EncodeToString(k) + "\n" }

func decodeKey(b []byte) (ed25519.PrivateKey, error) {
	s := strings.TrimSpace(string(b))
	raw, err := hex.DecodeString(s)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PrivateKeySize && len(raw) != ed25519.SeedSize {
		return nil, errors.New("key length mismatch")
	}
	if len(raw) == ed25519.SeedSize {
		return ed25519.NewKeyFromSeed(raw), nil
	}
	return ed25519.PrivateKey(raw), nil
}

// Fingerprint is the key-card fingerprint: sha256 over the public key bytes.
func Fingerprint(pub ed25519.PublicKey) string {
	return HashBytes(pub)
}

// KeyCard renders the printable key card. The whole trust model of the
// pre-registration mitigation lives in this card being sent to an advisor
// early — before any suspicion exists.
func KeyCard(pub ed25519.PublicKey) string {
	fp := Fingerprint(pub)
	return "DraftProof 作者密钥卡（Author Key Card）\n" +
		"--------------------------------------\n" +
		"公钥指纹: sha256:" + fp + "\n" +
		"生成时间: " + time.Now().Format("2006-01-02 15:04:05 MST") + "\n" +
		"\n" +
		"请尽早（最好在开始写作当天）把本卡拍照或转发给\n" +
		"导师 / 同学留档（邮件、微信均可）。\n" +
		"\n" +
		"事后用新钥匙重造的整条回执链，无法匹配这张卡上\n" +
		"事前登记的指纹——这是本地存证对抗伪造的关键。\n" +
		"密钥保存在本机 ~/.draftproof/author.key，草稿与\n" +
		"密钥都不出本机。\n"
}

// ---------------------------------------------------------------------------
// Signing and per-snapshot verification

// SignSnapshot fills s.Sig with an ed25519 signature over all other fields.
func (k *AuthorKeys) SignSnapshot(s *Snapshot) error {
	payload, err := s.signedPayload()
	if err != nil {
		return err
	}
	sig := ed25519.Sign(k.Private, payload)
	s.Sig = hex.EncodeToString(sig)
	return nil
}

// VerifySnapshot checks s.Sig against pub.
func VerifySnapshot(s Snapshot, pub ed25519.PublicKey) error {
	sig, err := hex.DecodeString(s.Sig)
	if err != nil {
		return fmt.Errorf("签名不是合法 hex: %w", err)
	}
	payload, err := s.signedPayload()
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, payload, sig) {
		return errors.New("ed25519 签名无效（记录被修改，或非作者密钥）")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Chain validation

// Problem describes one detected tamper point (or structural break) with its
// location in the checked snapshot slice.
type Problem struct {
	Index  int    `json:"index"` // 0-based position in the checked slice
	Seq    uint64 `json:"seq"`
	Reason string `json:"reason"`
}

func (p Problem) String() string {
	return fmt.Sprintf("快照 #%d（seq %d）: %s", p.Index+1, p.Seq, p.Reason)
}

// ValidateChain checks a contiguous run of snapshots of one document:
//   - every snapshot signature verifies against pub;
//   - seq numbers are contiguous and increasing (deletion/reorder breaks);
//   - each prev_hash equals the previous record's hash (edit breaks);
//   - when fullChain is true (receipt exported without --since), the first
//     snapshot must be the genesis (prev_hash == "").
//
// The first snapshot's own prev_hash is otherwise unchecked because exports
// may legitimately start mid-chain (--since selects a contiguous suffix).
func ValidateChain(snapshots []Snapshot, pub ed25519.PublicKey, fullChain bool) []Problem {
	var problems []Problem
	for i, s := range snapshots {
		if err := VerifySnapshot(s, pub); err != nil {
			problems = append(problems, Problem{Index: i, Seq: s.Seq, Reason: err.Error()})
		}
		if i == 0 {
			if fullChain && s.PrevHash != GenesisPrevHash {
				problems = append(problems, Problem{Index: 0, Seq: s.Seq,
					Reason: "完整链回执的首个快照 prev_hash 应为空（疑似截取或拼接）"})
			}
			continue
		}
		prev := snapshots[i-1]
		if s.Seq != prev.Seq+1 {
			problems = append(problems, Problem{Index: i, Seq: s.Seq,
				Reason: fmt.Sprintf("seq 不连续（上一快照 seq %d）——中间被删除或重排", prev.Seq)})
		}
		want, err := prev.RecordHash()
		if err != nil || s.PrevHash != want {
			problems = append(problems, Problem{Index: i, Seq: s.Seq,
				Reason: "prev_hash 与上一快照的记录哈希不匹配——上一条记录被改动"})
		}
	}
	return problems
}

// ClockRollbacks reports positions where the wall clock moved backwards
// within one monotonic run (same watcher session): mono advanced but
// saved_at went back. These are warnings, not failures — they make clock
// tampering visible rather than claiming it cannot happen.
func ClockRollbacks(snapshots []Snapshot) []Problem {
	var warns []Problem
	for i := 1; i < len(snapshots); i++ {
		prev, cur := snapshots[i-1], snapshots[i]
		if cur.MonoNanos >= prev.MonoNanos && cur.SavedAt.Before(prev.SavedAt) {
			warns = append(warns, Problem{Index: i, Seq: cur.Seq,
				Reason: fmt.Sprintf("墙钟回拨：saved_at 从 %s 倒退到 %s，而单调钟前进",
					prev.SavedAt.Format(time.RFC3339), cur.SavedAt.Format(time.RFC3339))})
		}
	}
	return warns
}
