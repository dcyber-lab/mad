import SwiftUI
#if canImport(UIKit)
import UIKit
typealias PlatformImage = UIImage
#elseif canImport(AppKit)
import AppKit
typealias PlatformImage = NSImage
#endif

/// Turning what the person picked or pasted into a picture the server keeps: it reads png, jpeg, gif
/// and webp (by what the bytes are), so anything else (a HEIC photo) is made a JPEG, and a picture over
/// the limit is made smaller.
public enum ChannelImage {
    /// The type of `data` by its first bytes; nil for any other.
    public static func sniff(_ data: Data) -> String? {
        let b = [UInt8](data.prefix(12))
        if b.starts(with: [0x89, 0x50, 0x4E, 0x47]) { return "image/png" }
        if b.starts(with: [0xFF, 0xD8, 0xFF]) { return "image/jpeg" }
        if b.starts(with: Array("GIF8".utf8)) { return "image/gif" }
        if b.count >= 12, b[0..<4].elementsEqual(Array("RIFF".utf8)), b[8..<12].elementsEqual(Array("WEBP".utf8)) { return "image/webp" }
        return nil
    }

    /// A draft of at most `ChannelLimits.imageBytes`, or nil when it is no picture or cannot be made small
    /// enough.
    public static func draft(from data: Data) -> ChannelDraftImage? {
        if let mime = sniff(data), data.count <= ChannelLimits.imageBytes { return ChannelDraftImage(mime: mime, data: data) }
        guard let image = PlatformImage(data: data) else { return nil }
        for quality in [0.85, 0.6, 0.4] {
            if let jpeg = jpeg(image, quality: quality), jpeg.count <= ChannelLimits.imageBytes {
                return ChannelDraftImage(mime: "image/jpeg", data: jpeg)
            }
        }
        return nil
    }

    static func jpeg(_ image: PlatformImage, quality: Double) -> Data? {
        #if canImport(UIKit)
        image.jpegData(compressionQuality: quality)
        #else
        guard let tiff = image.tiffRepresentation, let rep = NSBitmapImageRep(data: tiff) else { return nil }
        return rep.representation(using: .jpeg, properties: [.compressionFactor: quality])
        #endif
    }
}

extension Image {
    init?(data: Data) {
        guard let p = PlatformImage(data: data) else { return nil }
        #if canImport(UIKit)
        self.init(uiImage: p)
        #else
        self.init(nsImage: p)
        #endif
    }
}

/// One picture of a message, fetched on its own; a gray tile until it comes.
struct PictureThumb: View {
    let id: String
    let size: CGFloat
    let load: @Sendable (String) async -> Data?
    @State private var image: Image?
    @State private var failed = false

    var body: some View {
        ZStack {
            Color.chatLine.opacity(0.5)
            if let image {
                image.resizable().scaledToFill()
            } else if failed {
                Image(systemName: "photo").foregroundStyle(.secondary)
            } else {
                ProgressView()
            }
        }
        .frame(width: size, height: size)
        .clipShape(RoundedRectangle(cornerRadius: 12))
        .contentShape(RoundedRectangle(cornerRadius: 12))
        .accessibilityLabel("图片")
        .accessibilityAddTraits(.isImage)
        .task(id: id) {
            if let data = await load(id), let img = Image(data: data) { image = img } else { failed = true }
        }
    }
}

/// A picture shown large.
struct PictureViewer: View {
    let id: String
    let load: @Sendable (String) async -> Data?
    let close: () -> Void
    @State private var image: Image?

    var body: some View {
        ZStack(alignment: .topTrailing) {
            Color.black.ignoresSafeArea()
            if let image { image.resizable().scaledToFit().frame(maxWidth: .infinity, maxHeight: .infinity) } else { ProgressView().tint(.white).frame(maxWidth: .infinity, maxHeight: .infinity) }
            Button(action: close) {
                Image(systemName: "xmark.circle.fill").font(.title).symbolRenderingMode(.palette).foregroundStyle(.white, .gray.opacity(0.6))
            }
            .buttonStyle(.plain).padding(16).accessibilityLabel("关闭")
        }
        #if os(macOS)
        .frame(minWidth: 480, minHeight: 360)
        #endif
        .task(id: id) { if let d = await load(id) { image = Image(data: d) } }
    }
}
