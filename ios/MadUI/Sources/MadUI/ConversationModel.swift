import Foundation

/// What an agent's conversation screen shows. Plain values, decoupled from MadKit: the app maps its
/// chat session, the agent store and the inbox onto them, the screen draws them.
public struct AgentConversation: Equatable, Sendable {
    public var title: String
    public var agentID: String
    /// The agent's kind ("claude", "codex", ...): with the id, it picks the character.
    public var kind: String
    public var state: CharacterState
    public var entries: [ConvEntry]
    /// The message the agent is writing now, before it is an entry.
    public var live: LiveBubble?
    /// What waits on the person (a permission, a question); pinned above the composer.
    public var ask: AskCard?
    /// A request to create an agent or a team waiting on the person (the confirm card, in the ask's place).
    public var create: CreateCard?
    /// False when the agent can't take messages now (stopped, not on this Mac); `sendHint` says why.
    public var canSend: Bool
    public var sendHint: String
    /// The head's second line after the kind: "mad / main", and what it has cost ("$0.84"; empty for nothing).
    public var place: String
    public var cost: String
    /// The Details pane and the 改动 tab; nil before the deck says.
    public var details: AgentDetails?

    public init(title: String, agentID: String, kind: String = "", state: CharacterState = .idle,
                entries: [ConvEntry] = [], live: LiveBubble? = nil, ask: AskCard? = nil, create: CreateCard? = nil,
                canSend: Bool = true, sendHint: String = "", place: String = "", cost: String = "", details: AgentDetails? = nil) {
        self.place = place
        self.cost = cost
        self.details = details
        self.title = title
        self.agentID = agentID
        self.kind = kind
        self.state = state
        self.entries = entries
        self.live = live
        self.ask = ask
        self.create = create
        self.canSend = canSend
        self.sendHint = sendHint
    }

    /// How it stands, in words: for VoiceOver, and the Details pane.
    public var stateLine: String {
        if ask != nil || create != nil { return L("在等你处理") }
        return state.title
    }

    /// The head's second line: "claude · mad / main · $0.84" (the parts it has).
    public var headLine: String {
        [kind, place, cost].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    /// The waiting ring shows while something waits on the person.
    public var needsYou: Bool { ask != nil || create != nil || state == .needsYou }

    /// What the empty composer says. The title is quoted, as an agent's title is often its task ("给我绘制一个表格"),
    /// which read as a second 给 bare. A long title is cut: the hint stays one line, as the bar is.
    public var composerPrompt: String {
        let name = title.count > 28 ? String(title.prefix(28)) + "…" : title
        return Lf("发消息给「%@」", name)
    }

    /// The interrupt button shows while the agent works, and only when nothing is typed (typing turns it into send).
    public func showsInterrupt(draft: String, hasImages: Bool = false) -> Bool { state == .working && Self.isBlank(draft) && !hasImages }

    public func canSubmit(draft: String, hasImages: Bool = false) -> Bool { canSend && (!Self.isBlank(draft) || hasImages) }

    static func isBlank(_ s: String) -> Bool { s.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }
}

public struct ConvEntry: Identifiable, Equatable, Sendable {
    public enum Content: Equatable, Sendable {
        case user(text: String, images: [ConvImage])
        /// The agent's words, as markdown.
        case text(markdown: String)
        case tool(ConvTool)
        case image(ConvImage)
        /// A line saying there is more above ("earlier messages"); tapping it loads them.
        case earlier(String)
        /// mad's own line in place of the bot's agent_new / team_start step.
        case create(CreateLine)
    }

    public var id: String
    public var content: Content

    public init(id: String, _ content: Content) {
        self.id = id
        self.content = content
    }

    public static func user(_ id: String, _ text: String, images: [ConvImage] = []) -> ConvEntry {
        ConvEntry(id: id, .user(text: text, images: images))
    }
    public static func text(_ id: String, _ markdown: String) -> ConvEntry { ConvEntry(id: id, .text(markdown: markdown)) }
    public static func tool(_ id: String, _ tool: ConvTool) -> ConvEntry { ConvEntry(id: id, .tool(tool)) }
    public static func earlier(_ id: String = "earlier", _ note: String? = nil) -> ConvEntry {
        ConvEntry(id: id, .earlier(note ?? L("更早的消息")))
    }

    /// One label for the whole entry; `agent` is the agent's name.
    public func accessibilityLabel(agent: String) -> String {
        switch content {
        case let .user(text, images): return ["you", text.isEmpty ? nil : text, Self.imageNote(images.count)].compactMap { $0 }.joined(separator: ", ")
        case let .text(markdown): return "\(agent), \(Markdown.plain(markdown))"
        case let .tool(tool): return "\(agent), \(tool.accessibilityLabel)"
        case let .image(image): return "\(agent), \(image.alt.isEmpty ? "image" : "image, \(image.alt)")"
        case let .earlier(note): return note
        case let .create(line): return line.accessibilityLabel
        }
    }

    private static func imageNote(_ n: Int) -> String? { n == 0 ? nil : n == 1 ? "1 image" : "\(n) images" }
}

/// An image in the conversation: a placeholder until its data is there.
public struct ConvImage: Equatable, Hashable, Sendable {
    public var url: URL?
    public var alt: String
    /// An address only the app knows how to fetch (it needs the Mac's token): `ConversationActions.loadImage` gets the image by it.
    public var source: String
    public init(url: URL? = nil, alt: String = "", source: String = "") {
        self.url = url
        self.alt = alt
        self.source = source
    }
}

/// A tool call: one compact row, "Tool · label", that opens to its input and result.
public struct ConvTool: Equatable, Sendable {
    public var name: String
    /// What it was pointed at: "go test ./...", a file's path.
    public var label: String
    public var input: String
    /// Nil while it runs.
    public var result: String?
    public var error: Bool

    public init(name: String, label: String = "", input: String = "", result: String? = nil, error: Bool = false) {
        self.name = name
        self.label = label
        self.input = input
        self.result = result
        self.error = error
    }

    /// "Bash · go test ./...", or the name alone.
    public var summary: String { label.isEmpty ? name : "\(name) · \(label)" }

    /// There is something to open to.
    public var hasDetail: Bool { !input.isEmpty || !(result ?? "").isEmpty }

    public var running: Bool { result == nil && !error }

    public var accessibilityLabel: String {
        var s = "tool \(summary)"
        if error { s += ", failed" } else if running { s += ", running" }
        return s
    }
}

public struct LiveBubble: Equatable, Sendable {
    public var markdown: String
    public init(markdown: String = "") { self.markdown = markdown }
}

/// What waits on the person: a permission to run a command, a question. Its acts are the person's answers.
public struct AskCard: Equatable, Sendable {
    public var id: String
    public var title: String
    /// What it is about: a command, a path.
    public var detail: String
    /// The detail is code (a command): set in a monospaced box.
    public var mono: Bool
    public var acts: [AskAct]
    /// How long it has waited: "等了 4 分钟"; empty when not known.
    public var waited: String
    /// A command to allow (the card's head says "想运行一条命令"), rather than a question.
    public var permission: Bool
    /// A form of several questions, each with its own options to pick from; 发送 answers them all once each has a pick.
    /// Empty for a card of one question or a permission (its `acts` are the answers).
    public var questions: [AskQuestion]

    public init(id: String, title: String, detail: String = "", mono: Bool = false, acts: [AskAct], waited: String = "", permission: Bool = false,
                questions: [AskQuestion] = []) {
        self.questions = questions
        self.waited = waited
        self.permission = permission
        self.id = id
        self.title = title
        self.detail = detail
        self.mono = mono
        self.acts = acts
    }

    public var accessibilityLabel: String {
        detail.isEmpty ? "needs you, \(title)" : "needs you, \(title), \(detail)"
    }
}

/// One question of a form: what it asks, and the options to pick one from.
public struct AskQuestion: Identifiable, Equatable, Sendable {
    public var id: String
    public var text: String
    public var options: [String]
    public init(id: String, text: String, options: [String]) { self.id = id; self.text = text; self.options = options }
}

public struct AskAct: Identifiable, Equatable, Sendable {
    /// What an act does, where a screen draws some of them apart: Allow and Deny on a compact card, Return and Esc.
    public enum Kind: Equatable, Sendable { case allow, allowSession, deny, denyWhy, other }

    public var id: String
    public var label: String
    /// The one Return does; drawn filled.
    public var primary: Bool
    /// It takes words first (a reason, an answer of one's own): the card asks for text before it acts.
    public var input: Bool
    public var kind: Kind

    public init(id: String, label: String, primary: Bool = false, input: Bool = false, kind: Kind = .other) {
        self.kind = kind
        self.id = id
        self.label = label
        self.primary = primary
        self.input = input
    }
}

/// What the agent page's Details pane and 改动 tab show: how it stands, its project's git and the ways to finish its
/// branch, its cost, its session.
public struct AgentDetails: Equatable, Sendable {
    /// How long it has been running: "23 分钟"; empty when not known.
    public var since: String
    public var branch: String
    /// Files changed in its project, and commits not pushed.
    public var changed: Int
    public var ahead: Int
    public var cost: String
    /// Tokens in and out: "182k / 9.4k"; empty when none.
    public var tokens: String
    /// The model it runs ("Opus"); empty for the kind's default.
    public var model: String
    /// The session's id (its first 8 characters show) and its directory ("~/src/mad").
    public var session: String
    public var directory: String
    /// The ways to finish its branch (push, a PR, rebase, merge, the person's own), in the Mac's order.
    public var finish: [FinishButton]

    public init(since: String = "", branch: String = "", changed: Int = 0, ahead: Int = 0, cost: String = "", tokens: String = "",
                model: String = "", session: String = "", directory: String = "", finish: [FinishButton] = []) {
        self.since = since; self.branch = branch; self.changed = changed; self.ahead = ahead; self.cost = cost
        self.tokens = tokens; self.model = model; self.session = session; self.directory = directory; self.finish = finish
    }
}

/// One way to finish a branch, as a button: its place in the Mac's list (what is sent back with its name), its words,
/// and whether it is the primary one (push and open a PR).
public struct FinishButton: Identifiable, Equatable, Sendable {
    public var index: Int
    public var name: String
    public var label: String
    public var primary: Bool
    public var id: String { "\(index)\u{1}\(name)" }

    public init(index: Int, name: String, label: String, primary: Bool = false) {
        self.index = index; self.name = name; self.label = label; self.primary = primary
    }
}

/// What the screen does; each is a closure the app fills in.
public struct ConversationActions {
    public var send: (String, [ChannelDraftImage]) -> Void
    /// An ask's act: the card it was made on (its `id`), the act, and the words typed for an input act.
    public var act: (_ cardID: String, _ actID: String, _ text: String?) -> Void
    public var interrupt: () -> Void
    /// Nil when there is no way to load more: the earlier note is then plain text, not a button.
    public var loadEarlier: (() -> Void)?
    /// The bytes of an image by its `source`; nil keeps the placeholder.
    public var loadImage: ((ConvImage) async -> Data?)?
    public var back: () -> Void
    /// The ＋ beside the composer (attach).
    public var attach: () -> Void
    /// The agent's changes in the web page (they have no native view yet): the 改动 tab's button. Nil draws none.
    public var openWeb: (() -> Void)?
    /// The ⋯ menu's 在终端打开: the agent in mad's terminal deck. Nil leaves it out.
    public var openTerminal: (() -> Void)?
    /// A finish button of the Details pane or the 改动 tab was pressed.
    public var finish: ((FinishButton) -> Void)?
    /// The Details pane's buttons; nil leaves a button out.
    public var rename: ((String) -> Void)?
    public var sleep: (() -> Void)?
    /// 打开 on a create result line: the team's channel or the made agent.
    public var openCreated: (CreateLine.Target) -> Void

    public init(send: @escaping (String, [ChannelDraftImage]) -> Void = { _, _ in }, act: @escaping (String, String, String?) -> Void = { _, _, _ in },
                interrupt: @escaping () -> Void = {}, loadEarlier: (() -> Void)? = nil,
                loadImage: ((ConvImage) async -> Data?)? = nil,
                back: @escaping () -> Void = {}, attach: @escaping () -> Void = {},
                openWeb: (() -> Void)? = nil, openTerminal: (() -> Void)? = nil, finish: ((FinishButton) -> Void)? = nil,
                rename: ((String) -> Void)? = nil, sleep: (() -> Void)? = nil,
                openCreated: @escaping (CreateLine.Target) -> Void = { _ in }) {
        self.openCreated = openCreated
        self.openTerminal = openTerminal
        self.finish = finish
        self.rename = rename
        self.sleep = sleep
        self.openWeb = openWeb
        self.send = send
        self.act = act
        self.interrupt = interrupt
        self.loadEarlier = loadEarlier
        self.loadImage = loadImage
        self.back = back
        self.attach = attach
    }
}

/// Just enough markdown for a chat bubble: paragraphs (inline markdown inside) and fenced code blocks.
public enum Markdown {
    public enum Block: Equatable, Sendable {
        case text(String)
        case code(String)
        /// A pipe table: its header cells and its rows' cells (a short row is padded to the header's width).
        case table(header: [String], rows: [[String]])
    }

    /// Splits on ``` fences and pipe tables. An unclosed fence is code to the end (the message is still being written).
    /// Heading marks are dropped and bullets become "•", so no bare marks show.
    public static func blocks(_ source: String) -> [Block] {
        var out: [Block] = []
        var text: [String] = []
        var code: [String] = []
        var inCode = false
        func flushText() {
            let s = text.joined(separator: "\n").trimmingCharacters(in: .newlines)
            if !s.isEmpty { out.append(.text(s)) }
            text = []
        }
        let lines = source.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
        var i = 0
        while i < lines.count {
            let line = lines[i]
            if line.trimmingCharacters(in: .whitespaces).hasPrefix("```") {
                if inCode { out.append(.code(code.joined(separator: "\n"))); code = [] } else { flushText() }
                inCode.toggle()
            } else if inCode {
                code.append(line)
            } else if i + 1 < lines.count, isRow(line), isRule(lines[i + 1]) {
                // a header row, its rule, and the rows under it
                flushText()
                let header = cells(line)
                var rows: [[String]] = []
                i += 2
                while i < lines.count, isRow(lines[i]) {
                    var r = cells(lines[i])
                    if r.count < header.count { r += Array(repeating: "", count: header.count - r.count) }
                    rows.append(Array(r.prefix(header.count)))
                    i += 1
                }
                out.append(.table(header: header, rows: rows))
                continue
            } else {
                text.append(listLine(line))
            }
            i += 1
        }
        if inCode { out.append(.code(code.joined(separator: "\n"))) } else { flushText() }
        return out
    }

    static func isRow(_ line: String) -> Bool {
        let t = line.trimmingCharacters(in: .whitespaces)
        return t.hasPrefix("|") && t.count > 1
    }

    /// `|---|:--:|` under a header row.
    static func isRule(_ line: String) -> Bool {
        guard isRow(line) else { return false }
        let parts = cells(line)
        return !parts.isEmpty && parts.allSatisfy { p in
            let core = p.trimmingCharacters(in: CharacterSet(charactersIn: ":"))
            return !core.isEmpty && core.allSatisfy { $0 == "-" }
        }
    }

    /// A row's cells, trimmed; `\|` is a pipe inside a cell.
    static func cells(_ line: String) -> [String] {
        var t = line.trimmingCharacters(in: .whitespaces)
        if t.hasPrefix("|") { t.removeFirst() }
        if t.hasSuffix("|"), !t.hasSuffix("\\|") { t.removeLast() }
        var out: [String] = []
        var cur = ""
        for ch in t {
            if ch == "|", cur.hasSuffix("\\") {
                cur.removeLast()
                cur.append(ch)
            } else if ch == "|" {
                out.append(cur.trimmingCharacters(in: .whitespaces))
                cur = ""
            } else {
                cur.append(ch)
            }
        }
        out.append(cur.trimmingCharacters(in: .whitespaces))
        return out
    }

    static func listLine(_ line: String) -> String {
        let t = line.drop { $0 == " " }
        let indent = String(line.prefix(line.count - t.count))
        if t.hasPrefix("- ") || t.hasPrefix("* ") { return indent + "• " + t.dropFirst(2) }
        if t.hasPrefix("#") {
            let body = t.drop { $0 == "#" }
            if body.hasPrefix(" ") { return "**" + body.dropFirst() + "**" }
        }
        return line
    }

    /// What a screen reader says: the text without its marks, code blocks named once.
    public static func plain(_ source: String) -> String {
        blocks(source).map { block in
            switch block {
            case let .text(s):
                return (try? AttributedString(markdown: s, options: .init(interpretedSyntax: .inlineOnlyPreservingWhitespace)))
                    .map { String($0.characters) } ?? s
            case .code: return "code block"
            case let .table(header, rows):
                return ([header] + rows).map { $0.joined(separator: ", ") }.joined(separator: "; ")
            }
        }.joined(separator: " ").replacingOccurrences(of: "\n", with: " ")
    }
}
