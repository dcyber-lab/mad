import Foundation

/// An origin: scheme, host and port. The app lets only the paired Mac's own
/// origin load in the deck page and speak to the app.
public struct WebOrigin: Equatable, Sendable {
    public let scheme: String
    public let host: String
    public let port: Int

    /// Port 0, or none, is the scheme's own (WebKit says 0 for it).
    public init(scheme: String, host: String, port: Int?) {
        let s = scheme.lowercased()
        self.scheme = s
        self.host = host.lowercased()
        self.port = (port ?? 0) == 0 ? (s == "http" ? 80 : 443) : port!
    }

    public init(_ url: URL) {
        self.init(scheme: url.scheme ?? "", host: url.host ?? "", port: url.port)
    }
}

/// What the deck page can ask of the app, through
/// `window.webkit.messageHandlers.mad.postMessage({type: ...})`.
public enum DeckMessage: Equatable, Sendable {
    /// Back to the Macs list (the page's title and its menu's "switch Mac").
    case switchMac

    /// Reads a message body; nil for anything but a known type.
    public static func parse(_ body: Any) -> DeckMessage? {
        guard let dict = body as? [String: Any], let type = dict["type"] as? String else { return nil }
        switch type {
        case "switchMac": return .switchMac
        default: return nil
        }
    }
}
