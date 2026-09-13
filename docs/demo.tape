# vhs tape — DraftProof 篡改即翻红演示: watch 捕获写作 -> export 导出回执 ->
# verify PASS -> 篡改一个快照 -> verify FAIL 并定位断点。
#
# 渲染（仓库根目录）:
#   go build -o bin/draftproof .
#   vhs docs/demo.tape        # 写入 docs/demo-tamper-reject.gif
#
# 演示用 --debounce 700ms 加速；日常使用默认 5s 防抖。
# CJK 字体: 本地 macOS 自带; CI 在 demo.yml 中安装 fonts-noto-cjk。
Output docs/demo-tamper-reject.gif

Set Shell "bash"
Set FontSize 15
Set Width 1000
Set Height 620
Set Theme "Dracula"
Set Padding 16
Set TypingSpeed 25ms

# 隔离环境: 不触碰真实密钥与论文
Type "demo=$(mktemp -d) && export HOME=$demo/home && mkdir -p $HOME $demo/thesis && export PATH=$PWD/bin:$PATH"
Enter
Sleep 500ms

# 1) init: 生成 ed25519 密钥 + 密钥卡（指纹尽早发导师留档）
Type "# 1) 初始化作者密钥——密钥卡指纹请尽早发给导师留档"
Enter
Sleep 400ms
Type "draftproof init"
Enter
Sleep 1200ms

# 2) watch 后台常驻, 然后用任何编辑器写论文
Type "# 2) 后台监听论文目录（演示用 700ms 防抖, 默认 5s）"
Enter
Sleep 400ms
Type "draftproof watch $demo/thesis --debounce 700ms >/tmp/dp-watch.log 2>&1 &"
Enter
Sleep 500ms

Type "printf '# 绪论\n\n本文研究面向 AIGC 检测申诉的本地存证方法。\n' > $demo/thesis/thesis.md"
Enter
Sleep 1300ms
Type "printf '# 绪论\n\n本文研究面向 AIGC 检测申诉的本地存证方法。\n\n1.2 研究意义与工作量说明。\n' > $demo/thesis/thesis.md"
Enter
Sleep 1300ms
Type "printf '# 绪论\n\n本文研究面向 AIGC 检测申诉的本地存证方法。\n\n1.2 研究意义与工作量说明。\n\n2.1 系统设计: ed25519 签名与 sha256 哈希链。\n' > $demo/thesis/thesis.md"
Enter
Sleep 1300ms
Type "printf '# 绪论\n\n本文研究面向 AIGC 检测申诉的本地存证方法。\n\n1.2 研究意义与工作量说明。\n\n2.1 系统设计: ed25519 签名与 sha256 哈希链。\n\n2.2 离线验签与断点定位。\n' > $demo/thesis/thesis.md"
Enter
Sleep 1400ms

Type "cat /tmp/dp-watch.log"
Enter
Sleep 1000ms
Type "kill %1"
Enter
Sleep 400ms

# 3) 写作时间线
Type "# 3) 写作时间线（全部来自签名快照, 不读草稿正文）"
Enter
Sleep 400ms
Type "draftproof log $demo/thesis"
Enter
Sleep 1100ms

# 4) 导出回执 + 离线验签 -> PASS
Type "# 4) 被误判当天: 导出回执包 + 可打印 HTML 时间线"
Enter
Sleep 400ms
Type "draftproof export $demo/thesis --out $demo"
Enter
Sleep 1100ms
Type "# 5) 学位办视角: 同一二进制离线复核 -> PASS"
Enter
Sleep 400ms
Type "draftproof verify $demo/receipt-*.dpb"
Enter
Sleep 1600ms

# 5) 篡改一个快照 -> FAIL
Type "# 6) 篡改一个快照的 content_hash, 再验一次"
Enter
Sleep 400ms
Type "python3 examples/tamper-receipt.py $demo/receipt-*.dpb"
Enter
Sleep 900ms
Type "draftproof verify $demo/receipt-*.dpb"
Enter
Sleep 2200ms
