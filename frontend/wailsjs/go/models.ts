export namespace edits {
	
	export class EditItem {
	    start: number;
	    end: number;
	    type: string;
	    text?: string;
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new EditItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.start = source["start"];
	        this.end = source["end"];
	        this.type = source["type"];
	        this.text = source["text"];
	        this.reason = source["reason"];
	    }
	}
	export class EditsLedger {
	    version: number;
	    source: string;
	    items: EditItem[];
	
	    static createFrom(source: any = {}) {
	        return new EditsLedger(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.source = source["source"];
	        this.items = this.convertValues(source["items"], EditItem);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class EditsSummary {
	    item_count: number;
	    deleted_seconds: number;
	    by_type: Record<string, number>;
	    keep_count: number;
	    keep_seconds: number;
	    source_seconds: number;
	    compression_pct: number;
	
	    static createFrom(source: any = {}) {
	        return new EditsSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.item_count = source["item_count"];
	        this.deleted_seconds = source["deleted_seconds"];
	        this.by_type = source["by_type"];
	        this.keep_count = source["keep_count"];
	        this.keep_seconds = source["keep_seconds"];
	        this.source_seconds = source["source_seconds"];
	        this.compression_pct = source["compression_pct"];
	    }
	}
	export class KeepRange {
	    start: number;
	    end: number;
	
	    static createFrom(source: any = {}) {
	        return new KeepRange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.start = source["start"];
	        this.end = source["end"];
	    }
	}

}

export namespace ffmpeg {
	
	export class StreamInfo {
	    index: number;
	    codec_type: string;
	    codec_name: string;
	    width?: number;
	    height?: number;
	    duration?: string;
	    pix_fmt?: string;
	    sample_rate?: string;
	    channels?: number;
	
	    static createFrom(source: any = {}) {
	        return new StreamInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.codec_type = source["codec_type"];
	        this.codec_name = source["codec_name"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.duration = source["duration"];
	        this.pix_fmt = source["pix_fmt"];
	        this.sample_rate = source["sample_rate"];
	        this.channels = source["channels"];
	    }
	}
	export class ProbeResult {
	    duration: number;
	    format_name: string;
	    has_video: boolean;
	    has_audio: boolean;
	    width: number;
	    height: number;
	    streams: StreamInfo[];
	
	    static createFrom(source: any = {}) {
	        return new ProbeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.duration = source["duration"];
	        this.format_name = source["format_name"];
	        this.has_video = source["has_video"];
	        this.has_audio = source["has_audio"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.streams = this.convertValues(source["streams"], StreamInfo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class ASRStatusInfo {
	    enabled: boolean;
	    host: string;
	    port: number;
	    model: string;
	    compute_type: string;
	    hf_home: string;
	    idle_timeout_min: number;
	    hotwords: string[];
	    python: string;
	    healthy: boolean;
	    runtime_missing: boolean;
	    hint: string;
	
	    static createFrom(source: any = {}) {
	        return new ASRStatusInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.model = source["model"];
	        this.compute_type = source["compute_type"];
	        this.hf_home = source["hf_home"];
	        this.idle_timeout_min = source["idle_timeout_min"];
	        this.hotwords = source["hotwords"];
	        this.python = source["python"];
	        this.healthy = source["healthy"];
	        this.runtime_missing = source["runtime_missing"];
	        this.hint = source["hint"];
	    }
	}
	export class EditsView {
	    path: string;
	    ledger: edits.EditsLedger;
	    valid: boolean;
	    validation_error: string;
	    summary?: edits.EditsSummary;
	    keep_ranges: edits.KeepRange[];
	    source_duration: number;
	
	    static createFrom(source: any = {}) {
	        return new EditsView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.ledger = this.convertValues(source["ledger"], edits.EditsLedger);
	        this.valid = source["valid"];
	        this.validation_error = source["validation_error"];
	        this.summary = this.convertValues(source["summary"], edits.EditsSummary);
	        this.keep_ranges = this.convertValues(source["keep_ranges"], edits.KeepRange);
	        this.source_duration = source["source_duration"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MotionPlanView {
	    fx_path: string;
	    plan_path: string;
	    selected: string[];
	    reasoning: string;
	    rejected: subtitles.RejectedSkill[];
	    analysis?: subtitles.Analysis;
	    static: boolean;
	    style_id: string;
	    orientation: string;
	
	    static createFrom(source: any = {}) {
	        return new MotionPlanView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fx_path = source["fx_path"];
	        this.plan_path = source["plan_path"];
	        this.selected = source["selected"];
	        this.reasoning = source["reasoning"];
	        this.rejected = this.convertValues(source["rejected"], subtitles.RejectedSkill);
	        this.analysis = this.convertValues(source["analysis"], subtitles.Analysis);
	        this.static = source["static"];
	        this.style_id = source["style_id"];
	        this.orientation = source["orientation"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SubtitlesView {
	    srt_path: string;
	    ass_path: string;
	    cue_count: number;
	    split_count: number;
	    final_duration: number;
	    output_dir: string;
	    cues: subtitles.Cue[];
	
	    static createFrom(source: any = {}) {
	        return new SubtitlesView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.srt_path = source["srt_path"];
	        this.ass_path = source["ass_path"];
	        this.cue_count = source["cue_count"];
	        this.split_count = source["split_count"];
	        this.final_duration = source["final_duration"];
	        this.output_dir = source["output_dir"];
	        this.cues = this.convertValues(source["cues"], subtitles.Cue);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace mcpserver {
	
	export class CallEntry {
	    ts: string;
	    tool: string;
	    ok: boolean;
	    ms: number;
	    err: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new CallEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ts = source["ts"];
	        this.tool = source["tool"];
	        this.ok = source["ok"];
	        this.ms = source["ms"];
	        this.err = source["err"];
	        this.detail = source["detail"];
	    }
	}

}

export namespace service {
	
	export class SubtitleBurn {
	    ass_path: string;
	    encoder: string;
	    crf: number;
	    preset: string;
	
	    static createFrom(source: any = {}) {
	        return new SubtitleBurn(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ass_path = source["ass_path"];
	        this.encoder = source["encoder"];
	        this.crf = source["crf"];
	        this.preset = source["preset"];
	    }
	}
	export class BatchItem {
	    input_path: string;
	    start: number;
	    end: number;
	    strategy: string;
	    output_path: string;
	    subtitle?: SubtitleBurn;
	
	    static createFrom(source: any = {}) {
	        return new BatchItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.input_path = source["input_path"];
	        this.start = source["start"];
	        this.end = source["end"];
	        this.strategy = source["strategy"];
	        this.output_path = source["output_path"];
	        this.subtitle = this.convertValues(source["subtitle"], SubtitleBurn);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class BatchRequest {
	    items: BatchItem[];
	
	    static createFrom(source: any = {}) {
	        return new BatchRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], BatchItem);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class MergeItem {
	    input_path: string;
	    start: number;
	    end: number;
	    has_range: boolean;
	    strategy: string;
	
	    static createFrom(source: any = {}) {
	        return new MergeItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.input_path = source["input_path"];
	        this.start = source["start"];
	        this.end = source["end"];
	        this.has_range = source["has_range"];
	        this.strategy = source["strategy"];
	    }
	}
	export class MergeRequest {
	    items: MergeItem[];
	    mode: string;
	    output_path: string;
	    overwrite: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MergeRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], MergeItem);
	        this.mode = source["mode"];
	        this.output_path = source["output_path"];
	        this.overwrite = source["overwrite"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class TrimItem {
	    input_path: string;
	    output_path: string;
	    start: number;
	    end: number;
	    strategy: string;
	    status: string;
	    progress: number;
	    error: string;
	    has_range: boolean;
	    subtitle?: SubtitleBurn;
	
	    static createFrom(source: any = {}) {
	        return new TrimItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.input_path = source["input_path"];
	        this.output_path = source["output_path"];
	        this.start = source["start"];
	        this.end = source["end"];
	        this.strategy = source["strategy"];
	        this.status = source["status"];
	        this.progress = source["progress"];
	        this.error = source["error"];
	        this.has_range = source["has_range"];
	        this.subtitle = this.convertValues(source["subtitle"], SubtitleBurn);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Task {
	    task_id: string;
	    kind: string;
	    input_path: string;
	    output_path: string;
	    start: number;
	    end: number;
	    strategy: string;
	    status: string;
	    progress: number;
	    duration: number;
	    error: string;
	    canceled: boolean;
	    // Go type: time
	    created_at: any;
	    items: TrimItem[];
	    completed_count: number;
	    failed_count: number;
	    merge_mode: string;
	
	    static createFrom(source: any = {}) {
	        return new Task(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task_id = source["task_id"];
	        this.kind = source["kind"];
	        this.input_path = source["input_path"];
	        this.output_path = source["output_path"];
	        this.start = source["start"];
	        this.end = source["end"];
	        this.strategy = source["strategy"];
	        this.status = source["status"];
	        this.progress = source["progress"];
	        this.duration = source["duration"];
	        this.error = source["error"];
	        this.canceled = source["canceled"];
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.items = this.convertValues(source["items"], TrimItem);
	        this.completed_count = source["completed_count"];
	        this.failed_count = source["failed_count"];
	        this.merge_mode = source["merge_mode"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class TrimRequest {
	    input_path: string;
	    start: number;
	    end: number;
	    strategy: string;
	    output_path: string;
	    overwrite: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TrimRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.input_path = source["input_path"];
	        this.start = source["start"];
	        this.end = source["end"];
	        this.strategy = source["strategy"];
	        this.output_path = source["output_path"];
	        this.overwrite = source["overwrite"];
	    }
	}

}

export namespace subtitles {
	
	export class Analysis {
	    keyword_density: number;
	    content_type: string;
	    avg_cue_duration: number;
	    avg_chars_per_cue: number;
	    emphatic_ratio: number;
	    top_keywords: string[];
	
	    static createFrom(source: any = {}) {
	        return new Analysis(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.keyword_density = source["keyword_density"];
	        this.content_type = source["content_type"];
	        this.avg_cue_duration = source["avg_cue_duration"];
	        this.avg_chars_per_cue = source["avg_chars_per_cue"];
	        this.emphatic_ratio = source["emphatic_ratio"];
	        this.top_keywords = source["top_keywords"];
	    }
	}
	export class Word {
	    start: number;
	    end: number;
	    word: string;
	
	    static createFrom(source: any = {}) {
	        return new Word(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.start = source["start"];
	        this.end = source["end"];
	        this.word = source["word"];
	    }
	}
	export class Cue {
	    start: number;
	    end: number;
	    text: string;
	    words?: Word[];
	    split_at?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Cue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.start = source["start"];
	        this.end = source["end"];
	        this.text = source["text"];
	        this.words = this.convertValues(source["words"], Word);
	        this.split_at = source["split_at"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RejectedSkill {
	    skill: string;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new RejectedSkill(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.skill = source["skill"];
	        this.reason = source["reason"];
	    }
	}

}

