package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"

	"github.com/SuperMarioYL/draftproof/internal/chain"
	"github.com/SuperMarioYL/draftproof/internal/store"
)

// DefaultDebounce coalesces the burst of events one editor save produces.
// The plan fixes this at >= 5s so rapid autosaves land as one snapshot.
const DefaultDebounce = 5 * time.Second

// watchExts are the tracked draft formats. Files are hashed byte-wise, so
// the format does not matter to the chain — only to what gets watched.
var watchExts = map[string]bool{".docx": true, ".md": true, ".tex": true}

func eligible(name string) bool {
	n := strings.ToLower(name)
	if strings.HasPrefix(n, "~$") { // Word/WPS lock files
		return false
	}
	if strings.HasSuffix(n, ".tmp") || strings.HasSuffix(n, ".swp") {
		return false
	}
	return watchExts[filepath.Ext(n)]
}

// watcher captures qualifying saves under one directory into the store.
// All captures run on a single goroutine fed by captureCh, so chain-head
// state needs no locking; only the debounce timer table is shared with the
// fsnotify loop and is guarded by mu.
type watcher struct {
	dir          string
	st           *store.Store
	keys         *chain.AuthorKeys
	heads        map[string]store.DocState
	debounce     time.Duration
	processStart time.Time

	captureCh chan string
	mu        sync.Mutex
	timers    map[string]*time.Timer
	captured  int64
}

func newWatcher(dir string, st *store.Store, keys *chain.AuthorKeys, debounce time.Duration) (*watcher, error) {
	all, err := st.LoadAll()
	if err != nil {
		return nil, err
	}
	heads, err := store.Heads(all)
	if err != nil {
		return nil, err
	}
	return &watcher{
		dir: dir, st: st, keys: keys, heads: heads, debounce: debounce,
		processStart: time.Now(),
		captureCh:    make(chan string, 64),
		timers:       make(map[string]*time.Timer),
	}, nil
}

// schedule (re)arms the per-path debounce timer.
func (w *watcher) schedule(absPath string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if t, ok := w.timers[absPath]; ok {
		t.Stop()
	}
	w.timers[absPath] = time.AfterFunc(w.debounce, func() {
		w.captureCh <- absPath
	})
}

// capture reads the settled file and appends a signed snapshot, or skips it
// (content unchanged since the last snapshot of this document).
func (w *watcher) capture(absPath string) error {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil // editor removed the file between event and capture
	}
	rel, err := filepath.Rel(w.dir, absPath)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	docID := chain.DocID(rel)
	contentHash := chain.HashBytes(data)
	head := w.heads[docID]
	if head.LastContentHash == contentHash {
		return nil // same bytes as the last snapshot: dedupe
	}
	snap := chain.Snapshot{
		DocID:       docID,
		Path:        rel,
		Seq:         head.LastSeq + 1,
		SavedAt:     time.Now(),
		MonoNanos:   int64(time.Since(w.processStart)),
		PrevHash:    head.LastRecordHash,
		ContentHash: contentHash,
		Size:        int64(len(data)),
	}
	if err := w.keys.SignSnapshot(&snap); err != nil {
		return err
	}
	if err := w.st.Append(snap); err != nil {
		return fmt.Errorf("快照写入失败: %w", err)
	}
	recHash, err := snap.RecordHash()
	if err != nil {
		return err
	}
	w.heads[docID] = store.DocState{
		Path: rel, LastSeq: snap.Seq, LastContentHash: contentHash, LastRecordHash: recHash,
	}
	atomic.AddInt64(&w.captured, 1)
	fmt.Fprintf(os.Stdout, "[draftproof] 捕获 %s #%d  %dB  sha256:%s…\n",
		rel, snap.Seq, snap.Size, snap.ContentHash[:12])
	return nil
}

// runWatch drives the fsnotify loop and the capture loop until ctx ends.
func (w *watcher) runWatch(ctx context.Context, out io.Writer) error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer fw.Close()

	addTree := func(root string) error {
		dirs, err := store.WalkWatchable(root)
		if err != nil {
			return err
		}
		for _, d := range dirs {
			if err := fw.Add(d); err != nil {
				return err
			}
		}
		return nil
	}
	if err := addTree(w.dir); err != nil {
		return err
	}

	captureDone := make(chan struct{})
	go func() {
		defer close(captureDone)
		for path := range w.captureCh {
			if err := w.capture(path); err != nil {
				fmt.Fprintf(os.Stderr, "[draftproof] %v\n", err)
			}
		}
	}()
	defer func() {
		close(w.captureCh)
		<-captureDone
	}()

	fmt.Fprintf(out, "[draftproof] watch %s — 追踪 .docx/.md/.tex（任何编辑器的保存都会被捕获）\n", w.dir)
	fmt.Fprintf(out, "[draftproof] 存证库 %s · 防抖 %s · 作者指纹 sha256:%s…\n",
		w.st.Dir(), w.debounce, chain.Fingerprint(w.keys.Public)[:12])
	fmt.Fprintf(out, "[draftproof] 已有快照 %d 个（%d 个文档）；Ctrl-C 结束\n",
		w.countExisting(), len(w.heads))

	for {
		select {
		case <-ctx.Done():
			fmt.Fprintf(out, "[draftproof] 结束：本次新增 %d 个快照，存证库 %s\n",
				atomic.LoadInt64(&w.captured), w.st.Dir())
			return nil
		case err, ok := <-fw.Errors:
			if !ok {
				return nil
			}
			fmt.Fprintf(os.Stderr, "[draftproof] watch 错误: %v\n", err)
		case ev, ok := <-fw.Events:
			if !ok {
				return nil
			}
			if ev.Op&(fsnotify.Create) != 0 {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					_ = addTree(ev.Name)
					continue
				}
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 {
				continue
			}
			if !eligible(ev.Name) {
				continue
			}
			w.schedule(ev.Name)
		}
	}
}

func (w *watcher) countExisting() int {
	n := 0
	for _, h := range w.heads {
		n += int(h.LastSeq)
	}
	return n
}

// NewWatchCmd implements `draftproof watch <dir>`.
func NewWatchCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "watch <目录>",
		Short: "常驻监听论文目录，每次保存追加一条 ed25519 签名的哈希链快照",
		Long: "递归监听目录下的 *.docx/*.md/*.tex（跳过隐藏目录）。编辑器每次保存\n" +
			"（防抖 5 秒、同内容去重）都会追加一条签名快照到 <目录>/.draftproof/chain.jsonl，\n" +
			"写作期间零感知，草稿与密钥都不出本机。",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			debounce, err := c.Flags().GetDuration("debounce")
			if err != nil {
				return err
			}
			dir, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				return fmt.Errorf("目录不存在: %s", args[0])
			}
			keyDir, err := chain.DefaultKeyDir()
			if err != nil {
				return err
			}
			keys, err := chain.LoadKeys(keyDir)
			if err != nil {
				return fmt.Errorf("未找到作者密钥——请先运行 draftproof init")
			}
			st, err := store.Open(dir)
			if err != nil {
				return err
			}
			w, err := newWatcher(dir, st, keys, debounce)
			if err != nil {
				return err
			}
			// Ctrl-C / SIGTERM ends cleanly: stop the loop, flush pending captures.
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return w.runWatch(ctx, c.OutOrStdout())
		},
	}
	c.Flags().Duration("debounce", DefaultDebounce, "同一路径保存事件的合并等待（演示/测试可调小，如 600ms）")
	return c
}
