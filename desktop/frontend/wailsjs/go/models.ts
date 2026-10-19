export namespace activity {
	
	export class Day {
	    date: string;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new Day(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.date = source["date"];
	        this.count = source["count"];
	    }
	}
	export class Project {
	    path: string;
	    name: string;
	    messages: number;
	
	    static createFrom(source: any = {}) {
	        return new Project(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.name = source["name"];
	        this.messages = source["messages"];
	    }
	}
	export class Share {
	    driver: string;
	    model?: string;
	    messages: number;
	    tokens: number;
	
	    static createFrom(source: any = {}) {
	        return new Share(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.driver = source["driver"];
	        this.model = source["model"];
	        this.messages = source["messages"];
	        this.tokens = source["tokens"];
	    }
	}
	export class Totals {
	    messages: number;
	    sessions: number;
	    toolCalls: number;
	    tokens: number;
	
	    static createFrom(source: any = {}) {
	        return new Totals(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.messages = source["messages"];
	        this.sessions = source["sessions"];
	        this.toolCalls = source["toolCalls"];
	        this.tokens = source["tokens"];
	    }
	}
	export class Overview {
	    period: string;
	    project: string;
	    current: Totals;
	    previous: Totals;
	    days: Day[];
	    yearMessages: number;
	    activeDays: number;
	    currentStreak: number;
	    longestStreak: number;
	    busiestMonth: string;
	    busiestMonthMessages: number;
	    busiestWeekday: string;
	    busiestWeekdayMessages: number;
	    providers: Share[];
	    models: Share[];
	    projects: Project[];
	
	    static createFrom(source: any = {}) {
	        return new Overview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.period = source["period"];
	        this.project = source["project"];
	        this.current = this.convertValues(source["current"], Totals);
	        this.previous = this.convertValues(source["previous"], Totals);
	        this.days = this.convertValues(source["days"], Day);
	        this.yearMessages = source["yearMessages"];
	        this.activeDays = source["activeDays"];
	        this.currentStreak = source["currentStreak"];
	        this.longestStreak = source["longestStreak"];
	        this.busiestMonth = source["busiestMonth"];
	        this.busiestMonthMessages = source["busiestMonthMessages"];
	        this.busiestWeekday = source["busiestWeekday"];
	        this.busiestWeekdayMessages = source["busiestWeekdayMessages"];
	        this.providers = this.convertValues(source["providers"], Share);
	        this.models = this.convertValues(source["models"], Share);
	        this.projects = this.convertValues(source["projects"], Project);
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

export namespace attachments {
	
	export class Attachment {
	    id: string;
	    name: string;
	    path: string;
	    mime?: string;
	    size: number;
	    temporary?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Attachment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.mime = source["mime"];
	        this.size = source["size"];
	        this.temporary = source["temporary"];
	    }
	}

}

export namespace claudeimport {
	
	export class ExternalSession {
	    id: string;
	    title: string;
	    cwd: string;
	    model?: string;
	    preview?: string;
	    filePath: string;
	    messageCount: number;
	    startedAt: number;
	    updatedAt: number;
	    agentRun: boolean;
	    entrypoint?: string;
	    provider?: string;
	
	    static createFrom(source: any = {}) {
	        return new ExternalSession(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.cwd = source["cwd"];
	        this.model = source["model"];
	        this.preview = source["preview"];
	        this.filePath = source["filePath"];
	        this.messageCount = source["messageCount"];
	        this.startedAt = source["startedAt"];
	        this.updatedAt = source["updatedAt"];
	        this.agentRun = source["agentRun"];
	        this.entrypoint = source["entrypoint"];
	        this.provider = source["provider"];
	    }
	}
	export class Page {
	    sessions: ExternalSession[];
	    next: number;
	
	    static createFrom(source: any = {}) {
	        return new Page(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessions = this.convertValues(source["sessions"], ExternalSession);
	        this.next = source["next"];
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

export namespace devserver {
	
	export class Snapshot {
	    id: string;
	    label: string;
	    command: string;
	    cwd: string;
	    pid: number;
	    port?: number;
	    url?: string;
	    status: string;
	    exitCode?: number;
	    ownerThreadId?: string;
	    startedAt: number;
	    managed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.command = source["command"];
	        this.cwd = source["cwd"];
	        this.pid = source["pid"];
	        this.port = source["port"];
	        this.url = source["url"];
	        this.status = source["status"];
	        this.exitCode = source["exitCode"];
	        this.ownerThreadId = source["ownerThreadId"];
	        this.startedAt = source["startedAt"];
	        this.managed = source["managed"];
	    }
	}
	export class Spec {
	    label?: string;
	    command: string;
	    args?: string[];
	    cwd: string;
	    env?: Record<string, string>;
	    ownerThreadId?: string;
	
	    static createFrom(source: any = {}) {
	        return new Spec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.command = source["command"];
	        this.args = source["args"];
	        this.cwd = source["cwd"];
	        this.env = source["env"];
	        this.ownerThreadId = source["ownerThreadId"];
	    }
	}

}

export namespace domain {
	
	export class Account {
	    id: string;
	    name: string;
	    configDir?: string;
	
	    static createFrom(source: any = {}) {
	        return new Account(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.configDir = source["configDir"];
	    }
	}
	export class ApprovalOption {
	    id: string;
	    name: string;
	    kind: string;
	
	    static createFrom(source: any = {}) {
	        return new ApprovalOption(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	    }
	}
	export class FileDiff {
	    path: string;
	    oldText?: string;
	    newText?: string;
	
	    static createFrom(source: any = {}) {
	        return new FileDiff(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.oldText = source["oldText"];
	        this.newText = source["newText"];
	    }
	}
	export class ToolCall {
	    id: string;
	    name: string;
	    kind?: string;
	    status: string;
	    input?: Record<string, any>;
	    output?: string;
	    diffs?: FileDiff[];
	    durationMs?: number;
	    toolCount?: number;
	    tokens?: number;
	    async?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ToolCall(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.status = source["status"];
	        this.input = source["input"];
	        this.output = source["output"];
	        this.diffs = this.convertValues(source["diffs"], FileDiff);
	        this.durationMs = source["durationMs"];
	        this.toolCount = source["toolCount"];
	        this.tokens = source["tokens"];
	        this.async = source["async"];
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
	export class ApprovalRequest {
	    requestId: string;
	    title: string;
	    detail?: string;
	    tool?: ToolCall;
	    options?: ApprovalOption[];
	
	    static createFrom(source: any = {}) {
	        return new ApprovalRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requestId = source["requestId"];
	        this.title = source["title"];
	        this.detail = source["detail"];
	        this.tool = this.convertValues(source["tool"], ToolCall);
	        this.options = this.convertValues(source["options"], ApprovalOption);
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
	export class BranchInfo {
	    name: string;
	    current: boolean;
	    remote: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BranchInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.current = source["current"];
	        this.remote = source["remote"];
	    }
	}
	export class Commit {
	    sha: string;
	    short: string;
	    subject: string;
	    author: string;
	    at: number;
	    onBase: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Commit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sha = source["sha"];
	        this.short = source["short"];
	        this.subject = source["subject"];
	        this.author = source["author"];
	        this.at = source["at"];
	        this.onBase = source["onBase"];
	    }
	}
	export class DiffLine {
	    kind: string;
	    old?: number;
	    new?: number;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new DiffLine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.old = source["old"];
	        this.new = source["new"];
	        this.content = source["content"];
	    }
	}
	export class DiffHunk {
	    header: string;
	    lines: DiffLine[];
	
	    static createFrom(source: any = {}) {
	        return new DiffHunk(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.header = source["header"];
	        this.lines = this.convertValues(source["lines"], DiffLine);
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
	export class DiffFile {
	    path: string;
	    oldPath?: string;
	    hunks: DiffHunk[];
	    insertions: number;
	    deletions: number;
	    binary: boolean;
	    truncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DiffFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.oldPath = source["oldPath"];
	        this.hunks = this.convertValues(source["hunks"], DiffHunk);
	        this.insertions = source["insertions"];
	        this.deletions = source["deletions"];
	        this.binary = source["binary"];
	        this.truncated = source["truncated"];
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
	
	
	export class FileChange {
	    path: string;
	    oldPath?: string;
	    status: string;
	    staged: boolean;
	    insertions: number;
	    deletions: number;
	    binary: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FileChange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.oldPath = source["oldPath"];
	        this.status = source["status"];
	        this.staged = source["staged"];
	        this.insertions = source["insertions"];
	        this.deletions = source["deletions"];
	        this.binary = source["binary"];
	    }
	}
	
	export class FileRef {
	    path: string;
	    mime?: string;
	
	    static createFrom(source: any = {}) {
	        return new FileRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.mime = source["mime"];
	    }
	}
	export class OptionChoice {
	    id: string;
	    label: string;
	    default?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new OptionChoice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.default = source["default"];
	    }
	}
	export class OptionDescriptor {
	    id: string;
	    label: string;
	    type: string;
	    choices?: OptionChoice[];
	    default?: any;
	
	    static createFrom(source: any = {}) {
	        return new OptionDescriptor(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.type = source["type"];
	        this.choices = this.convertValues(source["choices"], OptionChoice);
	        this.default = source["default"];
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
	export class Model {
	    id: string;
	    displayName: string;
	    default?: boolean;
	    options?: OptionDescriptor[];
	
	    static createFrom(source: any = {}) {
	        return new Model(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.displayName = source["displayName"];
	        this.default = source["default"];
	        this.options = this.convertValues(source["options"], OptionDescriptor);
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
	
	
	export class PlanEntry {
	    content: string;
	    status: string;
	    priority?: string;
	
	    static createFrom(source: any = {}) {
	        return new PlanEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.content = source["content"];
	        this.status = source["status"];
	        this.priority = source["priority"];
	    }
	}
	export class ProviderSettings {
	    binaryPath?: string;
	    launchArgs?: string;
	    env?: Record<string, string>;
	    serverUrl?: string;
	    apiEndpoint?: string;
	    model?: string;
	    options?: Record<string, any>;
	    printTimeout?: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProviderSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.binaryPath = source["binaryPath"];
	        this.launchArgs = source["launchArgs"];
	        this.env = source["env"];
	        this.serverUrl = source["serverUrl"];
	        this.apiEndpoint = source["apiEndpoint"];
	        this.model = source["model"];
	        this.options = source["options"];
	        this.printTimeout = source["printTimeout"];
	        this.enabled = source["enabled"];
	    }
	}
	export class ProviderUpdate {
	    installed: string;
	    latest: string;
	    available: boolean;
	    checkedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new ProviderUpdate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.latest = source["latest"];
	        this.available = source["available"];
	        this.checkedAt = source["checkedAt"];
	    }
	}
	export class ProviderSnapshot {
	    instanceId: string;
	    driver: string;
	    displayName: string;
	    availability: string;
	    version?: string;
	    commandPath?: string;
	    message?: string;
	    models?: Model[];
	    checkedAt: number;
	    settings: ProviderSettings;
	    accounts?: Account[];
	    update?: ProviderUpdate;
	
	    static createFrom(source: any = {}) {
	        return new ProviderSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.instanceId = source["instanceId"];
	        this.driver = source["driver"];
	        this.displayName = source["displayName"];
	        this.availability = source["availability"];
	        this.version = source["version"];
	        this.commandPath = source["commandPath"];
	        this.message = source["message"];
	        this.models = this.convertValues(source["models"], Model);
	        this.checkedAt = source["checkedAt"];
	        this.settings = this.convertValues(source["settings"], ProviderSettings);
	        this.accounts = this.convertValues(source["accounts"], Account);
	        this.update = this.convertValues(source["update"], ProviderUpdate);
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
	
	export class QuestionItem {
	    question: string;
	    options?: string[];
	    isMultiSelect?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new QuestionItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.question = source["question"];
	        this.options = source["options"];
	        this.isMultiSelect = source["isMultiSelect"];
	    }
	}
	export class QuestionRequest {
	    requestId: string;
	    questions: QuestionItem[];
	
	    static createFrom(source: any = {}) {
	        return new QuestionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requestId = source["requestId"];
	        this.questions = this.convertValues(source["questions"], QuestionItem);
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
	export class RateLimit {
	    window: string;
	    status?: string;
	    usedPercent?: number;
	    resetsAt?: number;
	
	    static createFrom(source: any = {}) {
	        return new RateLimit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.window = source["window"];
	        this.status = source["status"];
	        this.usedPercent = source["usedPercent"];
	        this.resetsAt = source["resetsAt"];
	    }
	}
	export class Usage {
	    inputTokens?: number;
	    outputTokens?: number;
	    cacheReadTokens?: number;
	    cacheWriteTokens?: number;
	    contextWindow?: number;
	    contextTokens?: number;
	    costUsd?: number;
	
	    static createFrom(source: any = {}) {
	        return new Usage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.inputTokens = source["inputTokens"];
	        this.outputTokens = source["outputTokens"];
	        this.cacheReadTokens = source["cacheReadTokens"];
	        this.cacheWriteTokens = source["cacheWriteTokens"];
	        this.contextWindow = source["contextWindow"];
	        this.contextTokens = source["contextTokens"];
	        this.costUsd = source["costUsd"];
	    }
	}
	export class RuntimeEvent {
	    kind: string;
	    threadId: string;
	    turnId?: string;
	    instanceId?: string;
	    driver?: string;
	    model?: string;
	    account?: string;
	    seq: number;
	    itemId?: string;
	    parentToolId?: string;
	    at: number;
	    text?: string;
	    delta?: boolean;
	    icon?: string;
	    files?: FileRef[];
	    tool?: ToolCall;
	    plan?: PlanEntry[];
	    approval?: ApprovalRequest;
	    question?: QuestionRequest;
	    usage?: Usage;
	    rateLimits?: RateLimit[];
	    stopReason?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new RuntimeEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.threadId = source["threadId"];
	        this.turnId = source["turnId"];
	        this.instanceId = source["instanceId"];
	        this.driver = source["driver"];
	        this.model = source["model"];
	        this.account = source["account"];
	        this.seq = source["seq"];
	        this.itemId = source["itemId"];
	        this.parentToolId = source["parentToolId"];
	        this.at = source["at"];
	        this.text = source["text"];
	        this.delta = source["delta"];
	        this.icon = source["icon"];
	        this.files = this.convertValues(source["files"], FileRef);
	        this.tool = this.convertValues(source["tool"], ToolCall);
	        this.plan = this.convertValues(source["plan"], PlanEntry);
	        this.approval = this.convertValues(source["approval"], ApprovalRequest);
	        this.question = this.convertValues(source["question"], QuestionRequest);
	        this.usage = this.convertValues(source["usage"], Usage);
	        this.rateLimits = this.convertValues(source["rateLimits"], RateLimit);
	        this.stopReason = source["stopReason"];
	        this.error = source["error"];
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
	export class SendTurnInput {
	    threadId: string;
	    turnId: string;
	    text: string;
	    files?: FileRef[];
	
	    static createFrom(source: any = {}) {
	        return new SendTurnInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.threadId = source["threadId"];
	        this.turnId = source["turnId"];
	        this.text = source["text"];
	        this.files = this.convertValues(source["files"], FileRef);
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
	export class Session {
	    threadId: string;
	    instanceId: string;
	    driver: string;
	    account?: string;
	    providerSessionId?: string;
	    cwd: string;
	    model?: string;
	    startedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Session(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.threadId = source["threadId"];
	        this.instanceId = source["instanceId"];
	        this.driver = source["driver"];
	        this.account = source["account"];
	        this.providerSessionId = source["providerSessionId"];
	        this.cwd = source["cwd"];
	        this.model = source["model"];
	        this.startedAt = source["startedAt"];
	    }
	}
	export class SessionRecord {
	    id: string;
	    workspaceId?: string;
	    title: string;
	    customName?: string;
	    projectPath: string;
	    branch?: string;
	    driver: string;
	    model: string;
	    cliSessionId?: string;
	    status: string;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new SessionRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workspaceId = source["workspaceId"];
	        this.title = source["title"];
	        this.customName = source["customName"];
	        this.projectPath = source["projectPath"];
	        this.branch = source["branch"];
	        this.driver = source["driver"];
	        this.model = source["model"];
	        this.cliSessionId = source["cliSessionId"];
	        this.status = source["status"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
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
	export class SkillRef {
	    name: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new SkillRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	    }
	}
	export class SkillPolicy {
	    mode: string;
	    skills?: SkillRef[];
	    denied?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SkillPolicy(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.skills = this.convertValues(source["skills"], SkillRef);
	        this.denied = source["denied"];
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
	export class SessionStartInput {
	    threadId: string;
	    instanceId: string;
	    account?: string;
	    cwd: string;
	    model?: string;
	    permissionMode?: string;
	    resume?: string;
	    fork?: boolean;
	    options?: Record<string, any>;
	    planOnly?: boolean;
	    skills?: SkillPolicy;
	
	    static createFrom(source: any = {}) {
	        return new SessionStartInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.threadId = source["threadId"];
	        this.instanceId = source["instanceId"];
	        this.account = source["account"];
	        this.cwd = source["cwd"];
	        this.model = source["model"];
	        this.permissionMode = source["permissionMode"];
	        this.resume = source["resume"];
	        this.fork = source["fork"];
	        this.options = source["options"];
	        this.planOnly = source["planOnly"];
	        this.skills = this.convertValues(source["skills"], SkillPolicy);
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
	
	
	export class Worktree {
	    path: string;
	    branch: string;
	    baseBranch: string;
	    createdAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Worktree(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.branch = source["branch"];
	        this.baseBranch = source["baseBranch"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class Task {
	    id: string;
	    workspaceId: string;
	    threadId: string;
	    title: string;
	    prompt: string;
	    driver: string;
	    drivers?: string[];
	    account?: string;
	    model?: string;
	    models?: string[];
	    options?: Record<string, any>;
	    state: string;
	    permission?: string;
	    archived?: boolean;
	    titleSource?: string;
	    titleTurn?: number;
	    userTurns?: number;
	    worktree?: Worktree;
	    cwd?: string;
	    summary?: string;
	    createdAt: number;
	    updatedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Task(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workspaceId = source["workspaceId"];
	        this.threadId = source["threadId"];
	        this.title = source["title"];
	        this.prompt = source["prompt"];
	        this.driver = source["driver"];
	        this.drivers = source["drivers"];
	        this.account = source["account"];
	        this.model = source["model"];
	        this.models = source["models"];
	        this.options = source["options"];
	        this.state = source["state"];
	        this.permission = source["permission"];
	        this.archived = source["archived"];
	        this.titleSource = source["titleSource"];
	        this.titleTurn = source["titleTurn"];
	        this.userTurns = source["userTurns"];
	        this.worktree = this.convertValues(source["worktree"], Worktree);
	        this.cwd = source["cwd"];
	        this.summary = source["summary"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
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
	export class TaskHandoff {
	    taskId: string;
	    branch: string;
	    filesChanged: number;
	    insertions: number;
	    deletions: number;
	    commits: number;
	    conflicts?: string[];
	    cleanMerge: boolean;
	    summary?: string;
	
	    static createFrom(source: any = {}) {
	        return new TaskHandoff(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.taskId = source["taskId"];
	        this.branch = source["branch"];
	        this.filesChanged = source["filesChanged"];
	        this.insertions = source["insertions"];
	        this.deletions = source["deletions"];
	        this.commits = source["commits"];
	        this.conflicts = source["conflicts"];
	        this.cleanMerge = source["cleanMerge"];
	        this.summary = source["summary"];
	    }
	}
	export class TaskRecord {
	    id: number;
	    sessionId?: string;
	    content: string;
	    status: string;
	    priority: string;
	    orderIndex: number;
	    source: string;
	    // Go type: time
	    scheduledAt?: any;
	    // Go type: time
	    startedAt?: any;
	    // Go type: time
	    completedAt?: any;
	    // Go type: time
	    createdAt: any;
	
	    static createFrom(source: any = {}) {
	        return new TaskRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.sessionId = source["sessionId"];
	        this.content = source["content"];
	        this.status = source["status"];
	        this.priority = source["priority"];
	        this.orderIndex = source["orderIndex"];
	        this.source = source["source"];
	        this.scheduledAt = this.convertValues(source["scheduledAt"], null);
	        this.startedAt = this.convertValues(source["startedAt"], null);
	        this.completedAt = this.convertValues(source["completedAt"], null);
	        this.createdAt = this.convertValues(source["createdAt"], null);
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
	
	
	export class Workspace {
	    id: string;
	    title: string;
	    prompt: string;
	    cwd: string;
	    createdAt: number;
	    updatedAt: number;
	    archived?: boolean;
	    importedFrom?: string;
	    importedSession?: string;
	
	    static createFrom(source: any = {}) {
	        return new Workspace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.prompt = source["prompt"];
	        this.cwd = source["cwd"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.archived = source["archived"];
	        this.importedFrom = source["importedFrom"];
	        this.importedSession = source["importedSession"];
	    }
	}
	
	export class WorktreeChanges {
	    threadId?: string;
	    title: string;
	    path: string;
	    branch: string;
	    base?: string;
	    isMain: boolean;
	    orphaned: boolean;
	    files: FileChange[];
	    commits: Commit[];
	    ahead: number;
	    upstream?: string;
	    unpushed: number;
	    unpulled: number;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new WorktreeChanges(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.threadId = source["threadId"];
	        this.title = source["title"];
	        this.path = source["path"];
	        this.branch = source["branch"];
	        this.base = source["base"];
	        this.isMain = source["isMain"];
	        this.orphaned = source["orphaned"];
	        this.files = this.convertValues(source["files"], FileChange);
	        this.commits = this.convertValues(source["commits"], Commit);
	        this.ahead = source["ahead"];
	        this.upstream = source["upstream"];
	        this.unpushed = source["unpushed"];
	        this.unpulled = source["unpulled"];
	        this.error = source["error"];
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

export namespace files {
	
	export class Content {
	    path: string;
	    size: number;
	    binary: boolean;
	    text?: string;
	    truncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Content(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.size = source["size"];
	        this.binary = source["binary"];
	        this.text = source["text"];
	        this.truncated = source["truncated"];
	    }
	}
	export class Entry {
	    name: string;
	    path: string;
	    dir: boolean;
	    size?: number;
	    symlink?: boolean;
	    ignored?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Entry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.dir = source["dir"];
	        this.size = source["size"];
	        this.symlink = source["symlink"];
	        this.ignored = source["ignored"];
	    }
	}
	export class TreeStatus {
	    isGit: boolean;
	    repoRoot?: string;
	    prefix?: string;
	    files: Record<string, string>;
	    staged?: Record<string, boolean>;
	    dirs: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new TreeStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.isGit = source["isGit"];
	        this.repoRoot = source["repoRoot"];
	        this.prefix = source["prefix"];
	        this.files = source["files"];
	        this.staged = source["staged"];
	        this.dirs = source["dirs"];
	    }
	}

}

export namespace history {
	
	export class Meta {
	    workspace: domain.Workspace;
	    tasks: domain.Task[];
	    coordinatorThreadId?: string;
	    resume?: Record<string, string>;
	    version: number;
	
	    static createFrom(source: any = {}) {
	        return new Meta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workspace = this.convertValues(source["workspace"], domain.Workspace);
	        this.tasks = this.convertValues(source["tasks"], domain.Task);
	        this.coordinatorThreadId = source["coordinatorThreadId"];
	        this.resume = source["resume"];
	        this.version = source["version"];
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
	export class Session {
	    meta: Meta;
	    transcripts: Record<string, Array<domain.RuntimeEvent>>;
	
	    static createFrom(source: any = {}) {
	        return new Session(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.meta = this.convertValues(source["meta"], Meta);
	        this.transcripts = this.convertValues(source["transcripts"], Array<domain.RuntimeEvent>, true);
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

export namespace instructions {
	
	export class File {
	    id: string;
	    group: string;
	    label: string;
	    path?: string;
	    account?: string;
	    exists: boolean;
	    content?: string;
	    size: number;
	    modified?: number;
	    note?: string;
	    warning?: boolean;
	    truncated?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new File(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.group = source["group"];
	        this.label = source["label"];
	        this.path = source["path"];
	        this.account = source["account"];
	        this.exists = source["exists"];
	        this.content = source["content"];
	        this.size = source["size"];
	        this.modified = source["modified"];
	        this.note = source["note"];
	        this.warning = source["warning"];
	        this.truncated = source["truncated"];
	    }
	}
	export class Source {
	    driver: string;
	    name: string;
	    files: File[];
	
	    static createFrom(source: any = {}) {
	        return new Source(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.driver = source["driver"];
	        this.name = source["name"];
	        this.files = this.convertValues(source["files"], File);
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
	
	export class AccountAdded {
	    account: domain.Account;
	    copied?: string[];
	    warning?: string;
	
	    static createFrom(source: any = {}) {
	        return new AccountAdded(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.account = this.convertValues(source["account"], domain.Account);
	        this.copied = source["copied"];
	        this.warning = source["warning"];
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
	export class BackgroundSettings {
	    startMode: string;
	    keepRunningOnClose: boolean;
	    supported: boolean;
	    exePath: string;
	
	    static createFrom(source: any = {}) {
	        return new BackgroundSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.startMode = source["startMode"];
	        this.keepRunningOnClose = source["keepRunningOnClose"];
	        this.supported = source["supported"];
	        this.exePath = source["exePath"];
	    }
	}
	export class SkillTestInput {
	    driver: string;
	    prompt: string;
	    files?: domain.FileRef[];
	    skills: string[];
	    cwd: string;
	    account?: string;
	    model?: string;
	    options?: Record<string, any>;
	    permissionMode?: string;
	
	    static createFrom(source: any = {}) {
	        return new SkillTestInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.driver = source["driver"];
	        this.prompt = source["prompt"];
	        this.files = this.convertValues(source["files"], domain.FileRef);
	        this.skills = source["skills"];
	        this.cwd = source["cwd"];
	        this.account = source["account"];
	        this.model = source["model"];
	        this.options = source["options"];
	        this.permissionMode = source["permissionMode"];
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

export namespace projects {
	
	export class Project {
	    id: string;
	    name: string;
	    path: string;
	    isGit: boolean;
	    addedAt: number;
	    usedAt?: number;
	    order?: number;
	    missing?: boolean;
	    accounts?: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new Project(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.isGit = source["isGit"];
	        this.addedAt = source["addedAt"];
	        this.usedAt = source["usedAt"];
	        this.order = source["order"];
	        this.missing = source["missing"];
	        this.accounts = source["accounts"];
	    }
	}

}

export namespace provider {
	
	export class AccountStatus {
	    signedIn: boolean;
	    known: boolean;
	    detail?: string;
	
	    static createFrom(source: any = {}) {
	        return new AccountStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.signedIn = source["signedIn"];
	        this.known = source["known"];
	        this.detail = source["detail"];
	    }
	}

}

export namespace remote {
	
	export class RemoteInfo {
	    enabled: boolean;
	    port: number;
	    token: string;
	    pin: string;
	    localUrl?: string;
	    tailscaleUrl?: string;
	    publicUrl?: string;
	    bestUrl: string;
	    qrCodeSvg: string;
	    activeClients: number;
	    hostname: string;
	    lanIps?: string[];
	    connecting: boolean;
	    downloading: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.port = source["port"];
	        this.token = source["token"];
	        this.pin = source["pin"];
	        this.localUrl = source["localUrl"];
	        this.tailscaleUrl = source["tailscaleUrl"];
	        this.publicUrl = source["publicUrl"];
	        this.bestUrl = source["bestUrl"];
	        this.qrCodeSvg = source["qrCodeSvg"];
	        this.activeClients = source["activeClients"];
	        this.hostname = source["hostname"];
	        this.lanIps = source["lanIps"];
	        this.connecting = source["connecting"];
	        this.downloading = source["downloading"];
	        this.error = source["error"];
	    }
	}

}

export namespace servers {
	
	export class Server {
	    pid: number;
	    port: number;
	    address: string;
	    name: string;
	    command: string;
	    kind: string;
	    ownerThreadId?: string;
	    ours: boolean;
	    agent?: boolean;
	    cwd?: string;
	    id?: string;
	    managed?: boolean;
	    status?: string;
	
	    static createFrom(source: any = {}) {
	        return new Server(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pid = source["pid"];
	        this.port = source["port"];
	        this.address = source["address"];
	        this.name = source["name"];
	        this.command = source["command"];
	        this.kind = source["kind"];
	        this.ownerThreadId = source["ownerThreadId"];
	        this.ours = source["ours"];
	        this.agent = source["agent"];
	        this.cwd = source["cwd"];
	        this.id = source["id"];
	        this.managed = source["managed"];
	        this.status = source["status"];
	    }
	}
	export class Group {
	    threadId?: string;
	    title: string;
	    servers: Server[];
	
	    static createFrom(source: any = {}) {
	        return new Group(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.threadId = source["threadId"];
	        this.title = source["title"];
	        this.servers = this.convertValues(source["servers"], Server);
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

export namespace session {
	
	export class AgentView {
	    threadId: string;
	    title: string;
	    driver: string;
	    account?: string;
	    model?: string;
	    state: string;
	    cwd: string;
	    projectCwd?: string;
	    workspaceId: string;
	    branch?: string;
	    summary?: string;
	    live: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AgentView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.threadId = source["threadId"];
	        this.title = source["title"];
	        this.driver = source["driver"];
	        this.account = source["account"];
	        this.model = source["model"];
	        this.state = source["state"];
	        this.cwd = source["cwd"];
	        this.projectCwd = source["projectCwd"];
	        this.workspaceId = source["workspaceId"];
	        this.branch = source["branch"];
	        this.summary = source["summary"];
	        this.live = source["live"];
	    }
	}
	export class Config {
	    driver: string;
	    account?: string;
	    model?: string;
	    options?: Record<string, any>;
	    cwd?: string;
	    permissionMode?: string;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.driver = source["driver"];
	        this.account = source["account"];
	        this.model = source["model"];
	        this.options = source["options"];
	        this.cwd = source["cwd"];
	        this.permissionMode = source["permissionMode"];
	    }
	}
	export class DriverUsage {
	    driver: string;
	    account?: string;
	    accountName?: string;
	    inputTokens: number;
	    outputTokens: number;
	    cacheReadTokens: number;
	    cacheWriteTokens: number;
	    costUsd: number;
	    turns: number;
	    sessions: number;
	    lastActiveAt?: number;
	    limits?: domain.RateLimit[];
	    limitsFetchedAt?: number;
	    limitsError?: string;
	
	    static createFrom(source: any = {}) {
	        return new DriverUsage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.driver = source["driver"];
	        this.account = source["account"];
	        this.accountName = source["accountName"];
	        this.inputTokens = source["inputTokens"];
	        this.outputTokens = source["outputTokens"];
	        this.cacheReadTokens = source["cacheReadTokens"];
	        this.cacheWriteTokens = source["cacheWriteTokens"];
	        this.costUsd = source["costUsd"];
	        this.turns = source["turns"];
	        this.sessions = source["sessions"];
	        this.lastActiveAt = source["lastActiveAt"];
	        this.limits = this.convertValues(source["limits"], domain.RateLimit);
	        this.limitsFetchedAt = source["limitsFetchedAt"];
	        this.limitsError = source["limitsError"];
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
	export class TaskRequest {
	    action?: string;
	    targetThreadId?: string;
	    title: string;
	    prompt: string;
	    cwd?: string;
	
	    static createFrom(source: any = {}) {
	        return new TaskRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.action = source["action"];
	        this.targetThreadId = source["targetThreadId"];
	        this.title = source["title"];
	        this.prompt = source["prompt"];
	        this.cwd = source["cwd"];
	    }
	}
	export class LatestPlan {
	    turnId: string;
	    tasks: TaskRequest[];
	    workspaceId?: string;
	    executed: boolean;
	    at: number;
	
	    static createFrom(source: any = {}) {
	        return new LatestPlan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.turnId = source["turnId"];
	        this.tasks = this.convertValues(source["tasks"], TaskRequest);
	        this.workspaceId = source["workspaceId"];
	        this.executed = source["executed"];
	        this.at = source["at"];
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
	export class ResumeOutcome {
	    threadId: string;
	    live: boolean;
	    continued: boolean;
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new ResumeOutcome(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.threadId = source["threadId"];
	        this.live = source["live"];
	        this.continued = source["continued"];
	        this.reason = source["reason"];
	    }
	}
	export class ResumeResult {
	    workspaceId: string;
	    outcomes: ResumeOutcome[];
	
	    static createFrom(source: any = {}) {
	        return new ResumeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workspaceId = source["workspaceId"];
	        this.outcomes = this.convertValues(source["outcomes"], ResumeOutcome);
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
	export class SpawnOptions {
	    driver: string;
	    account?: string;
	    model?: string;
	    options?: Record<string, any>;
	    cwd: string;
	    useWorktree: boolean;
	    workspaceId?: string;
	    title?: string;
	    prompt?: string;
	    permissionMode?: string;
	
	    static createFrom(source: any = {}) {
	        return new SpawnOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.driver = source["driver"];
	        this.account = source["account"];
	        this.model = source["model"];
	        this.options = source["options"];
	        this.cwd = source["cwd"];
	        this.useWorktree = source["useWorktree"];
	        this.workspaceId = source["workspaceId"];
	        this.title = source["title"];
	        this.prompt = source["prompt"];
	        this.permissionMode = source["permissionMode"];
	    }
	}
	export class SpawnRequest {
	    title: string;
	    prompt: string;
	    driver?: string;
	    account?: string;
	    model?: string;
	    options?: Record<string, any>;
	    cwd?: string;
	    preamble?: string;
	    skills?: domain.SkillPolicy;
	    files?: domain.FileRef[];
	
	    static createFrom(source: any = {}) {
	        return new SpawnRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.prompt = source["prompt"];
	        this.driver = source["driver"];
	        this.account = source["account"];
	        this.model = source["model"];
	        this.options = source["options"];
	        this.cwd = source["cwd"];
	        this.preamble = source["preamble"];
	        this.skills = this.convertValues(source["skills"], domain.SkillPolicy);
	        this.files = this.convertValues(source["files"], domain.FileRef);
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
	export class SpawnResult {
	    workspace: domain.Workspace;
	    tasks: domain.Task[];
	    errors?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SpawnResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workspace = this.convertValues(source["workspace"], domain.Workspace);
	        this.tasks = this.convertValues(source["tasks"], domain.Task);
	        this.errors = source["errors"];
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
	
	export class UsageReport {
	    drivers: DriverUsage[];
	    totals: DriverUsage;
	    startedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new UsageReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.drivers = this.convertValues(source["drivers"], DriverUsage);
	        this.totals = this.convertValues(source["totals"], DriverUsage);
	        this.startedAt = source["startedAt"];
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

export namespace skills {
	
	export class Info {
	    name: string;
	    description: string;
	    source: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new Info(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.source = source["source"];
	        this.path = source["path"];
	    }
	}

}

export namespace slashcmd {
	
	export class Command {
	    name: string;
	    description: string;
	    source: string;
	    kind: string;
	    prompt?: string;
	
	    static createFrom(source: any = {}) {
	        return new Command(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.source = source["source"];
	        this.kind = source["kind"];
	        this.prompt = source["prompt"];
	    }
	}

}

