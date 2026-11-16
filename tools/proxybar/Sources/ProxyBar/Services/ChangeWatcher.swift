import Foundation
import SystemConfiguration

/// 监听系统代理外部改动（qtun 启停、系统设置手改）。
/// 通知键 State:/Network/Global/Proxies 镜像主服务的代理字典（已实测存在）。
/// 严格被动：只触发重新读取标注，绝不自动重新应用。
@MainActor
final class ChangeWatcher {
    private var store: SCDynamicStore?
    private var debounceTask: Task<Void, Never>?
    private let handler: @MainActor () -> Void

    init(handler: @escaping @MainActor () -> Void) {
        self.handler = handler
    }

    func start() {
        var context = SCDynamicStoreContext(
            version: 0,
            info: Unmanaged.passUnretained(self).toOpaque(),
            retain: nil,
            release: nil,
            copyDescription: nil
        )
        // C 回调不捕获任何上下文；info 裸指针是 Sendable，可安全跨闭包传递。
        let callback: SCDynamicStoreCallBack = { _, _, info in
            guard let info else { return }
            DispatchQueue.main.async {
                MainActor.assumeIsolated {
                    Unmanaged<ChangeWatcher>.fromOpaque(info).takeUnretainedValue().ping()
                }
            }
        }
        guard let s = SCDynamicStoreCreate(nil, "ProxyBar" as CFString, callback, &context) else { return }
        let keys = ["State:/Network/Global/Proxies" as CFString] as CFArray
        _ = SCDynamicStoreSetNotificationKeys(s, keys, nil)
        _ = SCDynamicStoreSetDispatchQueue(s, DispatchQueue.main)
        store = s
    }

    func stop() {
        if let s = store {
            _ = SCDynamicStoreSetDispatchQueue(s, nil)
        }
        store = nil
        debounceTask?.cancel()
    }

    /// 200ms 防抖：qtun 启停会连续触发多次通知。
    private func ping() {
        debounceTask?.cancel()
        debounceTask = Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(200))
            guard !Task.isCancelled else { return }
            self?.handler()
        }
    }
}