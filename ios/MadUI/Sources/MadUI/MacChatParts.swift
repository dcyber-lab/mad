import SwiftUI

#if os(macOS)
import AppKit

// The pieces both Mac chats share (design v6, docs/design/mac-v6.html): the top (a capsule in the middle, round
// buttons in the corner), the column, the foot (the approval card and the composer floating over the list's end), a
// message's hover buttons, the agent page's tool card and its approval card.

extension View {
    /// Centred, no wider than the message column: the messages, the approval card and the composer share these edges.
    func macColumn() -> some View {
        frame(maxWidth: MadColumn.maxWidth)
            .padding(.horizontal, MadColumn.side)
            .frame(maxWidth: .infinity)
    }

    /// A chat's foot: the paper behind it, and the list's last lines fading out above it, so the card and the composer
    /// float over the messages instead of cutting them.
    func macFootBar() -> some View {
        background(Color.madPaper)
            .background(alignment: .top) {
                LinearGradient(colors: [Color.madPaper.opacity(0), Color.madPaper], startPoint: .top, endPoint: .bottom)
                    .frame(height: 28)
                    .offset(y: -28)
                    .allowsHitTesting(false)
            }
    }
}

/// The top of a chat: the capsule floats in the middle, the round buttons in the right corner (and one at the left when
/// the page can go back). The capsule never reaches under them or under the show-sidebar button, which floats at the
/// left while the sidebar is hidden (`madHeadLeading`).
struct MacTopBar<Leading: View, Pill: View, Corner: View>: View {
    /// How much room the corner's controls take (the capsule keeps this much clear on both sides).
    let cornerWidth: CGFloat
    @ViewBuilder var leading: Leading
    @ViewBuilder var pill: Pill
    @ViewBuilder var corner: Corner
    @Environment(\.madHeadLeading) private var headLeading

    var body: some View {
        let left = headLeading > 0 ? 20 + headLeading : 0
        ZStack(alignment: .top) {
            pill
                .padding(.horizontal, max(cornerWidth, left) + 12)
                .padding(.top, 12)
            HStack(spacing: 8) {
                leading
                Spacer(minLength: 0)
                corner
            }
            .padding(.leading, left > 0 ? left : 16)
            .padding(.trailing, 16)
            .padding(.top, 13)
        }
        .frame(maxWidth: .infinity)
    }
}

extension MacTopBar where Leading == EmptyView {
    init(cornerWidth: CGFloat, @ViewBuilder pill: () -> Pill, @ViewBuilder corner: () -> Corner) {
        self.init(cornerWidth: cornerWidth, leading: { EmptyView() }, pill: pill, corner: corner)
    }
}

/// A round floating button whose label is a symbol; `on` colours it (a panel that is open).
struct MacSymbolButton: View {
    let symbol: String
    let label: String
    var on = false
    let action: () -> Void

    var body: some View {
        MacRoundButton(action: action) {
            Image(systemName: symbol).foregroundStyle(on ? Color.madAccent : Color.madFg)
        }
        .accessibilityLabel(label)
        .help(label)
    }
}

// MARK: hover buttons

/// What a hovered message offers: reply (where the chat can reply) and a menu with copy. Nothing that does nothing.
struct MacHoverRow<Bubble: View>: View {
    /// `.leading`: someone else's message, the buttons at its right. `.trailing`: mine, the buttons at its left.
    let side: HorizontalAlignment
    let copyText: String?
    var reply: (() -> Void)?
    @ViewBuilder var bubble: Bubble
    @State private var overRow = false
    @State private var overBar = false

    private var others: Bool { side == .leading }
    private var visible: Bool { overRow || overBar }

    var body: some View {
        Group {
            if others {
                bubble.overlay(alignment: .trailing) { bar.alignmentGuide(.trailing) { $0[.leading] - 12 } }
            } else {
                bubble.overlay(alignment: .leading) { bar.alignmentGuide(.leading) { $0[.trailing] + 12 } }
            }
        }
        .frame(maxWidth: .infinity, alignment: others ? .leading : .trailing)
        .contentShape(Rectangle())
        .onHover { overRow = $0 }
    }

    private var bar: some View {
        HStack(spacing: 2) {
            if let reply { button("arrowshape.turn.up.left", L("回复"), reply) }
            if let copyText {
                Menu {
                    Button { Self.copy(copyText) } label: { Label(L("复制"), systemImage: "doc.on.doc") }
                    if let reply { Button(action: reply) { Label(L("回复"), systemImage: "arrowshape.turn.up.left") } }
                } label: {
                    Image(systemName: "ellipsis")
                        .font(.system(size: 15, weight: .medium))
                        .foregroundStyle(Color.madFg3)
                        .frame(width: 28, height: 28)
                        .contentShape(Rectangle())
                }
                .menuStyle(.button)
                .buttonStyle(.plain)
                .menuIndicator(.hidden)
                .fixedSize()
                .accessibilityLabel(L("更多"))
            }
        }
        .fixedSize()
        .opacity(visible ? 1 : 0)
        .allowsHitTesting(visible)
        .animation(.easeOut(duration: 0.12), value: visible)
        .onHover { overBar = $0 }
    }

    private func button(_ symbol: String, _ label: String, _ action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(.system(size: 15, weight: .medium))
                .foregroundStyle(Color.madFg3)
                .frame(width: 28, height: 28)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
        .help(label)
    }

    static func copy(_ text: String) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
    }
}

// MARK: tool calls

/// Consecutive tool calls as one bordered card (14pt corners): a dot per call (green done, red failed, a spinner while it
/// runs), the verb in bold and what it was pointed at, a chevron; a call with detail opens in place.
struct MacToolCard: View {
    let tools: [ConvTool]

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(tools.enumerated()), id: \.offset) { _, tool in MacToolLine(tool: tool) }
        }
        .padding(.vertical, 4)
        .frame(minWidth: 300, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Color.madLine2.opacity(0.7), lineWidth: 1))
        .accessibilityElement(children: .contain)
    }
}

private struct MacToolLine: View {
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
        .padding(.horizontal, 15)
        .padding(.vertical, 8)
    }

    private var line: some View {
        HStack(spacing: 10) {
            dot
            summary
                .font(MadType.pill)
                .lineLimit(open ? nil : 1)
                .truncationMode(.tail)
            if tool.hasDetail {
                Spacer(minLength: 12)
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

// MARK: approval

/// What waits on the person, floating above the composer (design v6): a white card, 18pt corners and a shadow, as wide as
/// the column. A command: "想运行一条命令" in amber and how long it has waited, the whole command in a mono box, 允许
/// (black) and 拒绝 pills, 本次会话都允许 when the model offers it, and the keys named at the right (Return allows, Esc
/// denies). A question: its words and its answers. An act that takes words asks for them first.
struct AskCardView: View {
    let card: AskCard
    var agentID = ""
    var agentKind = ""
    let act: (String, String?) -> Void
    @State private var asking: AskAct?
    @State private var text = ""

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(card.title)
                    .font(.system(size: 16, weight: .semibold))
                    .foregroundStyle(Color.madWait)
                    .fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: 8)
                if !card.waited.isEmpty { Text(card.waited).font(.system(size: 14)).foregroundStyle(Color.madFg3) }
            }
            if !card.detail.isEmpty { detail }
            acts
        }
        .padding(.horizontal, 18)
        .padding(.top, 16)
        .padding(.bottom, 14)
        .fixedSize(horizontal: false, vertical: true)
        .background(Color.macFloatFill, in: RoundedRectangle(cornerRadius: Mac.cardRadius, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: Mac.cardRadius, style: .continuous).strokeBorder(Color.madLine, lineWidth: 1))
        .shadow(color: Color(red: 15 / 255, green: 18 / 255, blue: 30 / 255).opacity(0.12), radius: 17, x: 0, y: 10)
        .accessibilityElement(children: .contain)
        .accessibilityLabel(card.accessibilityLabel)
    }

    @ViewBuilder private var detail: some View {
        if card.mono {
            // the whole command, wrapping at spaces (a word too long for the line breaks inside itself)
            Text(card.detail)
                .font(MadType.code)
                .lineSpacing(5)
                .foregroundStyle(Color.madFg)
                .fixedSize(horizontal: false, vertical: true)
                .textSelection(.enabled)
                .padding(.horizontal, 13).padding(.vertical, 10)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Color.madCode, in: RoundedRectangle(cornerRadius: 11, style: .continuous))
        } else {
            Text(card.detail)
                .font(MadType.detail)
                .foregroundStyle(Color.madFg)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    @ViewBuilder private var acts: some View {
        if let asking {
            VStack(alignment: .leading, spacing: 10) {
                TextField(asking.label, text: $text, axis: .vertical)
                    .lineLimit(1...4)
                    .textFieldStyle(.plain)
                    .font(MadType.detail)
                    .padding(.horizontal, 13).padding(.vertical, 9)
                    .background(Color.madRaise, in: RoundedRectangle(cornerRadius: 11, style: .continuous))
                    .onSubmit(sendWords)
                HStack(spacing: 8) {
                    Button(asking.label, action: sendWords).buttonStyle(MacAskPill(filled: true))
                    Button(L("取消")) { self.asking = nil; text = "" }.buttonStyle(MacAskPill(filled: false))
                        .keyboardShortcut(.cancelAction)
                }
            }
        } else if !card.questions.isEmpty {
            AskForm(questions: card.questions) { answers in act("send", answers.joined(separator: "\n")) }
        } else {
            HStack(alignment: .top, spacing: 8) {
                AskChips(card: card, send: { act($0.id, nil) }, input: { asking = $0 })
                if card.permission, returnAct != nil || escapeAct != nil {
                    Spacer(minLength: 8)
                    HStack(spacing: 14) {
                        if returnAct != nil { hint("⏎", L("允许")) }
                        if escapeAct != nil { hint("esc", L("拒绝")) }
                    }
                    .font(.system(size: 13.5))
                    .foregroundStyle(Color.madFg3)
                    .fixedSize()
                    .frame(minHeight: 36)
                    .accessibilityHidden(true)
                }
            }
            .background {
                // Return allows and Esc denies, as the hint says (a focused field takes its own Return first)
                if let a = returnAct {
                    Button("") { act(a.id, nil) }.keyboardShortcut(.defaultAction).opacity(0).frame(width: 0, height: 0).accessibilityHidden(true)
                }
                if let a = escapeAct {
                    Button("") { act(a.id, nil) }.keyboardShortcut(.cancelAction).opacity(0).frame(width: 0, height: 0).accessibilityHidden(true)
                }
            }
        }
    }

    private func hint(_ key: String, _ what: String) -> some View {
        HStack(spacing: 5) {
            Text(key)
                .font(.system(size: 11, design: .monospaced))
                .padding(.horizontal, 5).padding(.vertical, 1)
                .overlay(RoundedRectangle(cornerRadius: 4, style: .continuous).strokeBorder(Color.madLine2, lineWidth: 1))
            Text(what)
        }
    }

    /// The act Return does: the card's Allow (a command's).
    private var returnAct: AskAct? { card.permission ? card.acts.first { $0.kind == .allow } : nil }
    /// The act Esc does: Deny, else Deny and explain.
    private var escapeAct: AskAct? {
        card.permission ? (card.acts.first { $0.kind == .deny } ?? card.acts.first { $0.kind == .denyWhy }) : nil
    }

    private func choose(_ a: AskAct) {
        if a.input { asking = a } else { act(a.id, nil) }
    }

    private func sendWords() {
        guard let asking else { return }
        act(asking.id, text.trimmingCharacters(in: .whitespacesAndNewlines))
        self.asking = nil
        text = ""
    }
}

/// An approval card's pill: 38pt, allow filled black, the rest outlined.
private struct MacAskPill: ButtonStyle {
    let filled: Bool
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 14.5, weight: .semibold))
            .lineLimit(3)
            .multilineTextAlignment(.center)
            .foregroundStyle(filled ? Color.madPaper : Color.madFg)
            .padding(.horizontal, 18)
            .frame(minHeight: 38)
            .background(filled ? Color.madFg : Color.macFloatFill, in: Capsule())
            .overlay(Capsule().strokeBorder(filled ? Color.madFg : Color.madLine2, lineWidth: 1))
            .opacity(configuration.isPressed ? 0.7 : 1)
            .contentShape(Capsule())
    }
}
#endif
