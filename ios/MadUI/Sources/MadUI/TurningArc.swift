import QuartzCore
import SwiftUI

/// The green arc of a working agent's ring, turned by Core Animation. The render server turns the layer, so the
/// app does no work per frame. A SwiftUI `repeatForever` rotation made the app lay out and draw every frame,
/// which cost several percent of a core on a home screen with a few working agents.
struct TurningArc: View {
    let size: CGFloat
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        ArcLayerView(size: size, still: reduceMotion).frame(width: size, height: size)
    }
}

/// Seconds for one full turn.
private let turn: CFTimeInterval = 1.4
private let spinKey = "mad.spin"

/// Builds and updates the arc layer. The arc is the design's stroke-dasharray 80 222 of a circle of r 48:
/// 80 / 301.6 of the way round, inset 2 and 5 wide in a box of 100.
@MainActor
private enum ArcLayer {
    static func make() -> CAShapeLayer {
        let l = CAShapeLayer()
        l.fillColor = nil
        l.lineCap = .round
        l.strokeStart = 0
        l.strokeEnd = 0.265
        return l
    }

    static func update(_ l: CAShapeLayer, in bounds: CGRect, size: CGFloat, color: CGColor, still: Bool) {
        let u = size / 100
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        l.bounds = bounds
        l.position = CGPoint(x: bounds.midX, y: bounds.midY)
        l.path = CGPath(ellipseIn: bounds.insetBy(dx: 2 * u, dy: 2 * u), transform: nil)
        l.lineWidth = 5 * u
        l.strokeColor = color
        CATransaction.commit()
        if still {
            l.removeAnimation(forKey: spinKey)
        } else if l.animation(forKey: spinKey) == nil {
            let a = CABasicAnimation(keyPath: "transform.rotation.z")
            a.fromValue = 0
            a.toValue = 2 * Double.pi
            a.duration = turn
            a.repeatCount = .infinity
            // kept when the view leaves the window or the app goes to the background, so it turns on return
            a.isRemovedOnCompletion = false
            l.add(a, forKey: spinKey)
        }
    }
}

#if canImport(UIKit)
import UIKit

struct ArcLayerView: UIViewRepresentable {
    let size: CGFloat
    let still: Bool

    func makeUIView(context: Context) -> ArcHost { ArcHost() }

    func updateUIView(_ v: ArcHost, context: Context) {
        v.size = size
        v.still = still
        v.color = Color.madWork.resolve(in: context.environment).cgColor
        v.setNeedsLayout()
    }

    final class ArcHost: UIView {
        let arc = ArcLayer.make()
        var size: CGFloat = 0
        var still = false
        var color: CGColor = CGColor(gray: 0, alpha: 0)

        override init(frame: CGRect) {
            super.init(frame: frame)
            isUserInteractionEnabled = false
            backgroundColor = .clear
            layer.addSublayer(arc)
        }

        required init?(coder: NSCoder) { fatalError("not from a nib") }

        override func layoutSubviews() {
            super.layoutSubviews()
            ArcLayer.update(arc, in: bounds, size: size, color: color, still: still)
        }
    }
}
#else
import AppKit

struct ArcLayerView: NSViewRepresentable {
    let size: CGFloat
    let still: Bool

    func makeNSView(context: Context) -> ArcHost { ArcHost() }

    func updateNSView(_ v: ArcHost, context: Context) {
        v.size = size
        v.still = still
        v.color = Color.madWork.resolve(in: context.environment).cgColor
        v.needsLayout = true
    }

    final class ArcHost: NSView {
        let arc = ArcLayer.make()
        var size: CGFloat = 0
        var still = false
        var color: CGColor = CGColor(gray: 0, alpha: 0)

        // flipped like UIKit, so a positive turn is clockwise on both
        override var isFlipped: Bool { true }

        override init(frame: NSRect) {
            super.init(frame: frame)
            wantsLayer = true
            layer?.addSublayer(arc)
        }

        required init?(coder: NSCoder) { fatalError("not from a nib") }

        override func hitTest(_ point: NSPoint) -> NSView? { nil }

        override func layout() {
            super.layout()
            ArcLayer.update(arc, in: bounds, size: size, color: color, still: still)
        }

    }
}
#endif
