import Foundation

/// Removing an agent from the app (the page's x kill): the session ends and the deck forgets it.
public protocol AgentRemoveAPI: Sendable {
    /// DELETE /api/agents?id=<id>: ends the agent's process and removes it from its project.
    func removeAgent(id: String) async throws
}

extension MadClient: AgentRemoveAPI {
    public func removeAgentRequest(id: String) -> URLRequest {
        var r = request("/api/agents", query: [URLQueryItem(name: "id", value: id)])
        r.httpMethod = "DELETE"
        return r
    }

    public func removeAgent(id: String) async throws {
        let (data, response) = try await runRequest(removeAgentRequest(id: id))
        try Self.check(data, response, [204, 200])
    }
}
