# 第三方来源与许可证

## dsh-connect-workbuddy

WorkBuddy 消息兼容代码参考并移植自：

- 仓库：https://github.com/dingminhua/dsh-connect-workbuddy
- 固定提交：`4cdba8a37b277a465b754328737f580254552d34`
- 源文件：`src/upstream.ts`
- 对应函数：`ensureSystemHead`、`neutralizeChannelFingerprints`、`isUnapprovedChannelOpening`，以及 `prepareChatBody` 的 developer 角色规范化部分。
- 本项目实现：`internal/logic/channel/workbuddy_body.go`
- 更新与差异说明：`docs/workbuddy-compatibility.md`

```text
MIT License

Copyright (c) 2026 LaoDing

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```
