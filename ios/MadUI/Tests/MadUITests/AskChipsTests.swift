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

    /// The sending words are looked up in MadUI's own catalog: each has an English entry there, or it would stay Chinese
    /// in English. SwiftPM does not compile the catalog for a test, so the file itself is read.
    /// Every word MadUI's create card and ask chips look up with L("…") has an English entry in MadUI's catalog (one that is
    /// missing reads in Chinese in English, as 发送 did).
    func testEveryWordOfTheCreateCardAndTheChipsHasAnEnglishEntry() throws {
        let sources = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .appendingPathComponent("Sources/MadUI")
        let catalog = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(contentsOf: sources.appendingPathComponent("Resources/Localizable.xcstrings"))) as? [String: Any])
        let strings = try XCTUnwrap(catalog["strings"] as? [String: [String: Any]])
        let looksUp = try NSRegularExpression(pattern: #"\bLf?\("([^"]+)""#)
        for name in ["CreateViews.swift", "AskChips.swift"] {
            let text = try String(contentsOf: sources.appendingPathComponent(name), encoding: .utf8)
            let found = looksUp.matches(in: text, range: NSRange(text.startIndex..., in: text)).compactMap { Range($0.range(at: 1), in: text).map { String(text[$0]) } }
            XCTAssertFalse(found.isEmpty, "\(name) looks up no words: the pattern is stale")
            for key in found {
                let en = ((strings[key]?["localizations"] as? [String: Any])?["en"] as? [String: Any])?["stringUnit"] as? [String: Any]
                XCTAssertNotNil(en?["value"], "“\(key)” (\(name)) has no English entry in MadUI's catalog")
            }
        }
    }

    func testTheSendingWordsHaveEnglishEntriesInMadUIsCatalog() throws {
        let file = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .appendingPathComponent("Sources/MadUI/Resources/Localizable.xcstrings")
        let catalog = try XCTUnwrap(JSONSerialization.jsonObject(with: Data(contentsOf: file)) as? [String: Any])
        let strings = try XCTUnwrap(catalog["strings"] as? [String: [String: Any]])
        for key in ["允许中…", "拒绝中…"] {
            let en = ((strings[key]?["localizations"] as? [String: Any])?["en"] as? [String: Any])?["stringUnit"] as? [String: Any]
            XCTAssertNotNil(en?["value"], "“\(key)” has no English entry in MadUI's catalog")
        }
    }
}
