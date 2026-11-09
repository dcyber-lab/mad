import XCTest

/// The native iPhone ask card against edge-home: a question (ask0001-dddd), a command (need0001-bbbb) and a TUI permission
/// (keys0001-eeee). The head says what is asked in words, the options are compact wrapping chips, and the tapped chip keeps
/// its words while the card does not change size.
final class AskCardTests: MadUITestCase {
    private func open(_ agent: String) {
        launch(native: true, extra: ["-nativeOpen", agent])
        XCTAssertTrue(element("对话").firstMatch.waitForExistence(timeout: 40), "the agent's conversation did not open")
    }

    private func chips(_ labels: [String]) -> [XCUIElement] {
        labels.map { l in app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", l)).firstMatch }
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
    }

    func testATerminalPermissionIsAskedInWords() {
        open("keys0001-eeee")
        XCTAssertTrue(element("想运行一条命令").firstMatch.waitForExistence(timeout: 10), "the card's head is not 想运行一条命令")
        XCTAssertFalse(app.staticTexts["Bash"].firstMatch.exists, "a code name is a title")
        for c in chips(["Yes"]) { XCTAssertLessThanOrEqual(c.frame.height, 90, "a chip is a tall pill: \(c.frame)") }
        shot("the terminal permission card")
    }

    func testTheCardsInDarkMode() {
        XCUIDevice.shared.appearance = .dark
        addTeardownBlock { XCUIDevice.shared.appearance = .light }
        for (agent, head) in [("ask0001-dddd", "想问你"), ("need0001-bbbb", "想运行一条命令"), ("keys0001-eeee", "想运行一条命令")] {
            app?.terminate()
            open(agent)
            XCTAssertTrue(element(head).firstMatch.waitForExistence(timeout: 10))
            shot("the \(agent) card, dark")
        }
    }
}
