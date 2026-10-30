---
name: oxr-document-parser
description: Convert PDFs and document images into Markdown, structured JSON, and localized image assets with OXR Batch. Use for OCR, document parsing, textbook conversion, or batch PDF/image extraction when the oxr-batch CLI is available.
---

# OXR Document Parser

Use `oxr-batch` as the execution layer. Do not recreate PDF splitting, API polling, retries, cache merging, or image localization in ad-hoc scripts.

## Before conversion

1. Locate the CLI with `command -v oxr-batch`; if working inside its source repository, use `./oxr-batch` or `go run ./cmd/oxr-batch`.
2. Confirm every input exists and inspect PDF size/page count when `pdfinfo` is available.
3. OXR remote modes upload document contents to a third-party service. If the user has not already authorized that upload, explain the destination and obtain explicit consent before the first request. Recommend self-hosting for sensitive material.
4. Never print, persist, or pass a secret API key on the command line. Formal API mode should read `OXR_API_KEY` from the environment.

## Choose a mode

- Use `--mode free` for evaluation or when no paid key is configured. It uses the public OXR queue, small chunks, one worker, and may take hours for books.
- Use `--mode api` when the user requests the production service and `OXR_API_KEY` is already configured. It uses the formal SoMark endpoint and higher concurrency.
- Use `--base-url` only when the user supplied a self-hosted or alternate endpoint.

Run conversions into a dedicated output directory:

```bash
oxr-batch convert --mode free --out ./output/document -- document.pdf
```

```bash
oxr-batch convert --mode api --out ./output/document -- document.pdf
```

For multiple inputs, pass every path after `--`. The CLI creates per-document subdirectories.

## Long-running jobs

Keep the process attached while possible and report meaningful progress without flooding the user. Each successful chunk is cached under the output directory. If execution is interrupted, rerun the identical command and output path to resume. Do not delete `.chunks` during an unfinished conversion.

On HTTP 413, lower `--chunk-pages`. On repeated 429 responses in free mode, keep one worker and allow the built-in wait behavior. Do not increase concurrency to bypass service limits.

## Validate and hand off

Before reporting completion:

- Confirm the Markdown, JSON, and ZIP exist.
- Check that JSON `total_pages` equals the number of page objects and that source pages are continuous.
- Run `unzip -t` on the archive when available.
- Report absolute, clickable paths and disclose whether remote image URLs were localized.

Read [references/output-schema.md](references/output-schema.md) only when the user needs to integrate, index, or programmatically consume the generated files.
