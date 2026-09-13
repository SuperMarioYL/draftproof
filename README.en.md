**English** | [简体中文](./README.md)

<div align="center">

<img src="https://readme-typing-svg.demolab.com/?font=Fira+Code&weight=600&size=19&pause=1200&color=A149C5&center=true&vCenter=true&random=false&width=720&lines=draftproof+watch+%7E%2Fthesis+%E2%80%94+every+save+from+any+editor+joins+the+signed+chain%3B+not+one+draft+byte+leaves+the+machine%3B+on+flag+day%2C+export+the+receipt%3B+the+degree+office+re-checks+it+offline+with+one+command." alt="draftproof watch ~/thesis — every save from any editor joins the signed chain; not one draft byte leaves the machine; on flag day, export the receipt; the degree office re-checks it offline with one command.">

# DraftProof

**Locally signed writing-process provenance: every save joins an ed25519 hash chain, so when an AIGC detector flags your paper, you hand over a receipt that can be verified offline.**

[![CI](https://github.com/SuperMarioYL/draftproof/actions/workflows/ci.yml/badge.svg)](https://github.com/SuperMarioYL/draftproof/actions/workflows/ci.yml)
[![Version](https://img.shields.io/badge/version-0.1.0-A149C5)](https://github.com/SuperMarioYL/draftproof/releases)
[![Go](https://img.shields.io/badge/go-1.24%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-A149C5)](./LICENSE)
[![Platform](https://img.shields.io/badge/platform-win%20%7C%20macOS%20%7C%20linux-7d8590)](https://github.com/SuperMarioYL/draftproof/releases)
![Local only](https://img.shields.io/badge/local_only-no_telemetry-1a7f37)

<picture>
  <source media="(max-width: 640px) and (prefers-color-scheme: dark)" srcset="assets/presentation/hero-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="assets/presentation/hero-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/hero-dark.svg">
  <img src="assets/presentation/hero-light.svg" width="960" alt="DraftProof — every save joins an ed25519 hash chain; four saves converge into one offline-verifiable .dpb receipt">
</picture>

</div>

---

humanizer's 47k stars prove how afraid authors are of being falsely flagged as AI — but it only teaches you how to hide. DraftProof is the inverse: it turns the writing process itself into cryptographically verifiable evidence.

## <img src="assets/icons/bulb.svg" width="22" alt=""> Why DraftProof

Mandatory AIGC detection (CNKI / VIP in China, detector-gated venues elsewhere) can block a defense or a submission on a probability score alone, and "I wrote this myself" is unfalsifiable from the reviewer's side. Detectors see the **final text** and never the **process**; humanizer-style rewording hides AI tells and proves nothing at the same time.

DraftProof fills the missing layer in between: **provenance**.

- **Before writing**: `draftproof init` generates an ed25519 key pair and prints a key card; send the public-key fingerprint to your advisor on day one — a chain rebuilt later with a new key cannot match the pre-registered fingerprint.
- **While writing**: `draftproof watch ~/thesis` runs in the background and appends one signed snapshot for every save from any editor (Word / WPS / Typora / VS Code). Not one draft byte leaves your machine; writing feels unchanged.
- **On flag day**: `draftproof export` produces a `receipt-*.dpb` bundle plus a one-page printable writing timeline in HTML.
- **Reviewer side**: the degree office or editorial office runs `draftproof verify` offline with the same single binary — PASS means the chain is intact; edit one snapshot, one timestamp, even one hash character, and it flips to FAIL with the breakpoint named.

Stated honestly: a receipt proves the **process existed and was not altered after export** — it does not prove "no AI was used". AI-assisted sessions are meant to be labeled as such in skill mode (see the [appeal kit preview](#-appeal-kit-m3-preview)). A forensics tool must be free to be credible: the author-side CLI and receipts are free forever; monetization lives on the adjudication side (see [Pricing](#-pricing)).

## <img src="assets/icons/schema.svg" width="22" alt=""> Architecture

<picture>
  <source media="(max-width: 640px) and (prefers-color-scheme: dark)" srcset="assets/presentation/architecture-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="assets/presentation/architecture-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/architecture-dark.svg">
  <img src="assets/presentation/architecture-light.svg" width="960" alt="On the author's machine, editor saves flow through fsnotify into an append-only hash chain; export produces .dpb and HTML; the reviewer side verifies offline with the same binary, PASS or FAIL with breakpoint localization.">
</picture>

One process, one binary, zero network, zero server. Snapshots are recorded by **byte hash** — no document format parsing, docx/md/tex treated alike; `.draftproof/chain.jsonl` is append-only, so insertion, deletion or reordering breaks the chain.

Source entry points: [cmd/watch.go](cmd/watch.go) · [cmd/export.go](cmd/export.go) · [cmd/verify.go](cmd/verify.go) · [internal/chain/chain.go](internal/chain/chain.go) · [internal/store/store.go](internal/store/store.go) · [internal/report/report.go](internal/report/report.go) · [internal/report/receipt.html](internal/report/receipt.html)

## <img src="assets/icons/rocket.svg" width="22" alt=""> Quickstart

Requires Go 1.24+ (or grab a single-file Windows/macOS/Linux binary with sha256 checksums from the [Releases](https://github.com/SuperMarioYL/draftproof/releases) page).

```bash
git clone https://github.com/SuperMarioYL/draftproof.git
cd draftproof
go build -o bin/draftproof .
```

First run the full tamper-reject loop in a temp directory (HOME isolated; your real keys and thesis are never touched):

```bash
DRAFTPROOF=bin/draftproof python3 examples/demo-tamper-reject.py
```

Then point it at your own thesis directory:

```bash
draftproof init              # generate the key + key card (send the fingerprint to your advisor today)
draftproof watch ~/thesis    # keep running in the background; zero friction while writing
# ...write and save as usual...
draftproof log ~/thesis      # inspect the writing timeline any time
draftproof export ~/thesis   # on flag day: emit .dpb + printable HTML
draftproof verify receipt-*.dpb   # reviewer side: offline, no install, no network
```

Those five commands are the entire v0.1 interface. `examples/demo-tamper-reject.py` creates its own sample thesis (`thesis.md`, four saves), so you need no input of your own.

## <img src="assets/icons/video.svg" width="22" alt=""> Recorded demo

The commands and outputs below are copied verbatim from a real run on 2026-09-13 (v0.1.0, fully offline); the full record lives in [docs/demo-results.json](docs/demo-results.json). The demo accelerates with `--debounce 700ms`; the shipped default is 5 seconds. (Console labels are in Chinese — the tool's primary audience is Chinese degree offices.)

![watch captures writing, export emits the receipt, verify PASSes; after tampering one snapshot, verify FAILs and localizes the breakpoint](docs/demo-tamper-reject.gif)

Recording script: [docs/demo.tape](docs/demo.tape) (vhs; re-renderable via [.github/workflows/demo.yml](.github/workflows/demo.yml)). The GIF is a **separate recording** of the same flow — its temp dir, fingerprint and timestamps belong to that recording session, not to the same run as the text output below.

### Every save is captured

```text
[draftproof] watch /tmp/draftproof-demo-flzwc_40/thesis — 追踪 .docx/.md/.tex（任何编辑器的保存都会被捕获）
[draftproof] 存证库 /tmp/draftproof-demo-flzwc_40/thesis/.draftproof · 防抖 700ms · 作者指纹 sha256:17801cd9d92f…
[draftproof] 已有快照 0 个（0 个文档）；Ctrl-C 结束
[draftproof] 捕获 thesis.md #1  71B  sha256:aee4d0ad8a0c…
[draftproof] 捕获 thesis.md #2  110B  sha256:d2c00340d85f…
[draftproof] 捕获 thesis.md #3  168B  sha256:bbcc21a62bc6…
[draftproof] 捕获 thesis.md #4  204B  sha256:4ecc37485d74…
[draftproof] 结束：本次新增 4 个快照，存证库 /tmp/draftproof-demo-flzwc_40/thesis/.draftproof
```

### Reviewer check: intact → PASS

```text
$ bin/draftproof verify /tmp/draftproof-demo-flzwc_40/receipt-20260913-114626.dpb
回执 /tmp/draftproof-demo-flzwc_40/receipt-20260913-114626.dpb
  文档     thesis.md（doc ce99f4e9a819…）
  作者     公钥指纹 sha256:17801cd9d92f05a0e40af2dcb47900b3a141e27331c87758c6bfe8b77d5d2486
  范围     4 个快照 · 2026-09-13 11:46 → 2026-09-13 11:46（完整链）
  会话     1 个 · 跨度 3s · 累计增量 204 B
  签名     4/4 个快照 ed25519 签名有效
  哈希链   3/3 个链接逐环匹配
  回执签名 有效（覆盖全部快照与会话统计）
PASS 整链未被篡改
复核提示：与作者事前预登记的密钥卡指纹比对 sha256:17801cd9d92f…
```

### Tamper one snapshot → FAIL + breakpoint

The demo script then flips 8 hex characters of seq 2's `content_hash` (`python3 examples/tamper-receipt.py` is the standalone equivalent) and verifies again:

```text
$ 篡改快照 seq 2 的 content_hash：d2c00340d85f… -> deadbeef…

$ bin/draftproof verify /tmp/draftproof-demo-flzwc_40/receipt-20260913-114626.dpb
回执 /tmp/draftproof-demo-flzwc_40/receipt-20260913-114626.dpb
  文档     thesis.md（doc ce99f4e9a819…）
  作者     公钥指纹 sha256:17801cd9d92f05a0e40af2dcb47900b3a141e27331c87758c6bfe8b77d5d2486
  范围     4 个快照 · 2026-09-13 11:46 → 2026-09-13 11:46（完整链）
  会话     1 个 · 跨度 3s · 累计增量 204 B
FAIL 检测到篡改
  问题 回执整体：回执级 ed25519 签名无效——导出后有人改动过包内内容
  问题 快照 #2（seq 2）: ed25519 签名无效（记录被修改，或非作者密钥）
  问题 快照 #3（seq 3）: prev_hash 与上一快照的记录哈希不匹配——上一条记录被改动
复核建议：向作者索取原始 .dpb；比对预登记密钥卡指纹 sha256:17801cd9d92f…
```

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/demo-0-dark.svg">
  <img src="assets/presentation/demo-0-light.svg" width="860" alt="Two endings of the same receipt: intact PASS; after flipping 8 characters of content_hash, the snapshot signature and the hash chain both break, FAIL with the breakpoint named.">
</picture>

## <img src="assets/icons/history.svg" width="22" alt=""> Data model and trust boundary

Each save is a **Snapshot**: `seq` (monotonic per document), `saved_at` (wall clock) plus `mono_nanos` (monotonic clock, making rollback visible), `prev_hash` (sha256 chaining to the previous record), `content_hash` (byte hash of the file — **content is never stored**), `size`, and `sig` (the author's ed25519 signature over all of the above). The **Receipt** envelope is signed once more by the author key, sealing snapshots, session distribution and stats into one self-contained `.dpb`.

<picture>
  <source media="(max-width: 640px) and (prefers-color-scheme: dark)" srcset="assets/presentation/process-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="assets/presentation/process-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/process-dark.svg">
  <img src="assets/presentation/process-light.svg" width="960" alt="Four stages: init pre-registers the key card; watch signs each save into the chain; export emits the receipt; verify re-checks offline — PASS when intact, FAIL with a breakpoint when any snapshot is edited.">
</picture>

Design trade-offs stated in the product, not buried in an FAQ:

- **Local timestamps cannot stop a "new key, rebuilt chain" forgery** — which is exactly why `init` prints a key card and asks you to send the fingerprint to an advisor before writing. A receipt whose fingerprint does not match the pre-registered card is worthless as evidence.
- **PASS does not mean "no AI"** — the receipt proves the process existed and was not altered; AI-assisted sessions are meant to be labeled honestly, giving reviewers a more trustworthy input, not a laundering tool.
- **docx and other compressed containers are byte-hashed, not parsed** — readable per-version diffs for .md/.tex drafts are out of scope for v0.1.
- **Single machine, single author**: multi-device sync, team signing, mobile apps and browser extensions are all out of scope for v0.1.

<picture>
  <source media="(max-width: 640px) and (prefers-color-scheme: dark)" srcset="assets/presentation/integrations-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="assets/presentation/integrations-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/integrations-dark.svg">
  <img src="assets/presentation/integrations-light.svg" width="960" alt="The byte-hash snapshot chain sits at the center, ringed by editors (Word/WPS/Typora/VS Code), formats (docx/md/tex), commands (init/watch/log/export/verify), artifacts (.dpb/receipt.html/key card) and the boundary (local only, no cloud; RFC 3161 is roadmap).">
</picture>

Explicitly out of scope for v0.1: editor plugins (directory-level watch already covers every editor), cloud backup / RFC 3161 remote timestamps / blockchain anchoring (unpublished drafts must not go to the cloud), AI-trace removal or rewriting (DraftProof is humanizer's opposite: record only, never rewrite a word), Web UI, detector-API integrations or automated appeals.

## <img src="assets/icons/file-text.svg" width="22" alt=""> Appeal kit (m3 preview)

A receipt is evidence; filing the appeal is still a human act. The Chinese appeal-letter template and the provenance Agent Skill are under development for the m3 milestone; what ships in this repo today is a **draft preview**, not yet verified on Chinese-model agent hosts:

- [skill/SKILL.md](skill/SKILL.md) — the provenance Agent Skill in humanizer's own skill format (draft)
- [docs/appeal_kit_zh.md](docs/appeal_kit_zh.md) — the Chinese template for attaching receipts to an appeal letter (draft, in Chinese)

## <img src="assets/icons/cash.svg" width="22" alt=""> Pricing

The author-side CLI, receipt generation and verification are **free forever (MIT)** — a forensics tool must be free to be credible, and every appealing author is a salesperson for the standard. Monetization lives on the **adjudication side**:

| Plan | Audience | Price | Contents |
| --- | --- | --- | --- |
| Author CLI (this repo) | Flagged thesis authors | Free · MIT | Full init / watch / log / export / verify |
| Institutional verification workstation (v0.2+) | Degree offices / departments | ¥19,800 / department / year | Air-gapped deployment, batch .dpb verification, receipt archive and search, review-memo templates; data never leaves campus |
| Journal editorial edition (v0.2+) | Journal editors | ¥6,800 / journal / year | Same, licensed per journal |
| First pilot | Interviewed institutions | ¥9,800 one-off | In exchange for a deployment case study and co-branding |

The workstation ships as an offline license file with bank transfer and VAT invoicing (no SaaS billing stack needed at the start). It is **not yet released** at v0.1; if you work at a degree office or journal editorial office wrestling with AIGC-detection disputes, open an issue — pilot users will help define the review-memo format.

## <img src="assets/icons/route.svg" width="22" alt=""> Roadmap

- **v0.1 (current, m1+m2 done)**: init keys and key card; watch capturing signed snapshots; log writing timeline; export `.dpb` + printable HTML receipt; verify with offline breakpoint localization; the tamper-reject demo recorded.
- **v0.1.x (m3, in progress)**: provenance Agent Skill verified and polished on Chinese-model agent hosts (Qwen / GLM / DeepSeek ecosystems); finalized Chinese appeal kit; Gitee mirror.
- **v0.2+**: the offline institutional verification workstation (batch verification, archive and search, review memos, air-gapped deployment).
- **Under long-term evaluation**: RFC 3161 remote timestamp anchoring (only on the premise of anchoring hashes, never draft content).

## <img src="assets/icons/license.svg" width="22" alt=""> License

[MIT](./LICENSE) · icons from [Tabler Icons](https://tabler.io) (MIT).

<p align="center"><sub><a href="./LICENSE">MIT</a> © 2026 SuperMarioYL</sub></p>
