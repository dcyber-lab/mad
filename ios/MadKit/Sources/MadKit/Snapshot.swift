import Foundation

/// The part of /api/deck the app reads (internal/desktop/snapshot.go).
/// Everything is optional or defaulted and unknown fields are ignored, so a
/// newer Mac does not break an older app.
public struct DeckSnapshot: Equatable, Decodable, Sendable {
    public struct Agent: Equatable, Decodable, Identifiable, Sendable {
        public var id: String
        public var name: String?
        public var title: String?
        public var status: String
        public var shown: String?

        /// What to call it: the user's name, else the conversation's title, else the id.
        public var label: String {
            if let n = name, !n.isEmpty { return n }
            if let t = title, !t.isEmpty { return t }
            return id
        }
        /// The state to draw.
        public var state: String { shown ?? status }

        enum CodingKeys: String, CodingKey { case id, name, title, status, shown }
        public init(from d: Decoder) throws {
            let c = try d.container(keyedBy: CodingKeys.self)
            id = try c.decode(String.self, forKey: .id)
            name = try c.decodeIfPresent(String.self, forKey: .name)
            title = try c.decodeIfPresent(String.self, forKey: .title)
            status = try c.decodeIfPresent(String.self, forKey: .status) ?? ""
            shown = try c.decodeIfPresent(String.self, forKey: .shown)
        }
        public init(id: String, name: String? = nil, title: String? = nil, status: String, shown: String? = nil) {
            self.id = id; self.name = name; self.title = title; self.status = status; self.shown = shown
        }
    }

    public struct Project: Equatable, Decodable, Identifiable, Sendable {
        public var name: String
        public var agents: [Agent]
        public var id: String { name }

        enum CodingKeys: String, CodingKey { case name, agents }
        public init(from d: Decoder) throws {
            let c = try d.container(keyedBy: CodingKeys.self)
            name = try c.decode(String.self, forKey: .name)
            agents = try c.decodeIfPresent([Agent].self, forKey: .agents) ?? []
        }
        public init(name: String, agents: [Agent]) { self.name = name; self.agents = agents }
    }

    /// One thing the person can do to an inbox item. The item describes its
    /// own acts, so no surface hard-codes buttons per kind.
    public struct Act: Equatable, Decodable, Identifiable, Sendable {
        public var verb: String
        public var label: String
        public var option: String?
        /// The act takes words first (a reply, an answer of one's own, why not).
        public var input: Bool
        public var row: Bool
        public var primary: Bool
        public var id: String { verb + "\u{1}" + (option ?? "") + "\u{1}" + label }

        /// "open" sends nothing: the surface goes to where the item is settled.
        public var isOpen: Bool { verb == "open" }

        enum CodingKeys: String, CodingKey { case verb, label, option, input, row, primary }
        public init(from d: Decoder) throws {
            let c = try d.container(keyedBy: CodingKeys.self)
            verb = try c.decode(String.self, forKey: .verb)
            label = try c.decodeIfPresent(String.self, forKey: .label) ?? verb
            option = try c.decodeIfPresent(String.self, forKey: .option)
            input = try c.decodeIfPresent(Bool.self, forKey: .input) ?? false
            row = try c.decodeIfPresent(Bool.self, forKey: .row) ?? false
            primary = try c.decodeIfPresent(Bool.self, forKey: .primary) ?? false
        }
        public init(verb: String, label: String, option: String? = nil, input: Bool = false,
                    row: Bool = false, primary: Bool = false) {
            self.verb = verb; self.label = label; self.option = option
            self.input = input; self.row = row; self.primary = primary
        }
    }

    /// Where an item is settled at length: one of the three is set.
    public struct Open: Equatable, Decodable, Sendable {
        public var agent: String?
        public var channel: String?
        public var page: String?
        public init(agent: String? = nil, channel: String? = nil, page: String? = nil) {
            self.agent = agent; self.channel = channel; self.page = page
        }
    }

    /// One thing in the inbox. `kind` is a plain string: an unknown kind
    /// still draws, since an item says its own title, text and acts.
    public struct InboxItem: Equatable, Decodable, Identifiable, Sendable {
        public var key: String
        public var kind: String
        public var agent: String?
        public var icon: String?
        public var title: String
        public var context: String?
        public var text: String?
        public var mono: Bool
        public var body: String?
        /// Body is part of what it asks, shown whole.
        public var full: Bool
        public var since: Date?
        public var blocks: Bool
        public var acts: [Act]
        public var open: Open
        public var id: String { key }

        enum CodingKeys: String, CodingKey {
            case key, kind, agent, icon, title, context, text, mono, body, full, since, blocks, acts, open
        }
        public init(from d: Decoder) throws {
            let c = try d.container(keyedBy: CodingKeys.self)
            key = try c.decode(String.self, forKey: .key)
            kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? ""
            agent = try c.decodeIfPresent(String.self, forKey: .agent)
            icon = try c.decodeIfPresent(String.self, forKey: .icon)
            title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
            context = try c.decodeIfPresent(String.self, forKey: .context)
            text = try c.decodeIfPresent(String.self, forKey: .text)
            mono = try c.decodeIfPresent(Bool.self, forKey: .mono) ?? false
            body = try c.decodeIfPresent(String.self, forKey: .body)
            full = try c.decodeIfPresent(Bool.self, forKey: .full) ?? false
            if let raw = try c.decodeIfPresent(String.self, forKey: .since) {
                since = Self.date(raw)
            }
            blocks = try c.decodeIfPresent(Bool.self, forKey: .blocks) ?? false
            acts = try c.decodeIfPresent([Act].self, forKey: .acts) ?? []
            open = try c.decodeIfPresent(Open.self, forKey: .open) ?? Open()
        }
        public init(key: String, kind: String = "", agent: String? = nil, title: String,
                    context: String? = nil, text: String? = nil, mono: Bool = false,
                    body: String? = nil, full: Bool = false, since: Date? = nil,
                    blocks: Bool = false, acts: [Act] = [], open: Open = Open()) {
            self.key = key; self.kind = kind; self.agent = agent; self.icon = nil
            self.title = title; self.context = context; self.text = text; self.mono = mono
            self.body = body; self.full = full; self.since = since; self.blocks = blocks
            self.acts = acts; self.open = open
        }

        /// Go's RFC 3339 times, with or without fractional seconds.
        static func date(_ s: String) -> Date? {
            let f = ISO8601DateFormatter()
            f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            if let d = f.date(from: s) { return d }
            f.formatOptions = [.withInternetDateTime]
            return f.date(from: s)
        }
    }

    public var projects: [Project]
    /// The inbox's items: the first `needs` need the person, the rest finished unseen.
    public var inbox: [InboxItem]
    public var needs: Int

    enum CodingKeys: String, CodingKey { case projects, inbox }
    enum InboxKeys: String, CodingKey { case items, needs }

    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        projects = try c.decodeIfPresent([Project].self, forKey: .projects) ?? []
        if let ib = try? c.nestedContainer(keyedBy: InboxKeys.self, forKey: .inbox) {
            inbox = try ib.decodeIfPresent([InboxItem].self, forKey: .items) ?? []
            needs = try ib.decodeIfPresent(Int.self, forKey: .needs) ?? 0
        } else {
            inbox = []
            needs = 0
        }
    }

    public init(projects: [Project], inbox: [InboxItem], needs: Int) {
        self.projects = projects; self.inbox = inbox; self.needs = needs
    }

    public var agentCount: Int { projects.reduce(0) { $0 + $1.agents.count } }
    /// The items that need the person, in the inbox's order.
    public var waiting: [InboxItem] { Array(inbox.prefix(min(max(needs, 0), inbox.count))) }
    /// The items that finished unseen.
    public var finished: [InboxItem] { Array(inbox.dropFirst(min(max(needs, 0), inbox.count))) }

    /// The agent an item is about, for its name.
    public func agent(id: String?) -> Agent? {
        guard let id else { return nil }
        for p in projects { if let a = p.agents.first(where: { $0.id == id }) { return a } }
        return nil
    }
}
