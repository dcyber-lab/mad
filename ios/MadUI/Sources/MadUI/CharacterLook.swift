import SwiftUI

/// What an agent looks like: one of eight shapes, one of five faces and one of
/// eight colors, picked by a stable hash of its id. This is the web page's
/// `charLook` (internal/desktop/web/app.js), so an agent looks the same on the
/// page and in the apps. Keep the tables in step with it.
public struct CharacterLook: Equatable {
    public let shape: Int
    public let face: Int
    public let color: Int

    init(shape: Int, face: Int, color: Int) { self.shape = shape; self.face = face; self.color = color }

    /// The page's key: the id, else the kind, else "?".
    public init(id: String, kind: String = "") {
        self.init(key: id.isEmpty ? (kind.isEmpty ? "?" : kind) : id)
    }

    init(key: String) {
        let h = Self.hash(key)
        shape = Int(h % 8)
        face = Int((h >> 6) % 5)
        color = Int((h >> 3) % 8)
    }

    /// FNV-1a over the key's Unicode code points, 32 bits, as `charHash`.
    public static func hash(_ key: String) -> UInt32 {
        var h: UInt32 = 2166136261
        for c in key.unicodeScalars { h = (h ^ c.value) &* 16777619 }
        return h
    }

    public var shapeName: String { Self.shapes[shape].name }
    public var colorHex: String { Self.colorHexes[color] }

    // MARK: tables, from CHAR_SHAPES, CHAR_FACES and CHAR_COLORS

    struct Shape {
        let name: String
        let faceOffset: CGFloat
        let body: Path
        /// Leaves drawn behind the body, in fixed colors.
        let leaves: [(Path, Color)]

        init(_ name: String, _ faceOffset: CGFloat, _ body: Path, leaves: [(Path, Color)] = []) {
            self.name = name; self.faceOffset = faceOffset; self.body = body; self.leaves = leaves
        }
        init(_ name: String, _ faceOffset: CGFloat, _ d: String) { self.init(name, faceOffset, SVGPath.parse(d)) }
    }

    /// `<ellipse cx=24 cy=27 rx=20 ry=15 transform="rotate(-8 24 27)">`
    private static let bean = Path(ellipseIn: CGRect(x: 4, y: 12, width: 40, height: 30)).applying(
        CGAffineTransform(translationX: -24, y: -27)
            .concatenating(CGAffineTransform(rotationAngle: -8 * .pi / 180))
            .concatenating(CGAffineTransform(translationX: 24, y: 27)))

    static let shapes: [Shape] = [
        Shape("pebble", 0, Path(ellipseIn: CGRect(x: 6, y: 8, width: 36, height: 36))),
        Shape("toast", 0, "M8 20 a10 10 0 0 1 10 -12 h12 a10 10 0 0 1 10 12 v16 a6 6 0 0 1 -6 6 h-20 a6 6 0 0 1 -6 -6 z"),
        Shape("bean", 0, bean),
        Shape("drop", 4, "M24 5 C33 16 41 23 41 31 a17 17 0 0 1 -34 0 C7 23 15 16 24 5 z"),
        Shape("cat", 4, "M8 40 V16 L15 8 L20 14 h8 L33 8 L40 16 V40 a4 4 0 0 1 -4 4 h-24 a4 4 0 0 1 -4 -4 z"),
        Shape("hex", 0, "M24 6 l15 8.6 a4 4 0 0 1 2 3.5 v13.8 a4 4 0 0 1 -2 3.5 L24 44 l-15 -8.6 a4 4 0 0 1 -2 -3.5 V18.1 a4 4 0 0 1 2 -3.5 z"),
        Shape("puff", 0, "M12 40 a8 8 0 0 1 -3 -15 a10 10 0 0 1 14 -12 a10 10 0 0 1 16 6 a8 8 0 0 1 0 21 z"),
        Shape("sprout", 4, Path(roundedRect: CGRect(x: 9, y: 14, width: 30, height: 29), cornerSize: CGSize(width: 14.5, height: 14.5)),
              leaves: [(SVGPath.parse("M24 14 C22 8 16 6 12 8 C14 13 19 14 24 14 Z"), Color(hex: 0x59b85a)),
                       (SVGPath.parse("M24 14 C26 7 32 5 36 7 C34 13 29 14 24 14 Z"), Color(hex: 0x7fd36b))]),
    ]

    static let colorHexes = ["#6f9bff", "#ffb36b", "#3dcf9c", "#b48cff", "#ff8fb3", "#ffd34e", "#5fd0e8", "#9be07f"]

    /// A face's marks. `eye` marks blink; `stroke` ones are drawn as lines, the rest filled.
    struct Mark {
        enum Kind { case eyeDot, eyeArc, mouth, mouthDot }
        let kind: Kind
        let path: Path
    }

    static func dot(_ x: CGFloat, _ y: CGFloat, _ r: CGFloat) -> Path {
        Path(ellipseIn: CGRect(x: x - r, y: y - r, width: 2 * r, height: 2 * r))
    }

    static let faces: [[Mark]] = [
        [Mark(kind: .eyeDot, path: dot(19, 23, 2.6)), Mark(kind: .eyeDot, path: dot(29, 23, 2.6)),
         Mark(kind: .mouth, path: SVGPath.parse("M21 29 q3 2.6 6 0"))],
        [Mark(kind: .eyeDot, path: dot(19, 23, 2.6)), Mark(kind: .eyeDot, path: dot(29, 23, 2.6)),
         Mark(kind: .mouthDot, path: dot(24, 30, 2))],
        [Mark(kind: .eyeArc, path: SVGPath.parse("M16.5 23 q2.5 -2.6 5 0")), Mark(kind: .eyeArc, path: SVGPath.parse("M26.5 23 q2.5 -2.6 5 0")),
         Mark(kind: .mouth, path: SVGPath.parse("M22 29 h4"))],
        [Mark(kind: .eyeDot, path: dot(19, 23, 2.6)), Mark(kind: .eyeDot, path: dot(29, 23, 2.6)),
         Mark(kind: .mouth, path: SVGPath.parse("M20 28.5 q2 2 4 0 q2 2 4 0"))],
        [Mark(kind: .eyeDot, path: dot(19, 23, 2.6)), Mark(kind: .mouth, path: SVGPath.parse("M26.5 23.5 h5")),
         Mark(kind: .mouth, path: SVGPath.parse("M21 28.5 q3 3 6 0"))],
    ]
}

extension Color {
    init(hex: UInt32) {
        self.init(.sRGB, red: Double((hex >> 16) & 255) / 255, green: Double((hex >> 8) & 255) / 255,
                  blue: Double(hex & 255) / 255, opacity: 1)
    }
}
