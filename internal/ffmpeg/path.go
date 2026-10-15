package ffmpeg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"videocut/internal/config"
)

// FFmpegPath 返回 ffmpeg 可执行文件路径。
// 策略（开发文档 v1.1 §4.1）：
//   - mode=system：exec.LookPath("ffmpeg.exe")
//   - mode=bundled（默认）：依次尝试
//       1) 与 exe 同目录的 <FFmpegDir>/ffmpeg.exe
//       2) 当前工作目录（wails dev 时即项目根）下的 resources/ffmpeg/ffmpeg.exe
//     命中即返回，用于解决 wails dev 下 resources 未被复制到 build/bin 的问题。
func FFmpegPath(cfg *config.Config) (string, error) {
	return resolve("ffmpeg.exe", cfg)
}

// FFprobePath 返回 ffprobe 可执行文件路径，逻辑同 FFmpegPath。
func FFprobePath(cfg *config.Config) (string, error) {
	return resolve("ffprobe.exe", cfg)
}

func resolve(bin string, cfg *config.Config) (string, error) {
	if cfg.FFmpegMode == "system" {
		p, err := exec.LookPath(bin)
		if err != nil {
			return "", fmt.Errorf("%s (system mode): not found in PATH: %w", bin, err)
		}
		return p, nil
	}

	dir := cfg.FFmpegDir
	if dir == "" {
		dir = "ffmpeg"
	}

	var candidates []string

	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), dir, bin))
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "resources", "ffmpeg", bin))
		// 某些布局 exe 与 resources 在同一根
		candidates = append(candidates, filepath.Join(wd, dir, bin))
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("%s (bundled mode): not found; searched: %v", bin, candidates)
}
