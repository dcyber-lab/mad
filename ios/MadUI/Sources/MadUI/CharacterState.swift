import Foundation

/// What a character says about its agent, as the page's `stateWord`.
public enum CharacterState: String, CaseIterable, Sendable {
    case idle, working, needsYou, starting, stopped

    /// The word a screen reader says: "working", "needs you", "starting", "stopped" or "idle".
    public var word: String {
        switch self {
        case .idle: "idle"
        case .working: "working"
        case .needsYou: "needs you"
        case .starting: "starting"
        case .stopped: "stopped"
        }
    }

    /// From the page's shown status: running, waiting, starting, stopped, exited, else idle.
    public init(status: String) {
        switch status {
        case "running": self = .working
        case "waiting": self = .needsYou
        case "starting": self = .starting
        case "stopped", "exited": self = .stopped
        default: self = .idle
        }
    }

    /// What a group of characters says: working, else needs you, else starting, else idle (a stopped member is idle).
    public static func group(_ states: [CharacterState]) -> CharacterState {
        if states.contains(.working) { return .working }
        if states.contains(.needsYou) { return .needsYou }
        if states.contains(.starting) { return .starting }
        return .idle
    }
}
