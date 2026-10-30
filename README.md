# OXR Batch

[![Go](https://img.shields.io/badge/Go-1.24%2B-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-c23124)](LICENSE)
[![Release](https://img.shields.io/github/v/release/hedykan/oxr-batch)](https://github.com/hedykan/oxr-batch/releases)

本地批量文档转换工作台：把 PDF 与扫描图片交给 OXR，输出 Markdown、带版面坐标的 JSON，以及包含本地化插图的 ZIP。提供 Cobra CLI、Web 工作台和可供 AI 自动调用的 Agent Skill。

## 特性

- 批量处理 PDF、JPG、PNG、WEBP 等文档图片
- 大 PDF 自动分片、异步轮询、重试和断点续跑
- 合并 Markdown 与 JSON，并恢复原始连续页码
- 下载 OXR 临时图片，生成可离线分发的 ZIP
- 免费 OXR 与 SoMark 正式 API 双模式
- 本地 Web 工作台与 Cobra CLI 共用同一转换核心
- `oxr-document-parser` Skill 可让 Codex 等兼容 Agent 自动编排 CLI

## 安装

### 下载发行版

从 [Releases](https://github.com/hedykan/oxr-batch/releases) 下载对应平台的压缩包，将 `oxr-batch` 放入 `PATH`。

Linux 处理大 PDF 还需要 Poppler：

```bash
sudo apt install poppler-utils
```

macOS：

```bash
brew install poppler
```

### 从源码构建

```bash
git clone https://github.com/hedykan/oxr-batch.git
cd oxr-batch
make test
make build
```

## 使用

启动 Web 工作台：

```bash
oxr-batch serve
```

浏览器打开 <http://127.0.0.1:8787>。

免费模式无需注册，但会排队且只允许单任务：

```bash
oxr-batch convert --mode free --out ./output -- book.pdf image.png
```

正式 API 模式默认使用更大分片与更高并发。建议通过环境变量提供密钥，避免密钥出现在进程列表或 Shell 历史中：

```bash
export OXR_API_KEY='sk-your-api-key'
oxr-batch convert --mode api --out ./output -- book.pdf
```

自托管 OXR：

```bash
oxr-batch convert \
  --mode free \
  --base-url http://127.0.0.1:8000/v1 \
  --out ./output \
  -- document.pdf
```

查看 Cobra 帮助、版本与 Shell 补全：

```bash
oxr-batch --help
oxr-batch --version
oxr-batch convert --help
oxr-batch completion zsh
```

## 免费与正式 API

| 模式 | 默认端点 | 鉴权 | 分片 | 并发 | 适合场景 |
|---|---|---:|---:|---:|---|
| `free` | `https://free-oxr.somark.cn/api/v1` | `dummy` | 20 页 | 1 | 体验、小批量、非敏感文档 |
| `api` | `https://somark.ai/api/v1` | `sk-...` | 300 页 | 4 | 生产任务、更快处理 |

免费镜像没有公开精确上传上限；实测约 25MB 的整本 PDF 会收到 HTTP 413，同一匿名来源同时提交任务会收到 HTTP 429 / code 1133。OXR Batch 会自动等待繁忙任务，大文件则按页拆分。

远程模式会把文档内容上传到所选服务。敏感材料应使用自托管 OXR。

## 安装 AI Skill

Skill 是标准的 `SKILL.md` 目录，位于 [`skills/oxr-document-parser`](skills/oxr-document-parser)。安装前请先确保 `oxr-batch` 已在 `PATH` 中。

Codex 本地安装：

```bash
git clone https://github.com/hedykan/oxr-batch.git
mkdir -p "${CODEX_HOME:-$HOME/.codex}/skills"
cp -R oxr-batch/skills/oxr-document-parser \
  "${CODEX_HOME:-$HOME/.codex}/skills/"
```

也可以在开发时建立软链接，以便仓库更新后 Skill 同步更新：

```bash
ln -s "$PWD/skills/oxr-document-parser" \
  "${CODEX_HOME:-$HOME/.codex}/skills/oxr-document-parser"
```

安装后可明确调用：

```text
Use $oxr-document-parser to convert this PDF into Markdown and JSON.
```

Skill 会选择免费或正式 API、确认远程上传授权、调用 Cobra CLI、监控长任务，并校验最终 Markdown、JSON、图片与 ZIP。其目录结构遵循 [OpenAI Skills 文档](https://developers.openai.com/api/docs/guides/tools-skills)描述的 `SKILL.md` 规范。

## 输出结构

```text
output/
├── document.md
├── document.json
├── document.zip
└── document-assets/
    ├── image-0001.png
    └── ...
```

- Markdown：正文、表格、LaTeX 公式和相对图片链接
- JSON：页码、元素类型、坐标、内容和 OXR 任务信息
- ZIP：Markdown、JSON 与全部本地化插图

## 致谢

特别感谢 [SoMarkAI/OXR](https://github.com/SoMarkAI/OXR) 团队开源优秀的文档解析模型，并提供免费 API、SDK 与部署文档。OXR Batch 是独立的社区项目，与 SoMarkAI 无官方隶属关系。

本项目还使用 [Cobra](https://github.com/spf13/cobra) 构建命令行界面。

OXR 模型、权重与官方代码受其各自许可证约束；本仓库不包含 OXR 权重或源代码。使用 OXR 服务或模型前，请阅读其 [OXR Open Source License](https://github.com/SoMarkAI/OXR/blob/main/LICENSE)。

## 许可证

OXR Batch 自身以 [MIT License](LICENSE) 发布。
