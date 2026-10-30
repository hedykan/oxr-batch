# Output schema

OXR Batch produces three user-facing artifacts per source document:

```text
document.md
document.json
document.zip
document-assets/
```

The Markdown contains recognized text, tables, LaTeX formulas, and relative links to localized images. The ZIP contains the Markdown, JSON, and asset directory.

The merged JSON root contains:

- `source_file`: original base file name.
- `total_pages`: original PDF page count.
- `generated_at`: merge timestamp in RFC 3339 format.
- `tasks`: OXR task IDs and source-page ranges for each chunk.
- `pages`: ordered page objects returned by OXR.

Each page receives two normalized fields:

- `page_num`: zero-based page index across the merged document.
- `source_page`: one-based page number in the original PDF.

OXR page objects preserve recognized blocks, their element types, bounding boxes, and content. Consumers should tolerate additional OXR fields and unknown block types.
