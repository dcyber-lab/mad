import Foundation

/// A model an agent of a kind can run on (GET /api/models): what is sent back, and how to show it.
public struct ModelChoice: Equatable, Decodable, Identifiable, Sendable {
    public var value: String
    public var name: String
    public var detail: String
    public var id: String { value }

    enum CodingKeys: String, CodingKey { case value, name, description }
    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        value = try c.decode(String.self, forKey: .value)
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
        detail = try c.decodeIfPresent(String.self, forKey: .description) ?? ""
    }
    public init(value: String, name: String = "", detail: String = "") {
        self.value = value; self.name = name; self.detail = detail
    }
}

/// A past session of a project that is not open as one of its agents (GET /api/history), newest first.
public struct PastSession: Equatable, Decodable, Identifiable, Sendable {
    public var kind: String
    public var id: String
    public var title: String
    public var cwd: String
    public var updated: Date?

    enum CodingKeys: String, CodingKey { case kind, id, title, cwd, updated }
    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? ""
        id = try c.decode(String.self, forKey: .id)
        title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
        cwd = try c.decodeIfPresent(String.self, forKey: .cwd) ?? ""
        updated = (try c.decodeIfPresent(String.self, forKey: .updated)).flatMap(Self.date)
    }
    public init(kind: String, id: String, title: String, cwd: String = "", updated: Date? = nil) {
        self.kind = kind; self.id = id; self.title = title; self.cwd = cwd; self.updated = updated
    }

    /// Go writes times as RFC 3339, with or without fractions of a second.
    static func date(_ s: String) -> Date? {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let d = f.date(from: s) { return d }
        f.formatOptions = [.withInternetDateTime]
        return f.date(from: s)
    }
}

/// What starting an agent answered: its id, and why its first message did not go when it did not.
public struct StartedAgent: Equatable, Decodable, Sendable {
    public var id: String
    public var error: String?
}

/// Starting a chat from the app: the models, the past sessions, and a new agent (the page's new chat).
public protocol NewChatAPI: Sendable {
    func models(kind: String, project: String) async throws -> [ModelChoice]
    func history(project: String) async throws -> [PastSession]
    /// POST /api/agents: an agent of `kind` in `project`, its first message `text`; `session` and `cwd` resume a past
    /// session instead.
    func startAgent(project: String, kind: String, model: String, text: String, session: String, cwd: String) async throws -> StartedAgent
}

extension MadClient: NewChatAPI {
    public func models(kind: String, project: String) async throws -> [ModelChoice] {
        let (data, response) = try await runRequest(request("/api/models", query: [
            URLQueryItem(name: "kind", value: kind), URLQueryItem(name: "project", value: project)]))
        try Self.check(data, response, [200])
        return try JSONDecoder().decode([ModelChoice].self, from: data)
    }

    public func history(project: String) async throws -> [PastSession] {
        let (data, response) = try await runRequest(request("/api/history", query: [URLQueryItem(name: "project", value: project)]))
        try Self.check(data, response, [200])
        return try JSONDecoder().decode([PastSession].self, from: data)
    }

    public func startRequest(project: String, kind: String, model: String, text: String, session: String, cwd: String) -> URLRequest {
        var r = request("/api/agents")
        r.httpMethod = "POST"
        r.setValue("application/json", forHTTPHeaderField: "Content-Type")
        var body = ["project": project, "kind": kind]
        if !model.isEmpty { body["model"] = model }
        if !text.isEmpty { body["text"] = text }
        if !session.isEmpty { body["session"] = session }
        if !cwd.isEmpty { body["cwd"] = cwd }
        r.httpBody = try? JSONSerialization.data(withJSONObject: body, options: [.sortedKeys])
        return r
    }

    public func startAgent(project: String, kind: String, model: String, text: String, session: String, cwd: String) async throws -> StartedAgent {
        let (data, response) = try await runRequest(startRequest(project: project, kind: kind, model: model, text: text, session: session, cwd: cwd))
        try Self.check(data, response, [200])
        return try JSONDecoder().decode(StartedAgent.self, from: data)
    }
}
