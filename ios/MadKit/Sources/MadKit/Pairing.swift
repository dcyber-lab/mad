import Foundation

/// A Mac running mad, as paired: where to reach it and the token that opens it.
public struct Mac: Hashable, Codable, Identifiable, Sendable {
    public var id: UUID
    public var name: String
    public var baseURL: URL
    public var token: String

    public init(id: UUID = UUID(), name: String, baseURL: URL, token: String) {
        self.id = id
        self.name = name
        self.baseURL = baseURL
        self.token = token
    }

    /// The full mad page, which stores the token itself.
    public var deckURL: URL {
        URL(string: baseURL.absoluteString + "/#token=" + token) ?? baseURL
    }
}

public enum PairingError: Error, Equatable, LocalizedError {
    case empty
    case notURL
    case notHTTPS
    case noHost
    case noToken
    case badToken

    public var errorDescription: String? {
        switch self {
        case .empty: return "Nothing to pair with: the code is empty."
        case .notURL: return "This is not a link."
        case .notHTTPS: return "The link must start with https://."
        case .noHost: return "The link has no host name."
        case .noToken: return "The link has no #token=… part. Show the code again with `mad phone`."
        case .badToken: return "The token must be 32 hex characters."
        }
    }
}

public enum Pairing {
    /// Parses what `mad phone` shows: https://<host>[:port]/#token=<32 hex>.
    /// Surrounding whitespace is ignored; anything else is rejected.
    public static func parse(_ text: String) throws -> Mac {
        let s = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if s.isEmpty { throw PairingError.empty }
        guard let c = URLComponents(string: s), c.scheme != nil, !s.contains(" ") else {
            throw PairingError.notURL
        }
        guard c.scheme?.lowercased() == "https" else { throw PairingError.notHTTPS }
        guard let host = c.host, !host.isEmpty else { throw PairingError.noHost }
        guard let frag = c.fragment, frag.hasPrefix("token=") else { throw PairingError.noToken }
        let token = String(frag.dropFirst("token=".count)).lowercased()
        guard token.count == 32, token.allSatisfy({ $0.isASCII && $0.isHexDigit }) else {
            throw PairingError.badToken
        }
        var base = URLComponents()
        base.scheme = "https"
        base.host = host
        base.port = c.port
        guard let url = base.url else { throw PairingError.notURL }
        let label = host.split(separator: ".").first.map(String.init) ?? host
        return Mac(name: label, baseURL: url, token: token)
    }
}
