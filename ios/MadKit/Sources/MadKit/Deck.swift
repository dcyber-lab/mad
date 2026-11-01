import Foundation

// What GET /api/deck returns (internal/desktop/snapshot.go), as the apps read it. Every field is
// optional or defaulted and unknown fields are ignored, so a newer Mac does not break an older app.

/// The whole deck: the Mac's name, its projects and agents, teams, channels, what waits on the
/// person, the subscriptions' usage, and how each state is worded.
public struct Deck: Equatable, Decodable, Sendable {
    /// The Mac's friendly name; nil from a server that does not send it.
    public var machine: String?
    public var projects: [Project]
    public var teams: [Team]
    /// The channels and DMs (`bus`), in the order the server lists them.
    public var channels: [Channel]
    public var inbox: Inbox
    /// How much of each kind's subscription is used, claude first.
    public var usage: [Usage]
    /// How each shown state is worded, most urgent first.
    public var states: [Look]
    /// The ids of the agents at work, longest in it first.
    public var working: [String]
    /// When the server made this look.
    public var at: Date?

    enum CodingKeys: String, CodingKey { case machine, projects, teams, bus, inbox, quota, states, working, at }

    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        let m = try c.decodeIfPresent(String.self, forKey: .machine)?.trimmingCharacters(in: .whitespacesAndNewlines)
        machine = (m?.isEmpty ?? true) ? nil : m
        projects = try c.decodeIfPresent([Project].self, forKey: .projects) ?? []
        teams = try c.decodeIfPresent([Team].self, forKey: .teams) ?? []
        channels = try c.decodeIfPresent([Channel].self, forKey: .bus) ?? []
        inbox = try c.decodeIfPresent(Inbox.self, forKey: .inbox) ?? Inbox()
        usage = (try? c.decodeIfPresent(UsageList.self, forKey: .quota))??.items ?? []
        states = try c.decodeIfPresent([Look].self, forKey: .states) ?? []
        working = try c.decodeIfPresent([String].self, forKey: .working) ?? []
        at = MadDate.decode(c, .at)
    }

    public init(machine: String? = nil, projects: [Project] = [], teams: [Team] = [], channels: [Channel] = [],
                inbox: Inbox = Inbox(), usage: [Usage] = [], states: [Look] = [], working: [String] = [], at: Date? = nil) {
        self.machine = machine; self.projects = projects; self.teams = teams; self.channels = channels
        self.inbox = inbox; self.usage = usage; self.states = states; self.working = working; self.at = at
    }

    // MARK: Lookups

    /// Every agent of every project.
    public var agents: [Agent] { projects.flatMap(\.agents) }

    public func agent(id: String?) -> Agent? {
        guard let id else { return nil }
        for p in projects { if let a = p.agents.first(where: { $0.id == id }) { return a } }
        return nil
    }

    public func channel(id: String?) -> Channel? {
        guard let id else { return nil }
        return channels.first { $0.id == id }
    }

    /// The word the apps show for a shown state ("在工作", "在等你"). The deck's own words stay
    /// English for the TUI; the apps translate by the server's word, and an unknown state is shown as it is.
    public func word(for state: String) -> String {
        let word = states.first { $0.state == state }?.word ?? state.prefix(1).uppercased() + state.dropFirst()
        return Deck.stateWords[word] ?? word
    }

    static let stateWords = ["Needs you": "在等你", "Working": "在工作", "Idle": "空闲", "Stopped": "已停止",
                             "Finished": "已完成", "Asleep": "已休眠", "Starting": "启动中"]

    /// The tone of a shown state: working, needs, finished, quiet or error.
    public func tone(for state: String) -> String {
        states.first { $0.state == state }?.tone ?? "quiet"
    }
}

/// A project (a git repository) with its agents.
public struct Project: Equatable, Decodable, Identifiable, Sendable {
    public var name: String
    public var path: String
    public var branch: String?
    public var changed: Int
    public var ahead: Int
    public var agents: [Agent]
    /// Its agents' shown states counted, most urgent first.
    public var rollup: [StateCount]
    public var id: String { path.isEmpty ? name : path }

    enum CodingKeys: String, CodingKey { case name, path, branch, changed, ahead, agents, rollup }
    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
        path = try c.decodeIfPresent(String.self, forKey: .path) ?? ""
        branch = try c.decodeIfPresent(String.self, forKey: .branch)
        changed = try c.decodeIfPresent(Int.self, forKey: .changed) ?? 0
        ahead = try c.decodeIfPresent(Int.self, forKey: .ahead) ?? 0
        agents = try c.decodeIfPresent([Agent].self, forKey: .agents) ?? []
        rollup = (try? c.decodeIfPresent([StateCount].self, forKey: .rollup)) ?? []
    }
    public init(name: String, path: String = "", branch: String? = nil, changed: Int = 0, ahead: Int = 0,
                agents: [Agent] = [], rollup: [StateCount] = []) {
        self.name = name; self.path = path; self.branch = branch; self.changed = changed; self.ahead = ahead
        self.agents = agents; self.rollup = rollup
    }
}

/// How many agents of a group are in one shown state.
public struct StateCount: Equatable, Decodable, Sendable {
    public var state: String
    public var n: Int
    public init(state: String, n: Int) { self.state = state; self.n = n }
}

/// A team's agents in a project, and how they stand.
public struct Team: Equatable, Decodable, Identifiable, Sendable {
    public var name: String
    public var project: String
    /// Agent ids, in the deck's order.
    public var members: [String]
    public var rollup: [StateCount]
    public var id: String { project + "\u{1}" + name }

    enum CodingKeys: String, CodingKey { case name, project, members, rollup }
    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        project = try c.decodeIfPresent(String.self, forKey: .project) ?? ""
        members = try c.decodeIfPresent([String].self, forKey: .members) ?? []
        rollup = (try? c.decodeIfPresent([StateCount].self, forKey: .rollup)) ?? []
    }
    public init(name: String, project: String, members: [String], rollup: [StateCount] = []) {
        self.name = name; self.project = project; self.members = members; self.rollup = rollup
    }
}

/// An agent: a claude, codex, pi or shell in a project.
public struct Agent: Equatable, Decodable, Identifiable, Sendable {
    public var id: String
    public var kind: String
    public var icon: String?
    public var color: String?
    /// The person's name for it.
    public var name: String?
    /// The conversation's title.
    public var title: String?
    /// The tracked status (running, waiting, idle, asleep, stopped, exited, starting).
    public var status: String
    /// The state to draw: `status`, with a finished unseen run told apart.
    public var shown: String?
    public var done: Bool
    public var cost: Double
    public var role: String?
    public var team: String?
    /// Its transcript reads as a conversation.
    public var chat: Bool
    /// It can run in window mode; `mode` is "window" when it does.
    public var headless: Bool
    public var mode: String?
    /// Since when it has been in its state, and what it does now.
    public var since: Date?
    public var tool: String?
    public var target: String?
    public var reply: String?
    /// Its last line, for a list.
    public var last: String

    /// What to call it: the person's name, else the conversation's title, else the kind and the
    /// first 8 characters of the id ("claude 6625f15d"), so untitled agents tell apart.
    public var label: String {
        if let n = name, !n.isEmpty { return n }
        if let t = title, !t.isEmpty { return t }
        if !kind.isEmpty { return "\(kind) \(id.prefix(8))" }
        return id
    }

    /// Whether the person or the conversation named it.
    public var isNamed: Bool { !(name ?? "").isEmpty || !(title ?? "").isEmpty }
    public var inWindowMode: Bool { mode == "window" }
    /// The state to draw.
    public var state: String { shown ?? status }

    enum CodingKeys: String, CodingKey {
        case id, kind, icon, color, name, title, status, shown, done, cost, role, team, chat, headless, mode
        case since, tool, target, reply, last
    }

    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? ""
        icon = try c.decodeIfPresent(String.self, forKey: .icon)
        color = try c.decodeIfPresent(String.self, forKey: .color)
        name = try c.decodeIfPresent(String.self, forKey: .name)
        title = try c.decodeIfPresent(String.self, forKey: .title)
        status = try c.decodeIfPresent(String.self, forKey: .status) ?? ""
        shown = try c.decodeIfPresent(String.self, forKey: .shown)
        done = try c.decodeIfPresent(Bool.self, forKey: .done) ?? false
        cost = try c.decodeIfPresent(Double.self, forKey: .cost) ?? 0
        role = try c.decodeIfPresent(String.self, forKey: .role)
        team = try c.decodeIfPresent(String.self, forKey: .team)
        chat = try c.decodeIfPresent(Bool.self, forKey: .chat) ?? false
        headless = try c.decodeIfPresent(Bool.self, forKey: .headless) ?? false
        mode = try c.decodeIfPresent(String.self, forKey: .mode)
        since = MadDate.decode(c, .since)
        tool = try c.decodeIfPresent(String.self, forKey: .tool)
        target = try c.decodeIfPresent(String.self, forKey: .target)
        reply = try c.decodeIfPresent(String.self, forKey: .reply)
        last = try c.decodeIfPresent(String.self, forKey: .last) ?? ""
    }

    public init(id: String, kind: String = "", name: String? = nil, title: String? = nil, status: String = "",
                shown: String? = nil, team: String? = nil, role: String? = nil, mode: String? = nil,
                headless: Bool = false, last: String = "", cost: Double = 0) {
        self.id = id; self.kind = kind; self.icon = nil; self.color = nil; self.name = name; self.title = title
        self.status = status; self.shown = shown; self.done = false; self.cost = cost; self.role = role
        self.team = team; self.chat = false; self.headless = headless; self.mode = mode; self.since = nil
        self.tool = nil; self.target = nil; self.reply = nil; self.last = last
    }
}

/// A channel or a DM, as the list draws it.
public struct Channel: Equatable, Decodable, Identifiable, Sendable {
    public var id: String
    /// #name, @agent (a DM with you), a ↔ b.
    public var title: String
    public var topic: String?
    public var dm: Bool
    /// A DM with you: you may write in it.
    public var yours: Bool
    public var members: [Member]
    /// The number of the last message.
    public var last: Int
    /// The last message as a line of a list: "who: its first line".
    public var lastText: String
    public var lastAt: Date?
    public var unread: Int
    /// An unread message in it asks you.
    public var mention: Bool
    public var listed: Bool
    public var paused: Bool
    /// The agent that answers for the channel's work.
    public var lead: String?
    /// The team it is the channel of.
    public var team: ChannelTeam?

    public struct Member: Equatable, Decodable, Identifiable, Sendable {
        public var id: String
        public var label: String
        public var kind: String
        enum CodingKeys: String, CodingKey { case id, label, kind }
        public init(from d: Decoder) throws {
            let c = try d.container(keyedBy: CodingKeys.self)
            id = try c.decodeIfPresent(String.self, forKey: .id) ?? ""
            label = try c.decodeIfPresent(String.self, forKey: .label) ?? ""
            kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? ""
        }
        public init(id: String, label: String, kind: String = "") { self.id = id; self.label = label; self.kind = kind }
    }

    public struct ChannelTeam: Equatable, Decodable, Sendable {
        public var name: String
        public var template: String
        public var roles: [Role]
        public struct Role: Equatable, Decodable, Sendable {
            public var role: String
            public var agent: String?
            public var lead: Bool
            enum CodingKeys: String, CodingKey { case role, agent, lead }
            public init(from d: Decoder) throws {
                let c = try d.container(keyedBy: CodingKeys.self)
                role = try c.decodeIfPresent(String.self, forKey: .role) ?? ""
                agent = try c.decodeIfPresent(String.self, forKey: .agent)
                lead = try c.decodeIfPresent(Bool.self, forKey: .lead) ?? false
            }
            public init(role: String, agent: String? = nil, lead: Bool = false) { self.role = role; self.agent = agent; self.lead = lead }
        }
        enum CodingKeys: String, CodingKey { case name, template, roles }
        public init(from d: Decoder) throws {
            let c = try d.container(keyedBy: CodingKeys.self)
            name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
            template = try c.decodeIfPresent(String.self, forKey: .template) ?? ""
            roles = try c.decodeIfPresent([Role].self, forKey: .roles) ?? []
        }
        public init(name: String, template: String = "", roles: [Role] = []) { self.name = name; self.template = template; self.roles = roles }
    }

    enum CodingKeys: String, CodingKey {
        case id, title, topic, dm, yours, members, last, lastText = "last_text", lastAt = "last_at"
        case unread, mention, listed, paused, lead, team
    }

    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        title = try c.decodeIfPresent(String.self, forKey: .title) ?? "#" + id
        topic = try c.decodeIfPresent(String.self, forKey: .topic)
        dm = try c.decodeIfPresent(Bool.self, forKey: .dm) ?? false
        yours = try c.decodeIfPresent(Bool.self, forKey: .yours) ?? false
        members = try c.decodeIfPresent([Member].self, forKey: .members) ?? []
        last = try c.decodeIfPresent(Int.self, forKey: .last) ?? 0
        lastText = try c.decodeIfPresent(String.self, forKey: .lastText) ?? ""
        lastAt = MadDate.decode(c, .lastAt)
        unread = try c.decodeIfPresent(Int.self, forKey: .unread) ?? 0
        mention = try c.decodeIfPresent(Bool.self, forKey: .mention) ?? false
        listed = try c.decodeIfPresent(Bool.self, forKey: .listed) ?? false
        paused = try c.decodeIfPresent(Bool.self, forKey: .paused) ?? false
        lead = try c.decodeIfPresent(String.self, forKey: .lead)
        team = try c.decodeIfPresent(ChannelTeam.self, forKey: .team)
    }

    public init(id: String, title: String? = nil, topic: String? = nil, dm: Bool = false, yours: Bool = false,
                members: [Member] = [], last: Int = 0, lastText: String = "", lastAt: Date? = nil, unread: Int = 0,
                mention: Bool = false, listed: Bool = false, paused: Bool = false, lead: String? = nil, team: ChannelTeam? = nil) {
        self.id = id; self.title = title ?? "#" + id; self.topic = topic; self.dm = dm; self.yours = yours
        self.members = members; self.last = last; self.lastText = lastText; self.lastAt = lastAt; self.unread = unread
        self.mention = mention; self.listed = listed; self.paused = paused; self.lead = lead; self.team = team
    }
}

/// How one shown state reads: the deck's word for it, its glyph and its tone.
public struct Look: Equatable, Decodable, Sendable {
    public var state: String
    public var word: String
    /// working, needs, finished, quiet or error.
    public var tone: String
    public var glyph: String
    public var meaning: String

    enum CodingKeys: String, CodingKey { case state, word, tone, glyph, meaning }
    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        state = try c.decode(String.self, forKey: .state)
        word = try c.decodeIfPresent(String.self, forKey: .word) ?? state
        tone = try c.decodeIfPresent(String.self, forKey: .tone) ?? "quiet"
        glyph = try c.decodeIfPresent(String.self, forKey: .glyph) ?? ""
        meaning = try c.decodeIfPresent(String.self, forKey: .meaning) ?? ""
    }
    public init(state: String, word: String, tone: String, glyph: String = "", meaning: String = "") {
        self.state = state; self.word = word; self.tone = tone; self.glyph = glyph; self.meaning = meaning
    }
}

/// What waits on the person: the first `needs` items need them, the rest finished unseen.
public struct Inbox: Equatable, Decodable, Sendable {
    public var items: [InboxItem]
    public var needs: Int

    enum CodingKeys: String, CodingKey { case items, needs }
    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        items = try c.decodeIfPresent([InboxItem].self, forKey: .items) ?? []
        needs = try c.decodeIfPresent(Int.self, forKey: .needs) ?? 0
    }
    public init(items: [InboxItem] = [], needs: Int = 0) { self.items = items; self.needs = needs }

    /// The items that need the person, in the inbox's order.
    public var waiting: [InboxItem] { Array(items.prefix(min(max(needs, 0), items.count))) }
    /// The items that finished unseen.
    public var finished: [InboxItem] { Array(items.dropFirst(min(max(needs, 0), items.count))) }
}

/// One thing the person can do to an inbox item. The item describes its
/// own acts, so no surface hard-codes buttons per kind.
public struct InboxAct: Equatable, Decodable, Identifiable, Sendable {
    public var verb: String
    public var label: String
    public var option: String?
    /// The act takes words first (a reply, an answer of one's own, why not).
    public var input: Bool
    public var row: Bool
    public var primary: Bool
    /// Which act of its verb this is where the verb alone does not say ("plan": the allow that
    /// approves a plan; "dm", "board", "read": what an open goes to), and what an open goes to
    /// ("#ios", an agent's name). Optional: an older server sends neither and the label shows.
    public var kind: String?
    public var target: String?
    public var id: String { verb + "\u{1}" + (option ?? "") + "\u{1}" + label }

    /// "open" sends nothing: the surface goes to where the item is settled.
    public var isOpen: Bool { verb == "open" }

    /// What the apps' button says. The server's label stays English (the TUI draws it), so this maps
    /// by the act's verb and flags, never by its text; an act with no word here (an option's own
    /// label, "Open #chan") keeps the server's.
    public var shownLabel: String {
        switch verb {
        case "allow": return option != nil ? label : kind == "plan" ? "批准计划" : "允许"
        case "always": return "总是允许"
        case "deny": return input ? (row ? "继续规划" : "改为告诉它…") : row ? "拒绝" : "取消"
        case "answer": return input ? "回复" : label
        case "reply": return "回复"
        case "approve": return "批准"
        case "discard": return "丢弃"
        case "seen": return "标为已读"
        case "send": return "立即发送"
        case "drop": return "放弃"
        case "resume": return "继续"
        case "raise": return option.map { "加 $\($0)" } ?? label
        case "lift": return "今天不限"
        case "open":
            switch kind {
            case "dm": return "打开私信"
            case "board": return "打开看板"
            case "read": return "去看看"
            default: return target.map { "打开 \($0)" } ?? label
            }
        default: return label
        }
    }

    enum CodingKeys: String, CodingKey { case verb, label, option, input, row, primary, kind, target }
    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        verb = try c.decode(String.self, forKey: .verb)
        label = try c.decodeIfPresent(String.self, forKey: .label) ?? verb
        option = try c.decodeIfPresent(String.self, forKey: .option)
        input = try c.decodeIfPresent(Bool.self, forKey: .input) ?? false
        row = try c.decodeIfPresent(Bool.self, forKey: .row) ?? false
        primary = try c.decodeIfPresent(Bool.self, forKey: .primary) ?? false
        kind = try c.decodeIfPresent(String.self, forKey: .kind)
        target = try c.decodeIfPresent(String.self, forKey: .target)
    }
    public init(verb: String, label: String, option: String? = nil, input: Bool = false,
                row: Bool = false, primary: Bool = false, kind: String? = nil, target: String? = nil) {
        self.verb = verb; self.label = label; self.option = option; self.kind = kind; self.target = target
        self.input = input; self.row = row; self.primary = primary
    }
}

/// Where an item is settled at length: one of the three is set.
public struct InboxOpen: Equatable, Decodable, Sendable {
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
    public var acts: [InboxAct]
    public var open: InboxOpen
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
        acts = try c.decodeIfPresent([InboxAct].self, forKey: .acts) ?? []
        open = try c.decodeIfPresent(InboxOpen.self, forKey: .open) ?? InboxOpen()
    }
    public init(key: String, kind: String = "", agent: String? = nil, title: String,
                context: String? = nil, text: String? = nil, mono: Bool = false,
                body: String? = nil, full: Bool = false, since: Date? = nil,
                blocks: Bool = false, acts: [InboxAct] = [], open: InboxOpen = InboxOpen()) {
        self.key = key; self.kind = kind; self.agent = agent; self.icon = nil
        self.title = title; self.context = context; self.text = text; self.mono = mono
        self.body = body; self.full = full; self.since = since; self.blocks = blocks
        self.acts = acts; self.open = open
    }

    static func date(_ s: String) -> Date? { InboxDates.parse(s) }
}

/// Dates: Go writes RFC 3339, with or without fractional seconds, and a zero time where it has none.
enum InboxDates {
    static func parse(_ s: String) -> Date? {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let d = f.date(from: s) { return d }
        f.formatOptions = [.withInternetDateTime]
        return f.date(from: s)
    }
}

enum MadDate {
    /// The date at `key`, nil when it is missing, unreadable or Go's zero time.
    static func decode<K: CodingKey>(_ c: KeyedDecodingContainer<K>, _ key: K) -> Date? {
        guard let raw = (try? c.decodeIfPresent(String.self, forKey: key)) ?? nil, let d = InboxDates.parse(raw),
              d.timeIntervalSince1970 > 946_684_800 else { return nil }
        return d
    }
}
