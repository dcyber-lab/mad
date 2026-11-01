import AVFoundation
import SwiftUI

/// A full-screen camera that reports the first QR code it reads, or nil on cancel.
struct ScannerScreen: View {
    let done: (String?) -> Void

    var body: some View {
        ZStack(alignment: .topTrailing) {
            ScannerView(found: { done($0) }).ignoresSafeArea()
            Button("取消") { done(nil) }
                .padding().background(.thinMaterial, in: Capsule()).padding()
        }
    }
}

struct ScannerView: UIViewControllerRepresentable {
    let found: (String) -> Void

    func makeUIViewController(context: Context) -> ScannerController {
        let c = ScannerController()
        c.found = found
        return c
    }

    func updateUIViewController(_ c: ScannerController, context: Context) {}
}

final class ScannerController: UIViewController, AVCaptureMetadataOutputObjectsDelegate {
    var found: ((String) -> Void)?
    private let session = AVCaptureSession()
    private var preview: AVCaptureVideoPreviewLayer?
    private var reported = false

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .black
        guard let device = AVCaptureDevice.default(for: .video),
              let input = try? AVCaptureDeviceInput(device: device),
              session.canAddInput(input) else { return showNote("No camera available. Paste the link instead.") }
        session.addInput(input)
        let output = AVCaptureMetadataOutput()
        guard session.canAddOutput(output) else { return }
        session.addOutput(output)
        output.setMetadataObjectsDelegate(self, queue: .main)
        output.metadataObjectTypes = [.qr]
        let layer = AVCaptureVideoPreviewLayer(session: session)
        layer.videoGravity = .resizeAspectFill
        view.layer.addSublayer(layer)
        preview = layer
    }

    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        preview?.frame = view.bounds
    }

    override func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated)
        let s = session
        DispatchQueue.global().async { if !s.isRunning { s.startRunning() } }
    }

    override func viewWillDisappear(_ animated: Bool) {
        super.viewWillDisappear(animated)
        let s = session
        DispatchQueue.global().async { if s.isRunning { s.stopRunning() } }
    }

    func metadataOutput(_ output: AVCaptureMetadataOutput, didOutput objects: [AVMetadataObject],
                        from connection: AVCaptureConnection) {
        guard !reported,
              let text = (objects.first as? AVMetadataMachineReadableCodeObject)?.stringValue else { return }
        reported = true
        found?(text)
    }

    private func showNote(_ text: String) {
        let l = UILabel()
        l.text = text
        l.textColor = .white
        l.numberOfLines = 0
        l.textAlignment = .center
        l.frame = view.bounds.insetBy(dx: 24, dy: 0)
        l.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(l)
    }
}
