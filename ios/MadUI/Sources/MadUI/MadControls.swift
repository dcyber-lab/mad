import SwiftUI
#if os(macOS)
import AppKit
import Collaboration
#endif

// The small pieces the approved design (v5) builds its screens from: buttons, pills, key caps, the dots of the
// section heads. Sizes are the design's (in points on the Mac); the phone's are set where a screen uses them.

/// The design's `.btn`: a hairline-bordered button on paper; `.primary` is filled with the text color, `.ghost` is a
/// soft fill with no border. `small` is `.btn.sm`; `large` is the phone's (a sheet's full-width buttons).
struct MadButtonStyle: ButtonStyle {
    enum Kind { case normal, primary, ghost, danger }
    enum Size { case small, regular, large }

    var kind = Kind.normal
    var size = Size.regular
    /// Fills the width it is given (the phone's sheet).
    var wide = false

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(font)
            .lineLimit(1)
            .foregroundStyle(foreground)
            .padding(.horizontal, hpad)
            .padding(.vertical, vpad)
            .frame(maxWidth: wide ? .infinity : nil)
            .background(background, in: RoundedRectangle(cornerRadius: radius, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: radius, style: .continuous)
                .strokeBorder(kind == .normal || kind == .danger ? Color.madLine2 : Color.clear, lineWidth: 1))
            .contentShape(RoundedRectangle(cornerRadius: radius, style: .continuous))
            .opacity(configuration.isPressed ? 0.7 : 1)
    }

    private var font: Font {
        if size == .small { return .system(size: 11.5) }
        if size == .regular { return .system(size: 12.5) }
        #if os(iOS)
        return .system(size: 15, weight: kind == .primary ? .semibold : .regular)
        #else
        return .system(size: 13.5, weight: kind == .primary ? .semibold : .regular)
        #endif
    }

    private var hpad: CGFloat { size == .small ? 8 : size == .regular ? 10 : 14 }
    private var vpad: CGFloat { size == .small ? 1 : size == .regular ? 3 : 10 }
    private var radius: CGFloat { size == .small ? 6 : size == .regular ? 8 : 12 }

    private var foreground: Color {
        switch kind {
        case .primary: .madPaper
        case .danger: .madDanger
        default: .madFg
        }
    }

    private var background: Color {
        switch kind {
        case .primary: .madFg
        case .ghost: .madRaise
        default: .madPaper
        }
    }
}

extension ButtonStyle where Self == MadButtonStyle {
    static func mad(_ kind: MadButtonStyle.Kind = .normal, _ size: MadButtonStyle.Size = .regular, wide: Bool = false) -> MadButtonStyle {
        MadButtonStyle(kind: kind, size: size, wide: wide)
    }
}

/// A key cap: "⌘K", "⏎". Shown on the Mac only (a phone has no keys to name).
struct Kbd: View {
    let keys: String
    init(_ keys: String) { self.keys = keys }

    var body: some View {
        #if os(macOS)
        Text(keys)
            .font(.system(size: 10.5, design: .monospaced))
            .foregroundStyle(Color.madFg3)
            .padding(.horizontal, 4)
            .padding(.bottom, 1)
            .background(Color.madPaper, in: RoundedRectangle(cornerRadius: 4))
            .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(Color.madLine2, lineWidth: 1))
            .overlay(alignment: .bottom) { Rectangle().fill(Color.madLine2).frame(height: 1).padding(.horizontal, 2) }
            .accessibilityHidden(true)
        #else
        EmptyView()
        #endif
    }
}

/// A small tinted word: "完成" in the done blue, a count in the accent.
struct MadPill: View {
    let text: String
    var color: Color = .madDone
    var fill: Color = .madDoneBg

    var body: some View {
        Text(text)
            .font(.system(size: 11.5, weight: .medium))
            .monospacedDigit()
            .foregroundStyle(color)
            .padding(.horizontal, 6)
            .frame(height: 19)
            .background(fill, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
            .fixedSize()
    }
}

/// The 7pt dot of a state's color before a section's title.
struct StateDot: View {
    let color: Color
    var body: some View { Circle().fill(color).frame(width: 7, height: 7) }
}

/// A command or a question in a box of code: one line, cut at the end.
struct CommandBox: View {
    let text: String
    var font: Font = .system(size: 11.5, design: .monospaced)

    var body: some View {
        Text(text)
            .font(font)
            .foregroundStyle(Color.madFg2)
            .lineLimit(1)
            .truncationMode(.tail)
            .padding(.horizontal, 7)
            .padding(.vertical, 3)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color.madCode, in: RoundedRectangle(cornerRadius: 6, style: .continuous))
    }
}

/// The person's own picture: their account's on the Mac, else their initial on the design's warm gradient.
struct PersonPicture: View {
    let name: String
    let size: CGFloat

    var body: some View {
        Group {
            #if os(macOS)
            if let image = PersonPicture.accountImage {
                Image(nsImage: image).resizable().scaledToFill()
            } else {
                initial
            }
            #else
            initial
            #endif
        }
        .frame(width: size, height: size)
        .clipShape(Circle())
        .accessibilityHidden(true)
    }

    #if os(macOS)
    /// The account's picture, read once.
    static let accountImage: NSImage? = CBUserIdentity(posixUID: getuid(), authority: CBIdentityAuthority.default())?.image
    #endif

    private var initial: some View {
        LinearGradient(colors: [Color(hex: 0xf59e0b), Color(hex: 0xef4444)], startPoint: .topLeading, endPoint: .bottomTrailing)
            .overlay(Text(String(name.prefix(1)).uppercased()).font(.system(size: size * 0.46, weight: .semibold)).foregroundStyle(.white))
    }
}
