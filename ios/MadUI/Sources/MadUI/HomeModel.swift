import Foundation

// The home list's input: plain values, so MadUI does not depend on MadKit. The
// app's store maps the deck's snapshot onto these.

/// The whole list: which Mac, its sections in order, and an optional usage line for a footer.
public struct HomeModel: Equatable, Sendable {
    public var machine: String
    public var sections: [HomeSection]
    /// One line for the sidebar's footer, e.g. "Claude 5 小时 42% · 本周 71%".
    public var usage: String?

    public init(machine: String, sections: [HomeSection], usage: String? = nil) {
        self.machine = machine
        self.sections = sections
        self.usage = usage
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

    public init(id: String, title: String, kind: Kind, rows: [HomeRow]) {
        self.id = id
        self.title = title
        self.kind = kind
        self.rows = rows
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
    /// An agent's state; a channel's is drawn from its members.
    public var state: CharacterState
    /// Something waits on the person here (the agent asks, a mention, an unread DM): a red dot on the character.
    public var needsYou: Bool
    /// Drawn at half strength: a stopped or sleeping agent.
    public var dimmed: Bool
    /// A channel's members, for its tile; a team's also feed search.
    public var members: [HomeMember]

    public init(id: String, kind: Kind = .agent, agentKind: String = "", name: String, preview: String = "",
                state: CharacterState = .idle, needsYou: Bool = false, dimmed: Bool = false, members: [HomeMember] = []) {
        self.id = id
        self.kind = kind
        self.agentKind = agentKind
        self.name = name
        self.preview = preview
        self.state = state
        self.needsYou = needsYou
        self.dimmed = dimmed
        self.members = members
    }

    /// What the row is called: its name, else "codex 32d23a28" for an untitled agent.
    public var title: String {
        if !name.isEmpty { return name }
        let kind = agentKind.isEmpty ? "agent" : agentKind
        return "\(kind) \(id.prefix(8))"
    }

    var tile: [GroupTile.Member] { members.map { (id: $0.id, kind: $0.kind, state: $0.state) } }
}

/// What the app is asked to do. Every closure has a do-nothing default.
public struct HomeActions {
    public var open: (HomeRow.ID) -> Void
    public var switchMac: () -> Void
    public var newChat: () -> Void
    /// The search text changed (the list filters itself; this is for an app that also searches elsewhere).
    public var search: (String) -> Void
    public var menu: (HomeMenuItem) -> Void

    public init(open: @escaping (HomeRow.ID) -> Void = { _ in }, switchMac: @escaping () -> Void = {},
                newChat: @escaping () -> Void = {}, search: @escaping (String) -> Void = { _ in },
                menu: @escaping (HomeMenuItem) -> Void = { _ in }) {
        self.open = open
        self.switchMac = switchMac
        self.newChat = newChat
        self.search = search
        self.menu = menu
    }
}

/// The ⋯ menu's items.
public enum HomeMenuItem: String, CaseIterable, Sendable {
    case usage, board, skills, devices, notifications, switchMac, addProject, theme

    public var title: String {
        switch self {
        case .usage: "用量"
        case .board: "看板"
        case .skills: "技能"
        case .devices: "手机和已配对设备"
        case .notifications: "通知"
        case .switchMac: "切换 Mac"
        case .addProject: "添加项目"
        case .theme: "主题"
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
