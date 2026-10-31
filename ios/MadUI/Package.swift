// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "MadUI",
    platforms: [.iOS(.v17), .macOS(.v14)],
    products: [.library(name: "MadUI", targets: ["MadUI"])],
    targets: [
        .target(name: "MadUI"),
        .testTarget(name: "MadUITests", dependencies: ["MadUI"]),
    ]
)
