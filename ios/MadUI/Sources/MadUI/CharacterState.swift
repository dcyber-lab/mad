import Foundation

/// What a character says about its agent, as the page's `stateWord`.
public enum CharacterState: String, CaseIterable, Sendable {
    /// `done`: a run finished that the person has not looked at yet (the page's "finished"): a blue dot.
    case idle, working, needsYou, starting, stopped, done

    /// The word a screen reader says: "working", "needs you", "starting", "stopped", "done" or "idle".
    public var word: String {
        switch self {
        case .idle: "idle"
        case .working: "working"
        case .needsYou: "needs you"
        case .starting: "starting"
        case .stopped: "stopped"
        case .done: "done"
        }
    }

    /// From the page's shown status: running, waiting, starting, stopped, exited, finished, else idle.
    public init(status: String) {
        switch status {
        case "running": self = .working
        case "waiting": self = .needsYou
        case "starting": self = .starting
        case "stopped", "exited": self = .stopped
        case "finished": self = .done
        default: self = .idle
        }
    }

    /// What a group of characters says: working, else needs you, else starting, else idle (a stopped or done member is idle).
    public static func group(_ states: [CharacterState]) -> CharacterState {
        if states.contains(.working) { return .working }
        if states.contains(.needsYou) { return .needsYou }
        if states.contains(.starting) { return .starting }
        return .idle
    }
}

extension CharacterState {
    /// What the person reads for the state: 在等你, 在工作, 刚完成, 空闲, 已停止, 启动中.
    public var title: String {
        switch self {
        case .idle: L("空闲")
        case .working: L("在工作")
        case .needsYou: L("在等你")
        case .starting: L("启动中")
        case .stopped: L("已停止")
        case .done: L("刚完成")
        }
    }
}
