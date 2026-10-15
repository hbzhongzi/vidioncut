package mcpserver

// M12 字幕 MCP 工具集成测试（字幕功能更新文档 §8）：真实 ffmpeg 跑通
// export → animate → burn 全链路，断言产物与任务契约。

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"videocut/internal/config"
	"videocut/internal/service"
)

// subFixture 构造字幕测试工作目录：work/signals.json + work/edits.json + 合成源视频。
// 语料设计为 tutorial（含"首先/步骤/如何"词表命中）+ 高频词 videocut×4（密度 0.8）。
func subFixture(t *testing.T, ff, dir string, secs int) (workDir, source string) {
	t.Helper()
	source = filepath.Join(dir, "demo.mp4")
	cmd := exec.CommandContext(context.Background(), ff,
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=25",
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+strconv.Itoa(secs),
		"-t", strconv.Itoa(secs), "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac",
		"-y", source,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("合成源视频失败: %v\n%s", err, out)
	}
	workDir = filepath.Join(dir, "out")
	work := filepath.Join(workDir, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	signals := `{
  "source": ` + jsonStr(source) + `,
  "duration": ` + strconv.Itoa(secs) + `,
  "segments": [
    {"start": 0.2, "end": 1.0, "text": "大家好，欢迎使用 videocut"},
    {"start": 1.1, "end": 2.2, "text": "首先我们看一下步骤，如何配置环境"},
    {"start": 2.3, "end": 3.2, "text": "videocut 的使用方法其实很简单"}
  ],
  "words": [
    {"start": 0.2, "end": 0.5, "word": "大家"},
    {"start": 0.5, "end": 0.7, "word": "好"},
    {"start": 0.7, "end": 0.85, "word": "欢迎"},
    {"start": 0.85, "end": 0.95, "word": "使用"},
    {"start": 0.95, "end": 1.0, "word": "videocut"},
    {"start": 1.1, "end": 1.4, "word": "首先"},
    {"start": 1.4, "end": 1.7, "word": "我们"},
    {"start": 1.7, "end": 2.0, "word": "videocut"},
    {"start": 2.0, "end": 2.2, "word": "骤"},
    {"start": 2.3, "end": 2.6, "word": "videocut"},
    {"start": 2.6, "end": 2.9, "word": "的使"},
    {"start": 2.9, "end": 3.2, "word": "用方"}
  ],
  "silences": []
}`
	if err := os.WriteFile(filepath.Join(work, "signals.json"), []byte(signals), 0o644); err != nil {
		t.Fatal(err)
	}
	// 删除 1.0–1.1 的微间隙？不删——用静音段：删 2.2–2.3 之间无内容。改删一段词：
	// 删 [2.0, 2.2]（"步骤"尾）→ 保留段 [0,2.0) 与 [2.2,3.2)
	edits := `{
  "version": 1,
  "source": ` + jsonStr(source) + `,
  "items": [
    {"start": 2.0, "end": 2.2, "type": "filler_word", "text": "步骤", "reason": "测试删除项"}
  ]
}`
	if err := os.WriteFile(filepath.Join(work, "edits.json"), []byte(edits), 0o644); err != nil {
		t.Fatal(err)
	}
	return workDir, source
}

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func newSubtitleServer(t *testing.T, ff, fp string) *Server {
	t.Helper()
	cfg := config.Default()
	svc := service.NewService(cfg, ff, fp)
	return New(cfg, svc, nil)
}

func TestSubtitleTools_ExportAnimateBurn(t *testing.T) {
	ff, fp := locateBinariesV14(t)
	dir := t.TempDir()
	workDir, _ := subFixture(t, ff, dir, 4)
	s := newSubtitleServer(t, ff, fp)
	ctx := context.Background()

	// ① 外显导出（both 格式）
	res, err := s.toolExportSubtitles(ctx, callReq("videocut_export_subtitles", map[string]any{
		"base_dir": workDir,
	}))
	if err != nil || res.IsError {
		t.Fatalf("export 失败: %v %s", err, resultText(res))
	}
	for _, p := range []string{filepath.Join(workDir, "edited.srt"), filepath.Join(workDir, "edited.ass")} {
		if !fileExistsV14(p) {
			t.Fatalf("缺少产物 %s", p)
		}
	}
	srt, _ := os.ReadFile(filepath.Join(workDir, "edited.srt"))
	if !strings.Contains(string(srt), "大家好") {
		t.Errorf("SRT 应含首条字幕:\n%s", srt)
	}
	// 成片时长 = 4 - 0.2(删除段) = 3.8；cue 应为 3 条（删除段内的词被剔除）
	if !strings.Contains(resultText(res), `"cue_count":3`) {
		t.Errorf("cue_count 期望 3: %s", resultText(res))
	}

	// ② 二次加工（tutorial → marker-sweep 主技能 + keyword-highlight 补充）
	res2, err := s.toolAnimateCaptions(ctx, callReq("videocut_animate_captions", map[string]any{
		"base_dir": workDir,
	}))
	if err != nil || res2.IsError {
		t.Fatalf("animate 失败: %v %s", err, resultText(res2))
	}
	text2 := resultText(res2)
	for _, want := range []string{"marker-sweep", "keyword-highlight", "motion-plan.json"} {
		if !strings.Contains(text2, want) {
			t.Errorf("animate 结果缺少 %q: %s", want, text2)
		}
	}
	fxPath := filepath.Join(workDir, "edited_fx.ass")
	fx, ferr := os.ReadFile(fxPath)
	if ferr != nil {
		t.Fatalf("读取 edited_fx.ass 失败: %v", ferr)
	}
	if !strings.Contains(string(fx), `\kf`) {
		t.Errorf("fx.ass 应含 \\kf override tags:\n%s", fx)
	}
	// 回归（v1.9 修复）：整段保留的 cue（fixture 中 cue1 未被边界切开）
	// 也必须参与词级特效——早期实现只在跨段断开时携带词级数据，导致
	// marker-sweep/karaoke 对大多数 cue 静默落空。
	for _, line := range strings.Split(string(fx), "\n") {
		if !strings.HasPrefix(line, "Dialogue:") {
			continue
		}
		if strings.Contains(line, "大家好") && !strings.Contains(line, `\kf`) {
			t.Errorf("含关键词的整段保留 cue 应带 \\kf（词级特效落空回归）:\n%s", line)
		}
	}
	if !strings.Contains(string(fx), "&H0000D7FF") {
		t.Errorf("fx.ass 应含金色关键词 &H0000D7FF:\n%s", fx)
	}

	// ③ 内嵌烧录（用 fx.ass；整片 0→时长）
	video := filepath.Join(dir, "demo_burned_src.mp4")
	// 先造"成片"：把源复制一份当最终成片（烧录不关心内容来自哪条管线）
	if out, err := exec.CommandContext(ctx, ff, "-i", filepath.Join(dir, "demo.mp4"), "-c", "copy", "-y", video).CombinedOutput(); err != nil {
		t.Fatalf("制备成片失败: %v\n%s", err, out)
	}
	res3, err := s.toolBurnSubtitles(ctx, callReq("videocut_burn_subtitles", map[string]any{
		"video_path": video,
		"ass_path":   fxPath,
	}))
	if err != nil || res3.IsError {
		t.Fatalf("burn 提交失败: %v %s", err, resultText(res3))
	}
	var burnOut struct {
		TaskID     string `json:"task_id"`
		OutputPath string `json:"output_path"`
	}
	if err := json.Unmarshal([]byte(resultText(res3)), &burnOut); err != nil {
		t.Fatalf("解析 burn 结果失败: %v", err)
	}
	if burnOut.TaskID == "" {
		t.Fatal("burn 应返回 task_id")
	}
	tk := waitTaskFinalV14(t, s.svc, burnOut.TaskID, 60*time.Second)
	if tk.Status != "completed" {
		for _, it := range tk.Items {
			t.Logf("item: status=%s err=%s out=%s", it.Status, it.Error, it.OutputPath)
		}
		t.Fatalf("烧录任务未完成: status=%s err=%s", tk.Status, tk.Error)
	}
	burned := filepath.Join(dir, "demo_burned_src_burned.mp4")
	if !fileExistsV14(burned) {
		t.Fatalf("烧录产物不存在: %s", burned)
	}
	// 产物时长 ≈ 源时长（4s；音频直复制不改变时长）
	probe, perr := s.svc.Probe(ctx, burned)
	if perr != nil {
		t.Fatalf("探测烧录产物失败: %v", perr)
	}
	if probe.Duration < 3.8 || probe.Duration > 4.2 {
		t.Errorf("烧录产物时长 %v 期望约 4.0s", probe.Duration)
	}
	if !probe.HasAudio {
		t.Error("烧录产物应保留音轨（-c:a copy）")
	}

	// ④ 抽帧验证字幕像素真的烧进去了（对比烧录前后同帧差异；0.5s 位于首条 cue 中部）
	frameDiff := framePixelDiff(t, ff, video, burned, 0.5)
	if frameDiff < 1.0 {
		t.Errorf("烧录帧与源帧差异 %v 过小，字幕可能未渲染", frameDiff)
	}
}

// framePixelDiff 抽取 t 秒帧做 blend difference，返回 YAVG（差异均值）。
func framePixelDiff(t *testing.T, ff, plain, burned string, at float64) float64 {
	t.Helper()
	tmp := t.TempDir()
	a := filepath.Join(tmp, "a.png")
	b := filepath.Join(tmp, "b.png")
	for src, dst := range map[string]string{plain: a, burned: b} {
		if out, err := exec.CommandContext(context.Background(), ff,
			"-hide_banner", "-loglevel", "error", "-ss", strconv.FormatFloat(at, 'f', 2, 64),
			"-i", src, "-frames:v", "1", "-y", dst).CombinedOutput(); err != nil {
			t.Fatalf("抽帧失败: %v\n%s", err, out)
		}
	}
	out, err := exec.CommandContext(context.Background(), ff,
		"-hide_banner", "-loglevel", "info",
		"-i", b, "-i", a,
		"-filter_complex", "blend=all_mode=difference,signalstats,metadata=print:key=lavfi.signalstats.YAVG",
		"-f", "null", "-").CombinedOutput()
	if err != nil {
		t.Fatalf("像素对比失败: %v\n%s", err, out)
	}
	// 取最后一行的 YAVG
	lines := strings.Split(string(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if idx := strings.Index(lines[i], "YAVG="); idx >= 0 {
			v := strings.TrimSpace(lines[i][idx+5:])
			f, ferr := strconv.ParseFloat(v, 64)
			if ferr == nil {
				return f
			}
		}
	}
	return 0
}

func TestSubtitleTools_ExportMissingSignals(t *testing.T) {
	s := newSubtitleServer(t, "", "")
	res, err := s.toolExportSubtitles(context.Background(), callReq("videocut_export_subtitles", map[string]any{
		"base_dir": t.TempDir(), // 无 work/signals.json
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("缺 signals.json 应返回错误")
	}
	if !strings.Contains(resultText(res), "signals.json") {
		t.Errorf("错误信息应指向 signals.json: %s", resultText(res))
	}
}

func TestSubtitleTools_BurnMissingAss(t *testing.T) {
	ff, fp := locateBinariesV14(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "x.mp4")
	if out, err := exec.CommandContext(context.Background(), ff,
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=25", "-t", "1",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-y", src).CombinedOutput(); err != nil {
		t.Fatalf("合成失败: %v\n%s", err, out)
	}
	s := newSubtitleServer(t, ff, fp)
	res, err := s.toolBurnSubtitles(context.Background(), callReq("videocut_burn_subtitles", map[string]any{
		"video_path": src,
		"ass_path":   filepath.Join(dir, "no-such.ass"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(resultText(res), "ASS") {
		t.Errorf("ASS 缺失应结构化报错: %s", resultText(res))
	}
}
