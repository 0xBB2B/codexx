// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "ModelBridge",
    platforms: [.macOS(.v13)],
    targets: [
        .executableTarget(name: "ModelBridge", path: "Sources/ModelBridge"),
        .testTarget(name: "ModelBridgeTests", dependencies: ["ModelBridge"], path: "Tests/ModelBridgeTests"),
    ]
)
