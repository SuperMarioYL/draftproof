package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/SuperMarioYL/draftproof/internal/chain"
	"github.com/SuperMarioYL/draftproof/internal/report"
	"github.com/SuperMarioYL/draftproof/internal/store"
)

// NewExportCmd implements `draftproof export [dir]`: bundle one document's
// signed chain into a .dpb receipt plus a printable HTML timeline.
func NewExportCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "export [目录]",
		Short: "导出 .dpb 回执包 + 可打印 HTML 写作时间线",
		Long: "从 <目录>/.draftproof/ 读取签名快照链，导出两个文件到 --out（默认当前目录）：\n" +
			"\n" +
			"  receipt-<时间戳>.dpb   自包含验签包（快照、会话、统计、作者公钥、回执签名；\n" +
			"                         不含任何草稿内容，只有哈希与元数据）\n" +
			"  receipt-<时间戳>.html  一页可打印写作时间线，可直接作为申诉材料附件\n" +
			"\n" +
			"--since 只导出某时刻之后的连续子链；默认导出完整链。\n" +
			"目录里有多个文档时用 --doc 按路径片段选择。",
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			outDir, _ := c.Flags().GetString("out")
			docFilter, _ := c.Flags().GetString("doc")
			sinceRaw, _ := c.Flags().GetString("since")

			var since time.Time
			sinceLabel := ""
			if sinceRaw != "" {
				parsed, err := parseSince(sinceRaw)
				if err != nil {
					return err
				}
				since, sinceLabel = parsed, sinceRaw
			}

			_, st, err := openStoreArg(args)
			if err != nil {
				return err
			}
			all, err := st.LoadAll()
			if err != nil {
				return err
			}
			if len(all) == 0 {
				return fmt.Errorf("存证库为空——先运行 draftproof watch 并保存几次文件")
			}
			grouped := store.ByDoc(all)
			ids := sortedDocIDs(grouped)

			selected := ids
			if docFilter != "" {
				selected = nil
				for _, id := range ids {
					if strings.Contains(grouped[id][0].Path, docFilter) {
						selected = append(selected, id)
					}
				}
				if len(selected) == 0 {
					return fmt.Errorf("没有路径包含 %q 的文档；可选：%s", docFilter, docPathList(grouped, ids))
				}
				if len(selected) > 1 {
					return fmt.Errorf("%q 匹配多个文档，请更精确：%s", docFilter, docPathList(grouped, selected))
				}
			} else if len(ids) > 1 {
				return fmt.Errorf("存证库里有 %d 个文档，请用 --doc 选择一个：%s", len(ids), docPathList(grouped, ids))
			}

			keyDir, err := chain.DefaultKeyDir()
			if err != nil {
				return err
			}
			keys, err := chain.LoadKeys(keyDir)
			if err != nil {
				return fmt.Errorf("未找到作者密钥——请先运行 draftproof init")
			}

			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return err
			}
			out := c.OutOrStdout()
			for _, id := range selected {
				r, err := report.BuildReceipt(grouped[id], keys, since, sinceLabel)
				if err != nil {
					return err
				}
				stamp := r.ExportedAt.Format("20060102-150405")
				dpbPath := filepath.Join(outDir, "receipt-"+stamp+".dpb")
				htmlPath := filepath.Join(outDir, "receipt-"+stamp+".html")
				if err := r.WriteFile(dpbPath); err != nil {
					return err
				}
				htmlFile, err := os.Create(htmlPath)
				if err != nil {
					return err
				}
				err = report.RenderHTML(htmlFile, r, c.Root().Version)
				htmlFile.Close()
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "导出回执 %s（doc %s…）\n", r.Doc.Path, r.Doc.ID[:12])
				fmt.Fprintf(out, "  快照 %d · 会话 %d · 跨度 %s · 累计增量 %s\n",
					r.Stats.SnapshotCount, r.Stats.SessionCount,
					fmtDur(time.Duration(r.Stats.SpanHours*float64(time.Hour))),
					report.FmtBytes(r.Stats.TotalBytesAdded))
				fmt.Fprintf(out, "  作者公钥指纹 sha256:%s\n", r.Author.Fingerprint)
				fmt.Fprintf(out, "  写入 %s\n  写入 %s（可打印）\n", dpbPath, htmlPath)
			}
			fmt.Fprint(out, "\n把 .dpb 发给学位办/期刊编辑部；对方离线运行：draftproof verify <文件>.dpb\n")
			fmt.Fprint(out, "提醒：确认导师已留档你的密钥卡指纹（draftproof init 可重看）。\n")
			return nil
		},
	}
	c.Flags().String("out", ".", "导出目录")
	c.Flags().String("doc", "", "按路径片段选择文档（存证库有多个文档时必填）")
	c.Flags().String("since", "", "只导出该时刻之后的连续子链，如 2026-06-01 或 2026-06-01 09:00")
	return c
}

// parseSince accepts YYYY-MM-DD[ HH:MM[:SS]] in local time.
func parseSince(raw string) (time.Time, error) {
	layouts := []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("--since 无法解析 %q（可用格式：2006-01-02[ 15:04[:SS]]）", raw)
}

// sortedDocIDs orders documents by when they were first captured.
func sortedDocIDs(grouped map[string][]chain.Snapshot) []string {
	ids := make([]string, 0, len(grouped))
	for id := range grouped {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return grouped[ids[i]][0].SavedAt.Before(grouped[ids[j]][0].SavedAt)
	})
	return ids
}

func docPathList(grouped map[string][]chain.Snapshot, ids []string) string {
	var parts []string
	for _, id := range ids {
		parts = append(parts, grouped[id][0].Path)
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}
