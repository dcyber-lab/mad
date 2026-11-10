import SwiftUI
#if canImport(AppKit)
import AppKit
import UniformTypeIdentifiers
#endif

#if os(macOS)
/// What a ⌘V should attach from the pasteboard.
enum PastedPictures {
    /// The pictures on the pasteboard: files copied in Finder, else a screenshot or copied image when no text came with
    /// it (copying text from a page can bring a picture too: the text wins). Empty when the paste is text.
    static func read(_ pb: NSPasteboard = .general) -> [Data] {
        let opts: [NSPasteboard.ReadingOptionKey: Any] = [.urlReadingFileURLsOnly: true,
                                                          .urlReadingContentsConformToTypes: [UTType.image.identifier]]
        if let urls = pb.readObjects(forClasses: [NSURL.self], options: opts) as? [URL], !urls.isEmpty {
            return urls.compactMap { try? Data(contentsOf: $0) }
        }
        if let s = pb.string(forType: .string), !s.isEmpty { return [] }
        for type in [NSPasteboard.PasteboardType.png, .tiff] {
            if let d = pb.data(forType: type) { return [d] }
        }
        return []
    }
}

/// ⌘V while a composer's field has the focus. The focused field's own editor takes ⌘V first and only pastes text, so
/// a picture on the pasteboard never reached `onPasteCommand`: a local key monitor looks first, and takes the key only
/// for a picture.
@MainActor
final class PasteMonitor {
    private var token: Any?

    func start(_ take: @escaping @MainActor ([Data]) -> Void) {
        stop()
        token = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { event in
            let flags = event.modifierFlags.intersection(.deviceIndependentFlagsMask).subtracting(.capsLock)
            guard flags == .command, event.charactersIgnoringModifiers?.lowercased() == "v",
                  event.window != nil, event.window === NSApp.keyWindow else { return event }
            let pictures = MainActor.assumeIsolated { PastedPictures.read() }
            if pictures.isEmpty { return event }
            MainActor.assumeIsolated { take(pictures) }
            return nil
        }
    }

    func stop() {
        if let token { NSEvent.removeMonitor(token) }
        token = nil
    }

    deinit { if let token { NSEvent.removeMonitor(token) } }
}
#endif

extension View {
    /// A picture pasted into the composer is handed to `take` (the Mac: ⌘V while `focused`, and Edit > Paste).
    @ViewBuilder func pastesPictures(focused: Bool, take: @escaping @MainActor (Data) -> Void) -> some View {
        #if os(macOS)
        modifier(PastePictures(focused: focused, take: take))
        #else
        self
        #endif
    }
}

#if os(macOS)
private struct PastePictures: ViewModifier {
    let focused: Bool
    let take: @MainActor (Data) -> Void
    @State private var monitor = PasteMonitor()

    func body(content: Content) -> some View {
        content
            .onPasteCommand(of: [.image]) { providers in
                for p in providers {
                    p.loadDataRepresentation(forTypeIdentifier: "public.image") { data, _ in
                        if let data { Task { @MainActor in take(data) } }
                    }
                }
            }
            .onChange(of: focused, initial: true) { _, on in
                if on { monitor.start { for d in $0 { take(d) } } } else { monitor.stop() }
            }
            .onDisappear { monitor.stop() }
    }
}
#endif

/// The pictures waiting above a composer, each with a button to take it out.
struct DraftImageStrip: View {
    @Binding var images: [ChannelDraftImage]

    var body: some View {
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
}
