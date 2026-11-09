import SwiftUI

#if os(iOS)
/// Pieces the iPhone's two chats share (design v6): the fades the list scrolls under, and the agent page's tool card.

/// The fade under a chat's top bar: the paper colour over the list's head, clear at its foot.
struct PhoneHeadFade: View {
    var body: some View {
        LinearGradient(stops: [.init(color: Color.madPaper, location: 0.62), .init(color: Color.madPaper.opacity(0), location: 1)],
                       startPoint: .top, endPoint: .bottom)
            .ignoresSafeArea(edges: .top)
            .allowsHitTesting(false)
    }
}

/// The fade over a chat's foot (behind the floating composer and the approval card), as the head's.
struct PhoneFootFade: View {
    var body: some View {
        LinearGradient(stops: [.init(color: Color.madPaper.opacity(0), location: 0), .init(color: Color.madPaper, location: 0.5)],
                       startPoint: .top, endPoint: .bottom)
            .padding(.top, -14)
            .ignoresSafeArea(edges: .bottom)
            .allowsHitTesting(false)
    }
}

/// Consecutive tool calls as one small bordered list: a dot per call (green done, red failed, a spinner while it runs),
/// the verb in bold and what it was pointed at. A call with detail opens in place on a tap, as before.
struct ToolCard: View {
    let tools: [ConvTool]

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(tools.enumerated()), id: \.offset) { _, tool in ToolCardLine(tool: tool) }
        }
        .padding(.vertical, 4)
        .background(RoundedRectangle(cornerRadius: 18, style: .continuous).strokeBorder(Color.madLine, lineWidth: 1))
        .accessibilityElement(children: .contain)
    }
}

private struct ToolCardLine: View {
    let tool: ConvTool
    @State private var open = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if tool.hasDetail {
                Button { withAnimation(.snappy(duration: 0.2)) { open.toggle() } } label: { line }
                    .buttonStyle(.plain)
                    .accessibilityLabel(tool.accessibilityLabel)
                    .accessibilityValue(open ? "expanded" : "collapsed")
                    .accessibilityHint(open ? "collapses" : "expands")
            } else {
                line
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel(tool.accessibilityLabel)
            }
            if open {
                if !tool.input.isEmpty { DetailBox(title: "input", text: tool.input, error: false) }
                if let result = tool.result, !result.isEmpty {
                    DetailBox(title: tool.error ? "error" : "result", text: result, error: tool.error)
                }
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 8)
    }

    private var line: some View {
        HStack(spacing: 10) {
            dot
            summary
                .font(Phone.bodySmall)
                .lineLimit(open ? nil : 1)
                .truncationMode(.tail)
            if tool.hasDetail {
                Spacer(minLength: 0)
                Image(systemName: "chevron.right")
                    .font(.system(size: 12, weight: .semibold))
                    .foregroundStyle(Color.madFg3)
                    .rotationEffect(.degrees(open ? 90 : 0))
            }
        }
        .contentShape(Rectangle())
    }

    @ViewBuilder private var dot: some View {
        if tool.running {
            ProgressView().controlSize(.mini).frame(width: 8, height: 8)
        } else {
            Circle().fill(tool.error ? Color.madDanger : Color.madWork).frame(width: 8, height: 8)
        }
    }

    private var summary: Text {
        tool.label.isEmpty
            ? Text(tool.name).bold().foregroundColor(.madFg)
            : Text(tool.name).bold().foregroundColor(.madFg) + Text(" \(tool.label)").foregroundColor(.madFg2)
    }
}
#endif
