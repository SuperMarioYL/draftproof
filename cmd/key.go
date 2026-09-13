// Package cmd implements the draftproof CLI subcommands.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/SuperMarioYL/draftproof/internal/chain"
)

// NewRootCmd builds the root command. Subcommands are attached by main so
// the command tree stays explicit in one place.
func NewRootCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "draftproof",
		Short: "本地签名的写作过程存证：保存即上链，误判时可离线验签自证",
		Long: "DraftProof 把写作过程变成密码学可验的证据。\n" +
			"\n" +
			"在论文目录运行 watch，之后任何编辑器（Word/WPS/Typora/VS Code）的每次保存\n" +
			"都会追加一条 ed25519 签名的哈希链快照，草稿一个字节都不出本机。\n" +
			"被 AIGC 检测误判时，export 导出回执包（.dpb）与可打印的写作时间线 HTML；\n" +
			"学位办/期刊编辑部用同一二进制离线运行 verify 即可复核：未动则 PASS，\n" +
			"改动任何快照或时间戳立即 FAIL 并定位断点。\n" +
			"\n" +
			"取证工具必须免费才可信：作者端 CLI 永久免费开源。",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
}

// NewInitCmd implements `draftproof init`: generate the author key pair and
// print the key card whose fingerprint should be pre-registered with an
// advisor before writing starts.
func NewInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "生成作者 ed25519 密钥并导出密钥卡（尽早发给导师留档）",
		Long: "生成（或复用）作者密钥对，保存到 ~/.draftproof/，并输出密钥卡。\n" +
			"\n" +
			"密钥卡上的公钥指纹是整个信任模型的关键：请在开始写作当天就发给\n" +
			"导师/同学留档。事后换钥匙重造的整条回执链，将无法匹配这张卡上\n" +
			"事前登记的指纹。",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			dir, err := chain.DefaultKeyDir()
			if err != nil {
				return fmt.Errorf("无法定位用户主目录: %w", err)
			}
			keys, created, err := chain.LoadOrCreateKeys(dir)
			if err != nil {
				return fmt.Errorf("密钥生成失败: %w", err)
			}
			card := chain.KeyCard(keys.Public)
			cardPath := filepath.Join(dir, "keycard.txt")
			if err := os.WriteFile(cardPath, []byte(card), 0o644); err != nil {
				return fmt.Errorf("密钥卡写入失败: %w", err)
			}
			if created {
				fmt.Fprintf(c.OutOrStdout(), "已生成作者密钥：%s\n", dir)
			} else {
				fmt.Fprintf(c.OutOrStdout(), "复用已有作者密钥：%s\n", dir)
			}
			fmt.Fprint(c.OutOrStdout(), "\n"+card+"\n")
			fmt.Fprintf(c.OutOrStdout(), "密钥卡已保存到 %s\n", cardPath)
			fmt.Fprint(c.OutOrStdout(), "下一步：在论文目录运行 draftproof watch <目录>\n")
			return nil
		},
	}
}
