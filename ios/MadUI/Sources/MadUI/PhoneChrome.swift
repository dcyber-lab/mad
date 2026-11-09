import SwiftUI

#if os(iOS)
/// The iPhone's look (design v6, docs/design/iphone-v6.html), measured from Grok bot's iPhone app: round floating
/// buttons and a title capsule over the content, big text, generous rows. Every iPhone screen takes its numbers and
/// pieces from here, so they stay one look.
public enum Phone {
    // MARK: list rows (Grok: 46pt avatar, 19pt name, 16pt preview, an 80pt row)
    public static let rowAvatar: CGFloat = 50
    public static let rowHeight: CGFloat = 80
    public static let rowSide: CGFloat = 22
    public static let rowGap: CGFloat = 14
    public static let rowName = Font.system(size: 19, weight: .medium)
    public static let rowPreview = Font.system(size: 16)
    public static let rowTime = Font.system(size: 14)
    /// A list's small grey head (群聊, a project's name).
    public static let sectionHead = Font.system(size: 14)

    // MARK: chat (Grok: 19pt text on a 27pt pitch, the sender in 14pt grey over the bubble)
    public static let message = Font.system(size: 19)
    public static let messageLineSpacing: CGFloat = 6
    public static let bubbleRadius: CGFloat = 24
    public static let bubblePadding = EdgeInsets(top: 13, leading: 17, bottom: 13, trailing: 17)
    public static let sender = Font.system(size: 14)
    public static let chatAvatar: CGFloat = 26
    public static let chatSide: CGFloat = 16
    /// The most of the column a bubble takes.
    public static let bubbleShare: CGFloat = 0.86

    // MARK: chrome
    public static let button: CGFloat = 44
    public static let title = Font.system(size: 17, weight: .semibold)
    public static let titleSub = Font.system(size: 12)
    public static let composerHeight: CGFloat = 58
    /// What sits at the foot of a screen keeps this far from the bottom edge (over the home indicator's inset).
    public static let footGap: CGFloat = 8
    /// A floating card (the approval, a sheet's tiles).
    public static let cardRadius: CGFloat = 28
    /// Body text of cards and settings rows.
    public static let body = Font.system(size: 17)
    public static let bodySmall = Font.system(size: 15)
    public static let code = Font.system(size: 15, design: .monospaced)
}

extension View {
    /// The soft shadow under everything that floats over the content (round buttons, the title capsule, the
    /// composer, the search capsule).
    public func phoneFloat() -> some View {
        shadow(color: Color.black.opacity(0.10), radius: 12, x: 0, y: 6)
            .shadow(color: Color.black.opacity(0.06), radius: 1.5, x: 0, y: 1)
    }
}

/// A round floating button: 44pt, the paper colour, a soft shadow (Grok's back, +, ⋯).
public struct PhoneRoundButton<Label: View>: View {
    let size: CGFloat
    let action: () -> Void
    let label: Label

    public init(size: CGFloat = Phone.button, action: @escaping () -> Void, @ViewBuilder label: () -> Label) {
        self.size = size
        self.action = action
        self.label = label()
    }

    public var body: some View {
        Button(action: action) {
            label
                .font(.system(size: 19, weight: .medium))
                .foregroundStyle(Color.madFg)
                .frame(width: size, height: size)
                .background(Color.madPaper, in: Circle())
                .contentShape(Circle())
        }
        .buttonStyle(.plain)
        .phoneFloat()
    }
}

/// The capsule in the middle of a screen's top bar: a picture, the title and an optional grey line under it.
public struct PhoneTitlePill<Picture: View>: View {
    let title: String
    let sub: String?
    let picture: Picture

    public init(title: String, sub: String? = nil, @ViewBuilder picture: () -> Picture) {
        self.title = title
        self.sub = sub
        self.picture = picture()
    }

    public var body: some View {
        HStack(spacing: 8) {
            picture
            VStack(alignment: .leading, spacing: 1) {
                Text(title).font(Phone.title).foregroundStyle(Color.madFg).lineLimit(1)
                if let sub, !sub.isEmpty { Text(sub).font(Phone.titleSub).foregroundStyle(Color.madFg3).lineLimit(1) }
            }
        }
        .padding(.leading, 6)
        .padding(.trailing, 16)
        .frame(height: Phone.button)
        .background(Color.madPaper, in: Capsule())
        .phoneFloat()
    }
}

extension PhoneTitlePill where Picture == EmptyView {
    public init(title: String, sub: String? = nil) { self.init(title: title, sub: sub) { EmptyView() } }
}

/// A screen's top bar: a round button at each side and the capsule between, over a fade from the paper colour so
/// the content scrolls under it.
public struct PhoneTopBar<Leading: View, Center: View, Trailing: View>: View {
    let leading: Leading
    let center: Center
    let trailing: Trailing

    public init(@ViewBuilder leading: () -> Leading, @ViewBuilder center: () -> Center, @ViewBuilder trailing: () -> Trailing) {
        self.leading = leading()
        self.center = center()
        self.trailing = trailing()
    }

    public var body: some View {
        HStack(spacing: 10) {
            leading
            Spacer(minLength: 0)
            center
            Spacer(minLength: 0)
            trailing
        }
        .padding(.horizontal, 16)
        .padding(.top, 6)
        .padding(.bottom, 14)
        .background(alignment: .top) {
            LinearGradient(stops: [.init(color: Color.madPaper, location: 0.55), .init(color: Color.madPaper.opacity(0), location: 1)],
                           startPoint: .top, endPoint: .bottom)
                .ignoresSafeArea(edges: .top)
                .allowsHitTesting(false)
        }
    }
}

/// The back button every pushed screen opens with.
public struct PhoneBackButton: View {
    let action: () -> Void
    public init(action: @escaping () -> Void) { self.action = action }
    public var body: some View {
        PhoneRoundButton(action: action) { Image(systemName: "chevron.backward").font(.system(size: 18, weight: .semibold)) }
            .accessibilityLabel(L("返回"))
    }
}

/// The composer's capsule: 58pt, the paper colour, a soft shadow; the screen puts its + , field and send or mic in it.
public struct PhoneComposerCapsule<Content: View>: View {
    let content: Content
    public init(@ViewBuilder content: () -> Content) { self.content = content() }
    public var body: some View {
        HStack(spacing: 10) { content }
            .padding(.horizontal, 8)
            .frame(minHeight: Phone.composerHeight)
            .background(Color.madPaper, in: RoundedRectangle(cornerRadius: Phone.composerHeight / 2, style: .continuous))
            .phoneFloat()
            .padding(.horizontal, 16)
            .padding(.bottom, Phone.footGap)
    }
}

/// A grey round button inside the composer (+, mic): 42pt.
public struct PhoneComposerButton: View {
    let symbol: String
    let label: String
    let action: () -> Void
    public init(symbol: String, label: String, action: @escaping () -> Void) {
        self.symbol = symbol
        self.label = label
        self.action = action
    }
    public var body: some View {
        Button(action: action) {
            Image(systemName: symbol).font(.system(size: 19, weight: .medium)).foregroundStyle(Color.madFg)
                .frame(width: 42, height: 42).background(Color.madRaise, in: Circle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
    }
}

/// A grey grouped card (Usage's sections, a sheet's list): 22pt corners on the side colour.
public struct PhoneCard<Content: View>: View {
    let title: String?
    let note: String?
    let content: Content
    public init(_ title: String? = nil, note: String? = nil, @ViewBuilder content: () -> Content) {
        self.title = title
        self.note = note
        self.content = content()
    }
    public var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if let title {
                HStack(alignment: .firstTextBaseline) {
                    Text(title).font(.system(size: 15, weight: .semibold)).foregroundStyle(Color.madFg2)
                    Spacer(minLength: 8)
                    if let note { Text(note).font(.system(size: 14)).foregroundStyle(Color.madFg3) }
                }
                .padding(.horizontal, 18).padding(.top, 14).padding(.bottom, 4)
            }
            content
        }
        .padding(.vertical, 6)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.madSide, in: RoundedRectangle(cornerRadius: 22, style: .continuous))
    }
}
#endif
