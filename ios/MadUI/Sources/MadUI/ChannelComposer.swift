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
    @Binding var replying: ChannelQuote?
    let send: @MainActor @Sendable (String, [ChannelDraftImage], Int?) async -> Bool

    @State private var text = ""
    @State private var images: [ChannelDraftImage] = []
    @State private var picking = false
    @State private var picked: [PhotosPickerItem] = []
    @State private var sending = false
    @State private var note: String?
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
            HStack(alignment: .bottom, spacing: 8) {
                plus
                TextField(placeholder, text: $text, axis: .vertical)
                    .lineLimit(1...5)
                    .textFieldStyle(.plain)
                    .font(MadType.message)
                    .focused($focused)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 8)
                    .frame(minHeight: 36)
                    .background(Color.chatCard, in: RoundedRectangle(cornerRadius: 19))
                    #if os(macOS)
                    .onSubmit { submit() }
                    #endif
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
            if let note { Text(note).font(MadType.small).foregroundStyle(.red) }
        }
        // no side padding of its own: the column the messages are in gives the edges
        .padding(.top, 8)
        .padding(.bottom, 8)
        #if os(macOS)
        .onPasteCommand(of: [.image]) { providers in pasteImages(providers) }
        #endif
        .photosPicker(isPresented: $picking, selection: $picked, maxSelectionCount: max(1, ChannelLimits.images - images.count), matching: .images)
        .onChange(of: picked) { _, items in loadPicked(items) }
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
                .background(Color.chatCard, in: Circle())
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
