import Foundation

/// A role of a team template (GET /api/team/templates).
public struct TeamRole: Equatable, Decodable, Identifiable, Sendable {
    public var name: String
    /// The agent it plays by default: "claude:opus@high", "codex".
    public var agent: String
    public var lead: Bool
    public var readOnly: Bool
    public var brief: String
    public var id: String { name }

    enum CodingKeys: String, CodingKey { case name, agent, lead, readOnly = "read_only", brief }
    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        agent = try c.decodeIfPresent(String.self, forKey: .agent) ?? ""
        lead = try c.decodeIfPresent(Bool.self, forKey: .lead) ?? false
        readOnly = try c.decodeIfPresent(Bool.self, forKey: .readOnly) ?? false
        brief = try c.decodeIfPresent(String.self, forKey: .brief) ?? ""
    }
    public init(name: String, agent: String = "claude", lead: Bool = false, readOnly: Bool = false, brief: String = "") {
        self.name = name; self.agent = agent; self.lead = lead; self.readOnly = readOnly; self.brief = brief
    }
}

public struct TeamTemplate: Equatable, Decodable, Identifiable, Sendable {
    public var name: String
    public var description: String
    public var roles: [TeamRole]
    public var id: String { name }

    enum CodingKeys: String, CodingKey { case name, description, roles }
    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        description = try c.decodeIfPresent(String.self, forKey: .description) ?? ""
        roles = try c.decodeIfPresent([TeamRole].self, forKey: .roles) ?? []
    }
    public init(name: String, description: String = "", roles: [TeamRole] = []) {
        self.name = name; self.description = description; self.roles = roles
    }

    /// How the template reads in a list: "name: description", or the name alone.
    public var label: String { description.isEmpty ? name : "\(name): \(description)" }
}

/// An agent a role can be played by ("claude:opus", "codex") and the efforts it takes (the default effort is not one).
public struct TeamChoice: Equatable, Decodable, Sendable {
    public var agent: String
    public var efforts: [String]

    enum CodingKeys: String, CodingKey { case agent, efforts }
    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        agent = try c.decode(String.self, forKey: .agent)
        efforts = try c.decodeIfPresent([String].self, forKey: .efforts) ?? []
    }
    public init(agent: String, efforts: [String] = []) { self.agent = agent; self.efforts = efforts }
}

/// What a new team is started from: the project's templates, the agents and efforts a role offers, the permission
/// levels and how many teams run at once.
public struct TeamTemplates: Equatable, Decodable, Sendable {
    public var templates: [TeamTemplate]
    public var choices: [TeamChoice]
    public var permissions: [String]
    public var maxTeams: Int
    /// Why some templates could not be read; the rest are usable.
    public var error: String?

    enum CodingKeys: String, CodingKey { case templates, choices, permissions, maxTeams = "max_teams", error }
    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        templates = try c.decodeIfPresent([TeamTemplate].self, forKey: .templates) ?? []
        choices = try c.decodeIfPresent([TeamChoice].self, forKey: .choices) ?? []
        permissions = try c.decodeIfPresent([String].self, forKey: .permissions) ?? []
        maxTeams = try c.decodeIfPresent(Int.self, forKey: .maxTeams) ?? 0
        error = try c.decodeIfPresent(String.self, forKey: .error).flatMap { $0.isEmpty ? nil : $0 }
    }
    public init(templates: [TeamTemplate] = [], choices: [TeamChoice] = [], permissions: [String] = [], maxTeams: Int = 0,
                error: String? = nil) {
        self.templates = templates; self.choices = choices; self.permissions = permissions; self.maxTeams = maxTeams
        self.error = error
    }

    public func choice(_ agent: String) -> TeamChoice? { choices.first { $0.agent == agent } }

    /// The agents a role can be played by: the choices, and the template's own agent when it is not one of them.
    public func agents(including agent: String) -> [String] {
        let all = choices.map(\.agent)
        return all.contains(agent) || agent.isEmpty ? all : [agent] + all
    }

    /// The efforts `agent` takes (none for an agent that is not a choice).
    public func efforts(of agent: String) -> [String] { choice(agent)?.efforts ?? [] }
}

/// A role's agent and effort, as one string ("claude:opus@high") and apart.
public enum TeamAgent {
    public static func split(_ s: String) -> (agent: String, effort: String) {
        let parts = s.split(separator: "@", maxSplits: 1, omittingEmptySubsequences: false)
        return (String(parts.first ?? ""), parts.count > 1 ? String(parts[1]) : "")
    }

    public static func join(agent: String, effort: String) -> String { effort.isEmpty ? agent : agent + "@" + effort }
}

/// What a team is started with (POST /api/team, op start).
public struct TeamStart: Equatable, Sendable {
    public var template: String
    public var project: String
    public var name: String
    public var goal: String
    public var permission: String
    /// The branch of the one worktree everyone shares; empty: each works in the project's directory.
    public var worktree: String
    /// Role → "agent@effort".
    public var agents: [String: String]

    public init(template: String, project: String, name: String = "", goal: String, permission: String, worktree: String = "",
                agents: [String: String] = [:]) {
        self.template = template; self.project = project; self.name = name; self.goal = goal
        self.permission = permission; self.worktree = worktree; self.agents = agents
    }
}

/// A team that came up: its name, its channel, and what did not go quite right while it did.
public struct StartedTeam: Equatable, Decodable, Sendable {
    public var name: String
    public var channel: String
    public var notes: [String]

    enum CodingKeys: String, CodingKey { case name, channel, notes }
    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
        channel = try c.decode(String.self, forKey: .channel)
        notes = try c.decodeIfPresent([String].self, forKey: .notes) ?? []
    }
    public init(name: String, channel: String, notes: [String] = []) { self.name = name; self.channel = channel; self.notes = notes }
}

/// Starting a team from the app (the page's 新团队).
public protocol NewTeamAPI: Sendable {
    func teamTemplates(project: String) async throws -> TeamTemplates
    /// POST /api/team: starts the team, which takes as long as its agents need to come up. A refusal throws its reason.
    func startTeam(_ start: TeamStart) async throws -> StartedTeam
}

extension MadClient: NewTeamAPI {
    public func teamTemplates(project: String) async throws -> TeamTemplates {
        let (data, response) = try await runRequest(request("/api/team/templates", query: [URLQueryItem(name: "project", value: project)]))
        try Self.check(data, response, [200])
        return try Self.decodeJSON(TeamTemplates.self, data)
    }

    public func startTeamRequest(_ s: TeamStart) -> URLRequest {
        var r = jsonRequest("/api/team", [
            "op": "start", "template": s.template, "project": s.project, "name": s.name, "goal": s.goal,
            "permission": s.permission, "worktree": s.worktree, "agents": s.agents,
        ])
        r.timeoutInterval = 300 // the agents come up one by one
        return r
    }

    public func startTeam(_ start: TeamStart) async throws -> StartedTeam {
        let (data, response) = try await runRequest(startTeamRequest(start))
        try Self.check(data, response, [200])
        return try Self.decodeJSON(StartedTeam.self, data)
    }
}
