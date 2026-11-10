#if os(macOS)
import AppKit
import SwiftUI
import XCTest
@testable import MadUI

/// A Return typed in the composer must never fire a card's hidden ⏎ Allow: the cards leave their shortcuts out while a text
/// has the keyboard (see MacTextFocus). Here the part that decides it, with a real window and a real first responder.
@MainActor final class MacTextFocusTests: XCTestCase {
    private final class Box { var value = false }

    func testOnlyATextIsATextFocus() {
        XCTAssertTrue(MacTextFocus.isText(NSTextView()))
        XCTAssertFalse(MacTextFocus.isText(NSView()))
        XCTAssertFalse(MacTextFocus.isText(nil))
    }

    /// The watcher follows the window's first responder: true while a text view has it, false when it is let go.
    func testTheWatcherFollowsTheFirstResponder() throws {
        let box = Box()
        let probe = Color.clear.watchesTextFocus(Binding(get: { box.value }, set: { box.value = $0 }))
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 300, height: 200), styleMask: [.titled], backing: .buffered, defer: false)
        let host = NSHostingView(rootView: probe)
        window.contentView = host
        let text = NSTextView(frame: NSRect(x: 0, y: 0, width: 100, height: 40))
        host.addSubview(text)
        func settle() { RunLoop.current.run(until: Date().addingTimeInterval(0.2)) }
        settle()
        XCTAssertFalse(box.value, "a text focus with nothing focused")
        XCTAssertTrue(window.makeFirstResponder(text))
        settle()
        XCTAssertTrue(box.value, "the watcher missed a text view taking the keyboard")
        XCTAssertTrue(window.makeFirstResponder(nil))
        settle()
        XCTAssertFalse(box.value, "the watcher still says a text has the keyboard")
    }
}
#endif
