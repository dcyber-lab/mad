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
    /// False when the agent can't take messages now (stopped, not on this Mac); `sendHint` says why.
    public var canSend: Bool
    public var sendHint: String

    public init(title: String, agentID: String, kind: String = "", state: CharacterState = .idle,
                entries: [ConvEntry] = [], live: LiveBubble? = nil, ask: AskCard? = nil,
                canSend: Bool = true, sendHint: String = "") {
        self.title = title
        self.agentID = agentID
        self.kind = kind
        self.state = state
        self.entries = entries
        self.live = live
        self.ask = ask
        self.canSend = canSend
        self.sendHint = sendHint
    }

    /// The small line under the name.
    public var stateLine: String {
        if ask != nil { return "在等你处理" }
        switch state {
        case .idle: return "空闲"
        case .working: return "在工作"
        case .needsYou: return "在等你"
        case .starting: return "启动中"
        case .stopped: return "已停止"
        }
    }

    /// The red dot shows while something waits on the person.
    public var needsYou: Bool { ask != nil || state == .needsYou }

    /// What the empty composer says.
    public var composerPrompt: String { "发消息给 \(title)" }

    /// The interrupt button shows while the agent works, and only when nothing is typed (typing turns it into send).
    public func showsInterrupt(draft: String) -> Bool { state == .working && Self.isBlank(draft) }

    public func canSubmit(draft: String) -> Bool { canSend && !Self.isBlank(draft) }

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
    public static func earlier(_ id: String = "earlier", _ note: String = "更早的消息") -> ConvEntry {
        ConvEntry(id: id, .earlier(note))
    }

    /// One label for the whole entry; `agent` is the agent's name.
    public func accessibilityLabel(agent: String) -> String {
        switch content {
        case let .user(text, images): return ["you", text.isEmpty ? nil : text, Self.imageNote(images.count)].compactMap { $0 }.joined(separator: ", ")
        case let .text(markdown): return "\(agent), \(Markdown.plain(markdown))"
        case let .tool(tool): return "\(agent), \(tool.accessibilityLabel)"
        case let .image(image): return "\(agent), \(image.alt.isEmpty ? "image" : "image, \(image.alt)")"
        case let .earlier(note): return note
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

    public init(id: String, title: String, detail: String = "", mono: Bool = false, acts: [AskAct]) {
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

public struct AskAct: Identifiable, Equatable, Sendable {
    public var id: String
    public var label: String
    /// The one Return does; drawn filled.
    public var primary: Bool
    /// It takes words first (a reason, an answer of one's own): the card asks for text before it acts.
    public var input: Bool

    public init(id: String, label: String, primary: Bool = false, input: Bool = false) {
        self.id = id
        self.label = label
        self.primary = primary
        self.input = input
    }
}

/// What the screen does; each is a closure the app fills in.
public struct ConversationActions {
    public var send: (String) -> Void
    /// An ask's act, with the words typed for an input act.
    public var act: (_ actID: String, _ text: String?) -> Void
    public var interrupt: () -> Void
    /// Nil when there is no way to load more: the earlier note is then plain text, not a button.
    public var loadEarlier: (() -> Void)?
    /// The bytes of an image by its `source`; nil keeps the placeholder.
    public var loadImage: ((ConvImage) async -> Data?)?
    public var back: () -> Void
    /// The ＋ beside the composer (attach).
    public var attach: () -> Void

    public init(send: @escaping (String) -> Void = { _ in }, act: @escaping (String, String?) -> Void = { _, _ in },
                interrupt: @escaping () -> Void = {}, loadEarlier: (() -> Void)? = nil,
                loadImage: ((ConvImage) async -> Data?)? = nil,
                back: @escaping () -> Void = {}, attach: @escaping () -> Void = {}) {
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
    }

    /// Splits on ``` fences. An unclosed fence is code to the end (the message is still being written).
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
        for line in source.split(separator: "\n", omittingEmptySubsequences: false).map(String.init) {
            if line.trimmingCharacters(in: .whitespaces).hasPrefix("```") {
                if inCode { out.append(.code(code.joined(separator: "\n"))); code = [] } else { flushText() }
                inCode.toggle()
            } else if inCode {
                code.append(line)
            } else {
                text.append(listLine(line))
            }
        }
        if inCode { out.append(.code(code.joined(separator: "\n"))) } else { flushText() }
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
            }
        }.joined(separator: " ").replacingOccurrences(of: "\n", with: " ")
    }
}
