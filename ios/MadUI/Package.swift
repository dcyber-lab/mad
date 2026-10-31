// swift-tools-version:5.9
import PackageDescription

// The SwiftUI views the iPhone and the Mac apps share. Other agents add to it.
let package = Package(
    name: "MadUI",
    platforms: [.iOS(.v17), .macOS(.v14)],
    products: [.library(name: "MadUI", targets: ["MadUI"])],
    dependencies: [.package(path: "../MadKit")],
    targets: [
        .target(name: "MadUI", dependencies: [.product(name: "MadKit", package: "MadKit")]),
        .testTarget(name: "MadUITests", dependencies: ["MadUI"]),
    ]
)
