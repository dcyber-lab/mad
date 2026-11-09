import Foundation

/// What the agent's list draws: an entry as it is, or a run of consecutive tool calls as one card.
enum ConvRow: Identifiable {
    case entry(ConvEntry)
    case tools(id: String, [ConvTool])

    var id: String {
        switch self {
        case let .entry(e): e.id
        case let .tools(id, _): id
        }
    }

    static func rows(_ entries: [ConvEntry]) -> [ConvRow] {
        var out: [ConvRow] = []
        for e in entries {
            if case let .tool(t) = e.content {
                if case let .tools(id, tools)? = out.last {
                    out[out.count - 1] = .tools(id: id, tools + [t])
                } else {
                    out.append(.tools(id: e.id, [t]))
                }
            } else {
                out.append(.entry(e))
            }
        }
        return out
    }
}
