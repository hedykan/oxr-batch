package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"oxr-batch/internal/convert"
	"oxr-batch/internal/profile"
	"oxr-batch/internal/server"
)

const version = "0.2.0"

func main() {
	if err := newRootCommand().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "oxr-batch",
		Short:         "批量把 PDF 与图片转换为 Markdown、JSON 和本地插图",
		Long:          "OXR Batch 是一个支持免费 OXR 与 SoMark 正式 API 双模式的本地文档转换工作台。",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.SetVersionTemplate("OXR Batch {{.Version}}\n")
	root.AddCommand(newServeCommand(), newConvertCommand())
	return root
}

func newServeCommand() *cobra.Command {
	var addr string
	var dataDir string
	command := &cobra.Command{
		Use:     "serve",
		Aliases: []string{"web"},
		Short:   "启动本地 Web 工作台",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			app := server.New(dataDir, log.Default())
			log.Printf("OXR Batch is ready at http://%s", addr)
			return http.ListenAndServe(addr, app.Handler())
		},
	}
	command.Flags().StringVar(&addr, "addr", "127.0.0.1:8787", "本地监听地址")
	command.Flags().StringVar(&dataDir, "data", "./data", "任务与输出数据目录")
	return command
}

func newConvertCommand() *cobra.Command {
	var mode string
	var baseURL string
	var apiKey string
	var out string
	var chunkPages int
	var workers int
	var localize bool
	var keepHeaders bool

	command := &cobra.Command{
		Use:   "convert [选项] <PDF或图片> [...]",
		Short: "转换一个或多个 PDF、扫描件或图片",
		Args:  cobra.MinimumNArgs(1),
		Example: `  # 免费模式
  oxr-batch convert --mode free --out ./output book.pdf

  # 正式 API 模式
  oxr-batch convert --mode api --api-key sk-... --out ./output book.pdf`,
		RunE: func(_ *cobra.Command, inputs []string) error {
			settings, err := profile.Resolve(mode, baseURL, apiKey, chunkPages, workers)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(out, 0o755); err != nil {
				return err
			}
			failed := make([]string, 0)
			for _, input := range inputs {
				outputDir := out
				if len(inputs) > 1 {
					outputDir = filepath.Join(out, strings.TrimSuffix(filepath.Base(input), filepath.Ext(input)))
				}
				converter := convert.Converter{
					Options: convert.Options{
						BaseURL: settings.BaseURL, APIKey: settings.APIKey, OutputDir: outputDir,
						ChunkPages: settings.ChunkPages, Workers: settings.Workers,
						LocalizeImages: localize, KeepHeaderFooter: keepHeaders,
					},
					Progress: func(progress convert.Progress) {
						if progress.Total > 0 {
							log.Printf("[%d/%d] %s", progress.Done, progress.Total, progress.Message)
						} else {
							log.Printf("%s", progress.Message)
						}
					},
				}
				result, err := converter.Convert(context.Background(), input)
				if err != nil {
					failed = append(failed, fmt.Sprintf("%s: %v", input, err))
					log.Printf("转换失败 %s: %v", input, err)
					continue
				}
				log.Printf("完成 %d 页\nMarkdown: %s\nJSON: %s\nZIP: %s", result.Pages, result.Markdown, result.JSON, result.ZIP)
			}
			if len(failed) > 0 {
				return fmt.Errorf("%d 个文件转换失败：%s", len(failed), strings.Join(failed, "; "))
			}
			return nil
		},
	}
	flags := command.Flags()
	flags.StringVar(&mode, "mode", env("OXR_MODE", profile.FreeMode), "服务模式：free 或 api")
	flags.StringVar(&baseURL, "base-url", env("OXR_BASE_URL", ""), "覆盖默认 OXR API 地址")
	flags.StringVar(&apiKey, "api-key", env("OXR_API_KEY", ""), "OXR API Key；api 模式必填")
	flags.StringVarP(&out, "out", "o", "./output", "输出目录")
	flags.IntVar(&chunkPages, "chunk-pages", 0, "每批页数（默认 free=20，api=300）")
	flags.IntVar(&workers, "workers", 0, "并发请求数（默认 free=1，api=4）")
	flags.BoolVar(&localize, "localize-images", true, "下载 OXR 临时图片到本地")
	flags.BoolVar(&keepHeaders, "keep-header-footer", false, "保留页眉与页脚")
	return command
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
