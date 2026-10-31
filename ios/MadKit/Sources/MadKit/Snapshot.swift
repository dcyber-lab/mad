import Foundation

/// What the app reads of /api/deck: the Mac's own name. The page does the rest.
/// Unknown fields are ignored, so a newer Mac does not break an older app.
public struct DeckSnapshot: Equatable, Decodable, Sendable {
    /// The Mac's friendly name; nil from a server that does not send it.
    public var machine: String?

    enum CodingKeys: String, CodingKey { case machine }

    public init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        let m = try c.decodeIfPresent(String.self, forKey: .machine)?.trimmingCharacters(in: .whitespacesAndNewlines)
        machine = (m?.isEmpty ?? true) ? nil : m
    }

    public init(machine: String? = nil) { self.machine = machine }
}
