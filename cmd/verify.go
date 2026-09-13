package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/SuperMarioYL/draftproof/internal/report"
)

// ErrVerifyFailed marks a fully-reported FAIL verification: main exits 1
// without printing an extra error line.
var ErrVerifyFailed = errors.New("verification failed")

// NewVerifyCmd implements `draftproof verify <receipt.dpb>` — the reviewer
// side of the loop. Runs offline, needs no keys, no install beyond the one
// binary.
func NewVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify <回执.dpb>",
		Short: "离线复核回执：未动 PASS，任何改动 FAIL 并定位断点",
		Long: "校验 .dpb 回执包的全部密码学证据：\n" +
			"\n" +
			"  1. schema 与作者公钥指纹自洽\n" +
			"  2. 回执级 ed25519 签名（覆盖全部快照与会话统计）\n" +
			"  3. 每个快照的 ed25519 签名（时间戳、大小、内容哈希都在签名内）\n" +
			"  4. prev_hash 哈希链逐环匹配、seq 连续（插入/删除/重排即断）\n" +
			"  5. 会话/统计与快照本身一致；会话内墙钟回拨会给出警告\n" +
			"\n" +
			"PASS = 包内容自导出后未被篡改。请同时与作者事前预登记的密钥卡\n" +
			"指纹比对（回执内含作者公钥指纹）。",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			r, err := report.LoadReceipt(args[0])
			if err != nil {
				return err
			}
			res := report.VerifyReceipt(r)
			out := c.OutOrStdout()

			fmt.Fprintf(out, "回执 %s\n", args[0])
			fmt.Fprintf(out, "  文档     %s（doc %s…）\n", r.Doc.Path, shortFingerprint(r.Doc.ID))
			fmt.Fprintf(out, "  作者     公钥指纹 sha256:%s\n", r.Author.Fingerprint)
			scope := "完整链"
			if r.Since != "" {
				scope = "自 " + r.Since + " 起的连续子链"
			}
			fmt.Fprintf(out, "  范围     %d 个快照 · %s → %s（%s）\n",
				res.SnapshotCount,
				r.Doc.FirstSavedAt.Format("2006-01-02 15:04"),
				r.Doc.LastSavedAt.Format("2006-01-02 15:04"),
				scope)
			fmt.Fprintf(out, "  会话     %d 个 · 跨度 %s · 累计增量 %s\n",
				r.Stats.SessionCount,
				fmtDur(time.Duration(r.Stats.SpanHours*float64(time.Hour))),
				report.FmtBytes(r.Stats.TotalBytesAdded))

			if res.OK {
				fmt.Fprintf(out, "  签名     %d/%d 个快照 ed25519 签名有效\n", res.SnapshotCount, res.SnapshotCount)
				fmt.Fprintf(out, "  哈希链   %d/%d 个链接逐环匹配\n", res.ChainLinks, res.ChainLinks)
				fmt.Fprintf(out, "  回执签名 有效（覆盖全部快照与会话统计）\n")
				for _, warn := range res.Warnings {
					fmt.Fprintf(out, "  注意     %s\n", warn)
				}
				fmt.Fprintf(out, "PASS 整链未被篡改\n")
				fmt.Fprintf(out, "复核提示：与作者事前预登记的密钥卡指纹比对 sha256:%s…\n",
					shortFingerprint(r.Author.Fingerprint))
				return nil
			}

			fmt.Fprintf(out, "FAIL 检测到篡改\n")
			for _, p := range res.EnvelopeProblems {
				fmt.Fprintf(out, "  问题 回执整体：%s\n", p)
			}
			for _, p := range res.ChainProblems {
				fmt.Fprintf(out, "  问题 %s\n", p)
			}
			for _, warn := range res.Warnings {
				fmt.Fprintf(out, "  注意 %s\n", warn)
			}
			fmt.Fprintf(out, "复核建议：向作者索取原始 .dpb；比对预登记密钥卡指纹 sha256:%s…\n",
				shortFingerprint(r.Author.Fingerprint))
			return ErrVerifyFailed
		},
	}
}

func shortFingerprint(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
