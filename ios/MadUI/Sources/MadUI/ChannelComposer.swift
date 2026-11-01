import PhotosUI
import SwiftUI
#if canImport(UIKit)
import UIKit
#elseif canImport(AppKit)
import AppKit
#endif

/// The one-line composer: ＋ for pictures, a field that grows, send. Pictures wait above it as
/// thumbnails the person can take out (six at most); what is being answered waits above them.
struct ChannelComposer: View {
    let placeholder: String
    let readOnly: Bool
    /// Who an @ can wake: typing @ offers them, as the web page does.
    let mentions: [ChannelMember]
    @Binding var replying: ChannelQuote?
    let send: @MainActor @Sendable (String, [ChannelDraftImage], Int?) async -> Bool

    @State private var text = {
        #if DEBUG
        UserDefaults.standard.string(forKey: "composerText") ?? "" // `-composerText "@a"`: to look at the @ list
        #else
        ""
        #endif
    }()
    @State private var images: [ChannelDraftImage] = []
    @State private var picking = false
    @State private var picked: [PhotosPickerItem] = []
    @State private var sending = false
    @State private var note: String?
    @State private var pick = 0
    @FocusState private var focused: Bool

    private var canSend: Bool {
        !sending && (!text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !images.isEmpty)
    }

    var body: some View {
        if readOnly {
            Text("这是两个 agent 之间的对话，只能看。")
                .font(MadType.small).foregroundStyle(.secondary)
                .frame(maxWidth: .infinity).padding(12)
        } else {
            composer
        }
    }

    private var composer: some View {
        VStack(alignment: .leading, spacing: 6) {
            if let q = replying { replyBar(q) }
            if !images.isEmpty { thumbnails }
            field
            if let note { Text(note).font(MadType.small).foregroundStyle(.red) }
        }
        // no side padding of its own: the column the messages are in gives the edges
        .padding(.top, 8)
        #if os(macOS)
        .padding(.bottom, 16)
        .onPasteCommand(of: [.image]) { providers in pasteImages(providers) }
        #else
        .padding(.bottom, 8)
        #endif
        // the @ list floats over the messages, its foot 8pt above the composer: the messages keep their size
        .overlay(alignment: .topLeading) {
            if !matches.isEmpty {
                // A zero-height line along the composer's top edge: the list stands on it (its foot 8pt above the
                // edge) and grows upward over the messages, at its own height.
                Color.clear.frame(height: 0)
                    .overlay(alignment: .bottomLeading) {
                        mentionList.fixedSize(horizontal: false, vertical: true).padding(.bottom, 8)
                    }
            }
        }
        .photosPicker(isPresented: $picking, selection: $picked, maxSelectionCount: max(1, ChannelLimits.images - images.count), matching: .images)
        .onChange(of: picked) { _, items in loadPicked(items) }
        .onChange(of: text) { _, _ in pick = 0 }
    }

    /// ＋, the field and send. On the Mac they are one rounded bar with a hairline and a soft shadow, as in a chat app;
    /// on the phone three pieces.
    @ViewBuilder private var field: some View {
        #if os(macOS)
        HStack(alignment: .bottom, spacing: 8) {
            plus
            textField.padding(.vertical, 7)
            sendButton
        }
        .padding(.horizontal, 8).padding(.vertical, 6)
        .background(RoundedRectangle(cornerRadius: 24, style: .continuous).fill(Color.chatComposer))
        .overlay(RoundedRectangle(cornerRadius: 24, style: .continuous).strokeBorder(Color.chatLine))
        .shadow(color: .black.opacity(0.07), radius: 10, y: 3)
        #else
        HStack(alignment: .bottom, spacing: 8) {
            plus
            textField
                .padding(.horizontal, 14)
                .padding(.vertical, 8)
                .frame(minHeight: 36)
                .background(Color.chatCard, in: RoundedRectangle(cornerRadius: 19))
            sendButton
        }
        #endif
    }

    private var textField: some View {
        TextField(placeholder, text: $text, axis: .vertical)
            .lineLimit(1...5)
            .textFieldStyle(.plain)
            .font(MadType.message)
            .focused($focused)
            #if os(macOS)
            .onSubmit { submit() }
            .onKeyPress(.upArrow) { matches.isEmpty ? .ignored : { pick = (pick + matches.count - 1) % matches.count; return .handled }() }
            .onKeyPress(.downArrow) { matches.isEmpty ? .ignored : { pick = (pick + 1) % matches.count; return .handled }() }
            .onKeyPress(.tab) { matches.isEmpty ? .ignored : { choose(matches[min(pick, matches.count - 1)]); return .handled }() }
            .onKeyPress(.return) { matches.isEmpty ? .ignored : { choose(matches[min(pick, matches.count - 1)]); return .handled }() }
            .onKeyPress(.escape) { matches.isEmpty ? .ignored : { text += " "; return .handled }() }
            #endif
    }

    private var sendButton: some View {
        Button(action: submit) {
            Image(systemName: "arrow.up")
                .font(.system(size: 17, weight: .semibold))
                .foregroundStyle(.white)
                .frame(width: 36, height: 36)
                .background(canSend ? Color.chatBlue : Color.chatBlue.opacity(0.35), in: Circle())
        }
        .buttonStyle(.plain)
        .disabled(!canSend)
        .accessibilityLabel("发送")
    }

    // MARK: @

    /// What is typed after the last @, when that @ starts a word and the word is not over; nil otherwise.
    private var mentionQuery: String? { ChannelMention.query(in: text) }

    /// The members the word could be: those whose name starts with it first, then those that contain it.
    private var matches: [ChannelMember] {
        guard let q = mentionQuery else { return [] }
        return ChannelMention.matches(for: q, in: mentions)
    }

    private func choose(_ m: ChannelMember) {
        text = ChannelMention.completing(text, with: m.name)
    }

    private var mentionList: some View {
        VStack(spacing: 0) {
            ForEach(Array(matches.enumerated()), id: \.element.id) { i, m in
                Button { choose(m) } label: {
                    HStack(spacing: 10) {
                        CharacterView(id: m.id, kind: m.kind, state: m.state, size: 24)
                        Text(m.name).font(MadType.rowLine).lineLimit(1)
                        if !m.role.isEmpty, m.role != m.name { Text(m.role).font(MadType.small).foregroundStyle(.secondary) }
                        Spacer(minLength: 0)
                    }
                    .padding(.horizontal, 10).padding(.vertical, 7)
                    // the selected one as the sidebar's open row is drawn: a soft rounded rect, inset
                    .background(i == min(pick, matches.count - 1) ? Color.primary.opacity(0.08) : Color.clear,
                                in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                    .padding(.horizontal, 4)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
        }
        .padding(.vertical, 4)
        .background(Color.chatComposer, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Color.chatLine))
        .shadow(color: .black.opacity(0.08), radius: 8, y: 2)
        .frame(maxWidth: 360, alignment: .leading)
        .accessibilityLabel("可以 @ 的成员")
    }

    private var plus: some View {
        Menu {
            Button { picking = true } label: { Label("照片", systemImage: "photo") }
            #if os(iOS)
            Button { pasteFromPasteboard() } label: { Label("粘贴图片", systemImage: "doc.on.clipboard") }
            #endif
        } label: {
            Image(systemName: "plus")
                .font(.system(size: 17, weight: .medium))
                .foregroundStyle(.secondary)
                .frame(width: 36, height: 36)
                #if os(iOS)
                .background(Color.chatCard, in: Circle())
                #endif
        }
        .menuStyle(.button)
        .buttonStyle(.plain)
        .menuIndicator(.hidden)
        .disabled(images.count >= ChannelLimits.images)
        .accessibilityLabel("添加图片")
    }

    private func replyBar(_ q: ChannelQuote) -> some View {
        HStack(spacing: 8) {
            Rectangle().fill(Color.chatBlue).frame(width: 2, height: 16)
            Text("回复 \(q.who)：\(q.text)").font(MadType.small).foregroundStyle(.secondary).lineLimit(1)
            Spacer(minLength: 0)
            Button { replying = nil } label: { Image(systemName: "xmark.circle.fill").foregroundStyle(.secondary) }
                .buttonStyle(.plain).accessibilityLabel("不回复")
        }
        .padding(.horizontal, 4)
    }

    private var thumbnails: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                ForEach(images) { img in
                    ZStack(alignment: .topTrailing) {
                        if let i = Image(data: img.data) {
                            i.resizable().scaledToFill().frame(width: 64, height: 64).clipShape(RoundedRectangle(cornerRadius: 12))
                        }
                        Button { images.removeAll { $0.id == img.id } } label: {
                            Image(systemName: "xmark.circle.fill").symbolRenderingMode(.palette).foregroundStyle(.white, .black.opacity(0.6))
                        }
                        .buttonStyle(.plain).offset(x: 5, y: -5).accessibilityLabel("去掉这张")
                    }
                    .padding(.top, 5).padding(.trailing, 5)
                }
            }
        }
    }

    private func submit() {
        guard canSend else { return }
        let body = text.trimmingCharacters(in: .whitespacesAndNewlines)
        let attached = images
        let to = replying?.seq
        sending = true
        note = nil
        Task { @MainActor in
            let ok = await send(body, attached, to)
            sending = false
            if ok {
                text = ""
                images = []
                picked = []
                replying = nil
            } else {
                note = "没有发出去：再试一次。"
            }
        }
    }

    /// Takes a picture's bytes: made a type the server keeps, and refused past six or too large.
    private func add(_ data: Data) {
        guard images.count < ChannelLimits.images else { note = "一条消息最多 \(ChannelLimits.images) 张图片。"; return }
        guard let d = ChannelImage.draft(from: data) else { note = "这张图片放不进去（不是图片，或太大）。"; return }
        images.append(d)
    }

    private func loadPicked(_ items: [PhotosPickerItem]) {
        guard !items.isEmpty else { return }
        Task { @MainActor in
            for item in items {
                if let data = try? await item.loadTransferable(type: Data.self) { add(data) }
            }
            picked = []
        }
    }

    #if os(macOS)
    private func pasteImages(_ providers: [NSItemProvider]) {
        for p in providers {
            p.loadDataRepresentation(forTypeIdentifier: "public.image") { data, _ in
                if let data { Task { @MainActor in add(data) } }
            }
        }
    }
    #endif

    #if os(iOS)
    private func pasteFromPasteboard() {
        guard let image = UIPasteboard.general.image, let data = image.pngData() else { note = "剪贴板里没有图片。"; return }
        add(data)
    }
    #endif
}


/// Typing @ in the composer: the word being typed, who it could be, and the text with it completed.
enum ChannelMention {
    /// What is typed after the last @, when that @ starts a word and the word is not over; nil otherwise.
    static func query(in text: String) -> String? {
        guard let at = text.lastIndex(of: "@") else { return nil }
        if let before = text[..<at].last, !before.isWhitespace { return nil }
        let word = text[text.index(after: at)...]
        return word.contains(where: \.isWhitespace) ? nil : String(word)
    }

    /// The members the word could be: those whose name starts with it first, then those that contain it; a faded
    /// role (no agent behind it) is not offered. At most six.
    static func matches(for query: String, in members: [ChannelMember]) -> [ChannelMember] {
        let q = query.lowercased()
        let names = members.filter { !$0.dim }
        let starts = names.filter { $0.name.lowercased().hasPrefix(q) }
        let has = names.filter { !$0.name.lowercased().hasPrefix(q) && $0.name.lowercased().contains(q) }
        return Array((starts + has).prefix(6))
    }

    /// `text` with the word being typed replaced by `@name ` (unchanged when there is none).
    static func completing(_ text: String, with name: String) -> String {
        guard query(in: text) != nil, let at = text.lastIndex(of: "@") else { return text }
        return String(text[..<at]) + "@\(name) "
    }
}
