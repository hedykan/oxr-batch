package profile

import (
	"fmt"
	"strings"
)

const (
	FreeMode = "free"
	APIMode  = "api"
	FreeURL  = "https://free-oxr.somark.cn/api/v1"
	APIURL   = "https://somark.ai/api/v1"
)

type Settings struct {
	Mode       string
	BaseURL    string
	APIKey     string
	ChunkPages int
	Workers    int
}

func Resolve(mode, baseURL, apiKey string, chunkPages, workers int) (Settings, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	baseURL = strings.TrimSpace(baseURL)
	apiKey = strings.TrimSpace(apiKey)
	if mode == "" {
		mode = FreeMode
	}
	settings := Settings{Mode: mode, BaseURL: baseURL, APIKey: apiKey, ChunkPages: chunkPages, Workers: workers}
	switch mode {
	case FreeMode:
		if settings.BaseURL == "" {
			settings.BaseURL = FreeURL
		}
		if settings.APIKey == "" {
			settings.APIKey = "dummy"
		}
		if settings.ChunkPages <= 0 {
			settings.ChunkPages = 20
		}
		if settings.Workers <= 0 {
			settings.Workers = 1
		}
	case APIMode:
		if settings.BaseURL == "" {
			settings.BaseURL = APIURL
		}
		if settings.APIKey == "" || settings.APIKey == "dummy" {
			return Settings{}, fmt.Errorf("正式 API 模式需要有效的 API Key（sk-...）")
		}
		if settings.ChunkPages <= 0 {
			settings.ChunkPages = 300
		}
		if settings.Workers <= 0 {
			settings.Workers = 4
		}
	default:
		return Settings{}, fmt.Errorf("不支持的模式 %q：可选 free 或 api", mode)
	}
	if settings.ChunkPages < 1 || settings.ChunkPages > 300 {
		return Settings{}, fmt.Errorf("每批页数必须在 1–300 之间")
	}
	if settings.Workers < 1 || settings.Workers > 4 {
		return Settings{}, fmt.Errorf("并发数必须在 1–4 之间")
	}
	return settings, nil
}
