// DraftProof — 本地签名的写作过程存证：每次保存进入 ed25519 哈希链，
// 被误判时导出可离线验签的回执。
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/SuperMarioYL/draftproof/cmd"
)

// version is set at build time by goreleaser (-X main.version).
var version = "0.1.0"

func main() {
	root := cmd.NewRootCmd(version)
	root.AddCommand(
		cmd.NewInitCmd(),
		cmd.NewWatchCmd(),
		cmd.NewLogCmd(),
		cmd.NewExportCmd(),
		cmd.NewVerifyCmd(),
	)
	if err := root.Execute(); err != nil {
		// A failed verification is a normal, fully-reported outcome, not a
		// usage error — print nothing extra, just exit non-zero.
		if !errors.Is(err, cmd.ErrVerifyFailed) {
			fmt.Fprintln(os.Stderr, "draftproof:", err)
		}
		os.Exit(1)
	}
}
