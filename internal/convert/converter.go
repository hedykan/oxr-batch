package convert

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"oxr-batch/internal/oxr"
)

var imagePattern = regexp.MustCompile(`!\[([^\]]*)\]\((https?://[^)]+)\)`)

type Options struct {
	BaseURL          string
	APIKey           string
	OutputDir        string
	ChunkPages       int
	Workers          int
	LocalizeImages   bool
	KeepHeaderFooter bool
}

type Progress struct {
	Stage   string `json:"stage"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Message string `json:"message"`
}

type Result struct {
	Source   string `json:"source"`
	Pages    int    `json:"pages"`
	Markdown string `json:"markdown"`
	JSON     string `json:"json"`
	ZIP      string `json:"zip"`
}

type Converter struct {
	Options  Options
	Progress func(Progress)
}

type chunk struct {
	Index int
	Start int
	End   int
	Path  string
}

type chunkResult struct {
	chunk
	result oxr.ParseResult
	err    error
}

type cachedResult struct {
	Markdown string          `json:"markdown"`
	JSON     json.RawMessage `json:"json"`
	Pages    int             `json:"pages"`
	TaskID   string          `json:"task_id"`
	Endpoint string          `json:"endpoint"`
}

func (c *Converter) Convert(ctx context.Context, input string) (Result, error) {
	opts := c.Options
	if opts.BaseURL == "" {
		opts.BaseURL = "https://free-oxr.somark.cn/api/v1"
	}
	if opts.APIKey == "" {
		opts.APIKey = "dummy"
	}
	if opts.OutputDir == "" {
		opts.OutputDir = "output"
	}
	if opts.ChunkPages <= 0 {
		opts.ChunkPages = 20
	}
	if opts.Workers <= 0 {
		opts.Workers = 1
	}
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return Result{}, err
	}

	chunks, pages, cleanup, err := c.prepareChunks(ctx, input, opts.ChunkPages)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()
	c.report(Progress{Stage: "convert", Total: len(chunks), Message: fmt.Sprintf("已拆分为 %d 个批次", len(chunks))})

	client := oxr.New(opts.BaseURL, opts.APIKey)
	results := make([]chunkResult, len(chunks))
	cacheDir := filepath.Join(opts.OutputDir, ".chunks")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return Result{}, err
	}
	pending := make([]chunk, 0, len(chunks))
	cachedCount := 0
	for _, item := range chunks {
		if parsed, ok := loadChunkCache(cacheDir, item, opts.BaseURL); ok {
			results[item.Index] = chunkResult{chunk: item, result: parsed}
			cachedCount++
			continue
		}
		pending = append(pending, item)
	}
	if cachedCount > 0 {
		c.report(Progress{Stage: "convert", Done: cachedCount, Total: len(chunks), Message: fmt.Sprintf("已恢复 %d 个完成批次", cachedCount)})
	}
	jobs := make(chan chunk)
	var wg sync.WaitGroup
	var done atomic.Int64
	done.Store(int64(cachedCount))
	workerCount := min(opts.Workers, len(pending))
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				parsed, parseErr := client.ParseAsync(ctx, item.Path, oxr.ParseOptions{KeepHeaderFooter: opts.KeepHeaderFooter})
				if parseErr == nil {
					parseErr = saveChunkCache(cacheDir, item, opts.BaseURL, parsed)
				}
				results[item.Index] = chunkResult{chunk: item, result: parsed, err: parseErr}
				completed := int(done.Add(1))
				message := fmt.Sprintf("第 %d–%d 页完成", item.Start, item.End)
				if parseErr != nil {
					message = fmt.Sprintf("第 %d–%d 页失败", item.Start, item.End)
				}
				c.report(Progress{Stage: "convert", Done: completed, Total: len(chunks), Message: message})
			}
		}()
	}
	for _, item := range pending {
		jobs <- item
	}
	close(jobs)
	wg.Wait()
	for _, item := range results {
		if item.err != nil {
			return Result{}, fmt.Errorf("pages %d-%d: %w", item.Start, item.End, item.err)
		}
	}

	stem := safeStem(filepath.Base(input))
	markdownPath := filepath.Join(opts.OutputDir, stem+".md")
	jsonPath := filepath.Join(opts.OutputDir, stem+".json")
	markdown := mergeMarkdown(results)
	if opts.LocalizeImages {
		c.report(Progress{Stage: "assets", Total: 1, Message: "正在保存远程插图"})
		markdown = localizeImages(ctx, markdown, filepath.Join(opts.OutputDir, stem+"-assets"), stem+"-assets")
	}
	if err := os.WriteFile(markdownPath, []byte(markdown), 0o644); err != nil {
		return Result{}, err
	}
	mergedJSON, err := mergeJSON(input, pages, results)
	if err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(jsonPath, mergedJSON, 0o644); err != nil {
		return Result{}, err
	}
	zipPath := filepath.Join(opts.OutputDir, stem+".zip")
	if err := zipOutputs(zipPath, opts.OutputDir, []string{markdownPath, jsonPath, filepath.Join(opts.OutputDir, stem+"-assets")}); err != nil {
		return Result{}, err
	}
	c.report(Progress{Stage: "done", Done: len(chunks), Total: len(chunks), Message: "转换完成"})
	return Result{Source: input, Pages: pages, Markdown: markdownPath, JSON: jsonPath, ZIP: zipPath}, nil
}

func chunkCachePath(cacheDir string, item chunk) string {
	return filepath.Join(cacheDir, fmt.Sprintf("%04d-pages-%d-%d.json", item.Index, item.Start, item.End))
}

func loadChunkCache(cacheDir string, item chunk, endpoint string) (oxr.ParseResult, bool) {
	data, err := os.ReadFile(chunkCachePath(cacheDir, item))
	if err != nil {
		return oxr.ParseResult{}, false
	}
	var cached cachedResult
	if json.Unmarshal(data, &cached) != nil || cached.Markdown == "" || cached.Endpoint != endpoint {
		return oxr.ParseResult{}, false
	}
	return oxr.ParseResult{Markdown: cached.Markdown, JSON: cached.JSON, Pages: cached.Pages, TaskID: cached.TaskID}, true
}

func saveChunkCache(cacheDir string, item chunk, endpoint string, result oxr.ParseResult) error {
	data, err := json.Marshal(cachedResult{Markdown: result.Markdown, JSON: result.JSON, Pages: result.Pages, TaskID: result.TaskID, Endpoint: endpoint})
	if err != nil {
		return err
	}
	path := chunkCachePath(cacheDir, item)
	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

func (c *Converter) prepareChunks(ctx context.Context, input string, chunkPages int) ([]chunk, int, func(), error) {
	ext := strings.ToLower(filepath.Ext(input))
	if ext != ".pdf" {
		return []chunk{{Index: 0, Start: 1, End: 1, Path: input}}, 1, func() {}, nil
	}
	pages, err := pdfPages(ctx, input)
	if err != nil {
		return nil, 0, func() {}, err
	}
	if pages <= chunkPages {
		return []chunk{{Index: 0, Start: 1, End: pages, Path: input}}, pages, func() {}, nil
	}
	if _, err := exec.LookPath("pdfseparate"); err != nil {
		return nil, 0, func() {}, fmt.Errorf("pdfseparate is required for large PDFs: %w", err)
	}
	if _, err := exec.LookPath("pdfunite"); err != nil {
		return nil, 0, func() {}, fmt.Errorf("pdfunite is required for large PDFs: %w", err)
	}
	tempDir, err := os.MkdirTemp("", "oxr-batch-chunks-")
	if err != nil {
		return nil, 0, func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(tempDir) }
	var chunks []chunk
	for start := 1; start <= pages; start += chunkPages {
		end := min(start+chunkPages-1, pages)
		index := len(chunks)
		pattern := filepath.Join(tempDir, fmt.Sprintf("part-%04d-page-%%d.pdf", index))
		cmd := exec.CommandContext(ctx, "pdfseparate", "-f", strconv.Itoa(start), "-l", strconv.Itoa(end), input, pattern)
		if output, err := cmd.CombinedOutput(); err != nil {
			cleanup()
			return nil, 0, func() {}, fmt.Errorf("split PDF pages %d-%d: %w: %s", start, end, err, output)
		}
		chunkPath := filepath.Join(tempDir, fmt.Sprintf("chunk-%04d-pages-%d-%d.pdf", index, start, end))
		args := make([]string, 0, end-start+2)
		for page := start; page <= end; page++ {
			args = append(args, fmt.Sprintf(pattern, page))
		}
		args = append(args, chunkPath)
		cmd = exec.CommandContext(ctx, "pdfunite", args...)
		if output, err := cmd.CombinedOutput(); err != nil {
			cleanup()
			return nil, 0, func() {}, fmt.Errorf("join PDF chunk %d: %w: %s", index, err, output)
		}
		chunks = append(chunks, chunk{Index: index, Start: start, End: end, Path: chunkPath})
	}
	return chunks, pages, cleanup, nil
}

func pdfPages(ctx context.Context, path string) (int, error) {
	cmd := exec.CommandContext(ctx, "pdfinfo", path)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("pdfinfo: %w", err)
	}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "Pages:") {
			return strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Pages:")))
		}
	}
	return 0, fmt.Errorf("page count not found in pdfinfo output")
}

func mergeMarkdown(results []chunkResult) string {
	var out strings.Builder
	for i, item := range results {
		if i > 0 {
			out.WriteString("\n\n---\n\n")
		}
		fmt.Fprintf(&out, "<!-- OXR Batch · source pages %d–%d -->\n\n", item.Start, item.End)
		out.WriteString(strings.TrimSpace(item.result.Markdown))
		out.WriteString("\n")
	}
	return out.String()
}

func mergeJSON(source string, totalPages int, results []chunkResult) ([]byte, error) {
	pages := make([]any, 0, totalPages)
	tasks := make([]map[string]any, 0, len(results))
	for _, item := range results {
		var document map[string]any
		if len(item.result.JSON) > 0 && string(item.result.JSON) != "null" {
			if err := json.Unmarshal(item.result.JSON, &document); err != nil {
				return nil, fmt.Errorf("decode JSON for pages %d-%d: %w", item.Start, item.End, err)
			}
		}
		if rawPages, ok := document["pages"].([]any); ok {
			for offset, raw := range rawPages {
				if page, ok := raw.(map[string]any); ok {
					page["page_num"] = item.Start - 1 + offset
					page["source_page"] = item.Start + offset
				}
				pages = append(pages, raw)
			}
		}
		tasks = append(tasks, map[string]any{"task_id": item.result.TaskID, "start_page": item.Start, "end_page": item.End})
	}
	merged := map[string]any{
		"source_file":  filepath.Base(source),
		"total_pages":  totalPages,
		"generated_at": time.Now().Format(time.RFC3339),
		"tasks":        tasks,
		"pages":        pages,
	}
	return json.MarshalIndent(merged, "", "  ")
}

func localizeImages(ctx context.Context, markdown, assetDir, relativeDir string) string {
	_ = os.MkdirAll(assetDir, 0o755)
	matches := imagePattern.FindAllStringSubmatch(markdown, -1)
	replacements := make(map[string]string)
	client := &http.Client{Timeout: 60 * time.Second}
	for i, match := range matches {
		url := match[2]
		if _, exists := replacements[url]; exists {
			continue
		}
		ext := filepath.Ext(strings.Split(url, "?")[0])
		if ext == "" || len(ext) > 5 {
			ext = ".png"
		}
		name := fmt.Sprintf("image-%04d%s", i+1, ext)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if err != nil || os.WriteFile(filepath.Join(assetDir, name), data, 0o644) != nil {
			continue
		}
		replacements[url] = filepath.ToSlash(filepath.Join(relativeDir, name))
	}
	for remote, local := range replacements {
		markdown = strings.ReplaceAll(markdown, remote, local)
	}
	return markdown
}

func zipOutputs(target, root string, paths []string) error {
	file, err := os.Create(target)
	if err != nil {
		return err
	}
	archive := zip.NewWriter(file)
	defer file.Close()
	defer archive.Close()
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if !info.IsDir() {
			if err := addZipFile(archive, root, path); err != nil {
				return err
			}
			continue
		}
		var files []string
		_ = filepath.WalkDir(path, func(item string, entry os.DirEntry, walkErr error) error {
			if walkErr == nil && !entry.IsDir() {
				files = append(files, item)
			}
			return nil
		})
		sort.Strings(files)
		for _, item := range files {
			if err := addZipFile(archive, root, item); err != nil {
				return err
			}
		}
	}
	return nil
}

func addZipFile(archive *zip.Writer, root, path string) error {
	input, err := os.Open(path)
	if err != nil {
		return err
	}
	defer input.Close()
	name, _ := filepath.Rel(root, path)
	output, err := archive.Create(filepath.ToSlash(name))
	if err != nil {
		return err
	}
	_, err = io.Copy(output, input)
	return err
}

func safeStem(name string) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	stem = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == 0 {
			return '-'
		}
		return r
	}, stem)
	if stem == "" || stem == "." {
		return "document"
	}
	return stem
}

func (c *Converter) report(progress Progress) {
	if c.Progress != nil {
		c.Progress(progress)
	}
}
