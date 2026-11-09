// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "MadUI",
    // The words are written in Chinese and translated to English in Localizable.xcstrings; the UI follows the
    // system's language.
    defaultLocalization: "zh-Hans",
    platforms: [.iOS(.v17), .macOS(.v14)],
    products: [.library(name: "MadUI", targets: ["MadUI"])],
    targets: [
        .target(name: "MadUI", resources: [.process("Resources")]),
        .testTarget(name: "MadUITests", dependencies: ["MadUI"]),
    ]
)
