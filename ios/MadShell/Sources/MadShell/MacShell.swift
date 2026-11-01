#if os(macOS)
import AppKit
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
    @ViewBuilder var detail: Detail

    @AppStorage("mad.sidebarWidth") private var storedWidth = 300.0
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// The resize cursor is pushed while the pointer is on the sidebar's edge; hiding the sidebar under it pops it.
    @State private var onEdge = false
    @State private var open = true
    @State private var dragFrom: Double?

    init(model: HomeModel, selection: Binding<HomeRow.ID?>, actions: HomeActions, @ViewBuilder detail: () -> Detail) {
        self.model = model
        _selection = selection
        self.actions = actions
        self.detail = detail()
        #if DEBUG
        // `-sidebarHidden YES` starts with the sidebar hidden (and `-mad.sidebarWidth 420` sets its width), to look at them
        _open = State(initialValue: !UserDefaults.standard.bool(forKey: "sidebarHidden"))
        #endif
    }

    static var leastWidth: Double { 240 }
    static var mostWidth: Double { 420 }
    private var width: CGFloat { CGFloat(min(Self.mostWidth, max(Self.leastWidth, storedWidth))) }

    var body: some View {
        HStack(spacing: 0) {
            sidebar
            detail
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .background(Color.chatBackdrop)
        }
        .overlay(alignment: .topLeading) {
            if !open {
                Button { toggle() } label: {
                    Image(systemName: "sidebar.left").font(.system(size: 14, weight: .medium)).foregroundStyle(.primary)
                        .frame(width: 34, height: 34)
                        .background(.regularMaterial, in: Circle())
                        .overlay(Circle().strokeBorder(Color.chatLine.opacity(0.7)))
                }
                .buttonStyle(.plain)
                .accessibilityLabel("显示侧栏")
                .padding(.leading, 88).padding(.top, 9)
                .transition(.opacity)
            }
        }
        .background(WindowStyle())
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

/// The window without a title bar: its content goes to the top, under the traffic lights.
private struct WindowStyle: NSViewRepresentable {
    func makeNSView(context: Context) -> NSView { Styler() }
    func updateNSView(_ view: NSView, context: Context) {}

    private final class Styler: NSView {
        override func viewDidMoveToWindow() {
            apply()
            // SwiftUI sets the window's title as it goes: take it away again once it has
            for delay in [0.1, 0.5, 1.5] { DispatchQueue.main.asyncAfter(deadline: .now() + delay) { [weak self] in self?.apply() } }
        }

        private func apply() {
            guard let window else { return }
            window.titlebarAppearsTransparent = true
            window.titleVisibility = .hidden
            window.styleMask.insert(.fullSizeContentView)
            window.toolbar = nil
        }
    }
}
#endif
