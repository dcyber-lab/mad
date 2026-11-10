import Foundation

// The home list's input: plain values, so MadUI does not depend on MadKit. The
// app's store maps the deck's snapshot onto these.

/// The whole list: which Mac, what waits on the person (pinned on top), its sections in order, and what the
/// Today page says.
public struct HomeModel: Equatable, Sendable {
    public var machine: String
    public var sections: [HomeSection]
    /// One line of usage, e.g. "Claude 5 小时 42% · 本周 71%". The sidebar no longer shows it (usage is its own
    /// screen); it is kept for a footer that wants it.
    public var usage: String?
    /// What waits on the person, oldest first: the 待处理 section on top of the list, and Today's cards.
    public var waiting: [HomeWaiting]
    /// The Today page (the Mac's first screen); nil before the deck is there.
    public var today: TodayModel?
    /// The person's full name, as the Mac says it, for the foot of the list; nil when it does not say.
    public var person: String?

    public init(machine: String, sections: [HomeSection], usage: String? = nil, waiting: [HomeWaiting] = [], today: TodayModel? = nil,
                person: String? = nil) {
        self.person = person
        self.machine = machine
        self.sections = sections
        self.usage = usage
        self.waiting = waiting
        self.today = today
    }
}

/// One thing that waits on the person: a command to allow, a question to answer. Its acts are the person's answers.
public struct HomeWaiting: Identifiable, Equatable, Sendable {
    /// The inbox item's key: what an act is sent for.
    public var id: String
    public var agentID: String
    public var agentKind: String
    public var name: String
    public var project: String
    /// "mad · claude · 想运行一条命令".
    public var whereLine: String
    /// How long it has waited: "4 分钟".
    public var age: String
    /// What the agent has cost: "$0.84"; empty when nothing.
    public var cost: String
    /// The command (with `mono`) or the question.
    public var detail: String
    public var mono: Bool
    public var acts: [AskAct]

    public init(id: String, agentID: String, agentKind: String = "", name: String, project: String = "", whereLine: String = "",
                age: String = "", cost: String = "", detail: String = "", mono: Bool = false, acts: [AskAct] = []) {
        self.id = id; self.agentID = agentID; self.agentKind = agentKind; self.name = name; self.project = project
        self.whereLine = whereLine; self.age = age; self.cost = cost; self.detail = detail; self.mono = mono; self.acts = acts
    }

    /// The act that allows, the one for the whole session, and the one that denies (without words), when it has them.
    public var allow: AskAct? { acts.first { $0.kind == .allow } }
    public var allowSession: AskAct? { acts.first { $0.kind == .allowSession } }
    public var deny: AskAct? { acts.first { $0.kind == .deny } }
    /// A question's own answers (its options), when it has some that take no words.
    public var options: [AskAct] { acts.filter { $0.kind == .other && !$0.input } }
    /// A command to allow, rather than a question.
    public var isPermission: Bool { allow != nil }
}

/// The Mac's first screen: one sentence of how things stand and what was spent, what waits (as `HomeModel.waiting`),
/// what works and what just finished.
public struct TodayModel: Equatable, Sendable {
    /// "11 月 9 日 周一".
    public var date: String
    public var waiting: Int
    public var working: Int
    public var done: Int
    /// What today's responses cost over every session on the Mac: "$6.82"; empty when nothing.
    public var spend: String
    public var workingLines: [TodayLine]
    public var doneLines: [TodayLine]

    public init(date: String, waiting: Int = 0, working: Int = 0, done: Int = 0, spend: String = "",
                workingLines: [TodayLine] = [], doneLines: [TodayLine] = []) {
        self.date = date; self.waiting = waiting; self.working = working; self.done = done; self.spend = spend
        self.workingLines = workingLines; self.doneLines = doneLines
    }
}

/// An agent on the Today page: its picture, name, what it does ("Bash · go test ./..."), how long and what it cost.
public struct TodayLine: Identifiable, Equatable, Sendable {
    public var id: String
    public var kind: String
    public var state: CharacterState
    public var name: String
    /// Before the tool: "#ios", or empty.
    public var place: String
    /// The tool ("Bash"), or empty.
    public var tool: String
    /// What it is pointed at, set in mono after the tool; or the last line when there is no tool.
    public var detail: String
    /// "6 分钟 · $2.31", "2 分钟前".
    public var number: String

    public init(id: String, kind: String = "", state: CharacterState, name: String, place: String = "", tool: String = "",
                detail: String = "", number: String = "") {
        self.id = id; self.kind = kind; self.state = state; self.name = name; self.place = place; self.tool = tool
        self.detail = detail; self.number = number
    }
}

/// What a project's header line says beside its name: `main · 3 改动 · ↑2 · $4.17`.
public struct HomeProjectMeta: Equatable, Sendable {
    public var branch: String
    public var changed: Int
    public var ahead: Int
    /// What its agents have cost, in dollars.
    public var cost: Double

    public init(branch: String = "", changed: Int = 0, ahead: Int = 0, cost: Double = 0) {
        self.branch = branch; self.changed = changed; self.ahead = ahead; self.cost = cost
    }

    /// The parts after the branch: "3 改动", "↑2", "$4.17"; a zero is left out.
    public var parts: [String] {
        var out: [String] = []
        if changed > 0 { out.append(Lf("%lld 改动", changed)) }
        if ahead > 0 { out.append("↑\(ahead)") }
        if cost > 0 { out.append(MadWords.money(cost)) }
        return out
    }
}

/// Words put together from numbers, in the language in use.
public enum MadWords {
    /// "$4.17"; "$123" from 100 up.
    public static func money(_ usd: Double) -> String {
        usd < 100 ? String(format: "$%.2f", usd) : "$\(Int(usd.rounded()))"
    }

    /// How long, short: "刚刚", "4 分钟", "3 小时", "2 天".
    public static func age(_ seconds: TimeInterval) -> String {
        let m = Int(max(0, seconds) / 60)
        if m < 1 { return L("刚刚") }
        if m < 60 { return Lf("%lld 分钟", m) }
        if m < 24 * 60 { return Lf("%lld 小时", m / 60) }
        return Lf("%lld 天", m / (24 * 60))
    }

    /// How long ago, short: "刚刚", "2 分钟前", "3 小时前", "2 天前".
    public static func ago(_ seconds: TimeInterval) -> String {
        let m = Int(max(0, seconds) / 60)
        if m < 1 { return L("刚刚") }
        if m < 60 { return Lf("%lld 分钟前", m) }
        if m < 24 * 60 { return Lf("%lld 小时前", m / 60) }
        return Lf("%lld 天前", m / (24 * 60))
    }

    /// Today's date as the Today page's head says it: "11 月 9 日 周一", "Mon, Nov 9".
    public static func today(_ date: Date, calendar: Calendar = .current) -> String {
        let f = DateFormatter()
        f.calendar = calendar
        f.timeZone = calendar.timeZone
        if madEnglish {
            f.locale = Locale(identifier: "en_US")
            f.dateFormat = "EEE, MMM d"
        } else {
            f.locale = Locale(identifier: "zh_CN")
            f.dateFormat = "M 月 d 日 EEE"
        }
        return f.string(from: date)
    }
}

/// A titled run of rows: the group chats first, then one section per project.
public struct HomeSection: Identifiable, Equatable, Sendable {
    public enum Kind: Equatable, Sendable {
        case chats
        case project(name: String)
    }

    public var id: String
    public var title: String
    public var kind: Kind
    public var rows: [HomeRow]
    /// A project's branch, changes, unpushed commits and cost, for its header line.
    public var meta: HomeProjectMeta?

    public init(id: String, title: String, kind: Kind, rows: [HomeRow], meta: HomeProjectMeta? = nil) {
        self.id = id
        self.title = title
        self.kind = kind
        self.rows = rows
        self.meta = meta
    }
}

/// One member of a group chat or direct message: its character, and what search needs to find it.
public struct HomeMember: Equatable, Sendable {
    public var id: String
    public var kind: String
    public var state: CharacterState
    public var name: String
    /// Its last line, shown as "name: preview" when a search matches its name.
    public var preview: String

    public init(id: String, kind: String = "", state: CharacterState = .idle, name: String, preview: String = "") {
        self.id = id
        self.kind = kind
        self.state = state
        self.name = name
        self.preview = preview
    }
}

/// A row: an agent, a group chat (`channel`) or a direct message (`dm`).
public struct HomeRow: Identifiable, Equatable, Sendable {
    public enum Kind: Equatable, Sendable { case agent, channel, dm }

    /// What the app opens. For an agent it is also the key of its character.
    public var id: String
    public var kind: Kind
    /// An agent's tool ("claude", "codex"): its character's key when the id is empty, and the name of an untitled agent.
    public var agentKind: String
    public var name: String
    /// The last line; one line, cut at the end by the view.
    public var preview: String
    /// An agent's state. A channel's is drawn from its members (see `displayState`); this is not read for it.
    public var state: CharacterState
    /// Something waits on the person here (the agent asks, a mention, an unread DM): a red dot on the character.
    public var needsYou: Bool
    /// Drawn at half strength: a stopped or sleeping agent.
    public var dimmed: Bool
    /// A channel's members, for its tile; a team's also feed search.
    public var members: [HomeMember]
    /// A team channel's other live members: they are not in its tile, but what they do counts (its state and dot).
    public var helpers: [HomeMember]
    /// The tool an agent is using, drawn in bold before the preview ("**Bash** go test ./..."); empty for none.
    public var tool: String
    /// The small line at the right: how long a working agent has been at it ("6 分钟"); empty for none.
    public var age: String
    /// A channel's unread messages: a count at the right.
    public var unread: Int

    public init(id: String, kind: Kind = .agent, agentKind: String = "", name: String, preview: String = "",
                state: CharacterState = .idle, needsYou: Bool = false, dimmed: Bool = false, members: [HomeMember] = [], helpers: [HomeMember] = [],
                tool: String = "", age: String = "", unread: Int = 0) {
        self.tool = tool
        self.age = age
        self.unread = unread
        self.id = id
        self.kind = kind
        self.agentKind = agentKind
        self.name = name
        self.preview = preview
        self.state = state
        self.needsYou = needsYou
        self.dimmed = dimmed
        self.members = members
        self.helpers = helpers
    }

    /// What the Mac's detail shows for no row: the Today page. The sidebar's 今日 row selects it.
    public static let todayID = "mad:today"
    /// The Usage screen, opened from the person's row at the sidebar's foot (or ⋯ → 用量).
    public static let usageID = "mad:usage"
    /// The Mac's 新对话 page in the detail pane (no row of its own).
    public static let newChatID = "mad:newchat"
    /// The Mac's 新群聊 and 新团队 pages in the detail pane (no row of their own).
    public static let newChannelID = "mad:newchannel"
    public static let newTeamID = "mad:newteam"

    /// What the row is called: its name, else "codex 32d23a28" for an untitled agent.
    public var title: String {
        if !name.isEmpty { return name }
        let kind = agentKind.isEmpty ? "agent" : agentKind
        return "\(kind) \(id.prefix(8))"
    }

    /// The state the row shows and says: an agent's own; a channel's from its members (working, else
    /// needs you, else starting, else idle), helpers included; its tile draws the team's.
    public var displayState: CharacterState {
        kind == .agent ? state : CharacterState.group((members + helpers).map(\.state))
    }

    /// What VoiceOver reads for the row, as one label: the name, the preview, the state word once (not
    /// for idle), and "needs you" only when the dot says it and the state does not already.
    public var accessibilityLabel: String {
        var parts = [title]
        if !preview.isEmpty { parts.append(preview) }
        if displayState != .idle { parts.append(displayState.word) }
        if needsYou && displayState != .needsYou { parts.append("needs you") }
        return parts.joined(separator: ", ")
    }

    var tile: [GroupTile.Member] { members.map { (id: $0.id, kind: $0.kind, state: $0.state) } }
}

/// What the + offers to start, as the page's + does.
public enum HomeNewItem: String, CaseIterable, Sendable {
    case chat, channel, team

    public var title: String {
        switch self {
        case .chat: L("新对话")
        case .channel: L("新群聊")
        case .team: L("新团队")
        }
    }

    public var symbol: String {
        switch self {
        case .chat: "square.and.pencil"
        case .channel: "bubble.left.and.bubble.right"
        case .team: "person.3"
        }
    }
}

/// What the app is asked to do. Every closure has a do-nothing default.
public struct HomeActions {
    public var open: (HomeRow.ID) -> Void
    public var switchMac: () -> Void
    /// The + menu's item was chosen.
    public var create: (HomeNewItem) -> Void
    /// The search text changed (the list filters itself; this is for an app that also searches elsewhere).
    public var search: (String) -> Void
    public var menu: (HomeMenuItem) -> Void
    /// An act on what waits (Allow, Deny, an answer): the item's id, the act's id, and the words for an input act.
    public var act: (_ itemID: String, _ actID: String, _ text: String?) -> Void
    /// Today's 打断 on a working agent.
    public var interrupt: (_ agentID: String) -> Void

    public init(open: @escaping (HomeRow.ID) -> Void = { _ in }, switchMac: @escaping () -> Void = {},
                create: @escaping (HomeNewItem) -> Void = { _ in }, search: @escaping (String) -> Void = { _ in },
                menu: @escaping (HomeMenuItem) -> Void = { _ in },
                act: @escaping (String, String, String?) -> Void = { _, _, _ in },
                interrupt: @escaping (String) -> Void = { _ in }) {
        self.act = act
        self.interrupt = interrupt
        self.open = open
        self.switchMac = switchMac
        self.create = create
        self.search = search
        self.menu = menu
    }
}

/// The ⋯ menu's items.
public enum HomeMenuItem: String, CaseIterable, Sendable {
    case usage, board, skills, devices, notifications, switchMac, addProject, theme

    public var title: String {
        switch self {
        case .usage: L("用量")
        case .board: L("看板")
        case .skills: L("技能")
        case .devices: L("手机和已配对设备")
        case .notifications: L("通知")
        case .switchMac: L("切换 Mac")
        case .addProject: L("添加项目")
        case .theme: L("主题")
        }
    }

    /// On a phone (as screen ① of the mockup), grouped by dividers.
    public static let phone: [[HomeMenuItem]] = [[.usage, .board, .skills, .devices, .notifications], [.switchMac], [.addProject, .theme]]
    /// In the Mac's sidebar (as desktop.html): switching Mac is the window's, not the menu's.
    public static let sidebar: [[HomeMenuItem]] = [[.usage, .board, .skills, .devices, .notifications], [.addProject, .theme]]
}

extension HomeModel {
    /// The model as a search for q sees it: rows whose name or preview contains q (ignoring case), and a
    /// row with members that match by a member's name, showing "builder: …" as its preview. Empty sections go.
    public func filtered(by q: String) -> HomeModel {
        let q = q.trimmingCharacters(in: .whitespaces)
        if q.isEmpty { return self }
        func has(_ s: String) -> Bool { s.range(of: q, options: [.caseInsensitive, .diacriticInsensitive]) != nil }
        var out = self
        out.sections = sections.compactMap { s in
            var s = s
            s.rows = s.rows.compactMap { r in
                if has(r.title) || has(r.preview) { return r }
                guard let m = r.members.first(where: { has($0.name) }) else { return nil }
                var r = r
                r.preview = m.preview.isEmpty ? m.name : "\(m.name): \(m.preview)"
                return r
            }
            return s.rows.isEmpty ? nil : s
        }
        return out
    }
}
