import XCTest
@testable import ModelBridge

private final class EventBox<T: Sendable>: @unchecked Sendable {
    private let lock = NSLock()
    private var items: [T] = []
    func append(_ item: T) { lock.lock(); items.append(item); lock.unlock() }
    var all: [T] { lock.lock(); defer { lock.unlock() }; return items }
}

private func waitUntil(timeout: TimeInterval = 10, _ condition: () -> Bool) async -> Bool {
    let deadline = Date().addingTimeInterval(timeout)
    while Date() < deadline {
        if condition() { return true }
        try? await Task.sleep(nanoseconds: 20_000_000)
    }
    return condition()
}

private let closedStdinCoreScript = """
#!/bin/sh
read -r line
id=$(echo "$line" | sed -E 's/.*"id":([0-9]+).*/\\1/')
echo "{\\"id\\":$id,\\"result\\":{}}"
exec 0<&-
exec sleep 30
"""

private let fakeCoreScript = """
#!/bin/sh
dir="$2"
echo "$@" >> "$dir/argv.txt"
env >> "$dir/env.txt"
held=""
while IFS= read -r line; do
  echo "$line" >> "$dir/requests.log"
  id=$(echo "$line" | sed -E 's/.*"id":([0-9]+).*/\\1/')
  case "$line" in
    *'"method":"init"'*)
      if [ -f "$dir/failinit" ]; then
        echo "{\\"id\\":$id,\\"error\\":{\\"code\\":\\"bad_params\\"}}"
      else
        echo "{\\"id\\":$id,\\"result\\":{}}"
      fi ;;
    *'"method":"emit_nel"'*)
      printf '{"event":"status","data":{"version":"1.\\302\\2052.0","forwarding":true}}\\n'
      echo "{\\"id\\":$id,\\"result\\":{}}" ;;
    *'"method":"emit_status"'*)
      echo '{"event":"status","data":{"version":"1.2.0","forwarding":true}}'
      echo "{\\"id\\":$id,\\"result\\":{}}" ;;
    *'"method":"crash"'*) exit 3 ;;
    *'"method":"fail"'*)
      echo "{\\"id\\":$id,\\"error\\":{\\"code\\":\\"config_invalid\\",\\"keys\\":[\\"a\\",\\"b\\"]}}" ;;
    *'"method":"hold"'*) held="$id" ;;
    *'"method":"release"'*)
      echo "{\\"id\\":$id,\\"result\\":{\\"who\\":\\"release\\"}}"
      echo "{\\"id\\":$held,\\"result\\":{\\"who\\":\\"hold\\"}}" ;;
    *) echo "{\\"id\\":$id,\\"result\\":{\\"echo\\":$id}}" ;;
  esac
done
"""

final class CoreProcessTests: XCTestCase {
    private var dir: URL!
    private let apiKey = "sk-example-0000"

    override func setUpWithError() throws {
        dir = FileManager.default.temporaryDirectory.appendingPathComponent("core-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let script = dir.appendingPathComponent("fake-core")
        try fakeCoreScript.write(to: script, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: script.path)
        let closed = dir.appendingPathComponent("closed-stdin-core")
        try closedStdinCoreScript.write(to: closed, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: closed.path)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: dir)
    }

    private func makeCore(
        restartDelay: TimeInterval = 0.2,
        stableResetInterval: TimeInterval = 60,
        script: String = "fake-core"
    ) -> CoreProcess {
        let key = apiKey
        return CoreProcess(
            executableURL: dir.appendingPathComponent(script),
            arguments: CoreProcess.arguments(dataDir: dir),
            restartDelay: restartDelay,
            stableResetInterval: stableResetInterval,
            initParams: { CoreInitParams(baseURL: "https://relay.example.com", apiKey: key, chatgptDir: nil) }
        )
    }

    private func logLines(_ name: String) -> [String] {
        let text = (try? String(contentsOf: dir.appendingPathComponent(name), encoding: .utf8)) ?? ""
        return text.split(separator: "\n").map(String.init)
    }

    private func initCount() -> Int {
        logLines("requests.log").filter { $0.contains("\"method\":\"init\"") }.count
    }

    private func json(_ data: Data) throws -> [String: Any] {
        try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
    }

    func testArguments_DataDirFlagFollowedByPath() {
        XCTAssertEqual(CoreProcess.arguments(dataDir: URL(fileURLWithPath: "/tmp/x")), ["--data-dir", "/tmp/x"])
    }

    func testExecutableURL_InsideAppBundle() {
        let app = URL(fileURLWithPath: "/Applications/ModelBridge.app")
        XCTAssertEqual(
            CoreProcess.executableURL(appBundleURL: app).path,
            "/Applications/ModelBridge.app/Contents/MacOS/model-bridge-core"
        )
    }

    func testDefaultDataDirectory_IsApplicationSupportModelBridge() {
        XCTAssertTrue(CoreProcess.defaultDataDirectory.path.hasSuffix("Library/Application Support/ModelBridge"))
    }

    func testInitParams_EncodedWithSnakeCaseKeysAndNulls() throws {
        let data = try JSONEncoder().encode(CoreInitParams(baseURL: nil, apiKey: nil, chatgptDir: nil))
        let obj = try json(data)
        XCTAssertEqual(Set(obj.keys), ["base_url", "api_key", "chatgpt_dir"])
        XCTAssertTrue(obj["api_key"] is NSNull)
    }

    func testStart_SendsInitOverStdinWithParams() async throws {
        let core = makeCore()
        try await core.start()
        defer { Task { await core.stop() } }
        let first = try XCTUnwrap(logLines("requests.log").first)
        XCTAssertTrue(first.contains("\"method\":\"init\""))
        XCTAssertTrue(first.contains("relay.example.com"))
        XCTAssertTrue(first.contains(apiKey))
    }

    func testCall_ReturnsResultOfMatchingId() async throws {
        let core = makeCore()
        try await core.start()
        defer { Task { await core.stop() } }
        let result = try json(try await core.call("ping", params: [String: String]()))
        XCTAssertNotNil(result["echo"])
    }

    func testCall_OutOfOrderResponsesMatchedById() async throws {
        let core = makeCore()
        try await core.start()
        defer { Task { await core.stop() } }
        async let held = core.call("hold", params: [String: String]())
        try await Task.sleep(nanoseconds: 200_000_000)
        async let released = core.call("release", params: [String: String]())
        let (h, r) = try await (held, released)
        XCTAssertEqual(try json(h)["who"] as? String, "hold")
        XCTAssertEqual(try json(r)["who"] as? String, "release")
    }

    func testCall_ErrorResponseThrowsRemoteErrorWithCodeAndKeys() async throws {
        let core = makeCore()
        try await core.start()
        defer { Task { await core.stop() } }
        do {
            _ = try await core.call("fail", params: [String: String]())
            XCTFail("应当抛错")
        } catch {
            XCTAssertEqual(error as? CoreError, .remote(code: "config_invalid", keys: ["a", "b"]))
        }
    }

    func testStatusEvent_DeliveredToSubscriber() async throws {
        let core = makeCore()
        try await core.start()
        defer { Task { await core.stop() } }
        let box = EventBox<CoreStatus>()
        let stream = core.statusEvents()
        let task = Task { for await s in stream { box.append(s) } }
        defer { task.cancel() }
        _ = try await core.call("emit_status", params: [String: String]())
        let got = await waitUntil { !box.all.isEmpty }
        XCTAssertTrue(got)
        XCTAssertEqual(box.all.first?.version, "1.2.0")
        XCTAssertEqual(box.all.first?.forwarding, true)
    }

    func testCrash_PendingCallFailsWithProcessExited() async throws {
        let core = makeCore()
        try await core.start()
        defer { Task { await core.stop() } }
        do {
            _ = try await core.call("crash", params: [String: String]())
            XCTFail("应当抛错")
        } catch {
            XCTAssertEqual(error as? CoreError, .processExited)
        }
    }

    func testCrash_RestartsOnceAfterDelayAndResendsInit() async throws {
        let delay: TimeInterval = 0.5
        let core = makeCore(restartDelay: delay)
        try await core.start()
        defer { Task { await core.stop() } }
        let events = EventBox<CoreLifecycleEvent>()
        let stream = core.lifecycleEvents()
        let task = Task { for await e in stream { events.append(e) } }
        defer { task.cancel() }

        let crashedAt = Date()
        _ = try? await core.call("crash", params: [String: String]())
        XCTAssertEqual(logLines("argv.txt").count, 1, "等待期内不应重启")

        let restarted = await waitUntil { initCount() == 2 }
        XCTAssertTrue(restarted)
        XCTAssertGreaterThanOrEqual(Date().timeIntervalSince(crashedAt), delay - 0.05)
        let gotEvent = await waitUntil { events.all.contains(.restarted) }
        XCTAssertTrue(gotEvent)
        let result = try json(try await core.call("ping", params: [String: String]()))
        XCTAssertNotNil(result["echo"])
    }

    func testCrash_SecondFailureReportsFailedAndNoMoreRetries() async throws {
        let delay: TimeInterval = 0.2
        let core = makeCore(restartDelay: delay)
        try await core.start()
        defer { Task { await core.stop() } }
        let events = EventBox<CoreLifecycleEvent>()
        let stream = core.lifecycleEvents()
        let task = Task { for await e in stream { events.append(e) } }
        defer { task.cancel() }

        _ = try? await core.call("crash", params: [String: String]())
        let restarted = await waitUntil { initCount() == 2 }
        XCTAssertTrue(restarted)
        _ = try? await core.call("crash", params: [String: String]())

        let failed = await waitUntil { events.all.contains(.failed) }
        XCTAssertTrue(failed)
        try await Task.sleep(nanoseconds: UInt64(delay * 3 * 1_000_000_000))
        XCTAssertEqual(logLines("argv.txt").count, 2)
        XCTAssertEqual(initCount(), 2)
    }

    func testApiKey_OnlyViaStdin_NotInArgvOrEnv() async throws {
        let core = makeCore()
        try await core.start()
        defer { Task { await core.stop() } }
        _ = try await core.call(
            "save_connection",
            params: ["base_url": "https://relay.example.com", "api_key": apiKey]
        )
        XCTAssertFalse(logLines("argv.txt").joined().contains(apiKey))
        XCTAssertFalse(logLines("env.txt").joined().contains(apiKey))
        XCTAssertTrue(logLines("requests.log").contains { $0.contains("\"method\":\"save_connection\"") && $0.contains(apiKey) })
    }

    func testDefaults_RestartDelay2sAndStableReset60s() {
        XCTAssertEqual(CoreProcess.defaultRestartDelay, 2)
        XCTAssertEqual(CoreProcess.defaultStableResetInterval, 60)
    }

    func testCrash_AfterStableRunRestartsAgain() async throws {
        let core = makeCore(restartDelay: 0.1, stableResetInterval: 0.3)
        try await core.start()
        defer { Task { await core.stop() } }
        _ = try? await core.call("crash", params: [String: String]())
        let first = await waitUntil(timeout: 5) { initCount() == 2 }
        XCTAssertTrue(first)
        try await Task.sleep(nanoseconds: 800_000_000)
        _ = try? await core.call("crash", params: [String: String]())
        let second = await waitUntil(timeout: 5) { initCount() == 3 }
        XCTAssertTrue(second, "稳定运行后再崩溃应再次重启")
    }

    func testCrash_QuickCrashAfterRestartReportsFailed() async throws {
        let core = makeCore(restartDelay: 0.1, stableResetInterval: 30)
        try await core.start()
        defer { Task { await core.stop() } }
        let events = EventBox<CoreLifecycleEvent>()
        let stream = core.lifecycleEvents()
        let task = Task { for await e in stream { events.append(e) } }
        defer { task.cancel() }
        _ = try? await core.call("crash", params: [String: String]())
        let restarted = await waitUntil(timeout: 5) { initCount() == 2 }
        XCTAssertTrue(restarted)
        _ = try? await core.call("crash", params: [String: String]())
        let failed = await waitUntil(timeout: 5) { events.all.contains(.failed) }
        XCTAssertTrue(failed)
    }

    func testCall_WriteFailureThrowsProcessExited() async throws {
        let core = makeCore(script: "closed-stdin-core")
        try await core.start()
        defer { Task { await core.stop() } }
        try await Task.sleep(nanoseconds: 300_000_000)
        do {
            _ = try await core.call("ping", params: [String: String]())
            XCTFail("应当抛错")
        } catch {
            XCTAssertEqual(error as? CoreError, .processExited)
        }
    }

    func testRestart_InitProtocolErrorStopsProcessAndReportsFailed() async throws {
        let core = makeCore(restartDelay: 0.1)
        try await core.start()
        defer { Task { await core.stop() } }
        let events = EventBox<CoreLifecycleEvent>()
        let stream = core.lifecycleEvents()
        let task = Task { for await e in stream { events.append(e) } }
        defer { task.cancel() }
        FileManager.default.createFile(atPath: dir.appendingPathComponent("failinit").path, contents: nil)
        _ = try? await core.call("crash", params: [String: String]())
        let failed = await waitUntil(timeout: 5) { events.all.contains(.failed) }
        XCTAssertTrue(failed)
        XCTAssertFalse(events.all.contains(.restarted))
        do {
            _ = try await core.call("ping", params: [String: String]())
            XCTFail("子进程应已停止")
        } catch {
            XCTAssertEqual(error as? CoreError, .processExited)
        }
    }

    func testRestart_AbandonedWhenStoppedAndStartedDuringDelay() async throws {
        let core = makeCore(restartDelay: 0.5)
        try await core.start()
        defer { Task { await core.stop() } }
        _ = try? await core.call("crash", params: [String: String]())
        await core.stop()
        try await core.start()
        XCTAssertEqual(initCount(), 2)
        try await Task.sleep(nanoseconds: 1_200_000_000)
        XCTAssertEqual(initCount(), 2, "延迟重启应被放弃")
        XCTAssertEqual(logLines("argv.txt").count, 2)
    }

    func testStatusEvent_RawU0085InsideJSONDoesNotSplitLine() async throws {
        let core = makeCore()
        try await core.start()
        defer { Task { await core.stop() } }
        let box = EventBox<CoreStatus>()
        let stream = core.statusEvents()
        let task = Task { for await s in stream { box.append(s) } }
        defer { task.cancel() }
        _ = try await core.call("emit_nel", params: [String: String]())
        let got = await waitUntil(timeout: 5) { !box.all.isEmpty }
        XCTAssertTrue(got)
        XCTAssertEqual(box.all.first?.version, "1.\u{85}2.0")
    }

    func testStop_WhileCallWaitingFailsWithProcessExited() async throws {
        let core = makeCore()
        try await core.start()
        let waiting = Task { try await core.call("hold", params: [String: String]()) }
        try await Task.sleep(nanoseconds: 200_000_000)
        await core.stop()
        do {
            _ = try await waiting.value
            XCTFail("应当抛错")
        } catch {
            XCTAssertEqual(error as? CoreError, .processExited)
        }
    }
}
