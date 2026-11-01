#if os(macOS)
import AppKit
import MadKit
import MadUI
import SwiftUI

/// The Mac window: a sidebar on the left and the detail beside it, as a chat app lays it out, on one surface with
/// the window's controls floating over it (no title bar). The sidebar is ours, not NavigationSplitView's: its width
/// is held between a least and a most (so the detail is never squeezed to nothing), it is dragged by its edge, and
/// hiding and showing it is animated.
struct MacShell<Detail: View>: View {
    let model: HomeModel
    @Binding var selection: HomeRow.ID?
    let actions: HomeActions
    /// How the Mac answers: when it does not, a capsule says so over the detail pane.
    let link: LinkState
    /// A channel or an agent is open (its name floats at the top, and the note goes under it).
    let rowOpen: Bool
    @ViewBuilder var detail: Detail

    @AppStorage("mad.sidebarWidth") private var storedWidth = 320.0
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// The resize cursor is pushed while the pointer is on the sidebar's edge; hiding the sidebar under it pops it.
    @State private var onEdge = false
    /// Where the traffic lights end (the window's left edge to the green one): the shown toggle is 12pt after it.
    @State private var lightsEdge: CGFloat = 72
    @State private var open = true
    @State private var dragFrom: Double?

    init(model: HomeModel, selection: Binding<HomeRow.ID?>, actions: HomeActions, link: LinkState = .ok, rowOpen: Bool = false,
         @ViewBuilder detail: () -> Detail) {
        self.model = model
        _selection = selection
        self.actions = actions
        self.link = link
        self.rowOpen = rowOpen
        self.detail = detail()
        #if DEBUG
        // `-sidebarHidden YES` starts with the sidebar hidden (and `-mad.sidebarWidth 420` sets its width), to look at them
        _open = State(initialValue: !UserDefaults.standard.bool(forKey: "sidebarHidden"))
        #endif
    }

    static var leastWidth: Double { 260 }
    static var mostWidth: Double { 420 }
    private var width: CGFloat { CGFloat(min(Self.mostWidth, max(Self.leastWidth, storedWidth))) }

    var body: some View {
        HStack(spacing: 0) {
            sidebar
            detail
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(Color.chatBackdrop)
                // Over the pane only, never the sidebar or the traffic lights' line, and it takes no room: nothing moves
                // when it comes or goes. With a row open it is 8pt under the name pill (44pt high, 12pt from the top).
                .overlay(alignment: .top) {
                    LinkNote(link: link)
                        .padding(.horizontal, 32)
                        .padding(.top, rowOpen ? 64 : 12)
                        .allowsHitTesting(false)
                }
        }
        .overlay(alignment: .topLeading) {
            if !open {
                Button { toggle() } label: {
                    Image(systemName: "sidebar.left").font(.system(size: 14, weight: .medium)).foregroundStyle(.primary)
                        .frame(width: 30, height: 30)
                        .background(.regularMaterial, in: Circle())
                        .overlay(Circle().strokeBorder(Color.chatLine.opacity(0.7)))
                }
                .buttonStyle(.plain)
                .accessibilityLabel("显示侧栏")
                // on the traffic lights' line (their centre is 16pt below the top), 12pt after the green one
                .padding(.leading, lightsEdge + 12).padding(.top, 1)
                .transition(.opacity)
            }
        }
        .background(WindowStyle { lightsEdge = $0 })
        .background {
            // ⌘\ hides and shows the sidebar
            Button("") { toggle() }.keyboardShortcut("\\", modifiers: .command).opacity(0).frame(width: 0, height: 0)
                .accessibilityHidden(true)
        }
        .ignoresSafeArea()
        .animation(reduceMotion ? nil : .easeInOut(duration: 0.22), value: open)
        .onChange(of: open) { _, isOpen in
            if !isOpen, onEdge { NSCursor.pop(); onEdge = false }
        }
    }

    /// The sidebar and its edge. Hidden it has no width (and is clipped), so that the change of width is animated.
    private var sidebar: some View {
        HStack(spacing: 0) {
            MacSidebar(model: model, selection: $selection, actions: actions, toggle: toggle)
                .frame(width: width)
            Rectangle().fill(Color.chatLine.opacity(0.7)).frame(width: 1)
        }
        .frame(width: open ? width + 1 : 0, alignment: .leading)
        .clipped()
        .overlay(alignment: .trailing) {
            if open {
                Color.clear.frame(width: 8).contentShape(Rectangle())
                    .offset(x: 4)
                    .onHover { inside in
                        if inside, !onEdge { NSCursor.resizeLeftRight.push(); onEdge = true }
                        else if !inside, onEdge { NSCursor.pop(); onEdge = false }
                    }
                    .gesture(DragGesture(minimumDistance: 1, coordinateSpace: .global)
                        .onChanged { v in
                            let from = dragFrom ?? Double(width)
                            dragFrom = from
                            storedWidth = min(Self.mostWidth, max(Self.leastWidth, from + v.translation.width))
                        }
                        .onEnded { _ in dragFrom = nil })
            }
        }
        .zIndex(1)
    }

    private func toggle() { open.toggle() }
}

/// The window without a title bar: its content goes to the top, under the traffic lights. It holds structurally:
/// SwiftUI puts a toolbar, a title or a separator back at times (when the inspector opens, a row is switched, the
/// window is resized or goes full screen), so the window is looked at after each update and put right when it
/// is not, rather than once at some moments. It also says where the traffic lights end.
private struct WindowStyle: NSViewRepresentable {
    let lights: (CGFloat) -> Void

    func makeNSView(context: Context) -> NSView { Styler(lights: lights) }
    func updateNSView(_ view: NSView, context: Context) {}

    private final class Styler: NSView {
        let lights: (CGFloat) -> Void
        private var observer: NSObjectProtocol?
        private var lastEdge: CGFloat = 0

        init(lights: @escaping (CGFloat) -> Void) {
            self.lights = lights
            super.init(frame: .zero)
        }

        required init?(coder: NSCoder) { nil }

        deinit { if let observer { NotificationCenter.default.removeObserver(observer) } }

        override func viewDidMoveToWindow() {
            if let observer { NotificationCenter.default.removeObserver(observer) }
            observer = nil
            guard let window else { return }
            observer = NotificationCenter.default.addObserver(forName: NSWindow.didUpdateNotification, object: window, queue: .main) { [weak self] _ in
                self?.apply()
            }
            apply()
        }

        private func apply() {
            guard let window else { return }
            if !window.titlebarAppearsTransparent { window.titlebarAppearsTransparent = true }
            if window.titleVisibility != .hidden { window.titleVisibility = .hidden }
            if window.titlebarSeparatorStyle != .none { window.titlebarSeparatorStyle = .none }
            if !window.styleMask.contains(.fullSizeContentView) { window.styleMask.insert(.fullSizeContentView) }
            if window.toolbar != nil { window.toolbar = nil }
            if let zoom = window.standardWindowButton(.zoomButton) {
                let edge = zoom.convert(zoom.bounds, to: nil).maxX
                if edge > 0, abs(edge - lastEdge) > 0.5 {
                    lastEdge = edge
                    lights(edge)
                }
            }
        }
    }
}
#endif
