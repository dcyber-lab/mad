import Foundation
import MadKit
import MadUI

/// A word of MadShell's (or of the apps', which keep theirs here), in the language in use.
func L(_ key: String) -> String { MadLocale.string(key, bundle: .module) }

/// A format of MadShell's, filled in.
func Lf(_ key: String, _ args: CVarArg...) -> String { MadLocale.format(key, bundle: .module, args) }

/// The apps' own words (the iPhone's Macs list, the Mac app's settings) are in MadShell's catalog: an app target has
/// none of its own. `madLocalized("关闭")` is the word in the language in use.
public func madLocalized(_ key: String) -> String { L(key) }

/// The same, filled in: `madLocalized("连不上 %@", name)`.
public func madLocalized(_ key: String, _ args: CVarArg...) -> String { MadLocale.format(key, bundle: .module, args) }

/// How an inbox act reads and what it does, for the native screens. The server's label stays English (the TUI draws
/// it), so this goes by the act's verb, kind and flags, never by its text; an act with no word here (an option's own
/// label, "Open #chan") keeps the server's.
enum ActWords {
    /// What the act does, for the screens that draw some acts apart (Allow and Deny on a compact card).
    static func kind(_ act: InboxAct) -> AskAct.Kind {
        switch act.verb {
        case "allow": return act.option == nil ? .allow : .other
        case "always": return .allowSession
        case "deny": return act.input ? (act.row ? .other : .denyWhy) : (act.row ? .deny : .other)
        default: return .other
        }
    }

    /// The button's words. `command` is what a permission would run: "本次会话都允许 python3" names its program.
    static func label(_ act: InboxAct, command: String? = nil) -> String {
        switch act.verb {
        case "allow": return act.option != nil ? act.label : act.kind == "plan" ? L("批准计划") : L("允许")
        case "always":
            if let program = command.flatMap(program(of:)) { return Lf("本次会话都允许 %@", program) }
            return L("本次会话都允许")
        case "deny": return act.input ? (act.row ? L("继续规划") : L("拒绝并说明…")) : act.row ? L("拒绝") : L("取消")
        case "answer": return act.input ? L("回复") : act.label
        case "reply": return L("回复")
        case "approve": return L("批准")
        case "discard": return L("丢弃")
        case "seen": return L("标为已读")
        case "send": return L("立即发送")
        case "drop": return L("放弃")
        case "resume": return L("继续")
        case "raise": return act.option.map { Lf("加 $%@", $0) } ?? act.label
        case "lift": return L("今天不限")
        case "open":
            switch act.kind {
            case "dm": return L("打开私信")
            case "board": return L("打开看板")
            case "read": return L("去看看")
            default: return act.target.map { Lf("打开 %@", $0) } ?? act.label
            }
        default: return act.label
        }
    }

    /// The program a command runs: its first word, past `$`, `sudo`, `env` and assignments ("python3" of
    /// "$ python3 make_table.py"); nil for an empty one.
    static func program(of command: String) -> String? {
        let words = command.split(whereSeparator: { $0 == " " || $0 == "\n" || $0 == "\t" }).map(String.init)
        for w in words where w != "$" && w != "sudo" && w != "env" && !w.contains("=") {
            return (w as NSString).lastPathComponent
        }
        return nil
    }

    /// The acts of an item as the native screens draw them: the open acts left out (they send nothing).
    static func acts(of item: InboxItem) -> [AskAct] {
        let command = item.mono ? item.text : nil
        return item.acts.filter { !$0.isOpen }.map {
            AskAct(id: $0.id, label: label($0, command: command), primary: $0.primary, input: $0.input, kind: kind($0))
        }
    }
}
