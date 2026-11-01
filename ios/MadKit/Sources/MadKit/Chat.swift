import Foundation

/// An image an entry shows. Its data stays on the Mac; the app shows a
/// placeholder for now.
public struct ChatImage: Equatable, Decodable, Sendable {
    public var mime: String?
    public var url: String?

    public init(mime: String? = nil, url: String? = nil) { self.mime = mime; self.url = url }
}

/// One thing a conversation shows (discover.Entry): kinds are strings and
/// unknown fields are ignored, so a newer Mac does not break the app.
public struct ChatEntry: Equatable, Decodable, Sendable {
    public var kind: String
    /// Markdown for a user or agent text; plain output for a result.
    public var text: String?
    /// A call's tool and what it was pointed at.
    public var tool: String?
    public var label: String?
    /// A call's input made readable: a command, a diff.
    public var input: String?
    public var images: [ChatImage]
    /// Pairs a call with its result.
    public var id: String?
    public var error: Bool
    /// The agent's message it is a part of (lines up with what is streamed).
    public var msg: String?
    public var at: Date?

    enum CodingKeys: String, CodingKey { case kind, text, tool, label, input, images, id, error, msg, at }

    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? ""
        text = try c.decodeIfPresent(String.self, forKey: .text)
        tool = try c.decodeIfPresent(String.self, forKey: .tool)
        label = try c.decodeIfPresent(String.self, forKey: .label)
        input = try c.decodeIfPresent(String.self, forKey: .input)
        images = try c.decodeIfPresent([ChatImage].self, forKey: .images) ?? []
        id = try c.decodeIfPresent(String.self, forKey: .id)
        error = try c.decodeIfPresent(Bool.self, forKey: .error) ?? false
        msg = try c.decodeIfPresent(String.self, forKey: .msg)
        if let raw = try? c.decodeIfPresent(String.self, forKey: .at) { at = InboxDates.parse(raw) }
    }

    public init(kind: String, text: String? = nil, tool: String? = nil, label: String? = nil, input: String? = nil,
                images: [ChatImage] = [], id: String? = nil, error: Bool = false, msg: String? = nil, at: Date? = nil) {
        self.kind = kind; self.text = text; self.tool = tool; self.label = label; self.input = input
        self.images = images; self.id = id; self.error = error; self.msg = msg; self.at = at
    }
}

/// One answer of /api/chat: the entries written after the offset asked for.
public struct ChatPage: Equatable, Decodable, Sendable {
    public var file: String
    public var offset: Int64
    public var entries: [ChatEntry]
    /// The entries start the conversation over.
    public var reset: Bool
    /// Entries before them were left out.
    public var earlier: Bool

    enum CodingKeys: String, CodingKey { case file, offset, entries, reset, earlier }

    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        file = try c.decodeIfPresent(String.self, forKey: .file) ?? ""
        offset = try c.decodeIfPresent(Int64.self, forKey: .offset) ?? 0
        entries = try c.decodeIfPresent([ChatEntry].self, forKey: .entries) ?? []
        reset = try c.decodeIfPresent(Bool.self, forKey: .reset) ?? false
        earlier = try c.decodeIfPresent(Bool.self, forKey: .earlier) ?? false
    }

    public init(file: String, offset: Int64, entries: [ChatEntry], reset: Bool = false, earlier: Bool = false) {
        self.file = file; self.offset = offset; self.entries = entries; self.reset = reset; self.earlier = earlier
    }
}

/// One part of the message being written (live.Block).
public struct LiveBlock: Equatable, Decodable, Sendable {
    /// text, thinking, tool; others as the API names them.
    public var kind: String
    public var text: String?
    public var tool: String?
    public var id: String?
    public var done: Bool

    enum CodingKeys: String, CodingKey { case kind, text, tool, id, done }
    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? ""
        text = try c.decodeIfPresent(String.self, forKey: .text)
        tool = try c.decodeIfPresent(String.self, forKey: .tool)
        id = try c.decodeIfPresent(String.self, forKey: .id)
        done = try c.decodeIfPresent(Bool.self, forKey: .done) ?? false
    }
    public init(kind: String, text: String? = nil, tool: String? = nil, id: String? = nil, done: Bool = false) {
        self.kind = kind; self.text = text; self.tool = tool; self.id = id; self.done = done
    }
}

/// What an agent's model is writing now, or wrote last (live.Message).
public struct LiveMessage: Equatable, Decodable, Sendable {
    public var id: String
    public var blocks: [LiveBlock]
    public var done: Bool
    public var version: Int64

    enum CodingKeys: String, CodingKey { case id, blocks, done, version }
    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        id = try c.decodeIfPresent(String.self, forKey: .id) ?? ""
        blocks = try c.decodeIfPresent([LiveBlock].self, forKey: .blocks) ?? []
        done = try c.decodeIfPresent(Bool.self, forKey: .done) ?? false
        version = try c.decodeIfPresent(Int64.self, forKey: .version) ?? 0
    }
    public init(id: String, blocks: [LiveBlock], done: Bool = false, version: Int64 = 0) {
        self.id = id; self.blocks = blocks; self.done = done; self.version = version
    }
}

/// A row of the conversation as the screen draws it.
public enum ChatRow: Equatable, Sendable {
    case user(ChatEntry)
    case text(ChatEntry)
    /// A call, with its result folded under it when it has come.
    case tool(call: ChatEntry, result: ChatEntry?)
}

/// The conversation as it has been read: the entries so far, where the next
/// look starts, and how to draw them.
public struct ChatTranscript: Equatable, Sendable {
    public private(set) var entries: [ChatEntry] = []
    /// Which transcript the offset is in; "" before the first look.
    public private(set) var file = ""
    public private(set) var offset: Int64 = 0
    public private(set) var earlier = false

    public init() {}

    /// Takes one answer of /api/chat: it replaces everything when it says
    /// `reset`, and is added after what is there otherwise.
    public mutating func apply(_ page: ChatPage) {
        if page.reset {
            entries = page.entries
            earlier = page.earlier
        } else {
            entries += page.entries
        }
        file = page.file
        offset = page.offset
    }

    /// Whether the transcript has an entry that is a part of message `id`:
    /// from then on the transcript, not the live bubble, shows it.
    public func hasMessage(_ id: String) -> Bool {
        !id.isEmpty && entries.contains { $0.msg == id }
    }

    /// The rows to draw: a result goes under the call with its id; kinds not
    /// known (commands, thinking, a result with no call) are left out.
    public var rows: [ChatRow] {
        var results: [String: ChatEntry] = [:]
        for e in entries where e.kind == "result" {
            if let id = e.id { results[id] = e }
        }
        var out: [ChatRow] = []
        for e in entries {
            switch e.kind {
            case "user": out.append(.user(e))
            case "text": out.append(.text(e))
            case "tool": out.append(.tool(call: e, result: e.id.flatMap { results[$0] }))
            default: break
            }
        }
        return out
    }
}

/// The message being written, and whether to show it: until the transcript
/// has an entry for it, and not once it ended a while ago.
public struct LiveState: Equatable, Sendable {
    public var message: LiveMessage?
    public var receivedAt: Date?
    /// How long an ended message may still show before the transcript's copy is trusted.
    public static let endedShows: TimeInterval = 5

    public init() {}

    public mutating func receive(_ m: LiveMessage, at now: Date = Date()) {
        guard m.version > (message?.version ?? -1) else { return }
        message = m
        receivedAt = now
    }

    public var version: Int64 { message?.version ?? 0 }

    public func visible(in transcript: ChatTranscript, now: Date = Date()) -> LiveMessage? {
        guard let m = message, !m.id.isEmpty, !m.blocks.isEmpty, !transcript.hasMessage(m.id) else { return nil }
        if m.done, let at = receivedAt, now.timeIntervalSince(at) > Self.endedShows { return nil }
        return m
    }
}
