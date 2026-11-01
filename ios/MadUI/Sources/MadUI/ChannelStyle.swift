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
    static var chatBubble: Color { dynamic(light: 0xf0f0f3, dark: 0x1f1f22) }
    /// The person's bubble, and the send button.
    static var chatBlue: Color { dynamic(light: 0x1f6bff, dark: 0x2f7bff) }
    /// The composer's field, the round buttons, the panel's lists.
    static var chatCard: Color { dynamic(light: 0xf4f4f6, dark: 0x151517) }
    /// The thin line of a folded message, and between rows.
    public static var chatLine: Color { dynamic(light: 0xdcdce2, dark: 0x2a2a2e) }
    /// The Mac's composer bar and menus: a surface over the window.
    static var chatComposer: Color { dynamic(light: 0xffffff, dark: 0x1c1c1f) }
    /// The Mac's detail pane behind the messages.
    public static var chatBackdrop: Color { dynamic(light: 0xffffff, dark: 0x121214) }
    /// The Mac's sidebar.
    static var chatSidebar: Color { dynamic(light: 0xf6f6f8, dark: 0x1a1a1d) }
    /// The 群信息 panel.
    static var chatPanel: Color { dynamic(light: 0xf7f7f9, dark: 0x0e0e10) }
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

/// A text with its @mentions in bold (and blue in an agent's bubble), its inline markdown (bold, code,
/// links) read: what a bubble shows.
func chatText(_ text: String, mine: Bool) -> AttributedString {
    var a = (try? AttributedString(markdown: text, options: .init(allowsExtendedAttributes: false,
                                                                    interpretedSyntax: .inlineOnlyPreservingWhitespace)))
        ?? AttributedString(text)
    // a link keeps its underline, and in the blue bubble is as white as the text (blue on blue is not read)
    for run in a.runs where run.link != nil {
        a[run.range].underlineStyle = .single
        if mine { a[run.range].foregroundColor = .white }
    }
    let plain = String(a.characters)
    guard let re = try? NSRegularExpression(pattern: "@[\\p{L}\\p{N}_-]+") else { return a }
    for m in re.matches(in: plain, range: NSRange(plain.startIndex..., in: plain)) {
        guard let r = Range(m.range, in: plain) else { continue }
        let from = a.characters.index(a.characters.startIndex, offsetBy: plain.distance(from: plain.startIndex, to: r.lowerBound))
        let to = a.characters.index(a.characters.startIndex, offsetBy: plain.distance(from: plain.startIndex, to: r.upperBound))
        a[from..<to].font = .body.weight(.semibold)
        if !mine { a[from..<to].foregroundColor = .chatBlue }
    }
    return a
}
