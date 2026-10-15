package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"videocut/internal/asr"
	"videocut/internal/config"
	"videocut/internal/ffmpeg"
	"videocut/internal/mcpserver"
	"videocut/internal/server"
	"videocut/internal/service"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// 子命令：mcp-serve = Codex MCP Server（stdio）；无参 = 既有 GUI（v1.7 §19.2）
	if len(os.Args) > 1 && os.Args[1] == "mcp-serve" {
		runMCPServe()
		return
	}
	runGUI()
}

// runMCPServe 以 stdio JSON-RPC 启动 MCP Server。
// 关键约束：stdout 仅协议帧——日志一律走 stderr（§19.2，R26）。
func runMCPServe() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[videocut-mcp] ")
	log.SetOutput(os.Stderr)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("配置加载失败: %v", err)
	}

	ffmpegPath, _ := ffmpeg.FFmpegPath(cfg)
	ffprobePath, _ := ffmpeg.FFprobePath(cfg)
	if ffmpegPath == "" {
		log.Print("警告: FFmpeg 未找到（探测/裁剪工具调用时将返回错误）")
	}

	svc := service.NewService(cfg, ffmpegPath, ffprobePath)
	asrClient := asr.NewClient(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	asrClient.StartIdleReaper(ctx)

	srv := mcpserver.New(cfg, svc, asrClient)
	log.Printf("MCP Server 启动（stdio，%d 个工具就绪）", 12)
	if err := srv.Serve(ctx); err != nil {
		log.Printf("MCP Server 退出: %v", err)
		os.Exit(1)
	}
}

func runGUI() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	ffmpegPath, err := ffmpeg.FFmpegPath(cfg)
	if err != nil {
		// 非致命：允许应用启动，运行期再提示用户放置二进制
		fmt.Println("警告: FFmpeg 未找到，请将 ffmpeg.exe/ffprobe.exe 放入 resources/ffmpeg/ 后重启:", err)
	}
	ffprobePath, err := ffmpeg.FFprobePath(cfg)
	if err != nil {
		fmt.Println("警告: FFprobe 未找到:", err)
	}

	svc := service.NewService(cfg, ffmpegPath, ffprobePath)
	app := NewApp(cfg, svc, asr.NewClient(cfg))
	preview := server.NewPreviewHandler(svc)

	err = wails.Run(&options.App{
		Title:  "视频裁剪器",
		Width:  1280,
		Height: 840,
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: preview,
		},
		Bind:      []interface{}{app},
		OnStartup: app.startup,
		// 开启窗口级文件拖放（前端 window.runtime.OnFileDrop 接收多文件路径，v1.6 UI 改版）
		DragAndDrop: &options.DragAndDrop{ EnableFileDrop: true },
		Windows: &windows.Options{
			WebviewIsTransparent: false,
		},
	})
	if err != nil {
		panic(err)
	}
}
