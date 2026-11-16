import Testing
import Foundation
@testable import ProxyBar

/// 每个测试用独立临时目录，结束即清理。
private func withTempDir(_ body: (URL) throws -> Void) throws {
    let dir = FileManager.default.temporaryDirectory
        .appendingPathComponent("proxybar-tests-\(UUID().uuidString)")
    try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: dir) }
    try body(dir)
}

private let sampleProfile = ProxyProfile(
    name: "Clash",
    http: ProxyEndpoint(host: "127.0.0.1", port: 7897),
    https: ProxyEndpoint(host: "127.0.0.1", port: 7897),
    socks: ProxyEndpoint(host: "127.0.0.1", port: 7897),
    pacURL: nil,
    bypass: ["localhost", "<local>"]
)

@Suite("ProfileStore JSON 持久化")
struct ProfileStoreTests {

    @Test func 保存后读取_round_trip一致() throws {
        try withTempDir { dir in
            let store = ProfileStore(fileURL: dir.appendingPathComponent("store.json"))
            let data = StoreData(profiles: [sampleProfile], serviceOverrides: ["Thunderbolt Bridge": true])
            try store.save(data)
            let result = store.load()
            #expect(result.data == data)
            #expect(!result.isFirstRun)
            #expect(result.corruptionNotice == nil)
        }
    }

    @Test func 文件不存在视为首次运行() throws {
        try withTempDir { dir in
            let store = ProfileStore(fileURL: dir.appendingPathComponent("store.json"))
            let result = store.load()
            #expect(result.isFirstRun)
            #expect(result.data == StoreData())
            #expect(result.data.schemaVersion == 1)
        }
    }

    @Test func 损坏JSON_备份后从空开始() throws {
        // 破坏点：解码失败直接崩溃或静默丢文件。
        try withTempDir { dir in
            let file = dir.appendingPathComponent("store.json")
            try "{ not valid json".write(to: file, atomically: true, encoding: .utf8)
            let store = ProfileStore(fileURL: file)
            let result = store.load()
            #expect(result.data == StoreData())
            #expect(result.corruptionNotice != nil)
            let backups = try FileManager.default.contentsOfDirectory(atPath: dir.path)
                .filter { $0.hasPrefix("store.json.corrupt-") }
            #expect(backups.count == 1)
            // 损坏后仍可正常保存。
            try store.save(StoreData(profiles: [sampleProfile]))
            #expect(store.load().data.profiles == [sampleProfile])
        }
    }

    @Test func 保存产物是合法JSON且父目录自动创建() throws {
        // 独立校验：不经过 store.load，直接用 JSONDecoder 解文件。
        try withTempDir { dir in
            let nested = dir.appendingPathComponent("sub/dir")
            let store = ProfileStore(fileURL: nested.appendingPathComponent("store.json"))
            try store.save(StoreData(profiles: [sampleProfile]))
            let raw = try Data(contentsOf: nested.appendingPathComponent("store.json"))
            let decoded = try JSONDecoder().decode(StoreData.self, from: raw)
            #expect(decoded.profiles == [sampleProfile])
        }
    }
}