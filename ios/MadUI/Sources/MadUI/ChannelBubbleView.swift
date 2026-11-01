import SwiftUI
#if canImport(UIKit)
import UIKit
#elseif canImport(AppKit)
import AppKit
#endif

/// One bubble of a channel: grey for an agent, blue for the person; folded into a bordered line when
/// the message is long or an agent's report, opened in place by a tap. A long press offers 回复 and 复制.
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

    var body: some View {
        Group {
            if let fold = bubble.fold { folded(fold) } else { whole }
        }
        .contextMenu {
            Button { reply() } label: { Label("回复", systemImage: "arrowshape.turn.up.left") }
            if !bubble.text.isEmpty {
                Button { copy(bubble.text) } label: { Label("复制", systemImage: "doc.on.doc") }
            }
        }
    }

    private var picturesOnly: Bool { bubble.text.isEmpty && bubble.quote == nil && !bubble.images.isEmpty }

    @ViewBuilder private var whole: some View {
        VStack(alignment: mine ? .trailing : .leading, spacing: 6) {
            if let q = bubble.quote { quote(q) }
            if !bubble.text.isEmpty {
                Text(chatText(bubble.text, mine: mine))
                    .font(MadType.message)
                    .multilineTextAlignment(.leading)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if !bubble.images.isEmpty { pictures }
        }
        .foregroundStyle(mine ? Color.white : Color.primary)
        .padding(.horizontal, picturesOnly ? 0 : 13)
        .padding(.vertical, picturesOnly ? 0 : 8)
        .hugging(maxWidth: maxWidth)
        .background(picturesOnly ? Color.clear : (mine ? Color.chatBlue : Color.chatBubble), in: RoundedRectangle(cornerRadius: 18))
        // as wide as its text, and no wider than its share of the column
        .fixedSize(horizontal: false, vertical: true)
    }

    private func quote(_ q: ChannelQuote) -> some View {
        HStack(spacing: 6) {
            Rectangle().fill(mine ? Color.white.opacity(0.7) : Color.secondary.opacity(0.5)).frame(width: 2)
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
            HStack(spacing: 8) {
                (Text(fold.head).fontWeight(.semibold).foregroundColor(.primary) + Text(fold.head.isEmpty ? fold.summary : "  " + fold.summary))
                    .font(MadType.pill)
                    .lineLimit(expanded ? 3 : 1)
                    .truncationMode(.tail)
                Spacer(minLength: 0)
                Image(systemName: "chevron.right")
                    .font(MadType.small.weight(.semibold))
                    .rotationEffect(.degrees(expanded ? 90 : 0))
            }
            if expanded {
                Text(chatText(bubble.text, mine: false))
                    .font(MadType.detail)
                    .foregroundStyle(.primary)
                    .multilineTextAlignment(.leading)
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .foregroundStyle(.secondary)
        .padding(.horizontal, 11)
        .padding(.vertical, 6)
        .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(Color.chatLine, lineWidth: 1))
        .contentShape(RoundedRectangle(cornerRadius: 14))
        .onTapGesture { withAnimation(.easeInOut(duration: 0.18)) { toggle() } }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(fold.head + " " + fold.summary)
        .accessibilityHint(expanded ? "收起" : "展开")
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
