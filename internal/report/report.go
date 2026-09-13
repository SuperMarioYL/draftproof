// Package report builds and verifies ProvenanceReceipts — the self-contained
// .dpb bundle a flagged author hands to a degree office — and renders the
// printable one-page HTML timeline.
//
// A receipt contains no draft content, only per-save metadata: content
// hashes, timestamps, sizes and the signed hash chain. The author signs the
// whole envelope, so the reviewer side can check with one offline command
// that nothing in the package was altered after export.
package report

import (
	"bytes"
	"crypto/ed25519"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"io"
	"os"
	"reflect"
	"strconv"
	"time"

	"github.com/SuperMarioYL/draftproof/internal/chain"
)

// ReceiptSchema identifies the .dpb envelope format.
const ReceiptSchema = "draftproof/receipt@1"

// SessionGap is the idle threshold that splits snapshots into writing
// sessions (a monotonic-clock reset always splits too — it marks a new
// watcher run).
const SessionGap = 30 * time.Minute

// Receipt is the .dpb envelope.
type Receipt struct {
	Schema     string           `json:"schema"`
	Doc        DocInfo          `json:"doc"`
	Author     AuthorInfo       `json:"author"`
	ExportedAt time.Time        `json:"exported_at"`
	Since      string           `json:"since,omitempty"` // set when exported with --since (sub-chain)
	Snapshots  []chain.Snapshot `json:"snapshots"`
	Sessions   []Session        `json:"sessions"`
	Stats      Stats            `json:"stats"`
	ReceiptSig string           `json:"receipt_sig"` // ed25519 over everything above
}

// DocInfo summarizes the documented file.
type DocInfo struct {
	ID            string    `json:"id"`
	Path          string    `json:"path"`
	FirstSavedAt  time.Time `json:"first_saved_at"`
	LastSavedAt   time.Time `json:"last_saved_at"`
	SnapshotCount int       `json:"snapshot_count"`
}

// AuthorInfo carries the author's public key and its fingerprint — the value
// that must match the key card pre-registered with an advisor.
type AuthorInfo struct {
	PublicKey   string `json:"public_key"`  // hex ed25519 public key
	Fingerprint string `json:"fingerprint"` // sha256 of the public key, hex
}

// Session is one contiguous writing sitting.
type Session struct {
	Index      int       `json:"index"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	Snapshots  int       `json:"snapshots"`
	BytesAdded int64     `json:"bytes_added"`
}

// Stats summarizes the writing process shown in the receipt.
type Stats struct {
	SnapshotCount   int       `json:"snapshot_count"`
	FirstSavedAt    time.Time `json:"first_saved_at"`
	LastSavedAt     time.Time `json:"last_saved_at"`
	SpanHours       float64   `json:"span_hours"`
	SessionCount    int       `json:"session_count"`
	TotalBytesAdded int64     `json:"total_bytes_added"`
	MaxGapHours     float64   `json:"max_gap_hours"`
}

// signedReceipt mirrors Receipt without the signature; it defines the bytes
// covered by the receipt signature.
type signedReceipt struct {
	Schema     string           `json:"schema"`
	Doc        DocInfo          `json:"doc"`
	Author     AuthorInfo       `json:"author"`
	ExportedAt time.Time        `json:"exported_at"`
	Since      string           `json:"since,omitempty"`
	Snapshots  []chain.Snapshot `json:"snapshots"`
	Sessions   []Session        `json:"sessions"`
	Stats      Stats            `json:"stats"`
}

func (r *Receipt) signedPayload() ([]byte, error) {
	return json.Marshal(signedReceipt{
		Schema: r.Schema, Doc: r.Doc, Author: r.Author, ExportedAt: r.ExportedAt,
		Since: r.Since, Snapshots: r.Snapshots, Sessions: r.Sessions, Stats: r.Stats,
	})
}

// AnalyzeSessions splits snapshots into writing sessions: a new session
// starts on a monotonic-clock reset (new watcher run) or an idle gap longer
// than SessionGap.
func AnalyzeSessions(snapshots []chain.Snapshot) []Session {
	var sessions []Session
	for i, s := range snapshots {
		newSession := i == 0
		if i > 0 {
			prev := snapshots[i-1]
			monoReset := s.MonoNanos < prev.MonoNanos
			idle := s.SavedAt.Sub(prev.SavedAt) > SessionGap
			newSession = monoReset || idle
		}
		if newSession {
			sessions = append(sessions, Session{Index: len(sessions) + 1, Start: s.SavedAt, End: s.SavedAt})
		}
		cur := &sessions[len(sessions)-1]
		cur.End = s.SavedAt
		cur.Snapshots++
		if i == 0 {
			cur.BytesAdded += s.Size
		} else {
			if d := s.Size - snapshots[i-1].Size; d > 0 {
				cur.BytesAdded += d
			}
		}
	}
	return sessions
}

// ComputeStats derives the summary numbers from snapshots and sessions.
func ComputeStats(snapshots []chain.Snapshot, sessions []Session) Stats {
	var st Stats
	if len(snapshots) == 0 {
		return st
	}
	st.SnapshotCount = len(snapshots)
	st.FirstSavedAt = snapshots[0].SavedAt
	st.LastSavedAt = snapshots[len(snapshots)-1].SavedAt
	st.SpanHours = st.LastSavedAt.Sub(st.FirstSavedAt).Hours()
	st.SessionCount = len(sessions)
	for _, sess := range sessions {
		st.TotalBytesAdded += sess.BytesAdded
	}
	for i := 1; i < len(snapshots); i++ {
		if gap := snapshots[i].SavedAt.Sub(snapshots[i-1].SavedAt).Hours(); gap > st.MaxGapHours {
			st.MaxGapHours = gap
		}
	}
	return st
}

// BuildReceipt assembles and signs a receipt for one document's snapshots.
// sinceFilter, when non-zero, keeps the contiguous run of snapshots saved at
// or after that moment (the cut always happens at a chain position, so the
// exported sub-chain stays verifiable).
func BuildReceipt(snapshots []chain.Snapshot, keys *chain.AuthorKeys, sinceFilter time.Time, sinceLabel string) (*Receipt, error) {
	if len(snapshots) == 0 {
		return nil, errors.New("没有可导出的快照")
	}
	selected := snapshots
	if !sinceFilter.IsZero() {
		cut := 0
		for i, s := range snapshots {
			if !s.SavedAt.Before(sinceFilter) {
				cut = i
				break
			}
			cut = i + 1
		}
		selected = snapshots[cut:]
		if len(selected) == 0 {
			return nil, fmt.Errorf("--since %s 之后没有快照", sinceLabel)
		}
	}
	docID := selected[0].DocID
	for _, s := range selected {
		if s.DocID != docID {
			return nil, errors.New("一次只导出一个文档的链（内部错误：混入多个 doc_id）")
		}
	}
	// Guard: the receipt must be signed by the same key that signed the
	// snapshots; a re-generated key would produce a receipt that can never
	// verify.
	for _, s := range selected {
		if err := chain.VerifySnapshot(s, keys.Public); err != nil {
			return nil, fmt.Errorf("快照 seq %d 不是由当前密钥签名（%w）——请用原密钥导出", s.Seq, err)
		}
	}
	sessions := AnalyzeSessions(selected)
	r := &Receipt{
		Schema:     ReceiptSchema,
		Doc:        DocInfo{ID: docID, Path: selected[0].Path, FirstSavedAt: selected[0].SavedAt, LastSavedAt: selected[len(selected)-1].SavedAt, SnapshotCount: len(selected)},
		Author:     AuthorInfo{PublicKey: hex.EncodeToString(keys.Public), Fingerprint: chain.Fingerprint(keys.Public)},
		ExportedAt: time.Now(),
		Since:      sinceLabel,
		Snapshots:  selected,
		Sessions:   sessions,
		Stats:      ComputeStats(selected, sessions),
	}
	payload, err := r.signedPayload()
	if err != nil {
		return nil, err
	}
	r.ReceiptSig = hex.EncodeToString(ed25519.Sign(keys.Private, payload))
	return r, nil
}

// Result is the outcome of verifying a receipt offline.
type Result struct {
	OK               bool
	SchemaOK         bool
	FingerprintOK    bool
	ReceiptSigOK     bool
	StatsConsistent  bool
	FullChain        bool
	SnapshotCount    int
	ChainLinks       int
	ChainProblems    []chain.Problem
	EnvelopeProblems []string
	Warnings         []chain.Problem
}

// VerifyReceipt re-checks everything a reviewer can check offline: schema,
// key fingerprint, the envelope signature, every snapshot signature, the
// hash-chain links, seq contiguity, and that the claimed session/stat
// numbers match what the snapshots themselves say.
func VerifyReceipt(r *Receipt) Result {
	res := Result{OK: false, SnapshotCount: len(r.Snapshots), ChainLinks: len(r.Snapshots) - 1, FullChain: r.Since == ""}

	res.SchemaOK = r.Schema == ReceiptSchema
	if !res.SchemaOK {
		res.EnvelopeProblems = append(res.EnvelopeProblems,
			fmt.Sprintf("schema %q 不是 %q——版本不匹配或文件损坏", r.Schema, ReceiptSchema))
	}

	pub, err := hex.DecodeString(r.Author.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		res.EnvelopeProblems = append(res.EnvelopeProblems, "作者公钥字段损坏（非合法 ed25519 公钥）")
		return res
	}
	pubKey := ed25519.PublicKey(pub)
	res.FingerprintOK = chain.Fingerprint(pubKey) == r.Author.Fingerprint
	if !res.FingerprintOK {
		res.EnvelopeProblems = append(res.EnvelopeProblems,
			"公钥指纹与公钥不匹配——字段被改动，或生成工具异常")
	}

	sig, err := hex.DecodeString(r.ReceiptSig)
	if err != nil {
		res.EnvelopeProblems = append(res.EnvelopeProblems, "回执签名不是合法 hex")
	} else {
		payload, err := r.signedPayload()
		if err == nil && !ed25519.Verify(pubKey, payload, sig) {
			res.EnvelopeProblems = append(res.EnvelopeProblems,
				"回执级 ed25519 签名无效——导出后有人改动过包内内容")
		} else if err == nil {
			res.ReceiptSigOK = true
		}
	}

	res.ChainProblems = chain.ValidateChain(r.Snapshots, pubKey, res.FullChain)
	res.Warnings = chain.ClockRollbacks(r.Snapshots)

	// The envelope signature already covers stats, but recomputing them from
	// the snapshots also catches malformed receipts produced by buggy tools.
	recomputed := ComputeStats(r.Snapshots, AnalyzeSessions(r.Snapshots))
	res.StatsConsistent = reflect.DeepEqual(recomputed, r.Stats)
	if !res.StatsConsistent {
		res.EnvelopeProblems = append(res.EnvelopeProblems,
			"会话/统计数字与快照本身不符——统计字段被改动")
	}

	res.OK = res.SchemaOK && res.FingerprintOK && res.ReceiptSigOK &&
		res.StatsConsistent && len(res.ChainProblems) == 0 && len(res.EnvelopeProblems) == 0
	return res
}

// LoadReceipt parses a .dpb file.
func LoadReceipt(path string) (*Receipt, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Receipt
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("解析回执失败（不是合法的 .dpb 文件）: %w", err)
	}
	return &r, nil
}

// WriteFile pretty-prints the receipt as JSON. The signature covers field
// values, not the file layout, so re-formatting whitespace changes nothing.
func (r *Receipt) WriteFile(path string) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// ---------------------------------------------------------------------------
// Printable HTML receipt

//go:embed receipt.html
var receiptHTML string

// htmlView is the template model; everything display-related is precomputed
// here so the template stays declarative.
type htmlView struct {
	Receipt          *Receipt
	Schema           string
	FingerprintShort string
	DocIDShort       string
	IsFullChain      bool
	Sessions         []sessionRow
	Chart            incrementChart
	Snapshots        []snapshotRow
	GeneratedAt      string
	ToolVersion      string
	SpanText         string
	TotalAddedText   string
	MaxGapText       string
}

type sessionRow struct {
	Index      int
	Start, End string
	Duration   string
	Snapshots  int
	BytesAdded string
}

type snapshotRow struct {
	Seq         uint64
	SavedAt     string
	Size        string
	Delta       string
	ContentHash string
	PrevHash    string
}

var htmlFuncs = htmltemplate.FuncMap{
	"fmtBytes": FmtBytes,
}

// FmtBytes renders byte counts in a compact human form.
func FmtBytes(n int64) string {
	switch {
	case n >= 1024*1024:
		return strconv.FormatFloat(float64(n)/(1024*1024), 'f', 1, 64) + " MB"
	case n >= 1024:
		return strconv.FormatFloat(float64(n)/1024, 'f', 1, 64) + " KB"
	default:
		return strconv.FormatInt(n, 10) + " B"
	}
}

func fmtDuration(d time.Duration) string {
	if d < time.Minute {
		return strconv.Itoa(int(d.Seconds())) + "s"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dh%dm", h, m)
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12] + "…"
	}
	return h
}

// RenderHTML writes the one-page printable receipt.
func RenderHTML(w io.Writer, r *Receipt, toolVersion string) error {
	tmpl, err := htmltemplate.New("receipt").Funcs(htmlFuncs).Parse(receiptHTML)
	if err != nil {
		return err
	}
	v := htmlView{
		Receipt:          r,
		Schema:           ReceiptSchema,
		FingerprintShort: shortHash(r.Author.Fingerprint),
		DocIDShort:       shortHash(r.Doc.ID),
		IsFullChain:      r.Since == "",
		GeneratedAt:      r.ExportedAt.Format("2006-01-02 15:04:05 MST"),
		ToolVersion:      toolVersion,
		SpanText:         fmtDuration(time.Duration(r.Stats.SpanHours * float64(time.Hour))),
		TotalAddedText:   FmtBytes(r.Stats.TotalBytesAdded),
		MaxGapText:       fmtDuration(time.Duration(r.Stats.MaxGapHours * float64(time.Hour))),
	}
	for _, sess := range r.Sessions {
		v.Sessions = append(v.Sessions, sessionRow{
			Index: sess.Index, Start: sess.Start.Format("01-02 15:04"), End: sess.End.Format("01-02 15:04"),
			Duration: fmtDuration(sess.End.Sub(sess.Start)), Snapshots: sess.Snapshots, BytesAdded: FmtBytes(sess.BytesAdded),
		})
	}
	prevSize := int64(0)
	for i, s := range r.Snapshots {
		delta := s.Size
		if i > 0 {
			delta = s.Size - prevSize
		}
		prevDisplay := shortHash(s.PrevHash)
		if s.PrevHash == chain.GenesisPrevHash {
			prevDisplay = "—（链首）"
		}
		v.Snapshots = append(v.Snapshots, snapshotRow{
			Seq: s.Seq, SavedAt: s.SavedAt.Format("01-02 15:04:05"),
			Size: FmtBytes(s.Size), Delta: signedBytes(delta),
			ContentHash: shortHash(s.ContentHash), PrevHash: prevDisplay,
		})
		prevSize = s.Size
	}
	v.Chart = buildChart(r.Snapshots)
	return tmpl.Execute(w, v)
}

func signedBytes(n int64) string {
	if n > 0 {
		return "+" + FmtBytes(n)
	}
	if n < 0 {
		return "-" + FmtBytes(-n)
	}
	return "±0"
}

// incrementChart is the per-snapshot byte-delta chart drawn as inline SVG.
type incrementChart struct {
	Width, Height int
	Bars          []chartBar
	MaxDelta      int64
}

type chartBar struct {
	X, Y, W, H float64
	Label      string
	Positive   bool
}

const chartW, chartH = 700, 150

func buildChart(snapshots []chain.Snapshot) incrementChart {
	c := incrementChart{Width: chartW, Height: chartH}
	n := len(snapshots)
	if n == 0 {
		return c
	}
	pad, base := 34.0, float64(chartH-28)
	slot := (float64(chartW) - 2*pad) / float64(n)
	maxDelta := int64(1)
	for i, s := range snapshots {
		d := s.Size
		if i > 0 {
			d = s.Size - snapshots[i-1].Size
		}
		if d > maxDelta {
			maxDelta = d
		}
	}
	c.MaxDelta = maxDelta
	for i, s := range snapshots {
		d := s.Size
		if i > 0 {
			d = s.Size - snapshots[i-1].Size
		}
		h := (float64(d) / float64(maxDelta)) * (base - 14)
		if h < 1 && d >= 0 {
			h = 1
		}
		x := pad + float64(i)*slot + slot*0.15
		w := slot * 0.7
		if w > 14 {
			w = 14
		}
		if d < 0 {
			h = 2
		}
		c.Bars = append(c.Bars, chartBar{
			X: x, W: w, Y: base - h, H: h,
			Label:    snapshots[i].SavedAt.Format("01-02 15:04"),
			Positive: d >= 0,
		})
	}
	return c
}

// RenderHTMLString is a convenience wrapper for tests and previews.
func RenderHTMLString(r *Receipt, toolVersion string) (string, error) {
	var buf bytes.Buffer
	if err := RenderHTML(&buf, r, toolVersion); err != nil {
		return "", err
	}
	return buf.String(), nil
}
