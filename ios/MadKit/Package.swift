// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "MadKit",
    platforms: [.iOS(.v17), .macOS(.v14)],
    products: [.library(name: "MadKit", targets: ["MadKit"])],
    targets: [
        .target(name: "MadKit"),
        .testTarget(name: "MadKitTests", dependencies: ["MadKit"]),
    ]
)
