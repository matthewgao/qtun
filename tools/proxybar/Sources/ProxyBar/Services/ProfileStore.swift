import Foundation

/// 持久化数据（schemaVersion 便于日后迁移）。
struct StoreData: Codable, Equatable, Sendable {
    var schemaVersion: Int
    var profiles: [ProxyProfile]
    var serviceOverrides: [String: Bool]

    init(schemaVersion: Int = 1, profiles: [ProxyProfile] = [], serviceOverrides: [String: Bool] = [:]) {
        self.schemaVersion = schemaVersion
        self.profiles = profiles
        self.serviceOverrides = serviceOverrides
    }
}

struct StoreLoadResult: Sendable {
    var data: StoreData
    var isFirstRun: Bool
    var corruptionNotice: String?
}

/// JSON 文件存储：原子写、损坏备份恢复。
struct ProfileStore: Sendable {
    let fileURL: URL

    init(fileURL: URL) {
        self.fileURL = fileURL
    }

    static func defaultFileURL() -> URL {
        let base = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
        let bundleID = Bundle.main.bundleIdentifier ?? "com.gaoshen.proxybar"
        return base.appendingPathComponent(bundleID).appendingPathComponent("store.json")
    }

    func load() -> StoreLoadResult {
        guard FileManager.default.fileExists(atPath: fileURL.path) else {
            return StoreLoadResult(data: StoreData(), isFirstRun: true, corruptionNotice: nil)
        }
        do {
            let raw = try Data(contentsOf: fileURL)
            let data = try JSONDecoder().decode(StoreData.self, from: raw)
            return StoreLoadResult(data: data, isFirstRun: false, corruptionNotice: nil)
        } catch {
            let timestamp = Int(Date().timeIntervalSince1970)
            let backup = fileURL.deletingLastPathComponent()
                .appendingPathComponent(fileURL.lastPathComponent + ".corrupt-\(timestamp)")
            try? FileManager.default.moveItem(at: fileURL, to: backup)
            return StoreLoadResult(
                data: StoreData(), isFirstRun: false,
                corruptionNotice: "配置文件损坏，已备份为 \(backup.lastPathComponent)，从空配置重新开始。"
            )
        }
    }

    func save(_ data: StoreData) throws {
        let dir = fileURL.deletingLastPathComponent()
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        let raw = try encoder.encode(data)
        try raw.write(to: fileURL, options: .atomic)
    }
}