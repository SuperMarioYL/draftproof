#!/usr/bin/env python3
"""DraftProof 端到端演示：watch 捕获写作 -> log 时间线 -> export 导出回执 ->
篡改一个快照 -> verify 由 PASS 翻 FAIL。

用法：
    go build -o bin/draftproof .
    DRAFTPROOF=bin/draftproof python3 examples/demo-tamper-reject.py

全部在临时目录完成（HOME 已隔离，不触碰真实密钥与论文）。演示用 700ms
防抖加速——日常使用请用默认 5s。脚本以非零退出码结束表示演示失败。
"""

import glob
import json
import os
import subprocess
import sys
import tempfile
import time

BIN = os.environ.get("DRAFTPROOF", "draftproof")
DEBOUNCE_MS = 700


def run(args, env, expect=None):
    print("$", " ".join(args))
    proc = subprocess.run(args, env=env, capture_output=True, text=True)
    out = proc.stdout
    if proc.stderr:
        out += "[stderr] " + proc.stderr
    print(out.rstrip())
    if expect is not None and proc.returncode != expect:
        print(f"!! 期望退出码 {expect}，实际 {proc.returncode}", file=sys.stderr)
        sys.exit(1)
    return proc.returncode, out


def main():
    workdir = tempfile.mkdtemp(prefix="draftproof-demo-")
    home = os.path.join(workdir, "home")
    thesis = os.path.join(workdir, "thesis")
    os.makedirs(home)
    os.makedirs(thesis)
    env = dict(os.environ, HOME=home)
    print(f"演示目录 {workdir}（HOME 已隔离，不触碰真实密钥与论文）\n")

    # 1) init：生成作者密钥 + 密钥卡（指纹应尽早发导师留档）
    run([BIN, "init"], env)

    # 2) watch 后台常驻（演示防抖 700ms；日常默认 5s）
    watch = subprocess.Popen(
        [BIN, "watch", thesis, "--debounce", f"{DEBOUNCE_MS}ms"],
        env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True,
    )
    time.sleep(0.8)

    # 3) 模拟写作：四次保存，模拟一个真实写作夜
    drafts = [
        "# 绪论\n\n本文研究面向 AIGC 检测申诉的本地存证方法。\n",
        "# 绪论\n\n本文研究面向 AIGC 检测申诉的本地存证方法。\n\n1.2 研究意义与工作量说明。\n",
        "# 绪论\n\n本文研究面向 AIGC 检测申诉的本地存证方法。\n\n1.2 研究意义与工作量说明。\n\n"
        "2.1 系统设计：ed25519 签名与 sha256 哈希链。\n",
        "# 绪论\n\n本文研究面向 AIGC 检测申诉的本地存证方法。\n\n1.2 研究意义与工作量说明。\n\n"
        "2.1 系统设计：ed25519 签名与 sha256 哈希链。\n\n2.2 离线验签与断点定位。\n",
    ]
    doc = os.path.join(thesis, "thesis.md")
    for i, text in enumerate(drafts, 1):
        with open(doc, "w", encoding="utf-8") as f:
            f.write(text)
        print(f"$ 保存第 {i} 版（{len(text.encode('utf-8'))} B，任何编辑器的保存都会被捕获）")
        time.sleep(DEBOUNCE_MS / 1000 + 0.45)
    time.sleep(1.0)

    # 4) 结束 watch，收尾输出
    watch.terminate()
    try:
        tail = watch.stdout.read()
        print(tail.rstrip())
        watch.wait(timeout=5)
    except subprocess.TimeoutExpired:
        watch.kill()

    # 5) log / export / verify（学位办视角）
    run([BIN, "log", thesis], env)
    run([BIN, "export", thesis, "--out", workdir], env)
    dpb = sorted(glob.glob(os.path.join(workdir, "receipt-*.dpb")))[-1]
    run([BIN, "verify", dpb], env, expect=0)

    # 6) 篡改：把第 2 个快照的 content_hash 前 8 位换成 deadbeef
    with open(dpb, encoding="utf-8") as f:
        receipt = json.load(f)
    snap = receipt["snapshots"][1]
    old = snap["content_hash"]
    snap["content_hash"] = "deadbeef" + old[8:]
    with open(dpb, "w", encoding="utf-8") as f:
        json.dump(receipt, f, indent=2, ensure_ascii=False)
    print(f"$ 篡改快照 seq {snap['seq']} 的 content_hash：{old[:12]}… -> deadbeef…\n")

    # 7) verify 必须翻红 FAIL（退出码 1）
    run([BIN, "verify", dpb], env, expect=1)
    print("\n演示结论：同一份回执，未动 -> PASS；改动任何一个快照 -> FAIL 并定位断点。")
    print("（回执不含草稿内容；学位办复核只需 draftproof verify 这一个命令，离线运行。）")


if __name__ == "__main__":
    main()
