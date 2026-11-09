import SwiftUI
#if canImport(UIKit)
import UIKit
#elseif canImport(AppKit)
import AppKit
#endif

/// The channel view's colors, from the approved mockup (v4): dark and light.
extension Color {
    static func dynamic(light: UInt32, dark: UInt32) -> Color {
        #if canImport(UIKit)
        Color(uiColor: UIColor { $0.userInterfaceStyle == .dark ? UIColor(hex: dark) : UIColor(hex: light) })
        #else
        Color(nsColor: NSColor(name: nil) { $0.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua ? NSColor(hex: dark) : NSColor(hex: light) })
        #endif
    }

    /// An agent's bubble.
    static var chatBubble: Color { dynamic(light: 0xf0f0f3, dark: 0x1c1c1f) }
    /// The person's bubble, and the send button.
    static var chatBlue: Color { dynamic(light: 0x1f6bff, dark: 0x2f7bff) }
    /// The composer's field, the round buttons, the panel's lists.
    static var chatCard: Color { dynamic(light: 0xf4f4f6, dark: 0x151517) }
    /// The thin line of a folded message, and between rows.
    public static var chatLine: Color { dynamic(light: 0xdcdce2, dark: 0x2a2a2e) }
    /// The Mac's composer bar and menus: a surface over the window.
    public static var chatComposer: Color { dynamic(light: 0xffffff, dark: 0x1c1c1f) }
    /// The Mac's detail pane behind the messages.
    public static var chatBackdrop: Color { dynamic(light: 0xffffff, dark: 0x0e0e10) }
    /// The Mac's sidebar.
    static var chatSidebar: Color { dynamic(light: 0xf6f6f8, dark: 0x131315) }
    /// The 群信息 panel.
    static var chatPanel: Color { dynamic(light: 0xf7f7f9, dark: 0x0e0e10) }
}

/// The tokens of the approved design (v5): one set of status colors, and the greys and fills around them. Red is for
/// errors only; a stopped agent is faded, never red.
extension Color {
    /// Waiting on the person: the amber ring, the Waiting section, its counts.
    public static var madWait: Color { dynamic(light: 0xc26a04, dark: 0xfbbf24) }
    public static var madWaitBg: Color { dynamic(light: 0xfff4e5, dark: 0x2e2410) }
    /// Working: the green arc.
    public static var madWork: Color { dynamic(light: 0x15803d, dark: 0x4ade80) }
    public static var madWorkBg: Color { dynamic(light: 0xe9f7ee, dark: 0x10291a) }
    /// Just finished: the blue dot.
    public static var madDone: Color { dynamic(light: 0x2563eb, dark: 0x60a5fa) }
    public static var madDoneBg: Color { dynamic(light: 0xebf1fe, dark: 0x14223a) }
    /// Errors: a failed tool call, a refused act, Stop.
    public static var madDanger: Color { dynamic(light: 0xc62828, dark: 0xf87171) }
    /// Secondary text (about 5:1 on the sidebar).
    public static var madFg3: Color { dynamic(light: 0x6b6b76, dark: 0x8a8a95) }
    /// Text between the main and the secondary: a sub line's bold tool name.
    public static var madFg2: Color { dynamic(light: 0x50505a, dark: 0xb0b0ba) }
    public static var madFg: Color { dynamic(light: 0x111114, dark: 0xececf1) }
    /// The window's paper and the raised fills on it.
    public static var madPaper: Color { dynamic(light: 0xffffff, dark: 0x151517) }
    public static var madSide: Color { dynamic(light: 0xf7f7f8, dark: 0x101012) }
    public static var madRaise: Color { dynamic(light: 0xefeff2, dark: 0x1f1f23) }
    public static var madRaise2: Color { dynamic(light: 0xe6e6ea, dark: 0x2a2a2f) }
    /// Hairlines: the faint one between rows, the stronger one around a button.
    public static var madLine: Color { dynamic(light: 0x111116, dark: 0xffffff).opacity(0.08) }
    public static var madLine2: Color { dynamic(light: 0x0f0f16, dark: 0xffffff).opacity(0.14) }
    /// Code: a command in a card.
    public static var madCode: Color { dynamic(light: 0xf6f6f8, dark: 0x0f0f11) }
    /// The person's own message: a light blue fill with dark text.
    public static var madMine: Color { dynamic(light: 0xe9f0fe, dark: 0x1c2a44) }
    public static var madMineFg: Color { dynamic(light: 0x0f2a66, dark: 0xdbe6ff) }
    public static var madAccent: Color { dynamic(light: 0x2563eb, dark: 0x7cacf8) }
    /// Lines added and taken out.
    public static var madAddFg: Color { dynamic(light: 0x166534, dark: 0x86efac) }
    public static var madDelFg: Color { dynamic(light: 0x9f1239, dark: 0xfda4af) }
    /// An agent's identity color (its avatar's), for its @name.
    public static func madIdentity(id: String, kind: String = "") -> Color {
        let look = CharacterLook(id: id, kind: kind)
        return Color(hex: GrokAvatar.colors[look.color % GrokAvatar.colors.count])
    }
}

#if canImport(UIKit)
extension UIColor {
    convenience init(hex: UInt32) {
        self.init(red: CGFloat((hex >> 16) & 0xff) / 255, green: CGFloat((hex >> 8) & 0xff) / 255, blue: CGFloat(hex & 0xff) / 255, alpha: 1)
    }
}
#else
extension NSColor {
    convenience init(hex: UInt32) {
        self.init(srgbRed: CGFloat((hex >> 16) & 0xff) / 255, green: CGFloat((hex >> 8) & 0xff) / 255, blue: CGFloat(hex & 0xff) / 255, alpha: 1)
    }
}
#endif

/// Compiled once: a bubble is drawn many times.
private let mentionPattern = try! NSRegularExpression(pattern: "@[\\p{L}\\p{N}_-]+")

/// A text with its @mentions in bold, each in its member's identity color on a light tint of it (the accent for a name
/// that is no member's), its inline markdown (bold, code, links) read: what a bubble shows. `colors` is a member's
/// name to its color.
func chatText(_ text: String, mine: Bool, colors: [String: Color] = [:]) -> AttributedString {
    var a = (try? AttributedString(markdown: text, options: .init(allowsExtendedAttributes: false,
                                                                    interpretedSyntax: .inlineOnlyPreservingWhitespace)))
        ?? AttributedString(text)
    // a link keeps its underline
    for run in a.runs where run.link != nil {
        a[run.range].underlineStyle = .single
    }
    let plain = String(a.characters)
    for m in mentionPattern.matches(in: plain, range: NSRange(plain.startIndex..., in: plain)) {
        guard let r = Range(m.range, in: plain) else { continue }
        let from = a.characters.index(a.characters.startIndex, offsetBy: plain.distance(from: plain.startIndex, to: r.lowerBound))
        let to = a.characters.index(a.characters.startIndex, offsetBy: plain.distance(from: plain.startIndex, to: r.upperBound))
        a[from..<to].font = .body.weight(.semibold)
        let name = String(plain[r].dropFirst())
        if let c = colors[name] {
            a[from..<to].foregroundColor = c
            a[from..<to].backgroundColor = c.opacity(mine ? 0.16 : 0.12)
        } else {
            a[from..<to].foregroundColor = Color.madAccent
        }
    }
    return a
}
