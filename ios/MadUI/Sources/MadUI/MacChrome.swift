import SwiftUI

#if os(macOS)
/// The Mac's look (design v6, docs/design/mac-v6.html): Grok bot's desktop layout, a step larger. No title bar over a
/// chat: a capsule floats in the middle of the top, round floating buttons in the corners, the messages in one
/// centred column, the composer a floating capsule under it. Every Mac screen takes its numbers and pieces from here.
public enum Mac {
    // MARK: sidebar
    public static let sidebarWidth: CGFloat = 310
    public static let rowAvatar: CGFloat = 34
    /// A row's height (58 with the gap): two lines beside a 34pt picture.
    public static let rowHeight: CGFloat = 54
    public static let rowRadius: CGFloat = 12
    public static let sectionHead = Font.system(size: 13.5)
    public static let sectionMeta = Font.system(size: 13, design: .monospaced)

    // MARK: chat
    /// The column the messages, the approval card and the composer share.
    public static let column: CGFloat = 820
    /// The widest a bubble gets; an agent's long answer may take `wideBubble`.
    public static let bubble: CGFloat = 600
    public static let wideBubble: CGFloat = 700
    public static let bubbleRadius: CGFloat = 20
    public static let bubblePadding = EdgeInsets(top: 13, leading: 18, bottom: 13, trailing: 18)
    public static let chatAvatar: CGFloat = 28
    /// Between one message and the next.
    public static let messageGap: CGFloat = 22
    public static let sender = Font.system(size: 13.5)
    public static let date = Font.system(size: 13.5)

    // MARK: chrome
    public static let button: CGFloat = 32
    public static let titleHeight: CGFloat = 38
    public static let title = Font.system(size: 16, weight: .semibold)
    public static let titleSub = Font.system(size: 13.5)
    public static let composerHeight: CGFloat = 54
    public static let composerText = Font.system(size: 16.5)
    public static let cardRadius: CGFloat = 18
}

extension Color {
    /// What floats over the content is filled with: white in light mode; in dark mode a step above the paper, as
    /// the paper would hide it.
    public static var macFloatFill: Color {
        Color(NSColor(name: nil) { $0.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua ? NSColor(Color.madRaise) : NSColor(Color.madPaper) })
    }
}

extension View {
    /// The soft shadow under what floats (round buttons, the title capsule, the composer, cards over the chat).
    public func macFloat() -> some View {
        shadow(color: Color.black.opacity(0.08), radius: 8, x: 0, y: 4)
            .shadow(color: Color.black.opacity(0.06), radius: 1, x: 0, y: 1)
    }
}

/// A round floating button: 32pt, the float fill, a soft shadow (Grok's search, +, the corner buttons).
public struct MacRoundButton<Label: View>: View {
    let action: () -> Void
    let label: Label
    public init(action: @escaping () -> Void, @ViewBuilder label: () -> Label) {
        self.action = action
        self.label = label()
    }
    public var body: some View {
        Button(action: action) {
            label
                .font(.system(size: 14, weight: .medium))
                .foregroundStyle(Color.madFg)
                .frame(width: Mac.button, height: Mac.button)
                .background(Color.macFloatFill, in: Circle())
                .contentShape(Circle())
        }
        .buttonStyle(.plain)
        .macFloat()
    }
}

/// The capsule floating in the middle of a chat's top: a picture, the title, a grey line beside it.
public struct MacTitlePill<Picture: View>: View {
    let title: String
    let sub: String
    let picture: Picture
    public init(title: String, sub: String = "", @ViewBuilder picture: () -> Picture) {
        self.title = title
        self.sub = sub
        self.picture = picture()
    }
    public var body: some View {
        HStack(spacing: 8) {
            picture
            Text(title).font(Mac.title).foregroundStyle(Color.madFg).lineLimit(1)
            if !sub.isEmpty { Text(sub).font(Mac.titleSub).foregroundStyle(Color.madFg3).lineLimit(1) }
        }
        .padding(.leading, 9)
        .padding(.trailing, 18)
        .frame(height: Mac.titleHeight)
        .background(Color.macFloatFill, in: Capsule())
        .macFloat()
    }
}

/// The fade at the top of a chat that the content scrolls under, behind the floating capsule.
public struct MacTopFade: View {
    public init() {}
    public var body: some View {
        LinearGradient(stops: [.init(color: Color.madPaper, location: 0.55), .init(color: Color.madPaper.opacity(0), location: 1)],
                       startPoint: .top, endPoint: .bottom)
            .frame(height: 64)
            .allowsHitTesting(false)
    }
}
#endif
