package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"videocut/internal/asr"
	"videocut/internal/edits"
)

// registerASRTools 注册 M9 工具（§19.3）：process / suggest_edits / set_hotwords。
// sidecar 未配置或缺失时，工具仍注册但返回 asr_runtime_missing + 安装指引。
func (s *Server) registerASRTools() {
	s.addTool(mcp.NewTool("videocut_process",
		mcp.WithDescription("转录视频 + 静音检测（faster-whisper sidecar）：输出 <output_base>/work/transcript.srt 与 signals.json"),
		mcp.WithString("video_path", mcp.Required(), mcp.Description("输入视频绝对路径")),
		mcp.WithString("output_base", mcp.Required(), mcp.Description("输出目录（其下 work/ 放 SRT 与 signals.json）")),
		mcp.WithArray("hotwords", mcp.Description("热词数组（提升专名识别；缺省用 asr.hotwords）"), mcp.WithStringItems()),
	), s.toolProcess)

	s.addTool(mcp.NewTool("videocut_suggest_edits",
		mcp.WithDescription("机械扫描候选删除项：读 <base_dir>/work/signals.json，按静音段与口水词词表生成删词账本候选（不写盘，由调用方决定）"),
		mcp.WithString("base_dir", mcp.Required(), mcp.Description("transcribe 输出目录（含 work/signals.json）")),
		mcp.WithNumber("min_silence", mcp.Description("静音判定阈值（秒，默认 1.0）")),
	), s.toolSuggestEdits)

	s.addTool(mcp.NewTool("videocut_set_hotwords",
		mcp.WithDescription("设置 ASR 热词表（透传 sidecar；持久化请写入 config.yaml 的 asr.hotwords）"),
		mcp.WithArray("hotwords", mcp.Required(), mcp.Description("热词数组"), mcp.WithStringItems()),
	), s.toolSetHotwords)
}

// ---- process ----

func (s *Server) toolProcess(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.asr == nil {
		return failResult(codeASRRuntimeMiss, asrMissingHint, "ASR sidecar 未启用"), nil
	}
	video, err := req.RequireString("video_path")
	if err != nil {
		return failResult(codeInvalidParams, "", "缺少参数 video_path"), nil
	}
	base, err := req.RequireString("output_base")
	if err != nil {
		return failResult(codeInvalidParams, "", "缺少参数 output_base"), nil
	}
	hotwords := req.GetStringSlice("hotwords", nil)

	res, terr := s.asr.Transcribe(ctx, asr.TranscribeRequest{VideoPath: video, OutputBase: base, Hotwords: hotwords})
	if terr != nil {
		if asr.IsRuntimeMissing(terr) {
			return failResult(codeASRRuntimeMiss, asrMissingHint, terr.Error()), nil
		}
		return failResult(codeTranscribeFailed, "", terr.Error()), nil
	}
	return jsonResult(map[string]any{
		"srt_path":      res.SRTPath,
		"signals_path":  res.SignalsPath,
		"duration":      res.Duration,
		"segments":      res.Segments,
		"silences":      res.Silences,
		"next":          "videocut_suggest_edits（扫描候选）→ 生成 edits.json → videocut_validate_edits",
	})
}

// ---- suggest_edits ----

// defaultFillers 口水词词表（中文口播常见；作为候选由人工/Codex 复核）。
var defaultFillers = []string{
	"嗯", "呃", "唔", "诶", "那个", "这个", "然后", "就是", "就是说",
	"其实", "对吧", "是吧", "OK", "okay",
}

// signalsFile serve.py 产出的 signals.json 结构（§19.5）。
type signalsFile struct {
	Source   string  `json:"source"`
	Duration float64 `json:"duration"`
	Segments []struct {
		Start float64 `json:"start"`
		End   float64 `json:"end"`
		Text  string  `json:"text"`
	} `json:"segments"`
	Words []struct {
		Start float64 `json:"start"`
		End   float64 `json:"end"`
		Word  string  `json:"word"`
	} `json:"words"`
	Silences []struct {
		Start float64 `json:"start"`
		End   float64 `json:"end"`
	} `json:"silences"`
}

func (s *Server) toolSuggestEdits(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	base, err := req.RequireString("base_dir")
	if err != nil {
		return failResult(codeInvalidParams, "", "缺少参数 base_dir"), nil
	}
	minSilence := req.GetFloat("min_silence", 1.0)

	items, source, duration, err := BuildCandidateItems(base, minSilence)
	if err != nil {
		return failResult(codeInvalidParams, "", err.Error()), nil
	}
	counts := map[string]int{}
	for _, it := range items {
		counts[it.Type]++
	}
	return jsonResult(map[string]any{
		"source":   source,
		"duration": duration,
		"items":    items,
		"counts":   counts,
		"note":     "候选未写盘：请审阅后生成 edits.json（version=1），再 videocut_validate_edits → videocut_trim_batch 按保留段剪切",
	})
}

// BuildCandidateItems 机械扫描候选删除项（静音段 + 口水词），与 videocut_suggest_edits
// 及 GUI 的 AICutGenerate 共用（§19.5）。baseDir 为 transcription 输出目录（其下 work/signals.json）。
// 返回候选项、源路径（signals.json 记录）、源时长。
//
// 不变式（v1.8 修复）：产物保证满足 edits.ValidateEdits——按 start 升序、互不重叠。
// 真实 faster-whisper 词级时间戳相邻 token 边界经常交叠（DTW 对齐伪影），口水词
// 也可能整体落在静音段内；原样入账本会被"同源区间不重叠"规则整本拒绝，
// AI 粗剪链路在真实素材上直接断裂（假 sidecar 测试无词级交叠，从未暴露）。
// 因此扫描后统一走 coalesceItems 求并集。
func BuildCandidateItems(baseDir string, minSilence float64) ([]edits.EditItem, string, float64, error) {
	sigPath := filepath.Join(baseDir, "work", "signals.json")
	data, err := os.ReadFile(sigPath)
	if err != nil {
		return nil, "", 0, fmt.Errorf("读取 signals.json 失败（%s）：请先运行 videocut_process", sigPath)
	}
	var sig signalsFile
	if jerr := json.Unmarshal(data, &sig); jerr != nil {
		return nil, "", 0, fmt.Errorf("signals.json 解析失败: %w", jerr)
	}

	// min_silence 防御：NaN/Inf 使比较恒假导致静默失效；≤0 会把每个微间隙
	// （含词间零长度缝）都判为静音，产生候选风暴与大量碎片保留段。
	if math.IsNaN(minSilence) || math.IsInf(minSilence, 0) {
		return nil, "", 0, fmt.Errorf("min_silence 必须是有限数值（当前 %v）", minSilence)
	}
	if minSilence <= 0 {
		return nil, "", 0, fmt.Errorf("min_silence 必须大于 0（当前 %v）：0 或负值会把每个微间隙都判为静音", minSilence)
	}
	if minSilence > 600 {
		return nil, "", 0, fmt.Errorf("min_silence 过大（%v>600s）：不会产生任何候选", minSilence)
	}

	items := make([]edits.EditItem, 0, 8)
	// 1. 静音段
	for _, si := range sig.Silences {
		if si.End-si.Start >= minSilence {
			items = append(items, edits.EditItem{
				Start: si.Start, End: si.End, Type: edits.TypeSilence,
				Reason: fmt.Sprintf("静音≥%.1fs（VAD）", minSilence),
			})
		}
	}
	// 2. 口水词（词级时间戳命中词表）
	for _, w := range sig.Words {
		if !matchFiller(w.Word) {
			continue
		}
		start, end := w.Start, w.End
		if start < 0 {
			start = 0 // 时钟抖动可能产出负值起点
		}
		if end <= start {
			continue // 零/负时长 token（标点对齐伪影）无法精确剪切，跳过
		}
		items = append(items, edits.EditItem{
			Start: start, End: end, Type: edits.TypeFillerWord,
			Text: w.Word, Reason: "口水词词表命中",
		})
	}
	sortEditItems(items)
	items = coalesceItems(items)

	source := sig.Source
	if source == "" {
		source = filepath.Join(baseDir, "source.mp4") // 兜底占位，调用方应修正
	}
	return items, source, sig.Duration, nil
}

// coalesceItems 合并相互重叠的候选区间（求并集），保证产物满足 ValidateEdits。
// 输入必须已按 start 升序。相接（prev.End==next.Start）不合并，保留颗粒度。
// 合并后：类型按 silence>filler_word>retake>manual 取舍（区间主体通常是静音），
// text/reason 去重拼接，便于人工复核时看清这一刀为什么切。
func coalesceItems(items []edits.EditItem) []edits.EditItem {
	if len(items) < 2 {
		return items
	}
	out := make([]edits.EditItem, 0, len(items))
	cur := items[0]
	for _, it := range items[1:] {
		if it.Start < cur.End { // 重叠
			if it.End > cur.End {
				cur.End = it.End
			}
			if it.Text != "" && !strings.Contains(cur.Text, it.Text) {
				cur.Text = strings.TrimSpace(cur.Text + " " + it.Text)
			}
			if it.Reason != "" && !strings.Contains(cur.Reason, it.Reason) {
				cur.Reason = strings.TrimSpace(cur.Reason + "；" + it.Reason)
			}
			cur.Type = pickEditType(cur.Type, it.Type)
			continue
		}
		out = append(out, cur)
		cur = it
	}
	return append(out, cur)
}

// pickEditType 重叠区间合并后的类型取舍。
func pickEditType(a, b string) string {
	priority := map[string]int{
		edits.TypeSilence:    3,
		edits.TypeFillerWord: 2,
		edits.TypeRetake:     1,
		edits.TypeManual:     0,
	}
	if priority[a] >= priority[b] {
		return a
	}
	return b
}

// fillerPunct 词 token 两侧需剥离的标点。Whisper 中文输出常把标点粘在词上
// （"那个，""嗯！""就是啊"），不剥离则整词匹配全部落空——这是真实素材上
// 口水词命中率低的主要原因（v1.8 修复）。
const fillerPunct = " \t\r\n,，.。、!！?？;；:：~～…·\"'“”‘’()（）[]【】{}"

// matchFiller 词命中词表。
// 中文词用包含匹配：多字词可能带语气后缀（"然后呢"含"然后"），且 tokenizer
// 偶有拆分；英文词（ok/okay）用整词大小写不敏感匹配，避免 "look" 误命中 "ok"。
func matchFiller(word string) bool {
	w := strings.Trim(word, fillerPunct)
	if w == "" {
		return false
	}
	lw := strings.ToLower(w)
	for _, f := range defaultFillers {
		f = strings.Trim(f, fillerPunct)
		if f == "" {
			continue
		}
		if isASCIIWord(f) {
			if lw == strings.ToLower(f) {
				return true
			}
			continue
		}
		if strings.Contains(w, f) {
			return true
		}
	}
	return false
}

// isASCIIWord 判断是否纯 ASCII 词（英文词表项走整词匹配）。
func isASCIIWord(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			return false
		}
	}
	return true
}

func sortEditItems(items []edits.EditItem) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].Start < items[j-1].Start; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

// ---- set_hotwords ----

func (s *Server) toolSetHotwords(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.asr == nil {
		return failResult(codeASRRuntimeMiss, asrMissingHint, "ASR sidecar 未启用"), nil
	}
	hotwords, err := req.RequireStringSlice("hotwords")
	if err != nil {
		return failResult(codeInvalidParams, "", "缺少参数 hotwords"), nil
	}
	if serr := s.asr.SetHotwords(ctx, hotwords); serr != nil {
		if asr.IsRuntimeMissing(serr) {
			return failResult(codeASRRuntimeMiss, asrMissingHint, serr.Error()), nil
		}
		return failResult(codeTranscribeFailed, "", serr.Error()), nil
	}
	return jsonResult(map[string]any{
		"ok":       true,
		"hotwords": hotwords,
		"note":     "已透传 sidecar；重启后失效，如需持久化请写入 config.yaml 的 asr.hotwords",
	})
}
