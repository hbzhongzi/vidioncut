// 桥接层：封装 Wails 自动生成的绑定方法（window.go.main.App.*）与运行时事件。
// 不依赖生成的 wailsjs 文件，直接调用注入的全局对象，降低构建耦合。

import type {
  TrimRequest,
  Task,
  ProbeResult,
  BatchRequest,
  MergeRequest,
  EditsView,
  EditsLedger,
  EditItem,
  ASRStatus,
  CallEntry,
  SubtitlesView,
  MotionPlanView,
} from '../types'

declare global {
  interface Window {
    go: {
      main: {
        App: {
          Trim(req: TrimRequest): Promise<string>
          TrimBatch(req: BatchRequest): Promise<string>
          Merge(req: MergeRequest): Promise<string>
          GetTask(id: string): Promise<Task>
          ListTasks(): Promise<Task[]>
          Cancel(id: string): Promise<void>
          Probe(path: string): Promise<ProbeResult>
          SelectFile(): Promise<string>
          SelectOutputDir(): Promise<string>
          RevealInFolder(path: string): Promise<void>
          ConfigGet(): Promise<Record<string, unknown>>
          ConfigSet(key: string, value: string): Promise<void>
          // M11：AI 粗剪 / Codex 面板
          ASRStatus(): Promise<ASRStatus>
          SetASRHotwords(words: string[]): Promise<void>
          ReadMCPCallLog(limit: number): Promise<CallEntry[]>
          LoadEditsFile(): Promise<string>
          ReadEditsLedger(path: string): Promise<EditsView>
          AICutGenerate(source: string, workDir: string): Promise<EditsView>
          AICutCompute(source: string, items: EditItem[]): Promise<EditsView>
          AICutSubmit(source: string, items: EditItem[], strategy: string): Promise<string>
          MergeCutSegments(taskID: string, outputPath: string): Promise<string>
          ExportTranscriptFile(baseDir: string, format: string): Promise<string>
          // M12：粗剪后字幕（app_v18.go）
          AICutExportSubtitles(workDir: string, finalVideo: string, format: string, stylePreset: string, maxChars: number, maxLines: number): Promise<SubtitlesView>
          AICutBurnSubtitles(videoPath: string, assPath: string, encoder: string, crf: number, preset: string): Promise<string>
          AICutAnimateCaptions(workDir: string, finalVideo: string, mode: string, skills: string[], maxSkills: number): Promise<MotionPlanView>
        }
      }
    }
    // Wails 运行时注入的全局对象（事件通信 + 拖放）
    runtime: {
      EventsOn(event: string, callback: (...data: any[]) => void): void
      EventsOff(event: string, callback?: (...data: any[]) => void): void
      EventsEmit(event: string, ...data: any[]): void
      // 文件拖放（需 Go 侧 DragAndDrop.EnableFileDrop=true）；useDropTarget=false 整窗可放
      OnFileDrop(callback: (x: number, y: number, paths: string[]) => void, useDropTarget?: boolean): void
    }
  }
}

// ---- 事件（v1.4 §18.6：载荷扩展 kind / item_index / completed / failed） ----

export interface ProgressEvent {
  task_id: string
  kind?: string
  item_index?: number
  percent: number
  item_percent?: number // v1.6：批量单项进度（与聚合 percent 并列下发）
  speed?: string
  out_time?: number
}
export interface DoneEvent {
  task_id: string
  kind?: string
  output_path?: string
  duration: number
  completed?: number
  failed?: number
}
export interface ErrorEvent {
  task_id: string
  kind?: string
  item_index?: number
  message: string
}
export interface ItemDoneEvent {
  task_id: string
  item_index: number
  output_path: string
  duration: number
}
export interface NoticeEvent {
  task_id: string
  message: string
}

export function OnProgress(cb: (p: ProgressEvent) => void) {
  window.runtime.EventsOn('trim:progress', cb as any)
}
export function OnDone(cb: (p: DoneEvent) => void) {
  window.runtime.EventsOn('trim:done', cb as any)
}
export function OnError(cb: (p: ErrorEvent) => void) {
  window.runtime.EventsOn('trim:error', cb as any)
}
export function OnItemDone(cb: (p: ItemDoneEvent) => void) {
  window.runtime.EventsOn('trim:item_done', cb as any)
}
export function OnNotice(cb: (p: NoticeEvent) => void) {
  window.runtime.EventsOn('merge:notice', cb as any)
}

// 窗口级文件拖放：回调解构为 (x, y, paths)。需 main.go 开启 DragAndDrop.EnableFileDrop。
export function OnFileDrop(cb: (paths: string[]) => void) {
  window.runtime.OnFileDrop((_x: number, _y: number, paths: string[]) => {
    if (Array.isArray(paths) && paths.length) cb(paths)
  }, false)
}

const App = window.go.main.App

export function SelectFile(): Promise<string> {
  return App.SelectFile()
}

export function SelectOutputDir(): Promise<string> {
  return App.SelectOutputDir()
}

export function Probe(path: string): Promise<ProbeResult> {
  return App.Probe(path)
}

export function Trim(req: TrimRequest): Promise<string> {
  return App.Trim(req)
}

export function TrimBatch(req: BatchRequest): Promise<string> {
  return App.TrimBatch(req)
}

export function Merge(req: MergeRequest): Promise<string> {
  return App.Merge(req)
}

export function GetTask(id: string): Promise<Task> {
  return App.GetTask(id)
}

export function ListTasks(): Promise<Task[]> {
  return App.ListTasks()
}

export function Cancel(id: string): Promise<void> {
  return App.Cancel(id)
}

export function RevealInFolder(path: string): Promise<void> {
  return App.RevealInFolder(path)
}

export function ConfigGet(): Promise<Record<string, unknown>> {
  return App.ConfigGet()
}

// ---- M11：AI 粗剪 / Codex 面板 ----

export function ASRStatus(): Promise<ASRStatus> {
  return App.ASRStatus()
}
export function SetASRHotwords(words: string[]): Promise<void> {
  return App.SetASRHotwords(words)
}
export function ReadMCPCallLog(limit: number): Promise<CallEntry[]> {
  return App.ReadMCPCallLog(limit)
}
export function LoadEditsFile(): Promise<string> {
  return App.LoadEditsFile()
}
export function ReadEditsLedger(path: string): Promise<EditsView> {
  return App.ReadEditsLedger(path)
}
export function AICutGenerate(source: string, workDir: string): Promise<EditsView> {
  return App.AICutGenerate(source, workDir)
}
export function AICutCompute(source: string, items: EditItem[]): Promise<EditsView> {
  return App.AICutCompute(source, items)
}
export function AICutSubmit(source: string, items: EditItem[], strategy: string): Promise<string> {
  return App.AICutSubmit(source, items, strategy)
}
export function MergeCutSegments(taskID: string, outputPath: string): Promise<string> {
  return App.MergeCutSegments(taskID, outputPath)
}
export function ExportTranscriptFile(baseDir: string, format: string): Promise<string> {
  return App.ExportTranscriptFile(baseDir, format)
}

// M12：字幕（app_v18.go）
export function AICutExportSubtitles(
  workDir: string,
  finalVideo: string,
  format: string,
  stylePreset: string,
  maxChars: number,
  maxLines: number,
): Promise<SubtitlesView> {
  return App.AICutExportSubtitles(workDir, finalVideo, format, stylePreset, maxChars, maxLines)
}
export function AICutBurnSubtitles(
  videoPath: string,
  assPath: string,
  encoder: string,
  crf: number,
  preset: string,
): Promise<string> {
  return App.AICutBurnSubtitles(videoPath, assPath, encoder, crf, preset)
}
export function AICutAnimateCaptions(
  workDir: string,
  finalVideo: string,
  mode: string,
  skills: string[],
  maxSkills: number,
): Promise<MotionPlanView> {
  return App.AICutAnimateCaptions(workDir, finalVideo, mode, skills, maxSkills)
}

// 避免未使用类型告警（类型仅用于声明全局对象契约）
export type _Unused = EditsView | EditsLedger | EditItem | ASRStatus | CallEntry | SubtitlesView | MotionPlanView
