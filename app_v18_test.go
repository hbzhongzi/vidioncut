package main

// M12 字幕 GUI 绑定链路测试（字幕功能更新文档 §8）：真实 ffmpeg 跑通
// 导出 → 烧录 → 特效全链路，确保前端对接无链路断裂。

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"videocut/internal/asr"
	"videocut/internal/config"
	"videocut/internal/edits"
	"videocut/internal/service"
	"videocut/internal/subtitles"
)

func TestConfigGetIncludesSubtitleDefaults(t *testing.T) {
	cfg := config.Default()
	app := NewApp(cfg, nil, nil)
	got := app.ConfigGet()
	if got["subtitles_mode"] != "sidecar" {
		t.Fatalf("subtitles_mode = %v, want sidecar", got["subtitles_mode"])
	}
	if got["subtitles_format"] != "both" {
		t.Fatalf("subtitles_format = %v, want both", got["subtitles_format"])
	}
	if got["subtitles_style"] != "default" {
		t.Fatalf("subtitles_style = %v, want default", got["subtitles_style"])
	}
	if got["subtitles_max_chars"] != 20 || got["subtitles_max_lines"] != 2 {
		t.Fatalf("subtitle layout defaults = %v/%v, want 20/2", got["subtitles_max_chars"], got["subtitles_max_lines"])
	}
}

// subWorkDir 构造字幕测试工作目录（signals + edits），返回 workDir 与源视频路径。
func subWorkDir(t *testing.T, ff, dir string, secs int) (string, string) {
	t.Helper()
	src := genSource(t, ff, dir, secs)
	workDir := filepath.Join(dir, "out")
	work := filepath.Join(workDir, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	signals := `{
  "source": ` + jsonQ(src) + `,
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
	edits := `{
  "version": 1,
  "source": ` + jsonQ(src) + `,
  "items": [{"start": 2.0, "end": 2.2, "type": "filler_word", "text": "步骤", "reason": "测试"}]
}`
	if err := os.WriteFile(filepath.Join(work, "edits.json"), []byte(edits), 0o644); err != nil {
		t.Fatal(err)
	}
	return workDir, src
}

func jsonQ(s string) string {
	return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"`
}

func newSubtitleApp(t *testing.T, ff, fp string) *App {
	t.Helper()
	cfg := config.Default()
	svc := service.NewService(cfg, ff, fp)
	return NewApp(cfg, svc, asr.NewClient(cfg))
}

// TestAICutExportSubtitles_GUI 外显导出：产物落成片同目录 + cues 回传 + 跨段断开标注。
func TestAICutExportSubtitles_GUI(t *testing.T) {
	ff, fp := locateFF(t)
	dir := t.TempDir()
	workDir, _ := subWorkDir(t, ff, dir, 4)
	app := newSubtitleApp(t, ff, fp)

	view, err := app.AICutExportSubtitles(workDir, "", "both", "talking-head", 20, 2)
	if err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	if view.SRTPath == "" || view.ASSPath == "" {
		t.Fatalf("both 模式应产出 SRT+ASS: %+v", view)
	}
	// 产物应与成片同目录（成片 = demo_cut.mp4 在 dir 下）
	if filepath.Dir(view.SRTPath) != dir {
		t.Errorf("SRT 应落成片同目录 %s，实际 %s", dir, filepath.Dir(view.SRTPath))
	}
	if view.CueCount != 3 {
		t.Errorf("cue 数 %d 期望 3", view.CueCount)
	}
	if view.FinalDuration < 3.7 || view.FinalDuration > 3.9 {
		t.Errorf("成片时长 %v 期望约 3.8s（4-0.2 删除段）", view.FinalDuration)
	}
	// 跨段断开：cue "首先…环境" 在 2.0 边界被切开 → 至少 1 条 SplitAt
	split := 0
	for _, c := range view.Cues {
		if c.SplitAt {
			split++
		}
	}
	if split < 1 {
		t.Error("跨段边界应产生词级断开 cue（SplitAt）")
	}
	// ASS 用 talking-head 预设（56 号字）
	ass, aerr := os.ReadFile(view.ASSPath)
	if aerr != nil {
		t.Fatal(aerr)
	}
	if !strings.Contains(string(ass), "Microsoft YaHei,56") {
		t.Errorf("ASS 应含 talking-head 预设 56 号字:\n%s", ass)
	}
	// 不变式：回传 cues 必过自检
	if verr := subtitles.ValidateCues(view.Cues, view.FinalDuration); verr != nil {
		t.Errorf("cues 自检失败: %v", verr)
	}
}

// TestAICutBurnSubtitles_GUI 内嵌烧录：task_id 异步契约 + 产物时长/音轨。
func TestAICutBurnSubtitles_GUI(t *testing.T) {
	ff, fp := locateFF(t)
	dir := t.TempDir()
	workDir, _ := subWorkDir(t, ff, dir, 4)
	app := newSubtitleApp(t, ff, fp)
	// 覆盖一个不可用编码器，验证真实烧录失败时自动回退 libx264。
	app.cfg.SubBurnEncoder = "h264_encoder_missing"

	view, err := app.AICutExportSubtitles(workDir, "", "ass", "default", 20, 2)
	if err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	// 造"成片"（源复制）作为烧录输入
	final := filepath.Join(dir, "demo_cut.mp4")
	if out, err := exec.CommandContext(context.Background(), ff,
		"-i", filepath.Join(dir, "demo.mp4"), "-c", "copy", "-y", final).CombinedOutput(); err != nil {
		t.Fatalf("制备成片失败: %v\n%s", err, out)
	}
	taskID, err := app.AICutBurnSubtitles(final, view.ASSPath, "", 0, "")
	if err != nil {
		t.Fatalf("烧录提交失败: %v", err)
	}
	tk := waitTask(t, app.svc, taskID, 60*time.Second)
	if tk.Status != "completed" {
		t.Fatalf("烧录任务未完成: %s %s", tk.Status, tk.Error)
	}
	burned := filepath.Join(dir, "demo_cut_burned.mp4")
	if !fileExists(burned) {
		t.Fatalf("烧录产物不存在: %s", burned)
	}
	probe, perr := app.svc.Probe(app.appContext(), burned)
	if perr != nil {
		t.Fatal(perr)
	}
	if probe.Duration < 3.8 || probe.Duration > 4.2 {
		t.Errorf("烧录产物时长 %v 期望约 4.0s", probe.Duration)
	}
	if !probe.HasAudio {
		t.Error("音轨应保留（-c:a copy）")
	}
}

// TestAICutAnimateCaptions_GUI 二次加工：tutorial 语料 → marker-sweep + keyword-highlight。
func TestAICutAnimateCaptions_GUI(t *testing.T) {
	ff, fp := locateFF(t)
	dir := t.TempDir()
	workDir, _ := subWorkDir(t, ff, dir, 4)
	app := newSubtitleApp(t, ff, fp)

	view, err := app.AICutAnimateCaptions(workDir, "", "", nil, 0)
	if err != nil {
		t.Fatalf("animate 失败: %v", err)
	}
	if view.Static {
		t.Fatal("教程语料不应落入静态态")
	}
	if strings.Join(view.Selected, ",") != "marker-sweep,keyword-highlight" {
		t.Fatalf("selected = %v 期望 [marker-sweep keyword-highlight]", view.Selected)
	}
	if view.Analysis == nil || view.Analysis.ContentType != "tutorial" {
		t.Fatalf("analysis 应识别 tutorial: %+v", view.Analysis)
	}
	if view.FXPath == "" || view.PlanPath == "" {
		t.Fatal("应产出 edited_fx.ass + motion-plan.json")
	}
	fx, ferr := os.ReadFile(view.FXPath)
	if ferr != nil {
		t.Fatal(ferr)
	}
	if !strings.Contains(string(fx), `\kf`) {
		t.Errorf("fx.ass 应含 \\kf:\n%s", fx)
	}
	// motion-plan.json 落盘且含 reasoning（审计契约）
	plan, perr := os.ReadFile(view.PlanPath)
	if perr != nil {
		t.Fatal(perr)
	}
	if !strings.Contains(string(plan), "reasoning") {
		t.Error("motion-plan.json 应含 reasoning")
	}
}

// TestAICutAnimateCaptions_ExplicitOverride 显式覆盖通道（skills 参数跳过自动选择）。
func TestAICutAnimateCaptions_ExplicitOverride(t *testing.T) {
	ff, fp := locateFF(t)
	dir := t.TempDir()
	workDir, _ := subWorkDir(t, ff, dir, 4)
	app := newSubtitleApp(t, ff, fp)

	view, err := app.AICutAnimateCaptions(workDir, "", "auto", []string{"karaoke-fill"}, 0)
	if err != nil {
		t.Fatalf("animate 失败: %v", err)
	}
	if len(view.Selected) != 1 || view.Selected[0] != "karaoke-fill" {
		t.Fatalf("显式覆盖应仅选 karaoke-fill: %v", view.Selected)
	}
}

// TestAICutAnimateCaptions_Off 静态模式（mode=off）：合法态，不产 fx.ass。
func TestAICutAnimateCaptions_Off(t *testing.T) {
	ff, fp := locateFF(t)
	dir := t.TempDir()
	workDir, _ := subWorkDir(t, ff, dir, 4)
	app := newSubtitleApp(t, ff, fp)

	view, err := app.AICutAnimateCaptions(workDir, "", "off", nil, 0)
	if err != nil {
		t.Fatalf("animate off 失败: %v", err)
	}
	if !view.Static || view.FXPath != "" {
		t.Errorf("off 模式应为静态态且不产 fx.ass: %+v", view)
	}
}

// TestAICutExportSubtitles_NoSignals 未转录时给出明确指引（§3.2 降级）。
func TestAICutExportSubtitles_NoSignals(t *testing.T) {
	ff, fp := locateFF(t)
	app := newSubtitleApp(t, ff, fp)
	_, err := app.AICutExportSubtitles(t.TempDir(), "", "", "", 0, 0)
	if err == nil {
		t.Fatal("缺 signals.json 应报错")
	}
	if !strings.Contains(err.Error(), "signals.json") {
		t.Errorf("错误应指向 signals.json: %v", err)
	}
}

// TestAICutSubmit_WritesBackLedger M12 语义回归：AICutSubmit 必须把用户
// 勾选后的最终账本写回 work/edits.json——字幕步骤（AICutExportSubtitles）
// 读盘获取保留段，若只提交不写盘，字幕会按"零删"处理（源时间轴），
// 与成片不符（真实端到端测试暴露的偏差）。
func TestAICutSubmit_WritesBackLedger(t *testing.T) {
	ff, fp := locateFF(t)
	dir := t.TempDir()
	src := genSource(t, ff, dir, 45)
	srt := "1\n00:00:00,000 --> 00:00:10,000\n大家好\n"
	signals := `{"source":"` + filepath.ToSlash(src) + `","duration":45,` +
		`"words":[{"start":3.0,"end":3.4,"word":"嗯"}],` +
		`"silences":[{"start":40,"end":41.5}]}`
	srv, _ := fakeASR(t, srt, signals)
	cfg := config.Default()
	u, perr := url.Parse(srv.URL)
	if perr != nil {
		t.Fatal(perr)
	}
	port, _ := strconv.Atoi(u.Port())
	cfg.ASRHost = u.Hostname()
	cfg.ASRPort = port
	cfg.ASREnabled = true
	cfg.TimeoutSeconds = 120

	svc := service.NewService(cfg, ff, fp)
	app := NewApp(cfg, svc, asr.NewClient(cfg))

	// 生成（记录 lastWorkDir）
	view, err := app.AICutGenerate(src, dir)
	if err != nil {
		t.Fatalf("AICutGenerate 失败: %v", err)
	}
	if len(view.Ledger.Items) != 2 {
		t.Fatalf("候选 %d 期望 2", len(view.Ledger.Items))
	}

	// 用户只勾选第 1 项（口水词），取消静音段
	enabled := []edits.EditItem{view.Ledger.Items[0]}
	if _, err := app.AICutSubmit(src, enabled, "reencode"); err != nil {
		t.Fatalf("AICutSubmit 失败: %v", err)
	}

	// 写回的账本应只含启用项（而非生成时的全量候选）
	back, rerr := app.ReadEditsLedger(filepath.Join(dir, "work", "edits.json"))
	if rerr != nil {
		t.Fatalf("读回账本失败: %v", rerr)
	}
	if len(back.Ledger.Items) != 1 {
		t.Fatalf("写回账本 %d 项，期望 1 项（仅用户勾选）", len(back.Ledger.Items))
	}
	if back.Ledger.Items[0].Type != edits.TypeFillerWord {
		t.Errorf("写回项类型 %q 期望 filler_word", back.Ledger.Items[0].Type)
	}
	// 字幕步骤据此重对齐：保留段应为全集减口水词段
	subView, serr := app.AICutExportSubtitles(dir, "", "srt", "default", 20, 2)
	if serr != nil {
		t.Fatalf("字幕导出失败: %v", serr)
	}
	if subView.FinalDuration < 44 || subView.FinalDuration > 45 {
		t.Errorf("成片时长 %v 期望约 44.6s（仅删 0.4s 口水词）", subView.FinalDuration)
	}
}
