# Changelog

Notable changes to DraftProof. Versions follow SemVer; each release ships
goreleaser binaries for Linux, macOS and Windows on the GitHub releases page.

## v0.3.0 — 2026-11-22

### Fixed

- `verify` now re-derives the receipt's document block (snapshot count, first and
  last save time, doc id, path) from the enclosed snapshots and fails when they
  disagree; a receipt with no snapshots is rejected instead of printing PASS.
- `export` validates the hash chain — seq contiguity, prev_hash links and the
  genesis record — before signing, and refuses to write a receipt that `verify`
  is bound to reject, naming every breakpoint it found.
- `export --since` with a moment at or before the first save is a full-chain
  export: a complete chain is no longer labelled a sub-chain (which had disabled
  the genesis assertion in `verify`).
- `--since` receipts no longer count the window's first snapshot's existing
  bytes as "bytes added"; the printable HTML marks that first row as a baseline
  instead of a size delta. Receipts exported by v0.2.0 keep verifying, because
  the verifier accepts both the old and the corrected accounting rule.

### Changed

- Sub-chain receipt stats report the growth inside the selected window.

## v0.2.0 — 2026-10-18

### Fixed

- `watch` flushes the last save still inside its debounce window on
  Ctrl-C/SIGTERM instead of dropping it, and timer callbacks can no longer race
  shutdown.
- `init` refuses to overwrite an unreadable or corrupt `author.key`, protecting
  the key-card fingerprint that was pre-registered with an advisor.

### Changed

- Version surfaces unified at 0.2.0; bilingual product site and Pages workflow.

## v0.1.0 — 2026-09-13

- First release: `init` (ed25519 key pair + printable key card), `watch` (signed
  snapshot hash chain), `log` (writing timeline), `export` (`.dpb` receipt +
  printable HTML) and `verify` (offline re-check with breakpoint localization).
