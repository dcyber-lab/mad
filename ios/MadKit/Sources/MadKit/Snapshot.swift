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

    /// One thing waiting on the person.
    public struct InboxItem: Equatable, Decodable, Identifiable, Sendable {
        public var key: String
        public var title: String
        public var context: String?
        public var text: String?
        public var blocks: Bool
        public var id: String { key }

        enum CodingKeys: String, CodingKey { case key, title, context, text, blocks }
        public init(from d: Decoder) throws {
            let c = try d.container(keyedBy: CodingKeys.self)
            key = try c.decode(String.self, forKey: .key)
            title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
            context = try c.decodeIfPresent(String.self, forKey: .context)
            text = try c.decodeIfPresent(String.self, forKey: .text)
            blocks = try c.decodeIfPresent(Bool.self, forKey: .blocks) ?? false
        }
        public init(key: String, title: String, context: String? = nil, text: String? = nil, blocks: Bool = false) {
            self.key = key; self.title = title; self.context = context; self.text = text; self.blocks = blocks
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
    public var waiting: [InboxItem] { Array(inbox.prefix(min(max(needs, 0), inbox.count))) }
}
