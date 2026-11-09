// swift-tools-version:5.9
import PackageDescription

// The native shell both apps run: the stores of MadKit wired to the views of MadUI. MadUI takes plain
// view-model structs and knows nothing of MadKit, so the mapping from the server's models to them
// lives here, and so do the views that read a store. Its words, and the apps' own, are in its
// Localizable.xcstrings (Chinese, translated to English).
let package = Package(
    name: "MadShell",
    defaultLocalization: "zh-Hans",
    platforms: [.iOS(.v17), .macOS(.v14)],
    products: [.library(name: "MadShell", targets: ["MadShell"])],
    dependencies: [.package(path: "../MadKit"), .package(path: "../MadUI")],
    targets: [
        .target(name: "MadShell", dependencies: [
            .product(name: "MadKit", package: "MadKit"),
            .product(name: "MadUI", package: "MadUI"),
        ], resources: [.process("Resources")]),
        .testTarget(name: "MadShellTests", dependencies: ["MadShell"], resources: [.copy("Fixtures")]),
    ]
)
