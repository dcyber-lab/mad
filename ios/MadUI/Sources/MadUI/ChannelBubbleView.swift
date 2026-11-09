import SwiftUI
#if canImport(UIKit)
import UIKit
#elseif canImport(AppKit)
import AppKit
#endif

/// One bubble of a channel: grey for an agent, light blue with dark text for the person (its bottom right corner
/// 4pt), @names in their member's color; folded into a dashed capsule when the message is long or an agent's report,
/// opened in place by a tap. A long press offers 回复 and 复制.
struct ChannelBubbleView: View {
    let bubble: ChannelBubble
    let mine: Bool
    /// The widest the bubble may be (its column's share); it is as wide as its text up to that.
    let maxWidth: CGFloat
    let expanded: Bool
    let toggle: () -> Void
    let reply: () -> Void
    let load: @Sendable (String) async -> Data?
    let openPicture: (String) -> Void
    /// A member's name to its identity color, for the @names.
    var colors: [String: Color] = [:]

    var body: some View {
        Group {
            if let fold = bubble.fold { folded(fold) } else { whole }
        }
        .contextMenu {
            Button { reply() } label: { Label(L("回复"), systemImage: "arrowshape.turn.up.left") }
            if !bubble.text.isEmpty {
                Button { copy(bubble.text) } label: { Label(L("复制"), systemImage: "doc.on.doc") }
            }
        }
    }

    #if os(iOS)
    private var bodyFont: Font { Phone.message }
    private var lineSpacing: CGFloat { Phone.messageLineSpacing }
    private var padding: EdgeInsets { Phone.bubblePadding }
    private var radius: CGFloat { Phone.bubbleRadius }
    #else
    private var bodyFont: Font { MadType.message }
    private var lineSpacing: CGFloat { MadType.messageLineSpacing }
    private var padding: EdgeInsets { mine ? EdgeInsets(top: 8, leading: 14, bottom: 8, trailing: 14) : EdgeInsets(top: 8, leading: 13, bottom: 8, trailing: 13) }
    private var radius: CGFloat { 18 }
    #endif

    private var picturesOnly: Bool { bubble.text.isEmpty && bubble.quote == nil && !bubble.images.isEmpty }

    @ViewBuilder private var whole: some View {
        VStack(alignment: mine ? .trailing : .leading, spacing: 6) {
            if let q = bubble.quote { quote(q) }
            if !bubble.text.isEmpty {
                Text(chatText(bubble.text, mine: mine, colors: colors))
                    .font(bodyFont)
                    .lineSpacing(lineSpacing)
                    .multilineTextAlignment(.leading)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if !bubble.images.isEmpty { pictures }
        }
        .foregroundStyle(mine ? Color.madMineFg : Color.primary)
        .padding(picturesOnly ? EdgeInsets() : padding)
        .hugging(maxWidth: maxWidth)
        .background {
            if !picturesOnly {
                if mine { MineShape().fill(Color.madMine) } else { RoundedRectangle(cornerRadius: radius, style: .continuous).fill(Color.chatBubble) }
            }
        }
        // as wide as its text, and no wider than its share of the column
        .fixedSize(horizontal: false, vertical: true)
    }

    private func quote(_ q: ChannelQuote) -> some View {
        HStack(spacing: 6) {
            Rectangle().fill(mine ? Color.madMineFg.opacity(0.5) : Color.secondary.opacity(0.5)).frame(width: 2)
            Text("\(q.who) \(q.text)")
                .font(MadType.small)
                .lineLimit(1)
                .truncationMode(.tail)
                .opacity(0.8)
        }
        // inside its own bubble: the bubble is as wide as its widest line, and the quote gives way at the end
        .frame(height: 18)
    }

    private var pictures: some View {
        LazyVGrid(columns: [GridItem(.adaptive(minimum: 92, maximum: 150), spacing: 6)], alignment: .leading, spacing: 6) {
            ForEach(Array(bubble.images.enumerated()), id: \.offset) { _, id in
                PictureThumb(id: id, size: 110, load: load)
                    .onTapGesture { openPicture(id) }
            }
        }
        .frame(maxWidth: 260)
    }

    private func folded(_ fold: ChannelBubble.Fold) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            // as wide as its text (the chevron 10pt after it), as a bubble is, and no wider than a bubble may be
            HStack(spacing: 10) {
                Text(fold.summary)
                    .font(MadType.pill)
                    .lineLimit(expanded ? 3 : 1)
                    .truncationMode(.tail)
                Image(systemName: "chevron.right")
                    .font(MadType.small.weight(.semibold))
                    .rotationEffect(.degrees(expanded ? 90 : 0))
            }
            if expanded {
                Text(chatText(bubble.text, mine: false, colors: colors))
                    .font(MadType.detail)
                    .foregroundStyle(.primary)
                    .multilineTextAlignment(.leading)
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .foregroundStyle(Color.madFg3)
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
        .hugging(maxWidth: maxWidth)
        // the design's folded run: a dashed outline
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Color.madLine2, style: StrokeStyle(lineWidth: 1, dash: [4, 3])))
        .contentShape(RoundedRectangle(cornerRadius: 10))
        .onTapGesture { withAnimation(.easeInOut(duration: 0.18)) { toggle() } }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(fold.summary)
        .accessibilityHint(expanded ? L("收起") : L("展开"))
        .accessibilityAddTraits(.isButton)
    }

    private func copy(_ text: String) {
        #if canImport(UIKit)
        UIPasteboard.general.string = text
        #else
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
        #endif
    }
}
