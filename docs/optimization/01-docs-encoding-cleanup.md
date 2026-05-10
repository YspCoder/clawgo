# 01 文档与编码清理

## 背景

当前 README、配置示例和部分源码输出中的中文在 PowerShell 中显示为乱码。乱码会影响新用户理解，也容易扩散到日志、WebUI、示例配置和后续文档。

## 目标

- 确认仓库文本文件统一使用 UTF-8。
- 修复 README、配置示例、CLI 输出、Agent 通知文案中的乱码中文。
- 保持文档内容准确，不顺手改变代码行为。

## 建议改动范围

主要文件：

- `README.md`
- `README_EN.md`
- `config.example.json`
- `cmd/*.go` 中用户可见输出
- `pkg/agent/*.go` 中用户可见中文通知
- `workspace/*.md`
- `workspace/skills/**/SKILL.md`

尽量避免修改：

- Provider 协议逻辑
- Gateway 生命周期逻辑
- 工具执行逻辑

## 实施步骤

1. 扫描乱码：

   ```bash
   rg "�|锛|鈹|闈|鎴|涓|乧|乀|銆" README.md README_EN.md config.example.json cmd pkg workspace
   ```

2. 判断每处乱码原始含义，优先从上下文、英文 README、测试断言和代码行为恢复。
3. 修复文档和用户可见字符串。
4. 如修改测试断言中的字符串，同步更新相关测试。
5. 确认所有 Markdown 文件可读，代码能编译。

## 验收标准

- `README.md` 中文可正常阅读。
- `config.example.json` 中文注释/示例文字不再乱码。
- CLI/Gateway 关键启动输出不再乱码。
- `rg` 扫描不再出现明显历史乱码片段。
- `go test ./...` 通过。

## 测试建议

```bash
go test ./...
go run ./cmd version
go run ./cmd --debug version
```

如果终端仍显示异常，确认终端编码和文件编码，而不是继续盲改文本。

## 并行注意

该任务可能触碰很多文件，但应只做文本修复。避免和结构重构任务互相覆盖同一段逻辑。
