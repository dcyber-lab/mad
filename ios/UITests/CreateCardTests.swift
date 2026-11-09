import XCTest

/// A bot's request to create an agent or a team (scenario `create-confirm`): the confirm card with its sending, change and
/// answered states, and mad's own lines after the answer. The fixture server moves a request out of the inbox when it is
/// answered (allow: creating, then created; deny: declined; deny with words: changes), so each class has a server of its own.
class CreateCardCase: MadUITestCase {
    /// The scenario's channel, where request c-00000001 (an agent called scout) was asked from.
    func openChannel() {
        launch(native: true, extra: ["-nativeOpen", "fold"])
        // the request from this channel is waiting: its card is how the channel says it is open
        XCTAssertTrue(card.waitForExistence(timeout: 40), "the channel did not open, or its confirm card did not show")
    }

    func openAgent() {
        launch(native: true, extra: ["-nativeOpen", "fold0001-aaaa"])
        XCTAssertTrue(element("对话").firstMatch.waitForExistence(timeout: 40), "the agent's conversation did not open")
    }

    var card: XCUIElement { app.descendants(matching: .any)["create.card"].firstMatch }
    func control(_ id: String) -> XCUIElement { app.descendants(matching: .any)[id].firstMatch }
}

class CreateAllowTests: CreateCardCase {
    func testWaitingThenAllowThenTheResultLine() {
        openChannel()
        XCTAssertTrue(card.waitForExistence(timeout: 15), "no confirm card in the channel the request came from")
        XCTAssertTrue(element("architect 想创建一个 agent").firstMatch.exists, "the head does not name the asker")
        XCTAssertTrue(element("scout").firstMatch.exists)
        XCTAssertFalse(elements(containing: "wants to").firstMatch.exists, "the server's English is on the card")
        XCTAssertFalse(elements(containing: "create:").firstMatch.exists, "an item key is on the card")
        shot("the agent card in its channel")
        let allow = control("create.allow")
        XCTAssertTrue(allow.exists)
        XCTAssertLessThanOrEqual(allow.frame.maxX, screenFrame.maxX)
        let before = allow.frame
        allow.tap()
        let allowing = app.buttons["允许中…"].firstMatch
        XCTAssertTrue(allowing.waitForExistence(timeout: 5), "the tapped 允许 does not say 允许中…")
        XCTAssertEqual(allowing.frame.height, before.height, accuracy: 1, "the pill changed height")
        XCTAssertEqual(allowing.frame.width, before.width, accuracy: 1, "the pill changed width: the card moves")
        shot("allowing")
        // the card leaves and mad's own line takes its place, a button that opens what was made
        let result = control("create.result")
        XCTAssertTrue(result.waitForExistence(timeout: 20), "no result line after 允许")
        XCTAssertFalse(card.exists, "the card is still there after the answer")
        XCTAssertTrue(element("「scout」建好了，打开").firstMatch.waitForExistence(timeout: 10), "the line does not say what was made")
        XCTAssertTrue(control("create.open").exists, "no 打开 pill")
        shot("the result line")
        // the home shows the made agent first in its project, and nothing waits on the request any more
        relaunch(native: true, extra: [])
        XCTAssertTrue(element("scout").firstMatch.waitForExistence(timeout: 40), "the made agent is not in the home list")
        shot("home after 允许")
    }
}

class CreateDenyTests: CreateCardCase {
    func testDenyLeavesAGreyLine() {
        openChannel()
        XCTAssertTrue(card.waitForExistence(timeout: 15))
        control("create.deny").tap()
        XCTAssertTrue(app.buttons["拒绝中…"].firstMatch.waitForExistence(timeout: 5) || control("create.note").waitForExistence(timeout: 10))
        let note = control("create.note")
        XCTAssertTrue(note.waitForExistence(timeout: 15), "no grey line after 拒绝")
        XCTAssertTrue(elements(containing: "你拒绝了，没有创建「scout」").firstMatch.exists)
        XCTAssertFalse(card.exists)
        shot("declined")
    }

    /// The asking agent has two requests; the first (scout) was declined above, so its conversation shows the team's card.
    func testTheAskingAgentsConversationShowsTheTeamCardNext() {
        openAgent()
        XCTAssertTrue(card.waitForExistence(timeout: 15), "no confirm card in the asking agent's conversation")
        XCTAssertTrue(element("想创建一个团队").firstMatch.exists, "the next card is not the team's")
        XCTAssertTrue(element("fix-flaky").firstMatch.exists)
        XCTAssertTrue(elements(containing: "负责人").firstMatch.exists, "the lead's row has no tag")
        shot("the team card in the conversation")
    }
}

class CreateChangeTests: CreateCardCase {
    func testChangeAsksForWordsThenSendsThem() {
        openChannel()
        XCTAssertTrue(card.waitForExistence(timeout: 15))
        control("create.change").tap()
        let field = control("create.field")
        XCTAssertTrue(field.waitForExistence(timeout: 5), "no field after 改一下…")
        XCTAssertTrue(control("create.cancel").exists)
        XCTAssertFalse(control("create.send").isEnabled, "发送 is on with nothing typed")
        field.tap()
        field.typeText("用 sonnet")
        XCTAssertTrue(control("create.send").isEnabled)
        shot("changing, words typed")
        control("create.send").tap()
        let note = control("create.note")
        XCTAssertTrue(note.waitForExistence(timeout: 15), "no grey line after 发送")
        XCTAssertTrue(elements(containing: "你要改一下：用 sonnet").firstMatch.exists)
        XCTAssertFalse(card.exists)
    }

    func testCancelGoesBackToWaiting() {
        openChannel()
        XCTAssertTrue(card.waitForExistence(timeout: 15))
        control("create.change").tap()
        XCTAssertTrue(control("create.field").waitForExistence(timeout: 5))
        control("create.cancel").tap()
        XCTAssertTrue(control("create.allow").waitForExistence(timeout: 5), "cancel did not go back to the card's pills")
    }
}

// The same cases in dark mode (a server of their own each).
final class CreateAllowDarkTests: CreateAllowTests {
    override func setUp() {
        super.setUp()
        XCUIDevice.shared.appearance = .dark
        addTeardownBlock { XCUIDevice.shared.appearance = .light }
    }
}

final class CreateDenyDarkTests: CreateDenyTests {
    override func setUp() {
        super.setUp()
        XCUIDevice.shared.appearance = .dark
        addTeardownBlock { XCUIDevice.shared.appearance = .light }
    }
}

final class CreateChangeDarkTests: CreateChangeTests {
    override func setUp() {
        super.setUp()
        XCUIDevice.shared.appearance = .dark
        addTeardownBlock { XCUIDevice.shared.appearance = .light }
    }
}
