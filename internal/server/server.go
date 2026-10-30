package server

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"oxr-batch/internal/convert"
	"oxr-batch/internal/profile"
)

//go:embed web/*
var webFiles embed.FS

type Job struct {
	ID        string           `json:"id"`
	FileName  string           `json:"file_name"`
	Status    string           `json:"status"`
	Progress  convert.Progress `json:"progress"`
	Result    *convert.Result  `json:"result,omitempty"`
	Error     string           `json:"error,omitempty"`
	CreatedAt time.Time        `json:"created_at"`
	Mode      string           `json:"mode"`
}

type Server struct {
	DataDir string
	logger  *log.Logger
	mu      sync.RWMutex
	jobs    map[string]*Job
}

func New(dataDir string, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}
	return &Server{DataDir: dataDir, logger: logger, jobs: make(map[string]*Job)}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/jobs", s.handleJobs)
	mux.HandleFunc("/api/jobs/", s.handleJob)
	static, _ := fs.Sub(webFiles, "web")
	mux.Handle("/", http.FileServer(http.FS(static)))
	return s.securityHeaders(mux)
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listJobs(w)
	case http.MethodPost:
		s.createJob(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) listJobs(w http.ResponseWriter) {
	s.mu.RLock()
	jobs := make([]Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, *job)
	}
	s.mu.RUnlock()
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].CreatedAt.After(jobs[j].CreatedAt) })
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (s *Server) createJob(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 512<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "无法读取上传文件: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer r.MultipartForm.RemoveAll()
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		http.Error(w, "请选择 PDF 或图片", http.StatusBadRequest)
		return
	}
	if len(files) > 50 {
		http.Error(w, "一次最多提交 50 个文件", http.StatusBadRequest)
		return
	}
	mode := strings.TrimSpace(r.FormValue("mode"))
	baseURL := strings.TrimSpace(r.FormValue("base_url"))
	apiKey := strings.TrimSpace(r.FormValue("api_key"))
	chunkPages := optionalIntForm(r, "chunk_pages")
	workers := optionalIntForm(r, "workers")
	settings, err := profile.Resolve(mode, baseURL, apiKey, chunkPages, workers)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	localize := r.FormValue("localize_images") != "false"
	keepHeaderFooter := r.FormValue("keep_header_footer") == "true"

	created := make([]Job, 0, len(files))
	for _, header := range files {
		job, input, output, err := s.saveUpload(header)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		job.Mode = settings.Mode
		s.mu.Lock()
		s.jobs[job.ID] = job
		s.mu.Unlock()
		created = append(created, *job)
		go s.runJob(job.ID, input, convert.Options{
			BaseURL: settings.BaseURL, APIKey: settings.APIKey, OutputDir: output,
			ChunkPages: settings.ChunkPages, Workers: settings.Workers, LocalizeImages: localize,
			KeepHeaderFooter: keepHeaderFooter,
		})
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"jobs": created})
}

func (s *Server) saveUpload(header *multipart.FileHeader) (*Job, string, string, error) {
	name := filepath.Base(header.Filename)
	if !supported(name) {
		return nil, "", "", fmt.Errorf("不支持的文件格式: %s", name)
	}
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	jobDir := filepath.Join(s.DataDir, id)
	inputDir := filepath.Join(jobDir, "input")
	outputDir := filepath.Join(jobDir, "output")
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		return nil, "", "", err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, "", "", err
	}
	source, err := header.Open()
	if err != nil {
		return nil, "", "", err
	}
	defer source.Close()
	input := filepath.Join(inputDir, name)
	target, err := os.Create(input)
	if err != nil {
		return nil, "", "", err
	}
	_, copyErr := io.Copy(target, source)
	closeErr := target.Close()
	if copyErr != nil {
		return nil, "", "", copyErr
	}
	if closeErr != nil {
		return nil, "", "", closeErr
	}
	job := &Job{ID: id, FileName: name, Status: "queued", CreatedAt: time.Now(), Progress: convert.Progress{Stage: "queued", Message: "等待处理"}}
	return job, input, outputDir, nil
}

func (s *Server) runJob(id, input string, opts convert.Options) {
	s.update(id, func(job *Job) {
		job.Status = "running"
		job.Progress = convert.Progress{Stage: "prepare", Message: "正在检查并拆分文档"}
	})
	converter := convert.Converter{Options: opts, Progress: func(progress convert.Progress) {
		s.update(id, func(job *Job) { job.Progress = progress })
	}}
	result, err := converter.Convert(context.Background(), input)
	if err != nil {
		s.logger.Printf("job %s failed: %v", id, err)
		s.update(id, func(job *Job) { job.Status = "failed"; job.Error = err.Error() })
		return
	}
	s.update(id, func(job *Job) { job.Status = "done"; job.Result = &result })
}

func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/jobs/"), "/")
	id := parts[0]
	s.mu.RLock()
	job, ok := s.jobs[id]
	if ok {
		copy := *job
		job = &copy
	}
	s.mu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 1 || parts[1] == "" {
		writeJSON(w, http.StatusOK, job)
		return
	}
	if parts[1] != "download" || job.Status != "done" || job.Result == nil {
		http.NotFound(w, r)
		return
	}
	var path string
	switch r.URL.Query().Get("format") {
	case "md":
		path = job.Result.Markdown
	case "json":
		path = job.Result.JSON
	default:
		path = job.Result.ZIP
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(path)))
	http.ServeFile(w, r, path)
}

func (s *Server) update(id string, change func(*Job)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if job := s.jobs[id]; job != nil {
		change(job)
	}
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func supported(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pdf", ".png", ".jpg", ".jpeg", ".bmp", ".tiff", ".tif", ".jp2", ".gif", ".heic", ".heif", ".webp":
		return true
	}
	return false
}

func optionalIntForm(r *http.Request, key string) int {
	raw := strings.TrimSpace(r.FormValue(key))
	if raw == "" {
		return 0
	}
	value, _ := strconv.Atoi(raw)
	return value
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
