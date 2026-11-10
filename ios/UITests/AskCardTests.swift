import UIKit
import XCTest

/// The native iPhone ask card against edge-home: a question (ask0001-dddd), a command (need0001-bbbb) and a TUI permission
/// (keys0001-eeee). The head says what is asked in words, the options are compact wrapping chips, and the tapped chip keeps
/// its words while the card does not change size.
final class AskCardTests: MadUITestCase {
    private func open(_ agent: String, dark: Bool = false) {
        if dark { useDark() }
        launch(native: true, extra: ["-nativeOpen", agent])
        XCTAssertTrue(element("对话").firstMatch.waitForExistence(timeout: 40), "the agent's conversation did not open")
    }

    /// The whole device in dark mode, put back when the case ends (the launch arguments do not turn a SwiftUI window dark).
    private func useDark() {
        XCUIDevice.shared.appearance = .dark
        addTeardownBlock { XCUIDevice.shared.appearance = .light }
        Thread.sleep(forTimeInterval: 2)
    }

    private func chips(_ labels: [String]) -> [XCUIElement] {
        labels.map { l in app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", l)).firstMatch }
    }

    /// The lightest and the darkest the chip's own fill is, over a second of its pulse (a point inside its left edge, past the
    /// corner): in light mode a chip greyed as disabled never gets near the card's ink, a filled one does at the top of its pulse.
    private func fillRange(of chip: XCUIElement) -> (darkest: CGFloat, lightest: CGFloat) {
        var lo: CGFloat = 1, hi: CGFloat = 0
        for _ in 0..<10 {
            if let cg = chip.screenshot().image.cgImage, let data = cg.dataProvider?.data, let bytes = CFDataGetBytePtr(data) {
                let x = Int(CGFloat(cg.width) * 0.06), y = cg.height / 2
                let o = y * cg.bytesPerRow + x * (cg.bitsPerPixel / 8)
                let l = (0.2126 * CGFloat(bytes[o]) + 0.7152 * CGFloat(bytes[o + 1]) + 0.0722 * CGFloat(bytes[o + 2])) / 255
                lo = min(lo, l); hi = max(hi, l)
            }
            Thread.sleep(forTimeInterval: 0.13)
        }
        return (lo, hi)
    }

    func testAQuestionIsAskedInWordsWithCompactChips() {
        open("ask0001-dddd")
        XCTAssertTrue(element("想问你").firstMatch.waitForExistence(timeout: 10), "the card's head is not 想问你")
        XCTAssertFalse(elements(containing: "AskUserQuestion").firstMatch.exists, "a code name is on the card")
        let options = chips(["agentapi", "其实是 gemini-cli", "回复"])
        for o in options {
            XCTAssertTrue(o.waitForExistence(timeout: 5), "a chip is missing")
            XCTAssertLessThanOrEqual(o.frame.height, 60, "a chip is a tall pill: \(o.frame)")
            XCTAssertLessThanOrEqual(o.frame.maxX, screenFrame.maxX, "a chip runs past the screen")
        }
        shot("the question card")
        // the tapped chip keeps its words and the card does not change size
        let card = element("想问你").firstMatch
        let before = options[1].frame
        options[1].tap()
        Thread.sleep(forTimeInterval: 0.6)
        XCTAssertTrue(options[1].exists, "the tapped chip lost its words")
        XCTAssertEqual(options[1].frame.size.height, before.size.height, accuracy: 1, "the tapped chip changed height")
        XCTAssertTrue(card.exists)
        shot("the question card, an option sent")
        XCTAssertLessThan(fillRange(of: options[1]).darkest, 0.3, "the tapped option lost its fill (greyed as disabled?)")
    }

    func testACommandIsAskedInWordsAndOnlyAllowIsFilled() {
        open("need0001-bbbb")
        XCTAssertTrue(element("想运行一条命令").firstMatch.waitForExistence(timeout: 10), "the card's head is not 想运行一条命令")
        let allow = app.buttons["允许"].firstMatch
        XCTAssertTrue(allow.waitForExistence(timeout: 5), "no 允许 chip")
        let before = allow.frame
        XCTAssertLessThanOrEqual(before.height, 60)
        shot("the command card")
        allow.tap()
        let allowing = app.buttons["允许中…"].firstMatch
        XCTAssertTrue(allowing.waitForExistence(timeout: 5), "the tapped Allow does not say 允许中…")
        XCTAssertEqual(allowing.frame.height, before.height, accuracy: 1, "the chip changed height")
        XCTAssertEqual(allowing.frame.width, before.width, accuracy: 1, "the chip changed width: the card moves")
        shot("the command card, allowing")
        XCTAssertLessThan(fillRange(of: allowing).darkest, 0.3, "the tapped Allow lost its fill (greyed as disabled?)")
    }

    func testATerminalPermissionIsAskedInWords() {
        open("keys0001-eeee")
        XCTAssertTrue(element("想运行一条命令").firstMatch.waitForExistence(timeout: 10), "the card's head is not 想运行一条命令")
        XCTAssertFalse(app.staticTexts["Bash"].firstMatch.exists, "a code name is a title")
        for c in chips(["Yes"]) { XCTAssertLessThanOrEqual(c.frame.height, 90, "a chip is a tall pill: \(c.frame)") }
        shot("the terminal permission card")
    }

    func testTheCardsInDarkMode() {
        for (agent, head) in [("ask0001-dddd", "想问你"), ("need0001-bbbb", "想运行一条命令"), ("keys0001-eeee", "想运行一条命令")] {
            app?.terminate()
            open(agent, dark: true)
            XCTAssertTrue(element(head).firstMatch.waitForExistence(timeout: 10))
            shot("the \(agent) card, dark")
        }
    }
}
