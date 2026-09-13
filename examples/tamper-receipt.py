#!/usr/bin/env python3
"""演示用篡改器：把 .dpb 回执中第 2 个快照的 content_hash 前 8 位改成 deadbeef。

只用于演示 verify 的「篡改即翻红」；真实申诉场景里没有人会替你篡改。

用法：
    python3 examples/tamper-receipt.py receipt-20260913-105034.dpb
    draftproof verify receipt-20260913-105034.dpb   # FAIL + 断点定位
"""

import json
import sys


def main():
    if len(sys.argv) != 2:
        sys.exit("用法: python3 examples/tamper-receipt.py <receipt.dpb>")
    path = sys.argv[1]
    with open(path, encoding="utf-8") as f:
        receipt = json.load(f)
    if len(receipt["snapshots"]) < 2:
        sys.exit("回执里快照不足 2 个，无从篡改")
    snap = receipt["snapshots"][1]
    old = snap["content_hash"]
    snap["content_hash"] = "deadbeef" + old[8:]
    with open(path, "w", encoding="utf-8") as f:
        json.dump(receipt, f, indent=2, ensure_ascii=False)
    print(f"已篡改快照 seq {snap['seq']}：content_hash {old[:12]}… -> deadbeef…")
    print("现在重新验签，观察 FAIL：")


if __name__ == "__main__":
    main()
