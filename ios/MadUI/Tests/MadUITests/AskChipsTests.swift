import XCTest
@testable import MadUI

final class AskChipsTests: XCTestCase {
    override func setUp() {
        super.setUp()
        MadLocale.language = "zh-Hans"
    }

    /// Only an Allow, or the primary option that sends, is filled; Reply (it takes words) and the rest are not.
    func testOnlyAnAllowOrThePrimaryOptionIsFilled() {
        XCTAssertTrue(AskChips.filled(AskAct(id: "a", label: "允许", primary: true, kind: .allow)))
        XCTAssertTrue(AskChips.filled(AskAct(id: "y", label: "Yes", primary: true)))
        XCTAssertFalse(AskChips.filled(AskAct(id: "r", label: "Reply", primary: true, input: true)))
        XCTAssertFalse(AskChips.filled(AskAct(id: "d", label: "拒绝", kind: .deny)))
        XCTAssertFalse(AskChips.filled(AskAct(id: "o", label: "柱状图")))
    }

    /// An Allow and a Deny say what they are doing once sent; an option keeps its words.
    func testASentActSaysWhatItIsDoingAndAnOptionKeepsItsWords() {
        XCTAssertEqual(AskChips.words(AskAct(id: "a", label: "允许", kind: .allow), sending: true), "允许中…")
        XCTAssertEqual(AskChips.words(AskAct(id: "d", label: "拒绝", kind: .deny), sending: true), "拒绝中…")
        XCTAssertEqual(AskChips.words(AskAct(id: "o", label: "其实是 gemini-cli"), sending: true), "其实是 gemini-cli")
        XCTAssertEqual(AskChips.words(AskAct(id: "a", label: "允许", kind: .allow), sending: false), "允许")
    }

    func testAFormStartsIncomplete() {
        let form = AskForm(questions: [AskQuestion(id: "q1", text: "颜色", options: ["红", "蓝"]), AskQuestion(id: "q2", text: "尺寸", options: ["大", "小"])],
                           submit: { _ in })
        XCTAssertFalse(form.complete)
    }
}
