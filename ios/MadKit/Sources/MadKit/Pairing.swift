import Foundation

/// A Mac running mad, as paired: where to reach it and the token that opens it.
public struct Mac: Hashable, Codable, Identifiable, Sendable {
    public var id: UUID
    public var name: String
    public var baseURL: URL
    public var token: String
    /// The Mac's id for this device, from pairing (nil for a pasted token).
    public var deviceID: String?

    public init(id: UUID = UUID(), name: String, baseURL: URL, token: String, deviceID: String? = nil) {
        self.id = id
        self.name = name
        self.baseURL = baseURL
        self.token = token
        self.deviceID = deviceID
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
    case badCode

    public var errorDescription: String? {
        switch self {
        case .empty: return "Nothing to pair with: the code is empty."
        case .notURL: return "This is not a link."
        case .notHTTPS: return "The link must start with https://."
        case .noHost: return "The link has no host name."
        case .noToken: return "The link has no #pair=… or #token=… part. Show the code again with mad phone."
        case .badToken: return "The token must be 32 hex characters."
        case .badCode: return "The pairing code must be 32 hex characters."
        }
    }
}

/// What a pairing link holds: a one-time code to exchange for a token of
/// the device's own (what `mad phone` shows), or a token already made.
public enum PairingLink: Equatable, Sendable {
    case pair(base: URL, code: String)
    case token(Mac)
}

public enum Pairing {
    /// Parses what `mad phone` shows: https://<host>[:port]/#pair=<32 hex>,
    /// or an already-made token, https://<host>[:port]/#token=<32 hex>.
    /// Surrounding whitespace is ignored; anything else is rejected.
    ///
    /// allowLocalHTTP also accepts http://127.0.0.1[:port] and
    /// http://localhost[:port], for running against a local `mad desktop
    /// -serve`. The caller decides (debug builds only); Release passes false.
    public static func parse(_ text: String, allowLocalHTTP: Bool = false) throws -> PairingLink {
        let s = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if s.isEmpty { throw PairingError.empty }
        guard let c = URLComponents(string: s), c.scheme != nil, !s.contains(" ") else {
            throw PairingError.notURL
        }
        let scheme = c.scheme?.lowercased() ?? ""
        guard let host = c.host, !host.isEmpty else {
            throw scheme == "https" || (allowLocalHTTP && scheme == "http") ? PairingError.noHost : PairingError.notHTTPS
        }
        let local = ["127.0.0.1", "localhost"].contains(host.lowercased())
        guard scheme == "https" || (allowLocalHTTP && scheme == "http" && local) else {
            throw PairingError.notHTTPS
        }
        guard let frag = c.fragment else { throw PairingError.noToken }
        var base = URLComponents()
        base.scheme = scheme
        base.host = host
        base.port = c.port
        guard let url = base.url else { throw PairingError.notURL }
        if frag.hasPrefix("pair=") {
            guard let code = hex32(String(frag.dropFirst("pair=".count))) else { throw PairingError.badCode }
            return .pair(base: url, code: code)
        }
        if frag.hasPrefix("token=") {
            guard let token = hex32(String(frag.dropFirst("token=".count))) else { throw PairingError.badToken }
            return .token(Mac(name: defaultName(for: url), baseURL: url, token: token))
        }
        throw PairingError.noToken
    }

    /// The host's first label ("box" for box.tail1.ts.net); an IP stays whole.
    public static func defaultName(for base: URL) -> String {
        let host = base.host ?? ""
        let isIP = host.allSatisfy { $0.isNumber || $0 == "." }
        return isIP ? host : (host.split(separator: ".").first.map(String.init) ?? host)
    }

    /// 32 hex digits, ASCII only (full-width digits are not), lowercased.
    private static func hex32(_ s: String) -> String? {
        let t = s.lowercased()
        return t.count == 32 && t.allSatisfy({ $0.isASCII && $0.isHexDigit }) ? t : nil
    }
}
