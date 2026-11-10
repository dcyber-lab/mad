import Foundation

/// A channel just made (POST /api/bus/channel): its id is what the window opens.
public struct CreatedChannel: Equatable, Decodable, Sendable {
    public var id: String
}

/// Making a group chat from the app (the page's 新群聊).
public protocol NewChannelAPI: Sendable {
    /// POST /api/bus/channel: a channel called `name` about `topic` (may be empty), with the agents `members` (by id).
    func createChannel(name: String, topic: String, members: [String]) async throws -> CreatedChannel
}

extension MadClient: NewChannelAPI {
    public func createChannelRequest(name: String, topic: String, members: [String]) -> URLRequest {
        jsonRequest("/api/bus/channel", ["name": name, "topic": topic, "members": members])
    }

    public func createChannel(name: String, topic: String, members: [String]) async throws -> CreatedChannel {
        let (data, response) = try await runRequest(createChannelRequest(name: name, topic: topic, members: members))
        try Self.checkBus(data, response, [200])
        return try Self.decodeJSON(CreatedChannel.self, data)
    }
}

extension Deck {
    /// The agents a new group can take in, as the page lists them: a conversation, and not a team's role unless it
    /// is on a team's roster.
    public var groupCandidates: [(agent: Agent, project: String)] {
        projects.flatMap { p in
            p.agents.filter { $0.chat && (($0.role ?? "").isEmpty || !($0.team ?? "").isEmpty) }.map { (agent: $0, project: p.name) }
        }
    }
}
