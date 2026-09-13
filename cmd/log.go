package cmd

import (
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/SuperMarioYL/draftproof/internal/chain"
	"github.com/SuperMarioYL/draftproof/internal/report"
	"github.com/SuperMarioYL/draftproof/internal/store"
)

func fmtDur(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h == 0 {
		return fmt.Sprintf("%dm", m)
	}
	if h >= 48 {
		return fmt.Sprintf("%.1f天", d.Hours()/24)
	}
	return fmt.Sprintf("%dh%dm", h, m)
}

// writeTimeline prints the writing timeline of one document: sessions,
// gaps and byte growth, straight from the recorded snapshots.
func writeTimeline(w io.Writer, snaps []chain.Snapshot) {
	doc := snaps[0]
	fmt.Fprintf(w, "写作时间线 · %s（doc %s…）\n", doc.Path, doc.DocID[:12])
	stats := report.ComputeStats(snaps, nil)
	fmt.Fprintf(w, "  快照 %d · %s → %s · 跨度 %s · 会话 %d\n\n",
		stats.SnapshotCount,
		stats.FirstSavedAt.Format("2006-01-02 15:04"),
		stats.LastSavedAt.Format("2006-01-02 15:04"),
		fmtDur(stats.LastSavedAt.Sub(stats.FirstSavedAt)),
		len(report.AnalyzeSessions(snaps)))

	sessions := report.AnalyzeSessions(snaps)
	for i, sess := range sessions {
		fmt.Fprintf(w, "  会话 %-2d %s → %s  %-7s 快照 %-3d 增量 %s\n",
			sess.Index,
			sess.Start.Format("01-02 15:04"),
			sess.End.Format("01-02 15:04"),
			fmtDur(sess.End.Sub(sess.Start)),
			sess.Snapshots,
			report.FmtBytes(sess.BytesAdded))
		if i+1 < len(sessions) {
			next := sessions[i+1]
			gap := next.Start.Sub(sess.End)
			fmt.Fprintf(w, "        间隔 %s（%s → %s）\n",
				fmtDur(gap),
				sess.End.Format("01-02 15:04"),
				next.Start.Format("01-02 15:04"))
		}
	}
	last := snaps[len(snaps)-1]
	fmt.Fprintf(w, "\n  终稿大小 %s · 首个快照 %s\n",
		report.FmtBytes(last.Size), report.FmtBytes(snaps[0].Size))
}

// NewLogCmd implements `draftproof log [dir]`.
func NewLogCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "log [目录]",
		Short: "打印写作时间线：会话、间隔与增量",
		Long: "读取 <目录>/.draftproof/chain.jsonl，按文档打印写作时间线：\n" +
			"每次写作会话的起止与时长、会话间隔、字节增量。\n" +
			"不联网、不读草稿正文——时间线全部来自已记录的签名快照。",
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			dir, st, err := openStoreArg(args)
			if err != nil {
				return err
			}
			all, err := st.LoadAll()
			if err != nil {
				return err
			}
			if len(all) == 0 {
				return fmt.Errorf("%s 还没有快照——先运行 draftproof watch %s 并保存几次文件", dir, dir)
			}
			grouped := store.ByDoc(all)
			ids := make([]string, 0, len(grouped))
			for id := range grouped {
				ids = append(ids, id)
			}
			sort.Slice(ids, func(i, j int) bool {
				return grouped[ids[i]][0].SavedAt.Before(grouped[ids[j]][0].SavedAt)
			})
			out := c.OutOrStdout()
			for i, id := range ids {
				if i > 0 {
					fmt.Fprintln(out)
				}
				writeTimeline(out, grouped[id])
			}
			return nil
		},
	}
}

// openStoreArg resolves the watched directory (default ".") and opens its
// store read-only, failing when nothing has been captured yet.
func openStoreArg(args []string) (string, *store.Store, error) {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	st, err := store.OpenExisting(dir)
	if err != nil {
		return "", nil, err
	}
	return dir, st, nil
}
