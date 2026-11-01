#if DEBUG && os(macOS)
import AppKit

/// Debug builds on the Mac, to take screenshots of the window from a script: `-windowSize 1200x800` sizes the
/// window once it is up, and `-windowInfo <path>` then writes its window number there, for
/// `screencapture -l <number>` (the app cannot capture itself without the permission the shell has).
enum DebugWindow {
    @MainActor static func runIfAsked() async {
        let args = ProcessInfo.processInfo.arguments
        func value(_ key: String) -> String? { args.firstIndex(of: key).flatMap { $0 + 1 < args.count ? args[$0 + 1] : nil } }
        guard value("-windowSize") != nil || value("-windowInfo") != nil else { return }
        // `-windowAppearance dark|light` (the system's setting is not taken from a launch argument)
        if let look = value("-windowAppearance") { NSApp.appearance = NSAppearance(named: look == "dark" ? .darkAqua : .aqua) }
        try? await Task.sleep(for: .seconds(2))
        guard let window = NSApp.windows.first(where: { $0.isVisible && $0.contentView != nil }) else { return }
        NSApp.activate(ignoringOtherApps: true)
        window.makeKeyAndOrderFront(nil)
        if window.styleMask.contains(.fullScreen) { window.toggleFullScreen(nil); try? await Task.sleep(for: .seconds(2)) }
        if let size = value("-windowSize")?.split(separator: "x").compactMap({ Double($0) }), size.count == 2 {
            // The window may be put back to another size as the content settles: set it until it holds.
            for _ in 0..<8 {
                let screen = window.screen?.visibleFrame ?? NSRect(x: 0, y: 0, width: 1512, height: 945)
                var frame = window.frameRect(forContentRect: NSRect(x: 0, y: 0, width: size[0], height: size[1]))
                frame.origin = NSPoint(x: screen.minX, y: screen.maxY - frame.height)
                window.setFrame(frame, display: true, animate: false)
                try? await Task.sleep(for: .seconds(1))
                if abs(window.frame.width - frame.width) < 1 { break }
            }
        }
        try? await Task.sleep(for: .seconds(1))
        if let want = value("-windowSize")?.split(separator: "x").compactMap({ Double($0) }).first, window.frame.width + 1 < want {
            // still not that wide (a size that SwiftUI refuses): zoom to the screen, which it takes
            window.zoom(nil)
            try? await Task.sleep(for: .seconds(1))
        }
        try? await Task.sleep(for: .seconds(3)) // the content draws at its new size
        if let path = value("-windowInfo") {
            try? (NSStringFromRect(window.frame) + " screens " + NSScreen.screens.map { NSStringFromRect($0.frame) + NSStringFromRect($0.visibleFrame) }.joined(separator: " | ") + " style \(window.styleMask.rawValue) full \(window.styleMask.contains(.fullScreen)) title \(window.titleVisibility.rawValue) transparent \(window.titlebarAppearsTransparent) separator \(window.titlebarSeparatorStyle.rawValue) toolbar \(window.toolbar == nil) layoutTop \(window.frame.height - window.contentLayoutRect.maxY)").write(toFile: path + ".frame", atomically: true, encoding: .utf8)
            try? String(window.windowNumber).write(toFile: path, atomically: true, encoding: .utf8)
        }
    }
}
#endif
