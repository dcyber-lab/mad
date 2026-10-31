import SwiftUI

/// What an agent looks like: one of eight shapes in one of eight colors, picked by a stable hash
/// of its id (the web page's `charLook`, internal/desktop/web/app.js), so an agent looks the same on
/// the page and in the apps. The shapes' drawing and the colors are the approved reference's.
/// Its face is not part of the look: that changes with what it is doing (`Expression`).
public struct CharacterLook: Equatable {
    public let shape: Int
    public let color: Int
    /// What its motion is drawn from: the page's key (the id, else the kind, else "?").
    let key: String
    let plan: CharacterPlan

    init(shape: Int, color: Int, key: String? = nil) {
        self.shape = shape
        self.color = color
        self.key = key ?? "shape\(shape)color\(color)"
        plan = CharacterPlan(key: self.key)
    }

    /// The page's key: the id, else the kind, else "?".
    public init(id: String, kind: String = "") {
        self.init(key: id.isEmpty ? (kind.isEmpty ? "?" : kind) : id)
    }

    init(key: String) {
        let h = Self.hash(key)
        self.init(shape: Int(h % 8), color: Int((h >> 3) % 8), key: key)
    }

    /// FNV-1a over the key's Unicode code points, 32 bits, as `charHash`.
    public static func hash(_ key: String) -> UInt32 {
        var h: UInt32 = 2166136261
        for c in key.unicodeScalars { h = (h ^ c.value) &* 16777619 }
        return h
    }

    public var shapeName: String { Self.shapes[shape].name }
    /// The body's pastel.
    public var colorHex: String { Self.colorHexes[color] }
    /// The strong color of the same hue, darkened 55%, that the face is drawn in.
    var faceColor: Color { Color(hex: Self.mix(Self.strongHexes[color], 0.55)) }

    // MARK: tables, from SHAPES, COLORS and STRONG of the reference

    struct Shape {
        let name: String
        /// Where the face sits, below the body's middle by this much.
        let faceOffset: CGFloat
        let body: Path
        /// Leaves drawn behind the body, in fixed colors; they sway.
        let leaves: [(Path, Color)]
        /// What the head turns about: the middle of the bottom of the head's bounds (body and leaves).
        let pivot: CGPoint
        /// What the leaves turn about, the same of their bounds.
        let leafPivot: CGPoint

        init(_ name: String, _ faceOffset: CGFloat, _ body: Path, leaves: [(Path, Color)] = []) {
            self.name = name; self.faceOffset = faceOffset; self.body = body; self.leaves = leaves
            func foot(_ r: CGRect) -> CGPoint { CGPoint(x: r.midX, y: r.maxY) }
            var head = body
            var leafPath = Path()
            for (leaf, _) in leaves { head.addPath(leaf); leafPath.addPath(leaf) }
            pivot = foot(head.cgPath.boundingBoxOfPath)
            leafPivot = leaves.isEmpty ? .zero : foot(leafPath.cgPath.boundingBoxOfPath)
        }
        init(_ name: String, _ faceOffset: CGFloat, _ d: String) { self.init(name, faceOffset, SVGPath.parse(d)) }
    }

    /// `<ellipse cx=24 cy=27 rx=19 ry=14 transform="rotate(-8 24 27)">`
    private static let bean = Path(ellipseIn: CGRect(x: 5, y: 13, width: 38, height: 28)).applying(
        CGAffineTransform(translationX: -24, y: -27)
            .concatenating(CGAffineTransform(rotationAngle: -8 * .pi / 180))
            .concatenating(CGAffineTransform(translationX: 24, y: 27)))

    static let shapes: [Shape] = [
        Shape("pebble", 0, Path(ellipseIn: CGRect(x: 7, y: 9, width: 34, height: 34))),
        Shape("toast", 0, "M9 20 a9.5 9.5 0 0 1 9.5 -11 h11 a9.5 9.5 0 0 1 9.5 11 v15.5 a6 6 0 0 1 -6 6 h-18 a6 6 0 0 1 -6 -6 z"),
        Shape("bean", 0, bean),
        Shape("drop", 4, "M24 6 C33 16 40 23 40 31 a16 16 0 0 1 -32 0 C8 23 15 16 24 6 z"),
        Shape("cat", 4, "M9 40 V17 L15 9 L20 14.5 h8 L33 9 L39 17 V40 a3.5 3.5 0 0 1 -3.5 3.5 h-23 a3.5 3.5 0 0 1 -3.5 -3.5 z"),
        Shape("hex", 0, "M24 7 l14 8 a3.5 3.5 0 0 1 1.8 3 v13.6 a3.5 3.5 0 0 1 -1.8 3 L24 43 l-14 -8 a3.5 3.5 0 0 1 -1.8 -3 V18 a3.5 3.5 0 0 1 1.8 -3 z"),
        Shape("puff", 0, "M12.5 40 a7.5 7.5 0 0 1 -3 -14.2 a9.6 9.6 0 0 1 13.6 -11.5 a9.6 9.6 0 0 1 15.4 5.8 a7.6 7.6 0 0 1 0 19.9 z"),
        Shape("sprout", 4, Path(roundedRect: CGRect(x: 10, y: 15, width: 28, height: 28), cornerSize: CGSize(width: 14, height: 14)),
              leaves: [(SVGPath.parse("M24 15 C22.5 9 17 7 12.5 8.5 C14 13.5 19 15 24 15 Z"), Color(hex: 0xbfe8b0)),
                       (SVGPath.parse("M24 15 C25.5 8 31 6 35.5 7.5 C34 13.5 29 15 24 15 Z"), Color(hex: 0xd6f2c9))]),
    ]

    static let colorHexes = ["#cfe0ff", "#ffe1c2", "#c6f0df", "#e6d9ff", "#ffd6e4", "#fff0b8", "#cdeff7", "#dcf4cf"]
    static let strongHexes = ["#6f9bff", "#ffa04d", "#2fbf8c", "#a77bff", "#ff7aa5", "#f2bd1d", "#3fc3e0", "#7dcc5a"]

    /// The reference's `mix(hex, t)`: each channel scaled by 1 - t (toward black), as a number 0xRRGGBB.
    static func mix(_ hex: String, _ t: Double) -> UInt32 {
        let n = UInt32(hex.dropFirst(), radix: 16)!
        func ch(_ shift: UInt32) -> UInt32 { UInt32((Double((n >> shift) & 255) * (1 - t)).rounded()) }
        return ch(16) << 16 | ch(8) << 8 | ch(0)
    }
}

extension Color {
    init(hex: UInt32) {
        self.init(.sRGB, red: Double((hex >> 16) & 255) / 255, green: Double((hex >> 8) & 255) / 255,
                  blue: Double(hex & 255) / 255, opacity: 1)
    }
}
