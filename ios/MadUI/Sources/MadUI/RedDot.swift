import SwiftUI

/// The red dot that marks something waiting on the person. It sits at a
/// character's corner as a separate element, so it is read on its own.
public struct RedDot: View {
    public var diameter: CGFloat
    public var ring: Color

    /// ring is the color of what the dot sits on, so its border looks cut out of it.
    public init(diameter: CGFloat = 11, ring: Color = RedDot.defaultRing) {
        self.diameter = diameter
        self.ring = ring
    }

    public static var defaultRing: Color {
        #if os(iOS)
        Color(.systemBackground)
        #else
        Color(nsColor: .windowBackgroundColor)
        #endif
    }

    public var body: some View {
        Circle()
            .fill(Color(hex: 0xff3b30))
            .overlay(Circle().strokeBorder(ring, lineWidth: diameter * 2.5 / 11))
            .frame(width: diameter, height: diameter)
            .accessibilityElement()
            .accessibilityLabel("needs you")
    }
}

extension View {
    /// The dot at the top trailing corner, when shown.
    func cornerDot(_ shown: Bool, size: CGFloat) -> some View {
        overlay(alignment: .topTrailing) {
            if shown { RedDot(diameter: max(9, min(14, size * 0.275))) }
        }
    }
}
