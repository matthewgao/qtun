import SwiftUI
import AppKit

/// 根模型：配置、真实状态、选中态、应用流程、导入、开机启动。
/// 对外部改动严格被动——只重新标注，绝不自动重新应用（与 qtun 共存的关键）。
@MainActor @Observable
final class AppModel {
    static let shared = AppModel()

    var profiles: [ProxyProfile] = []
    var allServices: [NetworkService] = []
    var serviceOverrides: [String: Bool] = [:]
    var states: [String: SystemProxyState] = [:]
    var selection: ActiveSelection = .custom
    var isApplying = false
    var notice: String?
    var editorSheet: EditorSheet?
    var launchAtLoginEnabled = false
    var launchAtLoginError: String?

    struct EditorSheet: Identifiable {
        let id = UUID()
        var profile: ProxyProfile
        var isNew: Bool
    }

    private let runner: CommandRunning
    private let store: ProfileStore
    private let applier: ProxyApplier
    private let reader: ProxyStateReader
    private var watcher: ChangeWatcher?
    private var didBootstrap = false

    init(
        runner: CommandRunning = NetworkSetupRunner(),
        store: ProfileStore = ProfileStore(fileURL: ProfileStore.defaultFileURL())
    ) {
        self.runner = runner
        self.store = store
        self.applier = ProxyApplier(runner: runner)
        self.reader = ProxyStateReader(runner: runner)
    }

    // MARK: - 派生状态

    var targetServices: [NetworkService] {
        ServiceEnumerator.targets(allServices, overrides: serviceOverrides)
    }

    var targetServiceNames: [String] { targetServices.map(\.name) }

    var menuIconName: String {
        selection == .direct ? "network.slash" : "network"
    }

    // MARK: - 启动

    func bootstrap() async {
        guard !didBootstrap else { return }
        didBootstrap = true

        let loaded = store.load()
        profiles = loaded.data.profiles
        serviceOverrides = loaded.data.serviceOverrides
        notice = loaded.corruptionNotice

        await refreshServices()
        await refresh()

        if loaded.isFirstRun {
            firstRunImport()
        }

        launchAtLoginEnabled = LaunchAtLogin.isEnabled

        let w = ChangeWatcher { [weak self] in self?.scheduleRefresh() }
        w.start()
        watcher = w

        if let notice {
            showAlert(title: "ProxyBar", text: notice)
            self.notice = nil
        }
    }

    /// 首次运行：当前非直连 → 自动导入一条配置（本机即捕获 qtun PAC）。
    private func firstRunImport() {
        guard let (_, state) = states.first(where: { !$0.value.isDirect }) else { return }
        profiles.append(ProxyProfile.from(state: state, name: "导入的配置"))
        persist()
        selection = ProfileMatcher.select(states: states, profiles: profiles)
    }

    private func scheduleRefresh() {
        Task { await refresh() }
    }

    // MARK: - 读取

    func refreshServices() async {
        let listAll = (try? await runner.run(["-listallnetworkservices"]))?.stdout ?? ""
        let order = (try? await runner.run(["-listnetworkserviceorder"]))?.stdout ?? ""
        allServices = ServiceEnumerator.parse(listAll: listAll, order: order)
    }

    func refresh() async {
        states = await reader.read(services: targetServiceNames)
        selection = ProfileMatcher.select(states: states, profiles: profiles)
    }

    // MARK: - 应用

    func apply(_ profile: ProxyProfile?) async {
        guard !isApplying else { return }
        isApplying = true
        defer { isApplying = false }
        let failures = await applier.apply(profile: profile, services: targetServiceNames)
        // 应用后重读真实状态推导勾选，绝不做乐观假设。
        await refresh()
        if !failures.isEmpty {
            let text = failures.map { f in
                "\(f.service): \(f.args.joined(separator: " "))\n\(f.message)"
            }.joined(separator: "\n\n")
            showAlert(title: "部分代理设置失败", text: text)
        }
    }

    // MARK: - 配置 CRUD

    func upsert(_ profile: ProxyProfile) {
        if let index = profiles.firstIndex(where: { $0.id == profile.id }) {
            profiles[index] = profile
        } else {
            profiles.append(profile)
        }
        persist()
        selection = ProfileMatcher.select(states: states, profiles: profiles)
    }

    func delete(at offsets: IndexSet) {
        profiles.remove(atOffsets: offsets)
        persist()
        selection = ProfileMatcher.select(states: states, profiles: profiles)
    }

    func move(from source: IndexSet, to destination: Int) {
        profiles.move(fromOffsets: source, toOffset: destination)
        persist()
    }

    func setOverride(_ serviceName: String, _ included: Bool) {
        serviceOverrides[serviceName] = included
        persist()
        Task { await refresh() }
    }

    // MARK: - 编辑器

    func beginAdd() {
        editorSheet = EditorSheet(profile: ProxyProfile(name: ""), isNew: true)
    }

    func beginEdit(_ profile: ProxyProfile) {
        editorSheet = EditorSheet(profile: profile, isNew: false)
    }

    func saveEditor(_ profile: ProxyProfile) {
        upsert(profile)
        editorSheet = nil
    }

    /// 导入当前系统配置：读主服务状态 → 预填编辑器草稿，确认才保存。
    func prepareImport() async {
        let serviceName = targetServiceNames.first
            ?? allServices.first(where: { ServiceEnumerator.isDefaultTarget($0) })?.name
        guard let serviceName else { return }
        let state = await reader.read(service: serviceName)
        let formatter = DateFormatter()
        formatter.dateFormat = "MM-dd HH:mm"
        let draft = ProxyProfile.from(state: state, name: "导入的配置 \(formatter.string(from: Date()))")
        editorSheet = EditorSheet(profile: draft, isNew: true)
    }

    // MARK: - 开机启动

    func setLaunchAtLogin(_ on: Bool) {
        do {
            try LaunchAtLogin.setEnabled(on)
            launchAtLoginError = nil
        } catch {
            launchAtLoginError = "设置失败：\(error.localizedDescription)"
        }
        launchAtLoginEnabled = LaunchAtLogin.isEnabled
    }

    // MARK: - 私有

    private func persist() {
        do {
            try store.save(StoreData(profiles: profiles, serviceOverrides: serviceOverrides))
        } catch {
            showAlert(title: "保存配置失败", text: error.localizedDescription)
        }
    }

    private func showAlert(title: String, text: String) {
        let alert = NSAlert()
        alert.messageText = title
        alert.informativeText = text
        alert.alertStyle = .warning
        NSApp.activate(ignoringOtherApps: true)
        alert.runModal()
    }
}