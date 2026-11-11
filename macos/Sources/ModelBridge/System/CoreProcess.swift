import Foundation

enum CoreError: Error, Equatable {
    case remote(code: String, keys: [String])
    case processExited
}

enum CoreLifecycleEvent: Sendable, Equatable {
    case restarted
    case failed
}

final class Broadcaster<T: Sendable>: @unchecked Sendable {
    private let lock = NSLock()
    private var continuations: [UUID: AsyncStream<T>.Continuation] = [:]

    func stream() -> AsyncStream<T> {
        let id = UUID()
        return AsyncStream { continuation in
            lock.lock()
            continuations[id] = continuation
            lock.unlock()
            continuation.onTermination = { [weak self] _ in
                guard let self else { return }
                lock.lock()
                continuations[id] = nil
                lock.unlock()
            }
        }
    }

    func send(_ value: T) {
        lock.lock()
        let targets = Array(continuations.values)
        lock.unlock()
        targets.forEach { $0.yield(value) }
    }
}

private struct Request<P: Encodable>: Encodable {
    let id: Int
    let method: String
    let params: P
}

actor CoreProcess {
    static let defaultRestartDelay: TimeInterval = 2
    static let defaultStableResetInterval: TimeInterval = 60
    static let defaultDataDirectory = ShellSettings.defaultDirectory

    static func arguments(dataDir: URL) -> [String] {
        ["--data-dir", dataDir.path]
    }

    static func executableURL(appBundleURL: URL) -> URL {
        appBundleURL.appendingPathComponent("Contents/MacOS/model-bridge-core")
    }

    private let executableURL: URL
    private let arguments: [String]
    private let restartDelay: TimeInterval
    private let stableResetInterval: TimeInterval
    private let initParams: @Sendable () -> CoreInitParams
    private let statusBroadcaster = Broadcaster<CoreStatus>()
    private let lifecycleBroadcaster = Broadcaster<CoreLifecycleEvent>()

    private var process: Process?
    private var input: FileHandle?
    private var generation = 0
    private var nextID = 1
    private var pending: [Int: CheckedContinuation<Data, Error>] = [:]
    private var stopped = false
    private var hasRestarted = false

    init(
        executableURL: URL,
        arguments: [String],
        restartDelay: TimeInterval = CoreProcess.defaultRestartDelay,
        stableResetInterval: TimeInterval = CoreProcess.defaultStableResetInterval,
        initParams: @escaping @Sendable () -> CoreInitParams
    ) {
        self.executableURL = executableURL
        self.arguments = arguments
        self.restartDelay = restartDelay
        self.stableResetInterval = stableResetInterval
        self.initParams = initParams
    }

    nonisolated func statusEvents() -> AsyncStream<CoreStatus> {
        statusBroadcaster.stream()
    }

    nonisolated func lifecycleEvents() -> AsyncStream<CoreLifecycleEvent> {
        lifecycleBroadcaster.stream()
    }

    func start() async throws {
        stopped = false
        try launch()
        _ = try await call("init", params: initParams())
    }

    func stop() {
        stopped = true
        input?.closeFile()
        process?.terminate()
        input = nil
        process = nil
        failPending()
    }

    func call<P: Encodable & Sendable>(_ method: String, params: P) async throws -> Data {
        guard let input else { throw CoreError.processExited }
        let id = nextID
        nextID += 1
        var line = try JSONEncoder.compact.encode(Request(id: id, method: method, params: params))
        line.append(0x0A)
        return try await withCheckedThrowingContinuation { continuation in
            pending[id] = continuation
            do {
                try input.write(contentsOf: line)
            } catch {
                pending.removeValue(forKey: id)?.resume(throwing: CoreError.processExited)
            }
        }
    }

    private func launch() throws {
        signal(SIGPIPE, SIG_IGN)
        let process = Process()
        process.executableURL = executableURL
        process.arguments = arguments
        let stdin = Pipe()
        let stdout = Pipe()
        process.standardInput = stdin
        process.standardOutput = stdout
        process.standardError = FileHandle.nullDevice
        try process.run()

        generation += 1
        let current = generation
        self.process = process
        input = stdin.fileHandleForWriting
        let reader = stdout.fileHandleForReading
        Task {
            var buffer: [UInt8] = []
            do {
                for try await byte in reader.bytes {
                    if byte == 0x0A {
                        handle(String(decoding: buffer, as: UTF8.self))
                        buffer.removeAll(keepingCapacity: true)
                    } else {
                        buffer.append(byte)
                    }
                }
            } catch {}
            handleExit(generation: current)
        }
    }

    private func handle(_ line: String) {
        guard
            let object = try? JSONSerialization.jsonObject(with: Data(line.utf8)) as? [String: Any]
        else { return }

        if object["event"] as? String == "status" {
            if let data = object["data"],
               let bytes = try? JSONSerialization.data(withJSONObject: data),
               let status = try? JSONDecoder.snakeCase.decode(CoreStatus.self, from: bytes) {
                statusBroadcaster.send(status)
            }
            return
        }

        guard let id = object["id"] as? Int, let continuation = pending.removeValue(forKey: id) else { return }
        if let error = object["error"] as? [String: Any] {
            continuation.resume(throwing: CoreError.remote(
                code: error["code"] as? String ?? "internal_error",
                keys: error["keys"] as? [String] ?? []
            ))
        } else if let result = object["result"],
                  let data = try? JSONSerialization.data(withJSONObject: result, options: .fragmentsAllowed) {
            continuation.resume(returning: data)
        } else {
            continuation.resume(throwing: CoreError.remote(code: "internal_error", keys: []))
        }
    }

    private func handleExit(generation exited: Int) {
        guard exited == generation, !stopped else { return }
        process = nil
        input = nil
        failPending()

        if hasRestarted {
            lifecycleBroadcaster.send(.failed)
            return
        }
        hasRestarted = true
        let delay = restartDelay
        let scheduled = generation
        Task {
            try? await Task.sleep(nanoseconds: UInt64(delay * 1_000_000_000))
            guard !stopped, generation == scheduled else { return }
            do {
                try launch()
                _ = try await call("init", params: initParams())
                lifecycleBroadcaster.send(.restarted)
                resetRestartAllowanceWhenStable()
            } catch CoreError.processExited {
            } catch {
                stop()
                lifecycleBroadcaster.send(.failed)
            }
        }
    }

    private func resetRestartAllowanceWhenStable() {
        let running = generation
        let interval = stableResetInterval
        Task {
            try? await Task.sleep(nanoseconds: UInt64(interval * 1_000_000_000))
            if generation == running, process != nil, !stopped { hasRestarted = false }
        }
    }

    private func failPending() {
        let waiting = pending
        pending = [:]
        waiting.values.forEach { $0.resume(throwing: CoreError.processExited) }
    }
}

private extension JSONEncoder {
    static var compact: JSONEncoder {
        let encoder = JSONEncoder()
        encoder.outputFormatting = .withoutEscapingSlashes
        return encoder
    }
}

private extension JSONDecoder {
    static var snakeCase: JSONDecoder {
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        return decoder
    }
}
